package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/clients/payments"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// SwapHandler exposes the Rach unified swap engine to DigitalFX users.
// Same-chain swaps route through the FiatSwapV2 DEX contracts (Polygon/BSC),
// cross-chain swaps through a bridge — the routing is invisible to the caller.
type SwapHandler struct {
	svc *services.Services
}

// NewSwapHandler creates a new swap handler.
func NewSwapHandler(svc *services.Services) *SwapHandler {
	return &SwapHandler{svc: svc}
}

// writeSwapError surfaces the payments engine's own status code and
// human-readable message (e.g. "unknown token …", "re-quote and try again")
// straight through to the caller instead of a generic 500.
func writeSwapError(w http.ResponseWriter, err error) {
	var apiErr *payments.APIError
	if errors.As(err, &apiErr) && apiErr.Status > 0 {
		code := "SWAP_ERROR"
		if apiErr.Status == http.StatusBadRequest {
			code = "VALIDATION_ERROR"
		}
		response.JSON(w, apiErr.Status, response.Envelope{
			Success: false,
			Error:   &response.Error{Code: code, Message: apiErr.Message},
		})
		return
	}
	response.JSON(w, http.StatusBadGateway, response.Envelope{
		Success: false,
		Error:   &response.Error{Code: "SWAP_UNAVAILABLE", Message: "swap service is temporarily unavailable"},
	})
}

// GetTokens godoc
//
//	@Summary      List supported swap tokens
//	@Description  Symbols you can use instead of raw contract addresses, per chain. Omit chain for all chains.
//	@Tags         swap
//	@Produce      json
//	@Security     BearerAuth
//	@Param        chain  query     string  false  "Chain key (POL, BSC, ETH, …)"
//	@Success      200    {object}  payments.SupportedTokensResponse
//	@Router       /swap/tokens [get]
func (h *SwapHandler) GetTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.svc.Swap.Tokens(r.Context(), r.URL.Query().Get("chain"))
	if err != nil {
		writeSwapError(w, err)
		return
	}
	response.OK(w, tokens)
}

// GetQuote godoc
//
//	@Summary      Get a swap price quote
//	@Description  Returns the best available price for a pair. Tokens may be symbols or 0x addresses; amount is human units (e.g. "5") or amount_in for base units.
//	@Tags         swap
//	@Produce      json
//	@Security     BearerAuth
//	@Param        from_chain  query     string  true   "Source chain (POL, BSC, ETH, …)"
//	@Param        to_chain    query     string  true   "Destination chain"
//	@Param        from_token  query     string  true   "Symbol, native, or 0x address"
//	@Param        to_token    query     string  true   "Symbol, native, or 0x address"
//	@Param        amount      query     string  false  "Human units, e.g. 5 or 1.5"
//	@Param        amount_in   query     string  false  "Base units (alternative to amount)"
//	@Success      200         {object}  payments.SwapQuoteResponse
//	@Failure      400         {object}  ErrorResponse
//	@Router       /swap/quote [get]
func (h *SwapHandler) GetQuote(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	in := services.SwapQuoteInput{
		FromChain: q.Get("from_chain"),
		ToChain:   q.Get("to_chain"),
		FromToken: q.Get("from_token"),
		ToToken:   q.Get("to_token"),
		Amount:    q.Get("amount"),
		AmountIn:  q.Get("amount_in"),
	}
	if in.FromChain == "" || in.ToChain == "" || in.FromToken == "" || in.ToToken == "" || (in.Amount == "" && in.AmountIn == "") {
		response.BadRequest(w, "VALIDATION_ERROR", "from_chain, to_chain, from_token, to_token and amount (or amount_in) are required")
		return
	}
	quote, err := h.svc.Swap.Quote(r.Context(), in)
	if err != nil {
		writeSwapError(w, err)
		return
	}
	response.OK(w, quote)
}

// SwapExecuteRequest is the execute payload.
type SwapExecuteRequest struct {
	FromChain    string `json:"from_chain"`
	ToChain      string `json:"to_chain"`
	FromToken    string `json:"from_token"`
	ToToken      string `json:"to_token"`
	Amount       string `json:"amount"`
	AmountIn     string `json:"amount_in"`
	AmountOutMin string `json:"amount_out_min"`
}

// Execute godoc
//
//	@Summary      Execute a swap
//	@Description  Swaps tokens from the authenticated user's WaaS wallet. Routing (DEX vs bridge) is internal; you get a tx hash. Slippage is auto-protected at 99.5% of a live quote unless amount_out_min is set.
//	@Tags         swap
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      SwapExecuteRequest  true  "Swap details"
//	@Success      201   {object}  payments.ExecuteSwapResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /swap/execute [post]
func (h *SwapHandler) Execute(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body SwapExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.FromChain == "" || body.ToChain == "" || body.FromToken == "" || body.ToToken == "" || (body.Amount == "" && body.AmountIn == "") {
		response.BadRequest(w, "VALIDATION_ERROR", "from_chain, to_chain, from_token, to_token and amount (or amount_in) are required")
		return
	}
	resp, err := h.svc.Swap.Execute(r.Context(), userID, services.SwapExecuteInput{
		FromChain:    body.FromChain,
		ToChain:      body.ToChain,
		FromToken:    body.FromToken,
		ToToken:      body.ToToken,
		Amount:       body.Amount,
		AmountIn:     body.AmountIn,
		AmountOutMin: body.AmountOutMin,
	})
	if err != nil {
		writeSwapError(w, err)
		return
	}
	response.Created(w, resp)
}

// GetHistory godoc
//
//	@Summary      Get swap history
//	@Description  The authenticated user's paginated swap history.
//	@Tags         swap
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page   query     int  false  "Page number (default 1)"
//	@Param        limit  query     int  false  "Results per page (default 20)"
//	@Success      200    {object}  payments.GetSwapHistoryResponse
//	@Router       /swap/history [get]
func (h *SwapHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	history, err := h.svc.Swap.History(r.Context(), userID, page, limit)
	if err != nil {
		writeSwapError(w, err)
		return
	}
	response.OK(w, history)
}

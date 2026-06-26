package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type ExchangeHandler struct {
	svc *services.Services
}

func NewExchangeHandler(svc *services.Services) *ExchangeHandler {
	return &ExchangeHandler{svc: svc}
}

// GetRate godoc
//
//	@Summary      Live exchange rate
//	@Description  Returns the live FX rate for 1 unit of the source currency converted to the target currency. Rate is sourced from Nilos (green dot = live); falls back to internal rates if Nilos is unavailable.
//	@Tags         exchange
//	@Produce      json
//	@Security     BearerAuth
//	@Param        from  query     string  true  "Source currency (USD, EUR, GBP, XAF, XOF)"
//	@Param        to    query     string  true  "Target currency"
//	@Success      200   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /exchange/rate [get]
func (h *ExchangeHandler) GetRate(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "from and to query params are required")
		return
	}
	rate, err := h.svc.Exchange.GetRate(r.Context(), from, to)
	if errors.Is(err, services.ErrSameCurrency) {
		response.BadRequest(w, "SAME_CURRENCY", "source and target currency must differ")
		return
	}
	if errors.Is(err, services.ErrUnsupportedPair) {
		response.BadRequest(w, "UNSUPPORTED_PAIR", "currency pair not supported")
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, rate)
}

// GetQuote godoc
//
//	@Summary      Exchange quote
//	@Description  Returns a locked FX quote for a specific amount. The quote_id can be passed to POST /exchange/execute to guarantee the displayed rate. Quotes expire quickly — execute immediately after confirming.
//	@Tags         exchange
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      ExchangeQuoteRequest  true  "Quote parameters"
//	@Success      200   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /exchange/quote [post]
func (h *ExchangeHandler) GetQuote(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body ExchangeQuoteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.From == "" || body.To == "" || body.Amount <= 0 {
		response.BadRequest(w, "VALIDATION_ERROR", "from, to, and amount are required")
		return
	}
	quote, err := h.svc.Exchange.GetQuote(r.Context(), body.From, body.To, body.Amount, body.Side)
	if errors.Is(err, services.ErrSameCurrency) {
		response.BadRequest(w, "SAME_CURRENCY", "source and target currency must differ")
		return
	}
	if errors.Is(err, services.ErrUnsupportedPair) {
		response.BadRequest(w, "UNSUPPORTED_PAIR", "currency pair not supported")
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, quote)
}

// Execute godoc
//
//	@Summary      Execute exchange
//	@Description  Converts fiat currency between the user's own accounts using a Nilos swap. The from and to accounts must both exist. Optionally pass a quote_id to lock the rate shown in the preview; otherwise the swap executes at the current market rate.
//	@Tags         exchange
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      ExchangeExecuteRequest  true  "Exchange details"
//	@Success      200   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      422   {object}  ErrorResponse
//	@Router       /exchange/execute [post]
func (h *ExchangeHandler) Execute(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body ExchangeExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.From == "" || body.To == "" || body.Amount <= 0 {
		response.BadRequest(w, "VALIDATION_ERROR", "from, to, and amount are required")
		return
	}

	result, err := h.svc.Exchange.Execute(r.Context(), services.ExecuteExchangeInput{
		UserID:  userID,
		From:    body.From,
		To:      body.To,
		Amount:  body.Amount,
		Side:    body.Side,
		QuoteID: body.QuoteID,
	})
	if errors.Is(err, services.ErrSameCurrency) {
		response.BadRequest(w, "SAME_CURRENCY", "source and target currency must differ")
		return
	}
	if errors.Is(err, services.ErrUnsupportedPair) {
		response.BadRequest(w, "UNSUPPORTED_PAIR", "currency pair not supported")
		return
	}
	if errors.Is(err, services.ErrInsufficientFunds) {
		response.BadRequest(w, "INSUFFICIENT_FUNDS", "insufficient balance in source account")
		return
	}
	if errors.Is(err, services.ErrAccountNotFound) {
		response.NotFound(w, "source or target account not found")
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, result)
}

// GetHistory godoc
//
//	@Summary      Exchange history
//	@Description  Returns the user's fiat exchange history with aggregate stats (total exchanges, volume, fees paid) and transactions grouped as THIS WEEK / LAST WEEK / EARLIER. Each row shows from/to currencies, amounts, rate used, and status.
//	@Tags         exchange
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page   query     int  false  "Page (default 1)"
//	@Param        limit  query     int  false  "Results per page (default 20)"
//	@Success      200    {object}  map[string]any
//	@Failure      401    {object}  ErrorResponse
//	@Router       /exchange/history [get]
func (h *ExchangeHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	history, err := h.svc.Exchange.GetHistory(r.Context(), userID, int32(page), int32(limit))
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, history)
}

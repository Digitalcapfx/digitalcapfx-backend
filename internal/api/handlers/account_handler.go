package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type AccountHandler struct {
	svc *services.Services
}

func NewAccountHandler(svc *services.Services) *AccountHandler {
	return &AccountHandler{svc: svc}
}

// ListAccounts godoc
//
//	@Summary      List accounts
//	@Description  Returns all fiat accounts (XAF, XOF, USD, GBP, EUR) for the authenticated user.
//	@Tags         accounts
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  AccountListResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /accounts [get]
func (h *AccountHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	accounts, err := h.svc.Account.ListAccounts(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, accounts)
}

// GetAccount godoc
//
//	@Summary      Get account by currency
//	@Description  Returns a single fiat account for the given currency code.
//	@Tags         accounts
//	@Produce      json
//	@Security     BearerAuth
//	@Param        currency  path      string  true  "Currency code" Enums(XAF,XOF,USD,GBP,EUR)
//	@Success      200       {object}  AccountResponse
//	@Failure      401       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Router       /accounts/{currency} [get]
func (h *AccountHandler) GetAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	currency := chi.URLParam(r, "currency")
	account, err := h.svc.Account.GetAccount(r.Context(), userID, currency)
	if err != nil {
		response.NotFound(w, "account not found")
		return
	}

	response.OK(w, account)
}

// GetTransactions godoc
//
//	@Summary      List transactions for an account
//	@Description  Returns a paginated list of transactions for the given currency account.
//	@Tags         accounts
//	@Produce      json
//	@Security     BearerAuth
//	@Param        currency  path      string  true   "Currency code" Enums(XAF,XOF,USD,GBP,EUR)
//	@Param        page      query     int     false  "Page number (default 1)"
//	@Param        per_page  query     int     false  "Results per page, max 100 (default 20)"
//	@Success      200       {object}  TransactionListResponse
//	@Failure      401       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Failure      500       {object}  ErrorResponse
//	@Router       /accounts/{currency}/transactions [get]
func (h *AccountHandler) GetTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	currency := chi.URLParam(r, "currency")
	account, err := h.svc.Account.GetAccount(r.Context(), userID, currency)
	if err != nil {
		response.NotFound(w, "account not found")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	result, err := h.svc.Account.ListTransactions(r.Context(), services.ListTransactionsInput{
		AccountID: account.ID,
		Page:      int32(page),
		PerPage:   int32(perPage),
	})
	if err != nil {
		response.InternalError(w)
		return
	}

	totalPages := int(result.Total) / perPage
	if int(result.Total)%perPage != 0 {
		totalPages++
	}

	response.OKPaginated(w, result.Transactions, response.Meta{
		Page:       page,
		PerPage:    perPage,
		Total:      int(result.Total),
		TotalPages: totalPages,
	})
}

// GetTransaction godoc
//
//	@Summary      Get a single transaction
//	@Description  Returns a specific transaction by its UUID.
//	@Tags         accounts
//	@Produce      json
//	@Security     BearerAuth
//	@Param        currency  path      string  true  "Currency code" Enums(XAF,XOF,USD,GBP,EUR)
//	@Param        id        path      string  true  "Transaction UUID"
//	@Success      200       {object}  TransactionResponse
//	@Failure      400       {object}  ErrorResponse  "Invalid UUID"
//	@Failure      401       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Router       /accounts/{currency}/transactions/{id} [get]
func (h *AccountHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "invalid transaction id")
		return
	}

	tx, err := h.svc.Account.GetTransaction(r.Context(), id)
	if err != nil {
		response.NotFound(w, "transaction not found")
		return
	}

	response.OK(w, tx)
}

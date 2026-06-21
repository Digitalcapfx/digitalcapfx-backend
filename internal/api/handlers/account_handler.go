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

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// AdminAccountsHandler exposes admin management of customer ledger accounts:
// view, banking-detail edits, balance set/adjust, and per-currency enable/disable.
type AdminAccountsHandler struct {
	svc *services.Services
}

func NewAdminAccountsHandler(svc *services.Services) *AdminAccountsHandler {
	return &AdminAccountsHandler{svc: svc}
}

func (h *AdminAccountsHandler) staffSet(r *http.Request) services.StaffPermissionSet {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	return set
}

// ListCustomerAccounts godoc
//
//	@Summary      List a customer's accounts (admin)
//	@Description  Returns every ledger account for a customer across all currencies (USD/GBP/EUR/NGN/XAF/XOF), including banking details, balances and status. Nothing is filtered — disabled ('inactive') accounts are returned too, so you can re-enable them. Requires the `accounts:view` permission.
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "Customer user UUID"
//	@Success      200 {object}  AccountListResponse
//	@Failure      400 {object}  ErrorResponse
//	@Failure      403 {object}  ErrorResponse
//	@Router       /admin/customers/{id}/accounts [get]
func (h *AdminAccountsHandler) ListCustomerAccounts(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid customer id")
		return
	}
	accounts, err := h.svc.AdminAccounts.ListCustomerAccounts(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, accounts)
}

// UpdateBanking godoc
//
//	@Summary      Update a customer account's banking details (admin)
//	@Description  Partial update of a customer's banking details for one currency. Only the fields you send are changed. Requires the `accounts:banking` permission.
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id        path      string                      true  "Customer user UUID"
//	@Param        currency  path      string                      true  "Currency code (USD/GBP/EUR/NGN/XAF/XOF)"
//	@Param        body      body      AdminUpdateBankingRequest   true  "Banking fields to update"
//	@Success      200       {object}  AccountResponse
//	@Failure      400       {object}  ErrorResponse
//	@Failure      403       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Router       /admin/customers/{id}/accounts/{currency}/banking [patch]
func (h *AdminAccountsHandler) UpdateBanking(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid customer id")
		return
	}
	currency := chi.URLParam(r, "currency")

	var body AdminUpdateBankingRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	acc, err := h.svc.AdminAccounts.UpdateBanking(r.Context(), h.staffSet(r), userID, currency, services.UpdateBankingInput{
		AccountNumber:          body.AccountNumber,
		Iban:                   body.Iban,
		Bic:                    body.Bic,
		SortCode:               body.SortCode,
		AccountNumberUK:        body.AccountNumberUK,
		NombaBankName:          body.NombaBankName,
		NombaBankAccountNumber: body.NombaBankAccountNumber,
		NombaBankAccountName:   body.NombaBankAccountName,
	}, r.RemoteAddr)
	if err != nil {
		h.writeAccountErr(w, err)
		return
	}
	response.OK(w, acc)
}

// SetAccountStatus godoc
//
//	@Summary      Enable/disable a customer's currency account (admin)
//	@Description  Enables or disables a specific currency account for a customer. The account is still returned by the API; the `status` flag ('active'/'inactive') tells the frontend whether to show it. Requires the `accounts:status` permission.
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id        path      string                     true  "Customer user UUID"
//	@Param        currency  path      string                     true  "Currency code"
//	@Param        body      body      AdminAccountStatusRequest  true  "Enable or disable"
//	@Success      200       {object}  AccountResponse
//	@Failure      400       {object}  ErrorResponse
//	@Failure      403       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Router       /admin/customers/{id}/accounts/{currency}/status [post]
func (h *AdminAccountsHandler) SetAccountStatus(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid customer id")
		return
	}
	currency := chi.URLParam(r, "currency")

	var body AdminAccountStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	acc, err := h.svc.AdminAccounts.SetAccountStatus(r.Context(), h.staffSet(r), userID, currency, body.Enabled, r.RemoteAddr)
	if err != nil {
		h.writeAccountErr(w, err)
		return
	}
	response.OK(w, acc)
}

// ChangeBalance godoc
//
//	@Summary      Set or adjust a customer's ledger balance (admin)
//	@Description  Sets an absolute balance (mode="set") or applies a signed delta (mode="adjust") to a customer's account. A reason is required. The change is audit-logged and records a matching ledger transaction so it appears in the customer's activity. Requires the `accounts:balance` permission.
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id        path      string                      true  "Customer user UUID"
//	@Param        currency  path      string                      true  "Currency code"
//	@Param        body      body      AdminBalanceChangeRequest   true  "Balance change"
//	@Success      200       {object}  AdminBalanceChangeResponse
//	@Failure      400       {object}  ErrorResponse
//	@Failure      403       {object}  ErrorResponse
//	@Failure      404       {object}  ErrorResponse
//	@Router       /admin/customers/{id}/accounts/{currency}/balance [post]
func (h *AdminAccountsHandler) ChangeBalance(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid customer id")
		return
	}
	currency := chi.URLParam(r, "currency")

	var body AdminBalanceChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	res, err := h.svc.AdminAccounts.SetOrAdjustBalance(r.Context(), h.staffSet(r), userID, currency, services.BalanceChangeInput{
		Mode:   body.Mode,
		Amount: body.Amount,
		Reason: body.Reason,
	}, r.RemoteAddr)
	if err != nil {
		h.writeAccountErr(w, err)
		return
	}
	response.OK(w, res)
}

func (h *AdminAccountsHandler) writeAccountErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrAccountNotFound):
		response.NotFound(w, "the customer has no account in that currency")
	case errors.Is(err, services.ErrBalanceModeInvalid):
		response.BadRequest(w, "INVALID_MODE", err.Error())
	case errors.Is(err, services.ErrBalanceReasonRequired):
		response.BadRequest(w, "REASON_REQUIRED", err.Error())
	case errors.Is(err, services.ErrAccountStatusInvalid):
		response.BadRequest(w, "INVALID_STATUS", err.Error())
	default:
		response.InternalError(w)
	}
}

// ─── Request DTOs ─────────────────────────────────────────────────────────────

// AdminUpdateBankingRequest — all fields optional; only those sent are updated.
type AdminUpdateBankingRequest struct {
	AccountNumber          *string `json:"account_number"`
	Iban                   *string `json:"iban"`
	Bic                    *string `json:"bic"`
	SortCode               *string `json:"sort_code"`
	AccountNumberUK        *string `json:"account_number_uk"`
	NombaBankName          *string `json:"nomba_bank_name"`
	NombaBankAccountNumber *string `json:"nomba_bank_account_number"`
	NombaBankAccountName   *string `json:"nomba_bank_account_name"`
}

type AdminAccountStatusRequest struct {
	Enabled bool `json:"enabled" example:"false"`
}

type AdminBalanceChangeRequest struct {
	Mode   string  `json:"mode" example:"adjust"`   // "set" | "adjust"
	Amount float64 `json:"amount" example:"100.00"` // set: absolute; adjust: signed delta
	Reason string  `json:"reason" example:"Goodwill credit for support ticket #123"`
}

// AdminBalanceChangeResponse documents the balance-change result (swagger DTO).
type AdminBalanceChangeResponse struct {
	PreviousBalance float64 `json:"previous_balance" example:"50.00"`
	NewBalance      float64 `json:"new_balance" example:"150.00"`
	Delta           float64 `json:"delta" example:"100.00"`
	Reference       string  `json:"reference" example:"ADJ-1a2b3c4d"`
}

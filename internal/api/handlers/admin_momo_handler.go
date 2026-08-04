package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// AdminMomoHandler serves the admin side of the manual mobile-money rails:
// managing the business collection numbers and reviewing (confirming/rejecting)
// customer deposit claims and cash-out requests.
type AdminMomoHandler struct {
	svc *services.Services
}

func NewAdminMomoHandler(svc *services.Services) *AdminMomoHandler {
	return &AdminMomoHandler{svc: svc}
}

// ─── Request DTOs ─────────────────────────────────────────────────────────────

// MomoAccountRequest creates or updates a business collection number.
type MomoAccountRequest struct {
	Provider     string `json:"provider" example:"wave"`
	DisplayName  string `json:"display_name" example:"Wave"`
	PhoneNumber  string `json:"phone_number" example:"+225 01 04 88 56 42"`
	AccountName  string `json:"account_name" example:"DigitalCapFX SARL"`
	Currency     string `json:"currency" example:"XOF" enums:"XOF,XAF"`
	Country      string `json:"country" example:"CI"`
	Instructions string `json:"instructions" example:"Send exactly the amount, then submit your reference."`
	IsActive     bool   `json:"is_active" example:"true"`
	SortOrder    int32  `json:"sort_order" example:"1"`
}

// ConfirmMomoDepositRequest confirms a deposit and credits the net amount.
type ConfirmMomoDepositRequest struct {
	// CreditedAmount is the amount to credit to the customer's ledger, after your charge.
	CreditedAmount float64 `json:"credited_amount" example:"9800"`
	// Note is an optional internal note (why this net amount, receipt ref, …).
	Note string `json:"note" example:"Received 10000, 200 charge"`
}

// CompleteMomoWithdrawalRequest marks a cash-out paid and records the charge.
type CompleteMomoWithdrawalRequest struct {
	// Charge is the business fee taken out of the amount (recipient receives amount - charge).
	Charge float64 `json:"charge" example:"100"`
	// Note is an optional internal note (payout reference, …).
	Note string `json:"note" example:"Paid via Wave, ref WD-778"`
}

// MomoReviewNoteRequest is the body for a reject action.
type MomoReviewNoteRequest struct {
	Note string `json:"note" example:"No payment received against this reference"`
}

// ─── Collection numbers ──────────────────────────────────────────────────────

// ListMomoAccounts godoc
//
//	@Summary      List all business mobile-money numbers (admin)
//	@Description  Returns every collection number including inactive ones. Requires momo:view.
//	@Tags         admin-momo
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {array}   db.ManualMomoAccount
//	@Failure      403  {object}  ErrorResponse
//	@Router       /admin/momo/accounts [get]
func (h *AdminMomoHandler) ListMomoAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.svc.ManualMomo.ListAllMomoAccounts(r.Context())
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, accounts)
}

// CreateMomoAccount godoc
//
//	@Summary      Add a business mobile-money number (admin)
//	@Description  Adds a collection number customers can pay to. Requires momo:manage. Currency must be XOF or XAF.
//	@Tags         admin-momo
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      MomoAccountRequest  true  "Number details"
//	@Success      201   {object}  db.ManualMomoAccount
//	@Failure      400   {object}  ErrorResponse
//	@Failure      403   {object}  ErrorResponse
//	@Router       /admin/momo/accounts [post]
func (h *AdminMomoHandler) CreateMomoAccount(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	var body MomoAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.Provider == "" || body.DisplayName == "" || body.PhoneNumber == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "provider, display_name and phone_number are required")
		return
	}
	acc, err := h.svc.ManualMomo.CreateMomoAccount(r.Context(), momoInput(body))
	if err != nil {
		momoBadRequest(w, err)
		return
	}
	h.svc.Staff.LogAction(r.Context(), set, "momo.account_create", "momo", acc.ID.String(), map[string]any{"provider": acc.Provider}, r.RemoteAddr)
	response.Created(w, acc)
}

// UpdateMomoAccount godoc
//
//	@Summary      Update a business mobile-money number (admin)
//	@Description  Edits a collection number (e.g. change the number, toggle active, reorder). Requires momo:manage.
//	@Tags         admin-momo
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string              true  "Momo account ID"
//	@Param        body  body      MomoAccountRequest  true  "Number details"
//	@Success      200   {object}  db.ManualMomoAccount
//	@Failure      400   {object}  ErrorResponse
//	@Failure      404   {object}  ErrorResponse
//	@Router       /admin/momo/accounts/{id} [put]
func (h *AdminMomoHandler) UpdateMomoAccount(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid id")
		return
	}
	var body MomoAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	acc, err := h.svc.ManualMomo.UpdateMomoAccount(r.Context(), id, momoInput(body))
	if err != nil {
		momoBadRequest(w, err)
		return
	}
	h.svc.Staff.LogAction(r.Context(), set, "momo.account_update", "momo", id.String(), nil, r.RemoteAddr)
	response.OK(w, acc)
}

// DeleteMomoAccount godoc
//
//	@Summary      Delete a business mobile-money number (admin)
//	@Description  Removes a collection number. Requires momo:manage. (Prefer toggling is_active off to keep history.)
//	@Tags         admin-momo
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "Momo account ID"
//	@Success      200  {object}  MessageResponse
//	@Failure      404  {object}  ErrorResponse
//	@Router       /admin/momo/accounts/{id} [delete]
func (h *AdminMomoHandler) DeleteMomoAccount(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid id")
		return
	}
	if err := h.svc.ManualMomo.DeleteMomoAccount(r.Context(), id); err != nil {
		response.InternalError(w)
		return
	}
	h.svc.Staff.LogAction(r.Context(), set, "momo.account_delete", "momo", id.String(), nil, r.RemoteAddr)
	response.OKWithMessage(w, "mobile money number removed", nil)
}

// ─── Deposit review ──────────────────────────────────────────────────────────

// ListDeposits godoc
//
//	@Summary      List manual deposit claims (admin)
//	@Description  Returns customer deposit claims for review, newest first. Filter with ?status=pending|confirmed|rejected. Requires momo:view.
//	@Tags         admin-momo
//	@Produce      json
//	@Security     BearerAuth
//	@Param        status    query     string  false  "Filter by status (pending|confirmed|rejected)"
//	@Param        page      query     int     false  "Page (default 1)"
//	@Param        per_page  query     int     false  "Per page, max 100 (default 20)"
//	@Success      200       {array}   db.ManualDeposit
//	@Failure      403       {object}  ErrorResponse
//	@Router       /admin/momo/deposits [get]
func (h *AdminMomoHandler) ListDeposits(w http.ResponseWriter, r *http.Request) {
	page, perPage := paginationParams(r)
	deps, err := h.svc.ManualMomo.ListDepositsByStatus(r.Context(), r.URL.Query().Get("status"), page, perPage)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, deps)
}

// ConfirmDeposit godoc
//
//	@Summary      Confirm a manual deposit (admin)
//	@Description  Confirms a customer's deposit claim and credits their ledger with credited_amount (the amount after your charge). Idempotent — a second confirm is rejected. Requires momo:deposits.
//	@Tags         admin-momo
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string                     true  "Deposit ID"
//	@Param        body  body      ConfirmMomoDepositRequest  true  "Net amount to credit"
//	@Success      200   {object}  db.ManualDeposit
//	@Failure      400   {object}  ErrorResponse
//	@Failure      404   {object}  ErrorResponse
//	@Failure      409   {object}  ErrorResponse
//	@Router       /admin/momo/deposits/{id}/confirm [post]
func (h *AdminMomoHandler) ConfirmDeposit(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid id")
		return
	}
	var body ConfirmMomoDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	staffID, _ := middleware.StaffIDFromContext(r.Context())
	dep, err := h.svc.ManualMomo.ConfirmDeposit(r.Context(), id, staffID, body.CreditedAmount, body.Note)
	if err != nil {
		momoBadRequest(w, err)
		return
	}
	h.svc.Staff.LogAction(r.Context(), set, "momo.deposit_confirm", "momo", id.String(), map[string]any{"credited": body.CreditedAmount}, r.RemoteAddr)
	response.OK(w, dep)
}

// RejectDeposit godoc
//
//	@Summary      Reject a manual deposit (admin)
//	@Description  Rejects a deposit claim (no funds are credited). Requires momo:deposits.
//	@Tags         admin-momo
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string                 true  "Deposit ID"
//	@Param        body  body      MomoReviewNoteRequest  false "Reason"
//	@Success      200   {object}  db.ManualDeposit
//	@Failure      404   {object}  ErrorResponse
//	@Failure      409   {object}  ErrorResponse
//	@Router       /admin/momo/deposits/{id}/reject [post]
func (h *AdminMomoHandler) RejectDeposit(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid id")
		return
	}
	var body MomoReviewNoteRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	staffID, _ := middleware.StaffIDFromContext(r.Context())
	dep, err := h.svc.ManualMomo.RejectDeposit(r.Context(), id, staffID, body.Note)
	if err != nil {
		momoBadRequest(w, err)
		return
	}
	h.svc.Staff.LogAction(r.Context(), set, "momo.deposit_reject", "momo", id.String(), nil, r.RemoteAddr)
	response.OK(w, dep)
}

// ─── Withdrawal review ───────────────────────────────────────────────────────

// ListWithdrawals godoc
//
//	@Summary      List manual cash-out requests (admin)
//	@Description  Returns customer cash-out requests for review, newest first. Filter with ?status=pending|completed|rejected. Requires momo:view.
//	@Tags         admin-momo
//	@Produce      json
//	@Security     BearerAuth
//	@Param        status    query     string  false  "Filter by status (pending|completed|rejected)"
//	@Param        page      query     int     false  "Page (default 1)"
//	@Param        per_page  query     int     false  "Per page, max 100 (default 20)"
//	@Success      200       {array}   db.ManualWithdrawal
//	@Failure      403       {object}  ErrorResponse
//	@Router       /admin/momo/withdrawals [get]
func (h *AdminMomoHandler) ListWithdrawals(w http.ResponseWriter, r *http.Request) {
	page, perPage := paginationParams(r)
	ws, err := h.svc.ManualMomo.ListWithdrawalsByStatus(r.Context(), r.URL.Query().Get("status"), page, perPage)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, ws)
}

// CompleteWithdrawal godoc
//
//	@Summary      Complete a manual cash-out (admin)
//	@Description  Marks a cash-out paid after you've sent the money manually. The customer was already debited the full amount at request time; charge records your fee (recipient got amount - charge). Idempotent. Requires momo:withdrawals.
//	@Tags         admin-momo
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string                         true  "Withdrawal ID"
//	@Param        body  body      CompleteMomoWithdrawalRequest  true  "Charge taken"
//	@Success      200   {object}  db.ManualWithdrawal
//	@Failure      400   {object}  ErrorResponse
//	@Failure      404   {object}  ErrorResponse
//	@Failure      409   {object}  ErrorResponse
//	@Router       /admin/momo/withdrawals/{id}/complete [post]
func (h *AdminMomoHandler) CompleteWithdrawal(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid id")
		return
	}
	var body CompleteMomoWithdrawalRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	staffID, _ := middleware.StaffIDFromContext(r.Context())
	wd, err := h.svc.ManualMomo.CompleteWithdrawal(r.Context(), id, staffID, body.Charge, body.Note)
	if err != nil {
		momoBadRequest(w, err)
		return
	}
	h.svc.Staff.LogAction(r.Context(), set, "momo.withdrawal_complete", "momo", id.String(), map[string]any{"charge": body.Charge}, r.RemoteAddr)
	response.OK(w, wd)
}

// RejectWithdrawal godoc
//
//	@Summary      Reject a manual cash-out (admin)
//	@Description  Cancels a pending cash-out and refunds the held amount to the customer's balance. Requires momo:withdrawals.
//	@Tags         admin-momo
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string                 true  "Withdrawal ID"
//	@Param        body  body      MomoReviewNoteRequest  false "Reason"
//	@Success      200   {object}  db.ManualWithdrawal
//	@Failure      404   {object}  ErrorResponse
//	@Failure      409   {object}  ErrorResponse
//	@Router       /admin/momo/withdrawals/{id}/reject [post]
func (h *AdminMomoHandler) RejectWithdrawal(w http.ResponseWriter, r *http.Request) {
	set, _ := middleware.StaffPermissionsFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid id")
		return
	}
	var body MomoReviewNoteRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	staffID, _ := middleware.StaffIDFromContext(r.Context())
	wd, err := h.svc.ManualMomo.RejectWithdrawal(r.Context(), id, staffID, body.Note)
	if err != nil {
		momoBadRequest(w, err)
		return
	}
	h.svc.Staff.LogAction(r.Context(), set, "momo.withdrawal_reject", "momo", id.String(), nil, r.RemoteAddr)
	response.OK(w, wd)
}

func momoInput(b MomoAccountRequest) services.MomoAccountInput {
	return services.MomoAccountInput{
		Provider: b.Provider, DisplayName: b.DisplayName, PhoneNumber: b.PhoneNumber,
		AccountName: b.AccountName, Currency: b.Currency, Country: b.Country,
		Instructions: b.Instructions, IsActive: b.IsActive, SortOrder: b.SortOrder,
	}
}

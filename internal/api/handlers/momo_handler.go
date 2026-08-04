package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// MomoHandler serves the customer-facing manual mobile-money endpoints: view the
// business collection numbers, claim a payment (top-up), and request a cash-out.
type MomoHandler struct {
	svc *services.Services
}

func NewMomoHandler(svc *services.Services) *MomoHandler {
	return &MomoHandler{svc: svc}
}

// ─── Request/response DTOs (for swagger) ─────────────────────────────────────

// SubmitMomoDepositRequest is the "I have paid" claim body.
type SubmitMomoDepositRequest struct {
	// MomoAccountID is the business collection number (from GET /momo/accounts) the customer paid to.
	MomoAccountID string `json:"momo_account_id" example:"6f1c2e4a-8b2d-4c9a-9f3e-1a2b3c4d5e6f"`
	// Amount the customer paid (in the number's currency, e.g. XOF).
	Amount float64 `json:"amount" example:"10000"`
	// SenderPhone is the customer's own mobile-money number they paid from (optional).
	SenderPhone string `json:"sender_phone" example:"+225 07 00 00 00 00"`
	// SenderName on the customer's mobile-money account (optional).
	SenderName string `json:"sender_name" example:"Ama Kone"`
	// Reference is the mobile-money transaction id/reference from the payment (optional but speeds confirmation).
	Reference string `json:"reference" example:"WAVE-TXN-123456"`
	// Note is any extra context for the reviewer (optional).
	Note string `json:"note" example:"Paid at 14:32"`
}

// RequestMomoWithdrawalRequest is the cash-out request body.
type RequestMomoWithdrawalRequest struct {
	// Currency to withdraw — XOF or XAF.
	Currency string `json:"currency" example:"XOF" enums:"XOF,XAF"`
	// Provider is the mobile-money network to pay the recipient on.
	Provider string `json:"provider" example:"wave"`
	// Amount to send (debited from the customer immediately; charge is taken out of this).
	Amount float64 `json:"amount" example:"5000"`
	// RecipientPhone is the mobile-money number to pay out to.
	RecipientPhone string `json:"recipient_phone" example:"+225 05 00 00 00 00"`
	// RecipientName of the payee (optional).
	RecipientName string `json:"recipient_name" example:"Kofi Mensah"`
	// Note for the reviewer (optional).
	Note string `json:"note" example:"Rent payment"`
}

// ListMomoAccounts godoc
//
//	@Summary      List business mobile-money numbers
//	@Description  Returns the active mobile-money collection numbers the customer can pay to for a manual deposit, one per provider (Wave, Orange Money, MTN, Moov, KBINE …). Display these and let the customer pick the one matching the app they'll pay from, then submit the claim via POST /momo/deposits. This is the manual rail; it runs alongside the automated Hub2 flow.
//	@Tags         Manual Mobile Money
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {array}   db.ManualMomoAccount
//	@Failure      401  {object}  ErrorResponse
//	@Router       /momo/accounts [get]
func (h *MomoHandler) ListMomoAccounts(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	accounts, err := h.svc.ManualMomo.ListActiveMomoAccounts(r.Context())
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, accounts)
}

// SubmitDeposit godoc
//
//	@Summary      Claim a manual mobile-money deposit ("I have paid")
//	@Description  After paying one of the business numbers out-of-band, the customer submits this to log the payment. It is recorded as pending, admins are emailed to confirm receipt, and the customer's ledger is credited (in the number's currency, minus the business charge) only once an admin confirms. Nothing is credited automatically.
//	@Tags         Manual Mobile Money
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      SubmitMomoDepositRequest  true  "Deposit claim"
//	@Success      201   {object}  db.ManualDeposit
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /momo/deposits [post]
func (h *MomoHandler) SubmitDeposit(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body SubmitMomoDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	momoID, err := uuid.Parse(body.MomoAccountID)
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid momo_account_id")
		return
	}
	if body.Amount <= 0 {
		response.BadRequest(w, "VALIDATION_ERROR", "amount must be greater than zero")
		return
	}
	dep, err := h.svc.ManualMomo.SubmitDeposit(r.Context(), services.SubmitDepositInput{
		UserID: userID, MomoAccountID: momoID, Amount: body.Amount,
		SenderPhone: body.SenderPhone, SenderName: body.SenderName, Reference: body.Reference, Note: body.Note,
	})
	if err != nil {
		momoBadRequest(w, err)
		return
	}
	response.Created(w, dep)
}

// ListMyDeposits godoc
//
//	@Summary      List my manual deposits
//	@Description  Returns the customer's manual mobile-money deposit claims and their status (pending, confirmed, rejected), newest first.
//	@Tags         Manual Mobile Money
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page      query     int  false  "Page (default 1)"
//	@Param        per_page  query     int  false  "Per page, max 100 (default 20)"
//	@Success      200       {array}   db.ManualDeposit
//	@Failure      401       {object}  ErrorResponse
//	@Router       /momo/deposits [get]
func (h *MomoHandler) ListMyDeposits(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	page, perPage := paginationParams(r)
	deps, err := h.svc.ManualMomo.ListUserDeposits(r.Context(), userID, page, perPage)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, deps)
}

// RequestWithdrawal godoc
//
//	@Summary      Request a manual mobile-money cash-out
//	@Description  Requests a payout to a mobile-money number. The amount is debited (held) from the customer's balance immediately to prevent double-spend, admins are emailed, and an admin sends the money manually then marks it complete (the charge is taken out of the amount). If rejected, the held amount is refunded. Supports XOF and XAF.
//	@Tags         Manual Mobile Money
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      RequestMomoWithdrawalRequest  true  "Cash-out request"
//	@Success      201   {object}  db.ManualWithdrawal
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /momo/withdrawals [post]
func (h *MomoHandler) RequestWithdrawal(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body RequestMomoWithdrawalRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.Amount <= 0 || body.RecipientPhone == "" || body.Provider == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "provider, recipient_phone and a positive amount are required")
		return
	}
	w2, err := h.svc.ManualMomo.RequestWithdrawal(r.Context(), services.RequestWithdrawalInput{
		UserID: userID, Currency: body.Currency, Provider: body.Provider, Amount: body.Amount,
		RecipientPhone: body.RecipientPhone, RecipientName: body.RecipientName, Note: body.Note,
	})
	if err != nil {
		momoBadRequest(w, err)
		return
	}
	response.Created(w, w2)
}

// ListMyWithdrawals godoc
//
//	@Summary      List my manual cash-outs
//	@Description  Returns the customer's manual mobile-money cash-out requests and their status (pending, completed, rejected), newest first.
//	@Tags         Manual Mobile Money
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page      query     int  false  "Page (default 1)"
//	@Param        per_page  query     int  false  "Per page, max 100 (default 20)"
//	@Success      200       {array}   db.ManualWithdrawal
//	@Failure      401       {object}  ErrorResponse
//	@Router       /momo/withdrawals [get]
func (h *MomoHandler) ListMyWithdrawals(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	page, perPage := paginationParams(r)
	ws, err := h.svc.ManualMomo.ListUserWithdrawals(r.Context(), userID, page, perPage)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, ws)
}

// ─── shared helpers ──────────────────────────────────────────────────────────

// momoBadRequest maps service errors to a 4xx response.
func momoBadRequest(w http.ResponseWriter, err error) {
	switch err {
	case services.ErrMomoNotFound, services.ErrManualNotFound:
		response.NotFound(w, err.Error())
	case services.ErrManualAlreadyReviewed:
		response.Conflict(w, "ALREADY_REVIEWED", err.Error())
	case services.ErrAccountNotFound:
		response.BadRequest(w, "ACCOUNT_NOT_FOUND", "you don't have an account in that currency")
	case services.ErrMomoInsufficientFunds:
		response.BadRequest(w, "INSUFFICIENT_FUNDS", err.Error())
	case services.ErrMomoInvalidAmount, services.ErrMomoUnsupportedCurrency, services.ErrMomoInactive,
		services.ErrMomoCreditTooHigh, services.ErrMomoChargeTooHigh:
		response.BadRequest(w, "VALIDATION_ERROR", err.Error())
	default:
		response.InternalError(w)
	}
}

func paginationParams(r *http.Request) (page, perPage int32) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pp, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	return int32(p), int32(pp)
}

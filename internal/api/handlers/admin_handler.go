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

type AdminHandler struct {
	svc *services.Services
}

func NewAdminHandler(svc *services.Services) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// ListPendingKYC godoc
//
//	@Summary      List users awaiting KYC review
//	@Description  Returns all users whose identity verification status is "under_review" or "submitted" — ready for admin decision.
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  object
//	@Failure      401  {object}  ErrorResponse
//	@Failure      403  {object}  ErrorResponse
//	@Router       /admin/kyc/pending [get]
func (h *AdminHandler) ListPendingKYC(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.KYC.ListPendingKYC(r.Context())
	if err != nil {
		response.InternalError(w)
		return
	}

	type item struct {
		ID          string  `json:"id"`
		PhoneNumber string  `json:"phone_number"`
		Email       *string `json:"email,omitempty"`
		FirstName   string  `json:"first_name"`
		LastName    string  `json:"last_name"`
		KycStatus   string  `json:"kyc_status"`
	}

	out := make([]item, 0, len(users))
	for _, u := range users {
		out = append(out, item{
			ID:          u.ID.String(),
			PhoneNumber: u.PhoneNumber,
			Email:       u.Email,
			FirstName:   u.FirstName,
			LastName:    u.LastName,
			KycStatus:   u.KycStatus,
		})
	}

	response.OK(w, out)
}

// ApproveKYC godoc
//
//	@Summary      Approve user KYC
//	@Description  Marks the user's identity as verified and activates full account access. Sends a confirmation email to the user.
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "User ID"
//	@Success      200  {object}  MessageResponse
//	@Failure      400  {object}  ErrorResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      403  {object}  ErrorResponse
//	@Router       /admin/kyc/{id}/approve [post]
func (h *AdminHandler) ApproveKYC(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := parseAdminKYCParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.KYC.AdminApproveKYC(r.Context(), userID, adminID); err != nil {
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "KYC approved — user account is now fully active", nil)
}

// ReleaseKYCToProvider godoc
//
//	@Summary      Release KYC control back to the provider
//	@Description  Clears the admin manual-override flag so the automated provider (Sumsub) can once again set the user's KYC status via webhook. Use this to undo a manual approve/reject and return to hybrid auto mode.
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "User ID"
//	@Success      200  {object}  MessageResponse
//	@Failure      400  {object}  ErrorResponse
//	@Failure      403  {object}  ErrorResponse
//	@Router       /admin/kyc/{id}/release [post]
func (h *AdminHandler) ReleaseKYCToProvider(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := parseAdminKYCParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.KYC.AdminReleaseKYCToProvider(r.Context(), userID, adminID); err != nil {
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "KYC control released — provider decisions will apply again", nil)
}

// RejectKYC godoc
//
//	@Summary      Reject user KYC
//	@Description  Marks the user's identity verification as rejected and notifies them with the reason. The user can resubmit.
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string             true  "User ID"
//	@Param        body  body      AdminKYCRejectRequest  true  "Rejection reason"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      403   {object}  ErrorResponse
//	@Router       /admin/kyc/{id}/reject [post]
func (h *AdminHandler) RejectKYC(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := parseAdminKYCParams(w, r)
	if !ok {
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Reason == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "reason is required")
		return
	}

	if err := h.svc.KYC.AdminRejectKYC(r.Context(), userID, adminID, body.Reason); err != nil {
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "KYC rejected — user has been notified", nil)
}

// ─── FX Rate management ───────────────────────────────────────────────────────

// SetWithdrawalRate godoc
//
//	@Summary      Set or update a business FX rate for fiat withdrawals
//	@Description  Creates or replaces the admin-controlled conversion rate for a
//	              currency pair. The business rate is applied when users withdraw
//	              (e.g. USD→XAF). The spread between this rate and the interbank
//	              rate is the business margin. Also configures the fee percentage
//	              and optional flat fee charged on each withdrawal.
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      AdminSetRateRequest  true  "Rate configuration"
//	@Success      200   {object}  object
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      403   {object}  ErrorResponse
//	@Router       /admin/withdrawal-rates [post]
func (h *AdminHandler) SetWithdrawalRate(w http.ResponseWriter, r *http.Request) {
	adminID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body AdminSetRateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.SourceCurrency == "" || body.TargetCurrency == "" || body.Rate <= 0 {
		response.BadRequest(w, "VALIDATION_ERROR", "source_currency, target_currency and rate are required")
		return
	}

	rate, err := h.svc.Withdrawal.SetRate(r.Context(), adminID, services.SetRateRequest{
		SourceCurrency: body.SourceCurrency,
		TargetCurrency: body.TargetCurrency,
		Rate:           body.Rate,
		FeePercent:     body.FeePercent,
		FlatFee:        body.FlatFee,
	})
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, rate)
}

// ListWithdrawalRates godoc
//
//	@Summary      List all business FX rates
//	@Description  Returns all configured withdrawal FX rates (active and inactive).
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {array}  object
//	@Router       /admin/withdrawal-rates [get]
func (h *AdminHandler) ListWithdrawalRates(w http.ResponseWriter, r *http.Request) {
	rates, err := h.svc.Withdrawal.ListRates(r.Context())
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, rates)
}

// AdminSetRateRequest is the body for SetWithdrawalRate.
type AdminSetRateRequest struct {
	SourceCurrency string  `json:"source_currency"` // e.g. "USD"
	TargetCurrency string  `json:"target_currency"` // e.g. "XAF"
	Rate           float64 `json:"rate"`            // e.g. 595.0 → 1 USD = 595 XAF
	FeePercent     float64 `json:"fee_percent"`     // e.g. 0.01 = 1% fee on source amount
	FlatFee        float64 `json:"flat_fee"`        // e.g. 0.50 flat fee in source currency
}

func parseAdminKYCParams(w http.ResponseWriter, r *http.Request) (userID, adminID uuid.UUID, ok bool) {
	adminID, valid := middleware.UserIDFromContext(r.Context())
	if !valid {
		response.Unauthorized(w, "unauthorized")
		return uuid.Nil, uuid.Nil, false
	}
	uid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid user id")
		return uuid.Nil, uuid.Nil, false
	}
	return uid, adminID, true
}

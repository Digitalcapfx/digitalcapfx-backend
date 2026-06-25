package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type SecurityHandler struct {
	svc *services.Services
}

func NewSecurityHandler(svc *services.Services) *SecurityHandler {
	return &SecurityHandler{svc: svc}
}

// GetStatus godoc
//
//	@Summary      Security status
//	@Description  Returns whether 2FA and biometrics are enabled for the authenticated user.
//	@Tags         security
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  map[string]any
//	@Failure      401  {object}  ErrorResponse
//	@Router       /security [get]
func (h *SecurityHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	status, err := h.svc.Security.GetStatus(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, status)
}

// ─── 2FA ──────────────────────────────────────────────────────────────────────

// Setup2FA godoc
//
//	@Summary      Set up 2FA
//	@Description  Generates a TOTP secret and OTP URI. Encode the URI as a QR code and scan it in an authenticator app, then call /security/2fa/confirm to activate.
//	@Tags         security
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  map[string]any
//	@Failure      400  {object}  ErrorResponse  "2FA already enabled"
//	@Failure      401  {object}  ErrorResponse
//	@Router       /security/2fa/setup [post]
func (h *SecurityHandler) Setup2FA(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	result, err := h.svc.Security.SetupTOTP(r.Context(), userID)
	switch {
	case errors.Is(err, services.ErrTOTPAlreadyActive):
		response.BadRequest(w, "TOTP_ACTIVE", err.Error())
	case err != nil:
		response.InternalError(w)
	default:
		response.OK(w, result)
	}
}

// Confirm2FA godoc
//
//	@Summary      Confirm and activate 2FA
//	@Description  Verifies the first TOTP code from the authenticator app and activates 2FA on the account.
//	@Tags         security
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      map[string]string  true  "{ code }"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse  "Invalid code or setup expired"
//	@Failure      401   {object}  ErrorResponse
//	@Router       /security/2fa/confirm [post]
func (h *SecurityHandler) Confirm2FA(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "code is required")
		return
	}
	err := h.svc.Security.ConfirmTOTP(r.Context(), userID, body.Code)
	switch {
	case errors.Is(err, services.ErrTOTPSetupExpired):
		response.BadRequest(w, "SETUP_EXPIRED", "setup session expired — restart 2FA setup")
	case errors.Is(err, services.ErrTOTPInvalid):
		response.BadRequest(w, "INVALID_CODE", "invalid TOTP code")
	case err != nil:
		response.InternalError(w)
	default:
		response.OKWithMessage(w, "2FA activated — all future sign-ins will require a TOTP code", nil)
	}
}

// Disable2FA godoc
//
//	@Summary      Disable 2FA
//	@Description  Verifies a live TOTP code and deactivates two-factor authentication.
//	@Tags         security
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      map[string]string  true  "{ code }"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /security/2fa [delete]
func (h *SecurityHandler) Disable2FA(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "code is required")
		return
	}
	err := h.svc.Security.DisableTOTP(r.Context(), userID, body.Code)
	switch {
	case errors.Is(err, services.ErrTOTPNotEnabled):
		response.BadRequest(w, "NOT_ENABLED", "2FA is not enabled on this account")
	case errors.Is(err, services.ErrTOTPInvalid):
		response.BadRequest(w, "INVALID_CODE", "invalid TOTP code")
	case err != nil:
		response.InternalError(w)
	default:
		response.OKWithMessage(w, "2FA disabled", nil)
	}
}

// ─── PIN Change ───────────────────────────────────────────────────────────────

// ChangePIN godoc
//
//	@Summary      Change PIN
//	@Description  Verifies the current PIN and sets a new one. Does not revoke existing sessions.
//	@Tags         security
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      map[string]string  true  "{ current_pin, new_pin }"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse  "Wrong current PIN"
//	@Failure      401   {object}  ErrorResponse
//	@Router       /security/pin/change [post]
func (h *SecurityHandler) ChangePIN(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body struct {
		CurrentPIN string `json:"current_pin"`
		NewPIN     string `json:"new_pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.CurrentPIN == "" || body.NewPIN == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "current_pin and new_pin are required")
		return
	}
	if len(body.NewPIN) < 4 {
		response.BadRequest(w, "VALIDATION_ERROR", "new_pin must be at least 4 digits")
		return
	}
	err := h.svc.Security.ChangePIN(r.Context(), userID, body.CurrentPIN, body.NewPIN)
	switch {
	case errors.Is(err, services.ErrWrongPIN):
		response.BadRequest(w, "WRONG_PIN", "current PIN is incorrect")
	case errors.Is(err, services.ErrSocialAuthUser):
		response.BadRequest(w, "SOCIAL_AUTH", "this account uses social login and has no PIN")
	case err != nil:
		response.InternalError(w)
	default:
		response.OKWithMessage(w, "PIN updated successfully", nil)
	}
}

// ─── Biometrics ───────────────────────────────────────────────────────────────

// EnableBiometrics godoc
//
//	@Summary      Enable biometrics
//	@Description  Marks biometric authentication as enabled in user preferences. Actual biometric credential management is handled on the device.
//	@Tags         security
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  MessageResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /security/biometrics/enable [post]
func (h *SecurityHandler) EnableBiometrics(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	if err := h.svc.Security.SetBiometrics(r.Context(), userID, true); err != nil {
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "biometrics enabled", nil)
}

// DisableBiometrics godoc
//
//	@Summary      Disable biometrics
//	@Description  Removes the biometrics enabled flag from user preferences.
//	@Tags         security
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  MessageResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /security/biometrics [delete]
func (h *SecurityHandler) DisableBiometrics(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	if err := h.svc.Security.SetBiometrics(r.Context(), userID, false); err != nil {
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "biometrics disabled", nil)
}

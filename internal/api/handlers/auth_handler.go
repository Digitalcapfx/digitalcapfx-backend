package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/pkg/token"
	"github.com/rachfinance/digitalfx/internal/services"
)

type AuthHandler struct {
	svc *services.Services
	cfg *config.Config
}

func NewAuthHandler(svc *services.Services, cfg *config.Config) *AuthHandler {
	return &AuthHandler{svc: svc, cfg: cfg}
}

// ─── Phone OTP ────────────────────────────────────────────────────────────────

// SendOTP godoc
//
//	@Summary      Send phone OTP
//	@Description  Sends a 6-digit OTP to the phone number via SMS. Required before register.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      SendOTPRequest  true  "Phone number (E.164)"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /auth/otp/send [post]
func (h *AuthHandler) SendOTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Phone == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "phone is required")
		return
	}
	if err := h.svc.Auth.SendOTP(r.Context(), body.Phone); err != nil {
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "OTP sent", nil)
}

// VerifyOTP godoc
//
//	@Summary      Verify phone OTP
//	@Description  Confirms the OTP sent to the phone. Call before register for new users.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      VerifyOTPRequest  true  "Phone + OTP code"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Router       /auth/otp/verify [post]
func (h *AuthHandler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if err := h.svc.Auth.VerifyOTP(r.Context(), body.Phone, body.Code); err != nil {
		response.BadRequest(w, "INVALID_OTP", "invalid or expired OTP")
		return
	}
	response.OKWithMessage(w, "OTP verified", nil)
}

// ─── Register ─────────────────────────────────────────────────────────────────

// Register godoc
//
//	@Summary      Register
//	@Description  Creates a new user account, provisions fiat accounts, and returns a JWT pair. A welcome email and email verification OTP are sent asynchronously.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      RegisterRequest   true  "New user details"
//	@Success      201   {object}  TokenPairResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      409   {object}  ErrorResponse  "Phone already registered"
//	@Failure      500   {object}  ErrorResponse
//	@Router       /auth/register [post]
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone     string `json:"phone"`
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		PIN       string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	pair, err := h.svc.Auth.Register(r.Context(), services.RegisterInput{
		Phone:     body.Phone,
		Email:     body.Email,
		FirstName: body.FirstName,
		LastName:  body.LastName,
		PIN:       body.PIN,
		DeviceIP:  realIP(r),
		DeviceUA:  r.UserAgent(),
	})
	if errors.Is(err, services.ErrUserExists) {
		response.Conflict(w, "USER_EXISTS", "a user with this phone number already exists")
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}
	notifyFromPair(r, pair.AccessToken, h.cfg.JWT.Secret, h.svc, services.CreateNotificationInput{
		Type:  services.NotifWelcome,
		Title: "Welcome to DigitalFX",
		Body:  "Your account is set up. Complete identity verification to unlock transfers and wallets.",
	})
	response.Created(w, pair)
}

// ─── Login ────────────────────────────────────────────────────────────────────

// Login godoc
//
//	@Summary      Login
//	@Description  Authenticates with phone + PIN. Creates a device session and sends a login notification email. Returns a JWT pair with a session_id for device management.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      LoginRequest  true  "Credentials"
//	@Success      200   {object}  TokenPairResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      403   {object}  ErrorResponse  "Account inactive"
//	@Failure      500   {object}  ErrorResponse
//	@Router       /auth/login [post]
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
		PIN   string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	result, err := h.svc.Auth.Login(r.Context(), services.LoginInput{
		Phone:    body.Phone,
		PIN:      body.PIN,
		DeviceIP: realIP(r),
		DeviceUA: r.UserAgent(),
	})
	switch {
	case errors.Is(err, services.ErrUserNotFound), errors.Is(err, services.ErrInvalidPIN):
		response.Unauthorized(w, "invalid credentials")
	case errors.Is(err, services.ErrAccountInactive):
		response.Forbidden(w, "account is inactive")
	case err != nil:
		response.InternalError(w)
	case result.Requires2FA:
		response.OK(w, map[string]any{
			"requires_2fa": true,
			"ref":          result.TOTPRef,
			"message":      "enter your authenticator code to complete sign-in",
		})
	default:
		notifyFromPair(r, result.Pair.AccessToken, h.cfg.JWT.Secret, h.svc, services.CreateNotificationInput{
			Type:  services.NotifLoginDetected,
			Title: "New Login Detected",
			Body:  fmt.Sprintf("A sign-in was detected from %s. Not you? Change your PIN immediately.", realIP(r)),
			Metadata: map[string]string{"ip": realIP(r), "device": r.UserAgent()},
		})
		response.OK(w, result.Pair)
	}
}

// CompleteTOTPLogin godoc
//
//	@Summary      Complete 2FA login
//	@Description  Exchanges a 2FA pending ref (from /auth/login) + a TOTP code for a full JWT pair.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      TOTPLoginRequest  true  "ref + TOTP code"
//	@Success      200   {object}  TokenPairResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /auth/2fa/login [post]
func (h *AuthHandler) CompleteTOTPLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ref  string `json:"ref"`
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Ref == "" || body.Code == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "ref and code are required")
		return
	}
	pair, err := h.svc.Auth.CompleteTOTPLogin(r.Context(), body.Ref, body.Code, realIP(r), r.UserAgent())
	if err != nil {
		response.Unauthorized(w, "invalid or expired 2FA code")
		return
	}
	notifyFromPair(r, pair.AccessToken, h.cfg.JWT.Secret, h.svc, services.CreateNotificationInput{
		Type:     services.NotifLoginDetected,
		Title:    "New Login Detected",
		Body:     fmt.Sprintf("A sign-in (2FA) was detected from %s.", realIP(r)),
		Metadata: map[string]string{"ip": realIP(r), "method": "totp"},
	})
	response.OK(w, pair)
}

// ─── Logout ───────────────────────────────────────────────────────────────────

// Logout godoc
//
//	@Summary      Logout
//	@Description  Revokes the current device session. The refresh token is immediately invalidated; access token remains valid until expiry (max 30 min). A sign-out notification email is sent.
//	@Tags         auth
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  MessageResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /auth/logout [post]
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	sessionID, _ := middleware.SessionIDFromContext(r.Context())
	if err := h.svc.Auth.Logout(r.Context(), userID, sessionID); err != nil {
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "logged out successfully", nil)
}

// ─── Token Refresh ────────────────────────────────────────────────────────────

// RefreshToken godoc
//
//	@Summary      Refresh token
//	@Description  Issues a new rotated access + refresh token pair. The old refresh token is invalidated. Validates the session is still active.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      RefreshTokenRequest  true  "Refresh token"
//	@Success      200   {object}  TokenPairResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /auth/token/refresh [post]
func (h *AuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RefreshToken == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "refresh_token is required")
		return
	}
	pair, err := h.svc.Auth.RefreshToken(r.Context(), body.RefreshToken)
	if err != nil {
		response.Unauthorized(w, "invalid or expired refresh token")
		return
	}
	response.OK(w, pair)
}

// ─── Google Sign-In ───────────────────────────────────────────────────────────

// GoogleSignIn godoc
//
//	@Summary      Google Sign-In
//	@Description  Verifies a Google ID token (from the mobile SDK or web client). Creates an account on first sign-in or logs into the existing linked account. New users must complete KYC before financial features unlock.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      GoogleSignInRequest   true  "Google ID token from the client SDK"
//	@Success      200   {object}  GoogleSignInResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /auth/google [post]
func (h *AuthHandler) GoogleSignIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IDToken == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "id_token is required")
		return
	}

	result, err := h.svc.Auth.GoogleSignIn(r.Context(), services.GoogleSignInInput{
		IDToken:  body.IDToken,
		DeviceIP: realIP(r),
		DeviceUA: r.UserAgent(),
	})
	if errors.Is(err, services.ErrInvalidToken) {
		response.Unauthorized(w, "invalid Google ID token")
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}

	nType, nTitle, nBody := services.NotifLoginDetected,
		"New Sign-In Detected",
		fmt.Sprintf("Sign-in via Google from %s.", realIP(r))
	if result.IsNewUser {
		nType, nTitle, nBody = services.NotifWelcome,
			"Welcome to DigitalFX",
			"Your Google account is linked. Complete identity verification to unlock full access."
	}
	notifyFromPair(r, result.Pair.AccessToken, h.cfg.JWT.Secret, h.svc, services.CreateNotificationInput{
		Type: nType, Title: nTitle, Body: nBody,
		Metadata: map[string]string{"ip": realIP(r), "provider": "google"},
	})

	response.JSON(w, http.StatusOK, GoogleSignInResponse{
		Success:   true,
		IsNewUser: result.IsNewUser,
		Data: TokenPairData{
			AccessToken:  result.Pair.AccessToken,
			RefreshToken: result.Pair.RefreshToken,
			ExpiresIn:    result.Pair.ExpiresIn,
			SessionID:    result.Pair.SessionID,
		},
	})
}

// ─── Forgot / Reset PIN ───────────────────────────────────────────────────────

// ForgotPIN godoc
//
//	@Summary      Forgot PIN
//	@Description  Sends a 6-digit reset code to the registered email. Accepts email or phone. Always returns 200 to prevent account enumeration.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      ForgotPINRequest  true  "Email or phone"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Router       /auth/forgot-pin [post]
func (h *AuthHandler) ForgotPIN(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EmailOrPhone string `json:"email_or_phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.EmailOrPhone == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "email_or_phone is required")
		return
	}
	_ = h.svc.Auth.ForgotPIN(r.Context(), body.EmailOrPhone)
	response.OKWithMessage(w, "if an account exists, a reset code has been sent to the registered email", nil)
}

// ResetPIN godoc
//
//	@Summary      Reset PIN
//	@Description  Verifies the 6-digit OTP and sets a new PIN. All active sessions are revoked. A confirmation email is sent.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      ResetPINRequest  true  "Email/phone + OTP + new PIN"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Router       /auth/reset-pin [post]
func (h *AuthHandler) ResetPIN(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EmailOrPhone string `json:"email_or_phone"`
		Code         string `json:"code"`
		NewPIN       string `json:"new_pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.EmailOrPhone == "" || body.Code == "" || body.NewPIN == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "email_or_phone, code, and new_pin are required")
		return
	}
	err := h.svc.Auth.ResetPIN(r.Context(), services.ResetPINInput{
		EmailOrPhone: body.EmailOrPhone,
		OTPCode:      body.Code,
		NewPIN:       body.NewPIN,
		DeviceUA:     r.UserAgent(),
	})
	switch {
	case errors.Is(err, services.ErrInvalidOTP):
		response.BadRequest(w, "INVALID_OTP", "invalid or expired reset code")
	case errors.Is(err, services.ErrUserNotFound):
		response.BadRequest(w, "NOT_FOUND", "account not found")
	case err != nil:
		response.InternalError(w)
	default:
		response.OKWithMessage(w, "PIN reset successfully — please log in with your new PIN", nil)
	}
}

// ─── Email Verification ───────────────────────────────────────────────────────

// SendEmailOTP godoc
//
//	@Summary      Resend email verification OTP
//	@Description  Sends a new 6-digit code to the authenticated user's registered email address.
//	@Tags         auth
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  MessageResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /auth/email/resend-otp [post]
func (h *AuthHandler) SendEmailOTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	if err := h.svc.Auth.SendEmailVerificationOTP(r.Context(), userID); err != nil {
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "verification code sent to your email", nil)
}

// VerifyEmail godoc
//
//	@Summary      Verify email address
//	@Description  Confirms the 6-digit OTP sent to the user's email and marks it as verified.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      VerifyEmailRequest  true  "OTP code"
//	@Success      200   {object}  MessageResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /auth/email/verify [post]
func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
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
	if err := h.svc.Auth.VerifyEmail(r.Context(), userID, body.Code); err != nil {
		if errors.Is(err, services.ErrInvalidOTP) {
			response.BadRequest(w, "INVALID_OTP", "invalid or expired verification code")
			return
		}
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "email verified successfully", nil)
}

// ─── Devices ──────────────────────────────────────────────────────────────────

// ListDevices godoc
//
//	@Summary      List active devices
//	@Description  Returns all active login sessions with device name, IP, and last activity.
//	@Tags         auth
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  DeviceListResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /auth/devices [get]
func (h *AuthHandler) ListDevices(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	sessions, err := h.svc.Auth.ListDevices(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	currentSessionID, _ := middleware.SessionIDFromContext(r.Context())
	devices := make([]DeviceData, 0, len(sessions))
	for _, s := range sessions {
		devices = append(devices, DeviceData{
			ID:         s.ID.String(),
			DeviceName: derefStr(s.DeviceName, "Unknown Device"),
			DeviceIP:   derefStr(s.DeviceIP, ""),
			LastUsedAt: s.LastUsedAt,
			CreatedAt:  s.CreatedAt,
			IsCurrent:  s.ID.String() == currentSessionID,
		})
	}
	response.OK(w, devices)
}

// DisconnectDevice godoc
//
//	@Summary      Disconnect a device
//	@Description  Revokes a specific login session. The device is signed out on next token refresh.
//	@Tags         auth
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path      string  true  "Session ID"
//	@Success      200  {object}  MessageResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      404  {object}  ErrorResponse
//	@Router       /auth/devices/{id} [delete]
func (h *AuthHandler) DisconnectDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(sessionID); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid session id")
		return
	}
	if err := h.svc.Auth.DisconnectDevice(r.Context(), userID, sessionID); err != nil {
		if errors.Is(err, services.ErrSessionNotFound) {
			response.NotFound(w, "session not found")
			return
		}
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "device disconnected", nil)
}

// DisconnectAllDevices godoc
//
//	@Summary      Disconnect all other devices
//	@Description  Revokes all active sessions except the current one.
//	@Tags         auth
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  MessageResponse
//	@Failure      401  {object}  ErrorResponse
//	@Router       /auth/devices [delete]
func (h *AuthHandler) DisconnectAllDevices(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	currentSessionID, _ := middleware.SessionIDFromContext(r.Context())
	if err := h.svc.Auth.DisconnectAllDevices(r.Context(), userID, currentSessionID); err != nil {
		response.InternalError(w)
		return
	}
	response.OKWithMessage(w, "all other devices have been disconnected", nil)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

func derefStr(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

// notifyFromPair extracts the userID from a freshly issued access token and
// fires a notification. Errors are swallowed — a notification must never fail a
// primary auth operation.
func notifyFromPair(r *http.Request, accessToken, jwtSecret string, svc *services.Services, in services.CreateNotificationInput) {
	claims, err := token.Parse(accessToken, jwtSecret)
	if err != nil {
		return
	}
	uid, err := uuid.Parse(claims.UserID)
	if err != nil {
		return
	}
	in.UserID = uid
	svc.Notifications.Create(r.Context(), in)
}


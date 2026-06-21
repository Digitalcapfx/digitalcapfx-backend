package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/services"
)

type AuthHandler struct {
	svc *services.Services
	cfg *config.Config
}

func NewAuthHandler(svc *services.Services, cfg *config.Config) *AuthHandler {
	return &AuthHandler{svc: svc, cfg: cfg}
}

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
	})
	if errors.Is(err, services.ErrUserExists) {
		response.Conflict(w, "USER_EXISTS", "a user with this phone number already exists")
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, pair)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
		PIN   string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}

	pair, err := h.svc.Auth.Login(r.Context(), services.LoginInput{
		Phone: body.Phone,
		PIN:   body.PIN,
	})
	switch {
	case errors.Is(err, services.ErrUserNotFound):
		response.Unauthorized(w, "invalid credentials")
	case errors.Is(err, services.ErrInvalidPIN):
		response.Unauthorized(w, "invalid credentials")
	case errors.Is(err, services.ErrAccountInactive):
		response.Forbidden(w, "account is inactive")
	case err != nil:
		response.InternalError(w)
	default:
		response.OK(w, pair)
	}
}

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

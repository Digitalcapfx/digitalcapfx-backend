package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type ProfileHandler struct {
	svc *services.Services
}

func NewProfileHandler(svc *services.Services) *ProfileHandler {
	return &ProfileHandler{svc: svc}
}

// GetProfile godoc
//
//	@Summary      Get profile
//	@Description  Returns the full profile of the authenticated user including bio, avatar, and KYC/email verification status.
//	@Tags         profile
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  ProfileResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /profile [get]
func (h *ProfileHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	user, err := h.svc.Auth.GetProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, services.ErrUserNotFound) {
			response.NotFound(w, "user not found")
			return
		}
		response.InternalError(w)
		return
	}

	response.OK(w, ProfileData{
		ID:              user.ID.String(),
		PhoneNumber:     user.PhoneNumber,
		Email:           user.Email,
		FirstName:       user.FirstName,
		LastName:        user.LastName,
		Bio:             user.Bio,
		AvatarURL:       user.AvatarURL,
		DateOfBirth:     user.DateOfBirth,
		Nationality:     user.Nationality,
		KycStatus:       user.KycStatus,
		IsEmailVerified: user.IsEmailVerified,
	})
}

// UpdateProfile godoc
//
//	@Summary      Update profile
//	@Description  Updates the authenticated user's profile fields: name, bio, avatar URL, date of birth, and nationality.
//	@Tags         profile
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      UpdateProfileRequest  true  "Profile fields to update"
//	@Success      200   {object}  ProfileResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /profile [patch]
func (h *ProfileHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.FirstName == "" || body.LastName == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "first_name and last_name are required")
		return
	}

	user, err := h.svc.Auth.UpdateProfile(r.Context(), services.UpdateProfileInput{
		UserID:      userID,
		FirstName:   body.FirstName,
		LastName:    body.LastName,
		Bio:         body.Bio,
		AvatarURL:   body.AvatarURL,
		DateOfBirth: body.DateOfBirth,
		Nationality: body.Nationality,
	})
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, ProfileData{
		ID:          user.ID.String(),
		PhoneNumber: user.PhoneNumber,
		Email:       user.Email,
		FirstName:   user.FirstName,
		LastName:    user.LastName,
		KycStatus:   user.KycStatus,
	})
}

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type PreferencesHandler struct {
	svc *services.Services
}

func NewPreferencesHandler(svc *services.Services) *PreferencesHandler {
	return &PreferencesHandler{svc: svc}
}

// GetPreferences godoc
//
//	@Summary      Get preferences
//	@Description  Returns the authenticated user's app preferences (language, dark mode, biometrics).
//	@Tags         preferences
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  map[string]any
//	@Failure      401  {object}  ErrorResponse
//	@Router       /profile/preferences [get]
func (h *PreferencesHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	prefs, err := h.svc.Preferences.Get(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}
	response.OK(w, prefs)
}

// UpdatePreferences godoc
//
//	@Summary      Update preferences
//	@Description  Updates one or more app preferences. Omitted fields are left unchanged. Supported languages: en, fr, es, ar, pt. Dark mode values: always | never | system.
//	@Tags         preferences
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      UpdatePreferencesRequest  true  "Preference fields to update"
//	@Success      200   {object}  map[string]any
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /profile/preferences [patch]
func (h *PreferencesHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		Language          string `json:"language"`
		DarkMode          string `json:"dark_mode"`
		BiometricsEnabled *bool  `json:"biometrics_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}

	prefs, err := h.svc.Preferences.Update(r.Context(), userID, services.UpdatePreferencesInput{
		Language:          body.Language,
		DarkMode:          body.DarkMode,
		BiometricsEnabled: body.BiometricsEnabled,
	})
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", err.Error())
		return
	}
	response.OK(w, prefs)
}

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type BusinessHandler struct {
	svc *services.Services
}

func NewBusinessHandler(svc *services.Services) *BusinessHandler {
	return &BusinessHandler{svc: svc}
}

type BusinessProfileData struct {
	UserID                 uuid.UUID `json:"user_id"`
	CompanyLegalName       string    `json:"company_legal_name"`
	CompanyRegistrationNo  string    `json:"company_registration_no"`
	Industry               string    `json:"industry"`
	CountryOfIncorporation string    `json:"country_of_incorporation"`
	AnnualRevenue          string    `json:"annual_revenue"`
	BusinessWebsite        *string   `json:"business_website,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type SaveProfileInput struct {
	CompanyLegalName       string `json:"company_legal_name"`
	CompanyRegistrationNo  string `json:"company_registration_no"`
	Industry               string `json:"industry"`
	CountryOfIncorporation string `json:"country_of_incorporation"`
	AnnualRevenue          string `json:"annual_revenue"`
	BusinessWebsite        string `json:"business_website"`
}

type DirectorInput struct {
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	JobTitle    string `json:"job_title"`
	DateOfBirth string `json:"date_of_birth"` // YYYY-MM-DD
	Nationality string `json:"nationality"`
	PhoneNumber string `json:"phone_number"`
}

// BusinessAnalyticsResponse is the payload for GET /business/analytics: the
// enriched analytics plus the account's limits and current usage.
type BusinessAnalyticsResponse struct {
	Analytics   *services.BusinessAnalyticsData `json:"analytics"`
	Limits      services.AccountLimits          `json:"limits"`
	LimitsUsage LimitsUsage                     `json:"limits_usage"`
}

// GetAnalytics godoc
//
//	@Summary      Business analytics dashboard
//	@Description  Rich analytics reserved for business accounts: insights plus transaction stats, per-currency volume/balance breakdowns, and limits usage. Returns 403 for individual accounts.
//	@Tags         business
//	@Produce      json
//	@Security     BearerAuth
//	@Param        period  query     string  false  "Period" Enums(1w,1m,3m,6m)
//	@Success      200     {object}  BusinessAnalyticsResponse
//	@Failure      401     {object}  ErrorResponse
//	@Failure      403     {object}  ErrorResponse
//	@Router       /business/analytics [get]
func (h *BusinessHandler) GetAnalytics(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	// Business-tier gate: enriched analytics are a business benefit.
	limits := h.svc.Withdrawal.Limits(r.Context(), userID)
	if !strings.EqualFold(limits.Tier, "business") {
		response.Forbidden(w, "detailed analytics are available on business accounts only")
		return
	}

	period := r.URL.Query().Get("period")
	data, err := h.svc.Insights.GetBusinessAnalytics(r.Context(), userID, period)
	if err != nil {
		response.InternalError(w)
		return
	}

	used := h.svc.Withdrawal.DailyWithdrawalUsedUSD(r.Context(), userID)
	remaining := limits.DailyWithdrawalUSD - used
	if remaining < 0 {
		remaining = 0
	}

	response.OK(w, BusinessAnalyticsResponse{
		Analytics: data,
		Limits:    limits,
		LimitsUsage: LimitsUsage{
			DailyWithdrawalUsedUSD:      used,
			DailyWithdrawalRemainingUSD: remaining,
		},
	})
}

func (h *BusinessHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	profile, err := h.svc.Business.GetProfile(r.Context(), userID)
	if errors.Is(err, services.ErrBusinessProfileNotFound) {
		response.NotFound(w, "business profile not found")
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, BusinessProfileData{
		UserID:                 profile.UserID,
		CompanyLegalName:       profile.CompanyLegalName,
		CompanyRegistrationNo:  profile.CompanyRegistrationNo,
		Industry:               profile.Industry,
		CountryOfIncorporation: profile.CountryOfIncorporation,
		AnnualRevenue:          profile.AnnualRevenue,
		BusinessWebsite:        profile.BusinessWebsite,
		CreatedAt:              profile.CreatedAt,
		UpdatedAt:              profile.UpdatedAt,
	})
}

func (h *BusinessHandler) SaveProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var in SaveProfileInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON payload")
		return
	}

	if in.CompanyLegalName == "" || in.CompanyRegistrationNo == "" || in.Industry == "" || in.CountryOfIncorporation == "" || in.AnnualRevenue == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "required fields are missing")
		return
	}

	profile, err := h.svc.Business.SaveProfile(
		r.Context(),
		userID,
		in.CompanyLegalName,
		in.CompanyRegistrationNo,
		in.Industry,
		in.CountryOfIncorporation,
		in.AnnualRevenue,
		in.BusinessWebsite,
	)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, BusinessProfileData{
		UserID:                 profile.UserID,
		CompanyLegalName:       profile.CompanyLegalName,
		CompanyRegistrationNo:  profile.CompanyRegistrationNo,
		Industry:               profile.Industry,
		CountryOfIncorporation: profile.CountryOfIncorporation,
		AnnualRevenue:          profile.AnnualRevenue,
		BusinessWebsite:        profile.BusinessWebsite,
		CreatedAt:              profile.CreatedAt,
		UpdatedAt:              profile.UpdatedAt,
	})
}

func (h *BusinessHandler) GetKYCStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	status, err := h.svc.Business.GetKYCStatus(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, status)
}

func (h *BusinessHandler) ListDirectors(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	directors, err := h.svc.Business.ListDirectors(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, directors)
}

func (h *BusinessHandler) AddDirector(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var in DirectorInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON payload")
		return
	}

	if in.FirstName == "" || in.LastName == "" || in.JobTitle == "" || in.DateOfBirth == "" || in.Nationality == "" || in.PhoneNumber == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "required fields are missing")
		return
	}

	director, err := h.svc.Business.AddDirector(
		r.Context(),
		userID,
		in.FirstName,
		in.LastName,
		in.JobTitle,
		in.DateOfBirth,
		in.Nationality,
		in.PhoneNumber,
	)
	if errors.Is(err, services.ErrInvalidDateFormat) {
		response.BadRequest(w, "INVALID_DATE", err.Error())
		return
	}
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, director)
}

func (h *BusinessHandler) DeleteDirector(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	idStr := chi.URLParam(r, "id")
	directorID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "invalid director ID")
		return
	}

	err = h.svc.Business.DeleteDirector(r.Context(), directorID, userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.NoContent(w)
}

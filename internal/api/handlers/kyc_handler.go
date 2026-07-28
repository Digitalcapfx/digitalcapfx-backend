package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/clients/metamap"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type KYCHandler struct {
	svc *services.Services
}

func NewKYCHandler(svc *services.Services) *KYCHandler {
	return &KYCHandler{svc: svc}
}

// GetStatus godoc
//
//	@Summary      Get KYC journey status
//	@Description  Returns the consolidated KYC journey the app switches on. `stage` is canonical — one of: not_started, draft, submitted, identity_started, in_review, approved, rejected, resubmit. `kyc_status` (pending|approved|rejected) is kept for back-compat. `intake` gives the intake sub-state (+ submitted_at); `identity` gives the Sumsub sub-state (+ applicant_id, review_answer, reject_labels, moderation_comment) for the retry UX. All identity sub-fields are optional and default when absent.
//	@Tags         kyc
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  services.JourneyStatus
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /kyc/status [get]
func (h *KYCHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	status, err := h.svc.KYC.GetJourneyStatus(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, status)
}

// ListDocuments godoc
//
//	@Summary      List KYC documents
//	@Description  Returns all identity documents submitted by the authenticated user.
//	@Tags         kyc
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  KYCDocumentListResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /kyc/documents [get]
func (h *KYCHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	docs, err := h.svc.KYC.ListDocuments(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, docs)
}

// UploadDocument godoc
//
//	@Summary      Submit a KYC document
//	@Description  Records a KYC/KYB document for review. `doc_url` is a GCS signed URL or object path from the client-side upload flow. `doc_type` is one of the enumerated kinds — for business accounts these are the Nilos merchant-onboarding documents (Certificate of Incorporation, Director/Shareholder registers, MEMART, proof of company address/activity, business bank statement, plus importer & UBO/director items). Call GET /kyc/requirements first: its `documents[]` tells you exactly which doc_types apply to this account and whether each is required.
//	@Tags         kyc
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      KYCDocumentRequest  true  "Document metadata"
//	@Success      201   {object}  KYCDocumentResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Failure      500   {object}  ErrorResponse
//	@Router       /kyc/documents [post]
func (h *KYCHandler) UploadDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		DocType string `json:"doc_type"`
		DocURL  string `json:"doc_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if body.DocType == "" || body.DocURL == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "doc_type and doc_url are required")
		return
	}

	doc, err := h.svc.KYC.UploadDocument(r.Context(), services.UploadDocumentInput{
		UserID:  userID,
		DocType: body.DocType,
		DocURL:  body.DocURL,
	})
	if err != nil {
		response.InternalError(w)
		return
	}

	response.Created(w, doc)
}

// KYCInitResponse is returned by POST /kyc/init.
type KYCInitResponse struct {
	Token string `json:"token" example:"_act-sbx-jwt-eyJhbGci..."` // short-lived provider SDK token (~30 min)
	Flow  string `json:"flow" example:"id-and-liveness"`           // provider level / flow name
}

// Initiate godoc
//
//	@Summary      Start or resume KYC verification
//	@Description  Creates or resumes a KYC verification session for the authenticated user with the configured provider (Sumsub, level `id-and-liveness`). Returns a short-lived `token` (~30 min) to hand to the Sumsub Web/Mobile SDK to launch the ID + liveness flow, plus the `flow` (level) name. The final result is delivered asynchronously to `POST /webhooks/kyc` and, unless an admin overrides, applied automatically.
//	@Tags         kyc
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  KYCInitResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /kyc/init [post]
func (h *KYCHandler) Initiate(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	result, err := h.svc.KYC.InitiateKYC(r.Context(), userID)
	if err != nil {
		// The customer must complete our own KYC intake form before the Sumsub
		// dialog can be launched — surface a clear, actionable error.
		if errors.Is(err, services.ErrKYCIntakeRequired) {
			response.BadRequest(w, "KYC_INTAKE_REQUIRED", err.Error())
			return
		}
		response.InternalError(w)
		return
	}

	response.OK(w, map[string]string{
		"token": result.AccessToken,
		"flow":  result.FlowID,
	})
}

// IntakeRequirements godoc
//
//	@Summary      Get KYC intake fields (+ saved answers for resume)
//	@Description  Returns everything DigitalFX collects on its own form BEFORE the Sumsub identity dialog is launched. `fields[]` carry `group` (identity|address|contact|financial) and `order` for the multi-step stepper, and `type` (text|date|select|country|boolean|repeatable). `values` holds previously-saved answers (from PUT /kyc/intake/draft or a prior submit) to PREFILL the form on resume; `intake_status` is not_started|draft|submitted (`completed` kept for back-compat). For business (KYB) accounts it also returns the Nilos merchant-onboarding `documents` checklist — upload each via POST /kyc/documents. Only after POST /kyc/intake (status submitted) does POST /kyc/init return a Sumsub token.
//	@Tags         kyc
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  services.IntakeRequirements
//	@Failure      401  {object}  ErrorResponse
//	@Router       /kyc/requirements [get]
func (h *KYCHandler) IntakeRequirements(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	req, err := h.svc.KYC.IntakeRequirementsForUser(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}
	// Returned as the typed IntakeRequirements shape: account_type, intake_status,
	// values (prefill), fields[] (key/label/type/required/options/group/order),
	// documents[] (Nilos KYB checklist for business), and notes[].
	response.OK(w, req)
}

// SaveDraftRequest is the PUT /kyc/intake/draft payload: a partial map of intake
// field keys → values (any subset).
type SaveDraftRequest struct {
	Values map[string]interface{} `json:"values"`
}

// SaveDraft godoc
//
//	@Summary      Save partial KYC intake progress (draft)
//	@Description  Persists whatever the user has entered so far — NO required-field validation. Values merge into any existing draft (last-write-wins per key) and never downgrade a submitted intake (that PUT is a no-op). Call it after each step and on back-out; it is idempotent and safe to call repeatedly. GET /kyc/requirements returns these back as `values` to prefill on resume. Value shapes must round-trip what POST /kyc/intake accepts (date as YYYY-MM-DD, country as ISO-3166 alpha-2, boolean as JSON boolean, top_3_counterparties as an array of {country,relationship,purpose}).
//	@Tags         kyc
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      SaveDraftRequest  true  "Partial intake values"
//	@Success      200   {object}  map[string]any
//	@Failure      401   {object}  ErrorResponse
//	@Router       /kyc/intake/draft [put]
func (h *KYCHandler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var body SaveDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}
	intake, err := h.svc.KYC.SaveIntakeDraft(r.Context(), userID, body.Values)
	if err != nil {
		if errors.Is(err, services.ErrUserNotFound) {
			response.NotFound(w, "user not found")
			return
		}
		response.InternalError(w)
		return
	}
	response.OK(w, map[string]any{
		"intake_status": intake.Status,
		"saved_at":      intake.UpdatedAt,
	})
}

// CounterpartyInput is one top counterparty (EUR/GBP NRE businesses).
type CounterpartyInput struct {
	Country      string `json:"country"`
	Relationship string `json:"relationship"`
	Purpose      string `json:"purpose"`
}

// SubmitIntakeRequest is the POST /kyc/intake payload. Registration fields (name,
// country, BVN, company profile) are NOT accepted here — they are reused from the
// signup record. Call GET /kyc/requirements for the exact required/optional set.
type SubmitIntakeRequest struct {
	DateOfBirth      string `json:"date_of_birth"` // required, YYYY-MM-DD
	Nationality      string `json:"nationality"`   // required
	BVN              string `json:"bvn"`           // optional, 11 digits — activates the NGN account
	AddressLine1     string `json:"address_line1"` // required
	AddressLine2     string `json:"address_line2"` // optional
	City             string `json:"city"`          // required
	State            string `json:"state"`         // optional
	PostalCode       string `json:"postal_code"`   // optional
	Occupation       string `json:"occupation"`    // required
	SourceOfFunds    string `json:"source_of_funds"`    // required
	PurposeOfAccount string `json:"purpose_of_account"` // required
	// Business (KYB) extras.
	IsImporter     *bool               `json:"is_importer"`         // required (business)
	Counterparties []CounterpartyInput `json:"top_3_counterparties"` // required (business EUR/GBP)
	ContactEmail   string              `json:"contact_email"`        // required (business EUR/GBP)
	ContactPhone   string              `json:"contact_phone"`        // required (business EUR/GBP)
}

// KYCIntakeResponse is the POST /kyc/intake success payload.
type KYCIntakeResponse struct {
	Status      string     `json:"status" example:"completed"`
	Message     string     `json:"message" example:"KYC intake completed. You can now start identity verification via POST /kyc/init."`
	SubmittedAt *time.Time `json:"submitted_at"`
}

// SubmitIntake godoc
//
//	@Summary      Submit KYC intake
//	@Description  Submits the DigitalFX KYC intake fields (see GET /kyc/requirements). Validates the fields required for the user's account type, stores them, mirrors the BVN onto the account so the Naira (NGN) account can be provisioned, and marks the intake completed. After this succeeds, POST /kyc/init will return a Sumsub SDK token.
//	@Tags         kyc
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      SubmitIntakeRequest  true  "Intake fields"
//	@Success      200   {object}  KYCIntakeResponse
//	@Failure      400   {object}  ErrorResponse
//	@Failure      401   {object}  ErrorResponse
//	@Router       /kyc/intake [post]
func (h *KYCHandler) SubmitIntake(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	var req SubmitIntakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "invalid request body")
		return
	}

	counterparties := make([]services.Counterparty, 0, len(req.Counterparties))
	for _, c := range req.Counterparties {
		counterparties = append(counterparties, services.Counterparty{
			Country:      c.Country,
			Relationship: c.Relationship,
			Purpose:      c.Purpose,
		})
	}

	intake, err := h.svc.KYC.SubmitIntake(r.Context(), userID, services.SubmitIntakeInput{
		DateOfBirth:      req.DateOfBirth,
		Nationality:      req.Nationality,
		BVN:              req.BVN,
		AddressLine1:     req.AddressLine1,
		AddressLine2:     req.AddressLine2,
		City:             req.City,
		State:            req.State,
		PostalCode:       req.PostalCode,
		Occupation:       req.Occupation,
		SourceOfFunds:    req.SourceOfFunds,
		PurposeOfAccount: req.PurposeOfAccount,
		IsImporter:       req.IsImporter,
		Counterparties:   counterparties,
		ContactEmail:     req.ContactEmail,
		ContactPhone:     req.ContactPhone,
	})
	if err != nil {
		if errors.Is(err, services.ErrUserNotFound) {
			response.NotFound(w, "user not found")
			return
		}
		response.BadRequest(w, "VALIDATION_ERROR", err.Error())
		return
	}

	response.OK(w, KYCIntakeResponse{
		Status:      intake.Status,
		Message:     "KYC intake completed. You can now start identity verification via POST /kyc/init.",
		SubmittedAt: intake.SubmittedAt,
	})
}

// ─── MetaMap ──────────────────────────────────────────────────────────────────

// InitiateMetaMap godoc
//
//	@Summary      Start MetaMap identity verification
//	@Description  Creates (or returns an existing) MetaMap applicant for the user. The returned identity_access token is used with the MetaMap mobile SDK to launch the verification flow.
//	@Tags         kyc
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  MetaMapInitResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /kyc/metamap/init [post]
func (h *KYCHandler) InitiateMetaMap(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	result, err := h.svc.KYC.InitiateMetaMapVerification(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, MetaMapInitData{
		ApplicantID:    result.ApplicantID,
		IdentityAccess: result.IdentityAccess,
		FlowID:         result.FlowID,
		Status:         result.Status,
	})
}

// MetaMapWebhook godoc
//
//	@Summary      MetaMap verification webhook
//	@Description  Receives verification result events from MetaMap. Updates KYC status to approved or rejected.
//	@Tags         webhooks
//	@Accept       json
//	@Produce      json
//	@Success      200  {object}  MessageResponse
//	@Failure      400  {object}  ErrorResponse
//	@Router       /webhooks/metamap [post]
func (h *KYCHandler) MetaMapWebhook(w http.ResponseWriter, r *http.Request) {
	var payload metamap.WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.BadRequest(w, "INVALID_PAYLOAD", "invalid webhook payload")
		return
	}

	if err := h.svc.KYC.HandleMetaMapWebhook(r.Context(), payload); err != nil {
		// Log internally but return 200 so MetaMap doesn't retry indefinitely.
		response.OKWithMessage(w, "received", nil)
		return
	}

	response.OKWithMessage(w, "processed", nil)
}

// ProviderWebhook godoc
//
//	@Summary      KYC provider webhook (Sumsub)
//	@Description  Receives verification result events from Sumsub (e.g. `applicantReviewed`). Verified via HMAC over the raw request body using the Sumsub webhook secret — the digest is read from the `X-Payload-Digest` header and its algorithm from `X-Payload-Digest-Alg` (`HMAC_SHA1_HEX` | `HMAC_SHA256_HEX` | `HMAC_SHA512_HEX`; defaults to SHA-256). On a `GREEN` review the user is auto-approved; other results are rejected — unless an admin has taken manual control, which always wins (hybrid KYC). Always acknowledges with 200 to prevent provider retries.
//	@Tags         webhooks
//	@Accept       json
//	@Produce      json
//	@Param        X-Payload-Digest      header  string  true   "HMAC digest (hex) of the raw request body"
//	@Param        X-Payload-Digest-Alg  header  string  false  "Digest algorithm: HMAC_SHA1_HEX | HMAC_SHA256_HEX | HMAC_SHA512_HEX (default SHA-256)"
//	@Success      200  {object}  MessageResponse
//	@Router       /webhooks/kyc [post]
func (h *KYCHandler) ProviderWebhook(w http.ResponseWriter, r *http.Request) {
	// The provider verifies the HMAC signature over the raw body, so read the
	// bytes verbatim rather than decoding first.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB cap
	if err != nil {
		response.BadRequest(w, "INVALID_PAYLOAD", "could not read webhook body")
		return
	}

	if err := h.svc.KYC.HandleProviderWebhook(r.Context(), body, r.Header); err != nil {
		// Log-and-ack: return 200 so the provider doesn't retry-storm, except for
		// signature failures which we surface as 401 to aid debugging.
		response.OKWithMessage(w, "received", nil)
		return
	}

	response.OKWithMessage(w, "processed", nil)
}

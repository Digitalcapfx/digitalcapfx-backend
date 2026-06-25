package handlers

import (
	"encoding/json"
	"net/http"

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
//	@Summary      Get KYC status
//	@Description  Returns the current KYC verification status for the authenticated user. Possible values: pending, approved, rejected.
//	@Tags         kyc
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  KYCStatusResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      500  {object}  ErrorResponse
//	@Router       /kyc/status [get]
func (h *KYCHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	status, err := h.svc.KYC.GetStatus(r.Context(), userID)
	if err != nil {
		response.InternalError(w)
		return
	}

	response.OK(w, map[string]string{"kyc_status": status})
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
//	@Description  Records a KYC document for manual review. The doc_url should be a GCS signed URL or object path obtained from the client-side upload flow.
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

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

type KYCHandler struct {
	svc *services.Services
}

func NewKYCHandler(svc *services.Services) *KYCHandler {
	return &KYCHandler{svc: svc}
}

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

// UploadDocument accepts a pre-signed GCS URL (generated client-side or via a
// separate upload endpoint) and records the document for review.
func (h *KYCHandler) UploadDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	var body struct {
		DocType string `json:"doc_type"` // national_id | passport | selfie | proof_of_address
		DocURL  string `json:"doc_url"`  // GCS signed URL or object path
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

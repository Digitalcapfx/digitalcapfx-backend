package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/rachfinance/digitalfx/internal/api/middleware"
	"github.com/rachfinance/digitalfx/internal/pkg/response"
	"github.com/rachfinance/digitalfx/internal/services"
)

// UploadHandler stores user files (KYC documents, avatars, …) in cloud storage.
type UploadHandler struct {
	svc *services.Services
}

// NewUploadHandler creates a new upload handler.
func NewUploadHandler(svc *services.Services) *UploadHandler {
	return &UploadHandler{svc: svc}
}

// maxUploadBytes caps a single upload at 15 MB (KYC photos, avatars).
const maxUploadBytes = 15 << 20

// Upload godoc
//
//	@Summary      Upload a file
//	@Description  Multipart upload of a KYC document, avatar, or other file. Returns the stored object path and a time-limited signed URL to read it. Pass the object or read_url to /kyc/documents, PATCH /profile, etc.
//	@Tags         uploads
//	@Accept       mpfd
//	@Produce      json
//	@Security     BearerAuth
//	@Param        file     formData  file    true   "The file to upload (max 15 MB)"
//	@Param        purpose  formData  string  false  "kyc | avatar | document | business | misc"
//	@Success      201  {object}  services.UploadResult
//	@Failure      400  {object}  ErrorResponse
//	@Failure      401  {object}  ErrorResponse
//	@Failure      413  {object}  ErrorResponse
//	@Router       /uploads [post]
func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		response.JSON(w, http.StatusRequestEntityTooLarge, response.Envelope{
			Success: false,
			Error:   &response.Error{Code: "FILE_TOO_LARGE", Message: "file exceeds the 15 MB limit or the form is invalid"},
		})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		response.BadRequest(w, "VALIDATION_ERROR", "a multipart 'file' field is required")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	purpose := r.FormValue("purpose")

	res, err := h.svc.Upload.Upload(r.Context(), userID, purpose, header.Filename, contentType, file)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrUploadsDisabled):
			response.ServiceUnavailable(w, "uploads are not available on this environment")
		default:
			response.BadRequest(w, "UPLOAD_ERROR", err.Error())
		}
		return
	}
	response.Created(w, res)
}

// GetReadURL godoc
//
//	@Summary      Get a signed read URL
//	@Description  Returns a fresh time-limited URL to download a previously uploaded object.
//	@Tags         uploads
//	@Produce      json
//	@Security     BearerAuth
//	@Param        object  query     string  true   "Object path returned by POST /uploads"
//	@Param        ttl     query     int     false  "Lifetime in seconds (default 604800)"
//	@Success      200  {object}  map[string]string
//	@Failure      400  {object}  ErrorResponse
//	@Router       /uploads/read-url [get]
func (h *UploadHandler) GetReadURL(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	object := r.URL.Query().Get("object")
	if object == "" {
		response.BadRequest(w, "VALIDATION_ERROR", "object is required")
		return
	}
	ttl := 7 * 24 * time.Hour
	if s := r.URL.Query().Get("ttl"); s != "" {
		if secs, err := strconv.Atoi(s); err == nil && secs > 0 {
			ttl = time.Duration(secs) * time.Second
		}
	}
	url, err := h.svc.Upload.ReadURL(object, ttl)
	if err != nil {
		if errors.Is(err, services.ErrUploadsDisabled) {
			response.ServiceUnavailable(w, "uploads are not available on this environment")
			return
		}
		response.BadRequest(w, "UPLOAD_ERROR", err.Error())
		return
	}
	response.OK(w, map[string]string{"object": object, "read_url": url})
}

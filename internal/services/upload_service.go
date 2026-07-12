package services

import (
	"context"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/pkg/storage"
)

// ErrUploadsDisabled is returned when no uploads bucket is configured.
var ErrUploadsDisabled = fmt.Errorf("uploads are not configured on this environment")

// UploadService stores user files (KYC documents, avatars, …) in GCS and
// returns time-limited signed URLs to read them back.
type UploadService struct {
	store  *storage.GCS
	logger *zap.Logger
}

// NewUploadService opens the GCS client from config. When no bucket is set
// (e.g. local dev) it returns a service whose calls report ErrUploadsDisabled,
// so the rest of the app still boots.
func NewUploadService(cfg *config.Config, logger *zap.Logger) *UploadService {
	if cfg.GCP.UploadsBucket == "" {
		logger.Warn("uploads disabled: no UPLOADS_BUCKET / KYC_BUCKET configured")
		return &UploadService{logger: logger}
	}
	store, err := storage.New(context.Background(), cfg.GCP.UploadsBucket, cfg.GCP.SignerSA)
	if err != nil {
		logger.Error("uploads disabled: failed to open GCS client", zap.Error(err))
		return &UploadService{logger: logger}
	}
	logger.Info("uploads enabled", zap.String("bucket", cfg.GCP.UploadsBucket))
	return &UploadService{store: store, logger: logger}
}

// UploadResult describes a stored object.
type UploadResult struct {
	Object   string `json:"object"`   // path within the bucket
	URI      string `json:"uri"`      // gs://bucket/object (stable reference)
	ReadURL  string `json:"read_url"` // signed HTTPS URL, time-limited
	Bucket   string `json:"bucket"`
	Purpose  string `json:"purpose"`
	MimeType string `json:"mime_type"`
}

var safeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// maxSignedTTL stays just under GCS's hard V4 limit of 604800s (7 days) to
// avoid the boundary being rejected once request latency/clock skew is added.
const maxSignedTTL = 7*24*time.Hour - 5*time.Minute

// validPurposes are the allowed logical buckets/prefixes for an upload.
var validPurposes = map[string]bool{
	"kyc": true, "avatar": true, "document": true, "business": true, "misc": true,
}

// Upload stores a file for a user under a purpose prefix and returns its
// object path plus a 7-day signed read URL.
func (s *UploadService) Upload(ctx context.Context, userID uuid.UUID, purpose, filename, contentType string, r io.Reader) (*UploadResult, error) {
	if s.store == nil {
		return nil, ErrUploadsDisabled
	}
	purpose = strings.ToLower(strings.TrimSpace(purpose))
	if purpose == "" {
		purpose = "misc"
	}
	if !validPurposes[purpose] {
		return nil, fmt.Errorf("invalid purpose %q (allowed: kyc, avatar, document, business, misc)", purpose)
	}

	clean := safeName.ReplaceAllString(path.Base(filename), "_")
	if clean == "" || clean == "_" {
		clean = "file"
	}
	object := fmt.Sprintf("%s/%s/%s-%s", purpose, userID.String(), uuid.NewString(), clean)

	uri, err := s.store.Upload(ctx, object, r, contentType)
	if err != nil {
		return nil, err
	}

	res := &UploadResult{
		Object:   object,
		URI:      uri,
		Bucket:   s.store.Bucket(),
		Purpose:  purpose,
		MimeType: contentType,
	}
	// Best-effort signed URL — upload still succeeds if signing is unavailable.
	if url, err := s.store.SignedReadURL(object, maxSignedTTL); err == nil {
		res.ReadURL = url
	} else {
		s.logger.Warn("could not sign read url", zap.String("object", object), zap.Error(err))
	}
	return res, nil
}

// ReadURL returns a fresh signed read URL for an already-stored object.
func (s *UploadService) ReadURL(object string, ttl time.Duration) (string, error) {
	if s.store == nil {
		return "", ErrUploadsDisabled
	}
	if ttl <= 0 || ttl > maxSignedTTL {
		ttl = maxSignedTTL
	}
	return s.store.SignedReadURL(object, ttl)
}

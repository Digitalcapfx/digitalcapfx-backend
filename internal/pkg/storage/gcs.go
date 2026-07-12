// Package storage wraps Google Cloud Storage for user uploads (KYC documents,
// avatars, etc.). It authenticates via Application Default Credentials — on
// Cloud Run that is the runtime service account, so no key file is needed.
// V4 signed URLs are produced through the IAM SignBlob API (the runtime SA
// must hold roles/iam.serviceAccountTokenCreator on itself).
package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"cloud.google.com/go/storage"
)

// GCS is a thin, bucket-scoped storage client.
type GCS struct {
	client   *storage.Client
	bucket   string
	signerSA string
}

// New opens a GCS client for the given bucket. signerSA is the service-account
// email used to sign V4 URLs (leave empty to disable signed URLs).
func New(ctx context.Context, bucket, signerSA string) (*GCS, error) {
	if bucket == "" {
		return nil, fmt.Errorf("storage: no bucket configured")
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: new client: %w", err)
	}
	return &GCS{client: client, bucket: bucket, signerSA: signerSA}, nil
}

// Bucket returns the configured bucket name.
func (g *GCS) Bucket() string { return g.bucket }

// Upload writes an object and returns its gs:// URI. contentType may be empty.
func (g *GCS) Upload(ctx context.Context, object string, r io.Reader, contentType string) (string, error) {
	w := g.client.Bucket(g.bucket).Object(object).NewWriter(ctx)
	if contentType != "" {
		w.ContentType = contentType
	}
	if _, err := io.Copy(w, r); err != nil {
		_ = w.Close()
		return "", fmt.Errorf("storage: upload %s: %w", object, err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("storage: finalize %s: %w", object, err)
	}
	return fmt.Sprintf("gs://%s/%s", g.bucket, object), nil
}

// SignedReadURL returns a time-limited HTTPS URL to download a private object.
// Signing uses the runtime SA via IAM (no local key), so signerSA must be set.
func (g *GCS) SignedReadURL(object string, ttl time.Duration) (string, error) {
	if g.signerSA == "" {
		return "", fmt.Errorf("storage: no signer service account configured")
	}
	return g.client.Bucket(g.bucket).SignedURL(object, &storage.SignedURLOptions{
		Scheme:         storage.SigningSchemeV4,
		Method:         "GET",
		GoogleAccessID: g.signerSA,
		Expires:        time.Now().Add(ttl),
	})
}

// Close releases the underlying client.
func (g *GCS) Close() error {
	if g.client == nil {
		return nil
	}
	return g.client.Close()
}

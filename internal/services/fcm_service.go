package services

import (
	"context"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"go.uber.org/zap"
	"google.golang.org/api/option"
)

// FCMService wraps Firebase Cloud Messaging for mobile push notifications.
// It is safe to call on a nil receiver (push simply becomes a no-op), so callers
// don't need to guard every call — this keeps push optional in local/dev where
// Firebase credentials aren't configured.
type FCMService struct {
	client *messaging.Client
	logger *zap.Logger
}

// NewFCMService initializes the Firebase SDK.
//   - credentialsJSON non-empty → use that service-account key.
//   - credentialsJSON empty     → Application Default Credentials (works on
//     Cloud Run when the service account has the Firebase Messaging role).
//
// Returns an error if init fails; the caller may treat that as "push disabled".
func NewFCMService(ctx context.Context, credentialsJSON string, logger *zap.Logger) (*FCMService, error) {
	var (
		app *firebase.App
		err error
	)
	if credentialsJSON != "" {
		app, err = firebase.NewApp(ctx, nil, option.WithCredentialsJSON([]byte(credentialsJSON)))
	} else {
		app, err = firebase.NewApp(ctx, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("init firebase app: %w", err)
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("init firebase messaging: %w", err)
	}

	return &FCMService{client: client, logger: logger}, nil
}

// Enabled reports whether push is actually configured.
func (s *FCMService) Enabled() bool {
	return s != nil && s.client != nil
}

// SendMulticast pushes a notification to many device tokens and returns the
// tokens FCM reported as permanently invalid (unregistered / malformed) so the
// caller can prune them. Batches in groups of 500 (the FCM limit).
func (s *FCMService) SendMulticast(ctx context.Context, tokens []string, title, body string, data map[string]string) (invalid []string, err error) {
	if !s.Enabled() || len(tokens) == 0 {
		return nil, nil
	}

	const batchSize = 500
	for i := 0; i < len(tokens); i += batchSize {
		end := i + batchSize
		if end > len(tokens) {
			end = len(tokens)
		}
		batch := tokens[i:end]

		resp, sendErr := s.client.SendEachForMulticast(ctx, &messaging.MulticastMessage{
			Tokens:       batch,
			Notification: &messaging.Notification{Title: title, Body: body},
			Data:         data,
		})
		if sendErr != nil {
			err = sendErr
			continue
		}
		for idx, r := range resp.Responses {
			if r.Success {
				continue
			}
			if messaging.IsUnregistered(r.Error) || messaging.IsInvalidArgument(r.Error) {
				invalid = append(invalid, batch[idx])
			}
		}
	}
	return invalid, err
}

// SendToToken pushes to a single device token (used for test pings).
func (s *FCMService) SendToToken(ctx context.Context, token, title, body string, data map[string]string) error {
	if !s.Enabled() {
		return fmt.Errorf("push notifications are not configured")
	}
	_, err := s.client.Send(ctx, &messaging.Message{
		Token:        token,
		Notification: &messaging.Notification{Title: title, Body: body},
		Data:         data,
	})
	return err
}

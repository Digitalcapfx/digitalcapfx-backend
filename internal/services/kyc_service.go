package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/metamap"
	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	"github.com/rachfinance/digitalfx/internal/config"
	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
	"github.com/rachfinance/digitalfx/internal/kyc"
	"github.com/rachfinance/digitalfx/internal/pkg/email"
)

type KYCService struct {
	pool        *pgxpool.Pool
	cfg         *config.Config
	logger      *zap.Logger
	provider    kyc.KYCProvider
	emailClient *email.Client
	notif       *NotificationService
	nilosClient *nilos.Client
}

func NewKYCService(pool *pgxpool.Pool, cfg *config.Config, logger *zap.Logger, provider kyc.KYCProvider, emailClient *email.Client, notif *NotificationService, nilosClient *nilos.Client) *KYCService {
	return &KYCService{pool: pool, cfg: cfg, logger: logger, provider: provider, emailClient: emailClient, notif: notif, nilosClient: nilosClient}
}

func (s *KYCService) GetStatus(ctx context.Context, userID uuid.UUID) (string, error) {
	q := db.New(s.pool)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return "", ErrUserNotFound
	}
	return user.KycStatus, nil
}

func (s *KYCService) ListDocuments(ctx context.Context, userID uuid.UUID) ([]db.KycDocument, error) {
	q := db.New(s.pool)
	return q.GetKYCDocumentsByUserID(ctx, userID)
}

type UploadDocumentInput struct {
	UserID  uuid.UUID
	DocType string
	DocURL  string
}

func (s *KYCService) UploadDocument(ctx context.Context, in UploadDocumentInput) (*db.KycDocument, error) {
	q := db.New(s.pool)

	doc, err := q.CreateKYCDocument(ctx, db.CreateKYCDocumentParams{
		UserID:  in.UserID,
		DocType: in.DocType,
		DocURL:  in.DocURL,
	})
	if err != nil {
		return nil, fmt.Errorf("create kyc document: %w", err)
	}

	if _, err := q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{
		ID:        in.UserID,
		KycStatus: "submitted",
	}); err != nil {
		s.logger.Error("update kyc status", zap.Error(err))
	}

	return &doc, nil
}

// ─── MetaMap ──────────────────────────────────────────────────────────────────

type MetaMapVerificationResult struct {
	ApplicantID    string `json:"applicant_id"`
	IdentityAccess string `json:"identity_access"` // SDK token for the mobile client
	FlowID         string `json:"flow_id"`
	Status         string `json:"status"`
}

// InitiateMetaMapVerification creates or returns an existing MetaMap applicant
// for the user. The mobile client uses the returned identity_access token with
// the MetaMap SDK to launch the verification flow.
func (s *KYCService) InitiateMetaMapVerification(ctx context.Context, userID uuid.UUID) (*MetaMapVerificationResult, error) {
	q := db.New(s.pool)

	// Return existing record if already initiated.
	existing, err := q.GetMetamapVerificationByUserID(ctx, userID)
	if err == nil {
		return &MetaMapVerificationResult{
			ApplicantID:    existing.ApplicantID,
			IdentityAccess: existing.IdentityAccess,
			FlowID:         existing.FlowID,
			Status:         existing.Status,
		}, nil
	}

	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	emailStr := ""
	if user.Email != nil {
		emailStr = *user.Email
	}

	session, err := s.provider.Initiate(ctx, userID.String(), user.PhoneNumber, emailStr)
	if err != nil {
		return nil, fmt.Errorf("kyc %s initiate: %w", s.provider.Name(), err)
	}

	record, err := q.CreateMetamapVerification(ctx, db.CreateMetamapVerificationParams{
		UserID:         userID,
		ApplicantID:    session.ExternalID,
		FlowID:         session.FlowID,
		IdentityAccess: session.AccessToken,
	})
	if err != nil {
		s.logger.Error("store metamap verification", zap.Error(err))
	}

	return &MetaMapVerificationResult{
		ApplicantID:    record.ApplicantID,
		IdentityAccess: record.IdentityAccess,
		FlowID:         record.FlowID,
		Status:         record.Status,
	}, nil
}

// HandleMetaMapWebhook processes a verification result pushed by MetaMap.
// It updates the local status and, if approved, sets the user's KYC status to "approved".
func (s *KYCService) HandleMetaMapWebhook(ctx context.Context, payload metamap.WebhookPayload) error {
	q := db.New(s.pool)

	applicantID := metamap.ApplicantIDFromResource(payload.Resource)
	if applicantID == "" {
		return fmt.Errorf("metamap webhook: empty applicant id in resource %q", payload.Resource)
	}

	verification, err := q.GetMetamapVerificationByApplicantID(ctx, applicantID)
	if err != nil {
		return fmt.Errorf("metamap verification not found for applicant %s", applicantID)
	}

	// Map MetaMap eventName to our internal status.
	status := mapMetaMapEvent(payload.EventName)

	resultJSON, _ := json.Marshal(payload.Status)
	updated, err := q.UpdateMetamapVerificationStatus(ctx, db.UpdateMetamapVerificationStatusParams{
		ApplicantID: applicantID,
		Status:      status,
		ResultData:  resultJSON,
	})
	if err != nil {
		s.logger.Error("update metamap status", zap.Error(err))
	}

	s.logger.Info("metamap webhook processed",
		zap.String("applicant_id", applicantID),
		zap.String("event", payload.EventName),
		zap.String("status", updated.Status),
	)

	if status == "under_review" {
		s.notif.Create(ctx, CreateNotificationInput{
			UserID: verification.UserID,
			Type:   NotifKYCSubmitted,
			Title:  "Identity Verification Submitted",
			Body:   "Your documents are under review. We'll notify you once a decision is made.",
		})
	}

	// Promote user KYC status when MetaMap approves.
	if status == "approved" {
		if _, err := q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{
			ID:        verification.UserID,
			KycStatus: "approved",
		}); err != nil {
			s.logger.Error("update user kyc status to approved", zap.Error(err))
		}
	}

	if status == "rejected" {
		if _, err := q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{
			ID:        verification.UserID,
			KycStatus: "rejected",
		}); err != nil {
			s.logger.Error("update user kyc status to rejected", zap.Error(err))
		}
	}

	return nil
}

func mapMetaMapEvent(eventName string) string {
	switch eventName {
	case "verification_completed", "step_completed":
		return "under_review" // awaits admin approval
	case "verification_rejected", "step_rejected":
		return "rejected"
	case "verification_started", "step_started":
		return "processing"
	default:
		return "pending"
	}
}

// ─── Hybrid provider webhook (Sumsub etc.) ────────────────────────────────────

// HandleProviderWebhook processes a webhook from the configured KYC provider
// (Sumsub in production). It always records the provider's automated decision in
// kyc_provider_status, but only promotes the user's final kyc_status when an
// admin has NOT taken manual control. This is what makes KYC hybrid: the
// provider can auto-approve, yet an admin's approve/reject always has the final
// say and is never silently reverted by a later webhook.
func (s *KYCService) HandleProviderWebhook(ctx context.Context, body []byte, headers http.Header) error {
	event, err := s.provider.HandleWebhook(ctx, body, headers)
	if err != nil {
		return err
	}
	userID, err := uuid.Parse(event.UserID)
	if err != nil {
		return fmt.Errorf("kyc webhook: invalid user id %q: %w", event.UserID, err)
	}

	q := db.New(s.pool)

	// Always record the provider's automated decision.
	providerStatus := event.Status
	if err := q.SetKycProviderStatus(ctx, db.SetKycProviderStatusParams{ID: userID, KycProviderStatus: &providerStatus}); err != nil {
		s.logger.Error("record kyc provider status", zap.Error(err))
	}

	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("kyc webhook: user %s not found", userID)
	}

	// Admin manual override wins — record the provider's opinion but do not
	// change the final decision.
	if user.KycManualOverride {
		s.logger.Info("kyc provider webhook ignored — admin manual override active",
			zap.String("user_id", userID.String()),
			zap.String("provider_status", providerStatus),
			zap.String("final_status", user.KycStatus),
		)
		return nil
	}

	s.logger.Info("kyc provider webhook processed",
		zap.String("user_id", userID.String()),
		zap.String("provider", s.provider.Name()),
		zap.String("status", providerStatus),
	)

	switch providerStatus {
	case "approved":
		if _, err := q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{ID: userID, KycStatus: "approved"}); err != nil {
			return fmt.Errorf("promote kyc approved: %w", err)
		}
		s.notifyKYCApproved(ctx, user)
	case "rejected":
		if _, err := q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{ID: userID, KycStatus: "rejected"}); err != nil {
			return fmt.Errorf("set kyc rejected: %w", err)
		}
		s.notifyKYCRejected(ctx, user, "Your verification was not approved by our identity provider.")
	case "under_review", "processing":
		if _, err := q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{ID: userID, KycStatus: providerStatus}); err != nil {
			s.logger.Error("update kyc status", zap.Error(err))
		}
		if providerStatus == "under_review" {
			s.notif.Create(ctx, CreateNotificationInput{
				UserID: userID,
				Type:   NotifKYCSubmitted,
				Title:  "Identity Verification Submitted",
				Body:   "Your documents are under review. We'll notify you once a decision is made.",
			})
		}
	}
	return nil
}

// notifyKYCApproved sends the approval email + notification.
func (s *KYCService) notifyKYCApproved(ctx context.Context, user db.User) {
	if user.Email != nil {
		go func() {
			subj, html := email.KYCApproved(*user.Email, user.FirstName)
			if err := s.emailClient.Send(*user.Email, subj, html); err != nil {
				s.logger.Error("send kyc approved email", zap.Error(err))
			}
		}()
	}
	s.notif.Create(ctx, CreateNotificationInput{
		UserID: user.ID,
		Type:   NotifKYCApproved,
		Title:  "Identity Verified ✓",
		Body:   "Your identity has been verified. You now have full access to transfers, wallets, and crypto.",
	})
}

// notifyKYCRejected sends the rejection email + notification.
func (s *KYCService) notifyKYCRejected(ctx context.Context, user db.User, reason string) {
	if user.Email != nil {
		go func() {
			subj, html := email.KYCRejected(*user.Email, user.FirstName, reason)
			if err := s.emailClient.Send(*user.Email, subj, html); err != nil {
				s.logger.Error("send kyc rejected email", zap.Error(err))
			}
		}()
	}
	s.notif.Create(ctx, CreateNotificationInput{
		UserID:   user.ID,
		Type:     NotifKYCRejected,
		Title:    "Identity Verification Unsuccessful",
		Body:     fmt.Sprintf("Your verification was not approved: %s. Please resubmit your documents.", reason),
		Metadata: map[string]string{"reason": reason},
	})
}

// ─── Admin KYC Management ─────────────────────────────────────────────────────

func (s *KYCService) ListPendingKYC(ctx context.Context) ([]db.UserFull, error) {
	q := db.New(s.pool)
	return q.ListUsersAwaitingKYCReview(ctx)
}

// AdminApproveKYC sets kyc_status to "approved", logs the admin action, and
// notifies the user by email. Account financial features unlock immediately.
func (s *KYCService) AdminApproveKYC(ctx context.Context, userID, adminID uuid.UUID) error {
	q := db.New(s.pool)

	if _, err := q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{
		ID:        userID,
		KycStatus: "approved",
	}); err != nil {
		return fmt.Errorf("approve kyc: %w", err)
	}

	// Take manual control so a later provider webhook cannot revert this.
	if err := q.SetKycManualOverride(ctx, db.SetKycManualOverrideParams{ID: userID, KycManualOverride: true}); err != nil {
		s.logger.Error("set kyc manual override", zap.Error(err))
	}

	if _, err := q.RecordKycAdminAction(ctx, db.RecordKycAdminActionParams{
		UserID:  userID,
		AdminID: adminID,
		Action:  "approved",
	}); err != nil {
		s.logger.Error("record kyc admin action", zap.Error(err))
	}

	user, err := q.GetUserByID(ctx, userID)
	if err == nil && user.Email != nil {
		go func() {
			subj, html := email.KYCApproved(*user.Email, user.FirstName)
			if err := s.emailClient.Send(*user.Email, subj, html); err != nil {
				s.logger.Error("send kyc approved email", zap.Error(err))
			}
		}()
	}

	s.notif.Create(ctx, CreateNotificationInput{
		UserID: userID,
		Type:   NotifKYCApproved,
		Title:  "Identity Verified ✓",
		Body:   "Your identity has been verified. You now have full access to transfers, wallets, and crypto.",
	})

	s.logger.Info("kyc approved by admin", zap.String("user_id", userID.String()), zap.String("admin_id", adminID.String()))
	return nil
}

// AdminRejectKYC sets kyc_status to "rejected", logs the admin action, and
// notifies the user with the rejection reason.
func (s *KYCService) AdminRejectKYC(ctx context.Context, userID, adminID uuid.UUID, reason string) error {
	q := db.New(s.pool)

	if _, err := q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{
		ID:        userID,
		KycStatus: "rejected",
	}); err != nil {
		return fmt.Errorf("reject kyc: %w", err)
	}

	// Take manual control so a later provider webhook cannot revert this.
	if err := q.SetKycManualOverride(ctx, db.SetKycManualOverrideParams{ID: userID, KycManualOverride: true}); err != nil {
		s.logger.Error("set kyc manual override", zap.Error(err))
	}

	reasonPtr := &reason
	if _, err := q.RecordKycAdminAction(ctx, db.RecordKycAdminActionParams{
		UserID:  userID,
		AdminID: adminID,
		Action:  "rejected",
		Reason:  reasonPtr,
	}); err != nil {
		s.logger.Error("record kyc admin action", zap.Error(err))
	}

	user, err := q.GetUserByID(ctx, userID)
	if err == nil && user.Email != nil {
		go func() {
			subj, html := email.KYCRejected(*user.Email, user.FirstName, reason)
			if err := s.emailClient.Send(*user.Email, subj, html); err != nil {
				s.logger.Error("send kyc rejected email", zap.Error(err))
			}
		}()
	}

	s.notif.Create(ctx, CreateNotificationInput{
		UserID:   userID,
		Type:     NotifKYCRejected,
		Title:    "Identity Verification Unsuccessful",
		Body:     fmt.Sprintf("Your verification was not approved: %s. Please resubmit your documents.", reason),
		Metadata: map[string]string{"reason": reason},
	})

	s.logger.Info("kyc rejected by admin", zap.String("user_id", userID.String()), zap.String("reason", reason))
	return nil
}

// AdminReleaseKYCToProvider clears the manual-override flag, handing the KYC
// decision back to the automated provider. The next provider webhook will once
// again be allowed to set the final status.
func (s *KYCService) AdminReleaseKYCToProvider(ctx context.Context, userID, adminID uuid.UUID) error {
	q := db.New(s.pool)
	if err := q.SetKycManualOverride(ctx, db.SetKycManualOverrideParams{ID: userID, KycManualOverride: false}); err != nil {
		return fmt.Errorf("release kyc override: %w", err)
	}
	if _, err := q.RecordKycAdminAction(ctx, db.RecordKycAdminActionParams{
		UserID:  userID,
		AdminID: adminID,
		Action:  "released_to_provider",
	}); err != nil {
		s.logger.Error("record kyc admin action", zap.Error(err))
	}
	s.logger.Info("kyc control released to provider", zap.String("user_id", userID.String()), zap.String("admin_id", adminID.String()))
	return nil
}

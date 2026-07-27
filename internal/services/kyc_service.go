package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/clients/metamap"
	"github.com/rachfinance/digitalfx/internal/clients/nilos"
	"github.com/rachfinance/digitalfx/internal/clients/nomba"
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
	nombaClient *nomba.Client
}

func NewKYCService(pool *pgxpool.Pool, cfg *config.Config, logger *zap.Logger, provider kyc.KYCProvider, emailClient *email.Client, notif *NotificationService, nilosClient *nilos.Client, nombaClient *nomba.Client) *KYCService {
	return &KYCService{pool: pool, cfg: cfg, logger: logger, provider: provider, emailClient: emailClient, notif: notif, nilosClient: nilosClient, nombaClient: nombaClient}
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

// InitiateKYC generates an access token for the configured KYC provider (e.g. Sumsub).
// If the user previously started the process, providing the same user ID resumes it.
func (s *KYCService) InitiateKYC(ctx context.Context, userID uuid.UUID) (*kyc.VerificationSession, error) {
	q := db.New(s.pool)

	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Gate: our own KYC intake fields must be collected and completed BEFORE the
	// Sumsub SDK dialog is launched. This is what makes the frontend/mobile show
	// our form first and only then pop the Sumsub flow.
	if intake, ierr := q.GetKYCIntake(ctx, userID); ierr != nil || intake.Status != "completed" {
		return nil, ErrKYCIntakeRequired
	}

	emailStr := ""
	if user.Email != nil {
		emailStr = *user.Email
	}

	session, err := s.provider.Initiate(ctx, userID.String(), user.PhoneNumber, emailStr)
	if err != nil {
		return nil, fmt.Errorf("kyc %s initiate: %w", s.provider.Name(), err)
	}

	return session, nil
}

// ─── KYC Intake (collected before the Sumsub dialog) ──────────────────────────

// ErrKYCIntakeRequired is returned by InitiateKYC when the user has not yet
// completed our own KYC intake form. The frontend/mobile must collect the
// intake fields (GET /kyc/requirements → POST /kyc/intake) before the Sumsub
// SDK dialog is launched.
var ErrKYCIntakeRequired = errors.New("kyc intake required: complete the KYC form before starting identity verification")

// IntakeFieldSpec describes one field the frontend should render on the intake
// form. Options is populated for enum-like fields (e.g. source_of_funds).
type IntakeFieldSpec struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"` // text | date | select | country | boolean | counterparties
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
	Help     string   `json:"help,omitempty"`
}

// KYC document types. The business set mirrors the Nilos merchant-onboarding
// requirements — these documents are collected here and submitted to Nilos
// during account provisioning. Upload each via POST /kyc/documents with the
// matching doc_type.
const (
	DocCertificateOfIncorporation = "certificate_of_incorporation"
	DocDirectorRegister           = "director_register"
	DocShareholderRegister        = "shareholder_register"
	DocArticlesOfAssociation      = "articles_of_association"
	DocProofOfAddress             = "proof_of_address"
	DocProofOfCompanyActivity     = "proof_of_company_activity"
	DocBusinessBankStatement      = "business_bank_statement"
	DocProofOfImports             = "proof_of_imports"
	DocProofOfWealth              = "proof_of_wealth" // UBO/director
	DocIDDocument                 = "id_document"     // UBO/director
	DocIDVLiveness                = "idv_liveness"    // 1 director, via Sumsub
)

// DocumentSpec describes a document the customer must upload (via POST
// /kyc/documents). AppliesTo scopes conditional documents; Scope indicates
// whether it concerns the company or a UBO/director.
type DocumentSpec struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Required     bool   `json:"required"`
	AppliesTo    string `json:"applies_to"`             // all | importers | eur_gbp_nre
	Scope        string `json:"scope"`                  // company | director_ubo
	MaxAgeMonths int    `json:"max_age_months,omitempty"` // freshness requirement, if any
	Help         string `json:"help,omitempty"`
}

// IntakeOptions drives the conditional parts of the business (KYB) requirements.
type IntakeOptions struct {
	IsImporter bool // adds the Proof of Imports document
	NeedsNRE   bool // EUR/GBP non-resident-entity extras (counterparties, proof of wealth, contact)
}

// IntakeRequirements is the full, account-type-aware checklist the frontend
// renders: structured fields, document uploads, and notes.
type IntakeRequirements struct {
	AccountType string            `json:"account_type"`
	Completed   bool              `json:"completed"`
	Fields      []IntakeFieldSpec `json:"fields"`
	Documents   []DocumentSpec    `json:"documents,omitempty"`
	Notes       []string          `json:"notes,omitempty"`
}

// personIdentityFields are the identity + address + AML fields for an individual
// or a business's authorised representative.
func personIdentityFields() []IntakeFieldSpec {
	sourceOfFunds := []string{"Salary", "Business Income", "Savings", "Investment", "Inheritance", "Gift", "Other"}
	purpose := []string{"Personal Use", "Business Payments", "Remittance", "Savings", "Trading", "Other"}
	return []IntakeFieldSpec{
		{Key: "legal_first_name", Label: "Legal First Name", Type: "text", Required: true},
		{Key: "legal_last_name", Label: "Legal Last Name", Type: "text", Required: true},
		{Key: "date_of_birth", Label: "Date of Birth", Type: "date", Required: true, Help: "YYYY-MM-DD"},
		{Key: "nationality", Label: "Nationality", Type: "country", Required: true},
		{Key: "bvn", Label: "BVN (Bank Verification Number)", Type: "text", Required: false, Help: "Required to activate your Naira (NGN) account"},
		{Key: "address_line1", Label: "Address Line 1", Type: "text", Required: true},
		{Key: "address_line2", Label: "Address Line 2", Type: "text", Required: false},
		{Key: "city", Label: "City", Type: "text", Required: true},
		{Key: "state", Label: "State / Province", Type: "text", Required: false},
		{Key: "postal_code", Label: "Postal Code", Type: "text", Required: false},
		{Key: "country", Label: "Country of Residence", Type: "country", Required: true},
		{Key: "occupation", Label: "Occupation", Type: "text", Required: true},
		{Key: "source_of_funds", Label: "Source of Funds", Type: "select", Required: true, Options: sourceOfFunds},
		{Key: "purpose_of_account", Label: "Purpose of Account", Type: "select", Required: true, Options: purpose},
	}
}

// BuildIntakeRequirements returns the complete, account-type-aware intake
// checklist. Individuals complete identity fields (ID + liveness are handled by
// the Sumsub dialog afterwards). Businesses (KYB) additionally submit the Nilos
// merchant-onboarding document set, with extra items for importers and for
// EUR/GBP non-resident entities (NRE).
func BuildIntakeRequirements(accountType string, opts IntakeOptions) IntakeRequirements {
	if accountType != "business" {
		return IntakeRequirements{
			AccountType: accountType,
			Fields:      personIdentityFields(),
			Documents: []DocumentSpec{
				{Key: DocProofOfAddress, Label: "Proof of Address", Required: false, AppliesTo: "all", Scope: "director_ubo", MaxAgeMonths: 3, Help: "Utility bill or bank statement, ≤3 months old"},
			},
			Notes: []string{
				"Your ID document and liveness check are completed via the identity-verification dialog after this form.",
			},
		}
	}

	// ── Business (KYB) — authorised representative identity ──────────────────
	fields := personIdentityFields()
	for i := range fields {
		switch fields[i].Key {
		case "legal_first_name":
			fields[i].Label = "Representative First Name"
		case "legal_last_name":
			fields[i].Label = "Representative Last Name"
		case "address_line1":
			fields[i].Label = "Registered Business Address Line 1"
		case "occupation":
			fields[i].Label = "Role / Position"
		}
	}
	// Business context fields.
	fields = append(fields,
		IntakeFieldSpec{Key: "is_importer", Label: "Is your business an importer?", Type: "boolean", Required: true, Help: "Importers must additionally provide proof of imports"},
	)
	if opts.NeedsNRE {
		fields = append(fields,
			IntakeFieldSpec{Key: "top_3_counterparties", Label: "Top 3 Counterparties (inbound/outbound)", Type: "counterparties", Required: true, Help: "For each: country, business relationship, and purpose of payments (required for EUR/GBP accounts)"},
			IntakeFieldSpec{Key: "contact_email", Label: "Contact Email (UBO/Director)", Type: "text", Required: true},
			IntakeFieldSpec{Key: "contact_phone", Label: "Contact Phone (UBO/Director)", Type: "text", Required: true},
		)
	}

	// ── Company documents (Nilos merchant onboarding) ───────────────────────
	docs := []DocumentSpec{
		{Key: DocCertificateOfIncorporation, Label: "Certificate of Incorporation", Required: true, AppliesTo: "all", Scope: "company"},
		{Key: DocDirectorRegister, Label: "Director Register", Required: true, AppliesTo: "all", Scope: "company", MaxAgeMonths: 3, Help: "Dated within 3 months, signed by a director"},
		{Key: DocShareholderRegister, Label: "Shareholder Register", Required: true, AppliesTo: "all", Scope: "company", MaxAgeMonths: 3, Help: "Dated within 3 months, signed by a director"},
		{Key: DocArticlesOfAssociation, Label: "Articles of Association (MEMART)", Required: true, AppliesTo: "all", Scope: "company"},
		{Key: DocProofOfAddress, Label: "Proof of Company Address", Required: true, AppliesTo: "all", Scope: "company", MaxAgeMonths: 3, Help: "Utility bill or bank statement ≤3 months. No mobile bills, personal utilities, or virtual-office statements. For USD accounts, the office IP address can be sufficient."},
		{Key: DocProofOfCompanyActivity, Label: "Proof of Company Activity", Required: true, AppliesTo: "all", Scope: "company", Help: "Recent invoices issued to clients. If no activity yet, a contract with future clients is accepted."},
		{Key: DocBusinessBankStatement, Label: "Business Bank Statement (past 90 days)", Required: true, AppliesTo: "all", Scope: "company", MaxAgeMonths: 3},
		{Key: DocProofOfImports, Label: "Proof of Imports", Required: opts.IsImporter, AppliesTo: "importers", Scope: "company", Help: "Air Waybill / Bill of Lading / CMR + Proof of Delivery, Customs Declaration, Freight/Courier Invoice. Related supplier invoices may also be accepted."},
		// UBOs & directors
		{Key: DocIDDocument, Label: "Director/UBO ID Document", Required: true, AppliesTo: "all", Scope: "director_ubo", Help: "Passport, National ID, Residence permit, or Driver's license (for each natural-person shareholder)"},
		{Key: DocProofOfAddress + "_ubo", Label: "Director/UBO Proof of Address", Required: true, AppliesTo: "all", Scope: "director_ubo", MaxAgeMonths: 3, Help: "Utility bill or bank statement ≤3 months old"},
		{Key: DocIDVLiveness, Label: "Director Liveness Check (IDV)", Required: true, AppliesTo: "all", Scope: "director_ubo", Help: "Required for one director. Completed via the identity-verification dialog after this form."},
		{Key: DocProofOfWealth, Label: "Proof of Wealth (UBO/Director)", Required: opts.NeedsNRE, AppliesTo: "eur_gbp_nre", Scope: "director_ubo", Help: "Sale of asset, pay-slips, bank statements, or investment-account statements (required for EUR/GBP accounts)"},
	}

	notes := []string{
		"All documents must be dated within the specified timeframes (registers & proofs of address ≤3 months; bank statement ≤90 days). Alternatives are accepted where noted.",
		"Where a shareholder is itself a company, also provide that company's Shareholder Register and Articles of Association.",
		"Company identity fields (legal name, registration number, industry, country of incorporation) were captured at signup and are reused.",
	}
	if opts.IsImporter {
		notes = append(notes, "As an importer, Proof of Imports documentation is required.")
	}
	if opts.NeedsNRE {
		notes = append(notes, "EUR/GBP accounts (non-resident entity) require the top-3 counterparties, proof of wealth, and contact information.")
	}

	return IntakeRequirements{
		AccountType: "business",
		Fields:      fields,
		Documents:   docs,
		Notes:       notes,
	}
}

// GetIntakeRequirements returns just the structured field schema for the given
// account type (backward-compatible helper used by intake validation). Use
// BuildIntakeRequirements for the full document-aware checklist.
func GetIntakeRequirements(accountType string) []IntakeFieldSpec {
	return BuildIntakeRequirements(accountType, IntakeOptions{NeedsNRE: accountType == "business"}).Fields
}

// IntakeRequirementsForUser resolves the user's account type and returns the
// full, document-aware intake checklist to render, with `completed` set if the
// intake was already submitted. Business (KYB) accounts get the Nilos merchant
// document set; EUR/GBP NRE requirements are on by default (every customer is
// provisioned EUR/GBP), and the importer flag reflects any prior answer.
func (s *KYCService) IntakeRequirementsForUser(ctx context.Context, userID uuid.UUID) (IntakeRequirements, error) {
	q := db.New(s.pool)
	user, uerr := q.GetUserByID(ctx, userID)
	if uerr != nil {
		return IntakeRequirements{}, ErrUserNotFound
	}

	opts := IntakeOptions{NeedsNRE: user.AccountType == "business"}
	completed := false
	if intake, ierr := q.GetKYCIntake(ctx, userID); ierr == nil {
		completed = intake.Status == "completed"
		if intake.IsImporter != nil {
			opts.IsImporter = *intake.IsImporter
		}
	}

	req := BuildIntakeRequirements(user.AccountType, opts)
	req.Completed = completed
	return req, nil
}

// Counterparty is one of a business's top counterparties (EUR/GBP NRE).
type Counterparty struct {
	Country      string `json:"country"`
	Relationship string `json:"relationship"`
	Purpose      string `json:"purpose"`
}

// SubmitIntakeInput is the payload for POST /kyc/intake.
type SubmitIntakeInput struct {
	LegalFirstName   string
	LegalLastName    string
	DateOfBirth      string // YYYY-MM-DD
	Nationality      string
	BVN              string
	AddressLine1     string
	AddressLine2     string
	City             string
	State            string
	PostalCode       string
	Country          string
	Occupation       string
	SourceOfFunds    string
	PurposeOfAccount string

	// Business (KYB) extras.
	IsImporter     *bool
	Counterparties []Counterparty
	ContactEmail   string
	ContactPhone   string
}

// SubmitIntake validates the collected fields against the account-type
// requirements, persists them (marking the intake completed), and mirrors the
// BVN onto the user record so NGN (Nomba) provisioning can use it. Once this
// succeeds, InitiateKYC will issue the Sumsub token.
func (s *KYCService) SubmitIntake(ctx context.Context, userID uuid.UUID, in SubmitIntakeInput) (*db.KycIntake, error) {
	q := db.New(s.pool)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Validate required fields for the account type.
	values := map[string]string{
		"legal_first_name":   strings.TrimSpace(in.LegalFirstName),
		"legal_last_name":    strings.TrimSpace(in.LegalLastName),
		"date_of_birth":      strings.TrimSpace(in.DateOfBirth),
		"nationality":        strings.TrimSpace(in.Nationality),
		"bvn":                strings.TrimSpace(in.BVN),
		"address_line1":      strings.TrimSpace(in.AddressLine1),
		"address_line2":      strings.TrimSpace(in.AddressLine2),
		"city":               strings.TrimSpace(in.City),
		"state":              strings.TrimSpace(in.State),
		"postal_code":        strings.TrimSpace(in.PostalCode),
		"country":            strings.TrimSpace(in.Country),
		"occupation":         strings.TrimSpace(in.Occupation),
		"source_of_funds":    strings.TrimSpace(in.SourceOfFunds),
		"purpose_of_account": strings.TrimSpace(in.PurposeOfAccount),
		"contact_email":      strings.TrimSpace(in.ContactEmail),
		"contact_phone":      strings.TrimSpace(in.ContactPhone),
	}

	opts := IntakeOptions{NeedsNRE: user.AccountType == "business"}
	if in.IsImporter != nil {
		opts.IsImporter = *in.IsImporter
	}

	var missing []string
	for _, spec := range BuildIntakeRequirements(user.AccountType, opts).Fields {
		// boolean and counterparties are validated separately below.
		if spec.Type == "boolean" || spec.Type == "counterparties" {
			continue
		}
		if spec.Required && values[spec.Key] == "" {
			missing = append(missing, spec.Key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required KYC fields: %s", strings.Join(missing, ", "))
	}
	if bvn := values["bvn"]; bvn != "" && len(bvn) != 11 {
		return nil, fmt.Errorf("BVN must be exactly 11 digits")
	}
	// Business-specific validation: importer flag is mandatory; EUR/GBP (NRE)
	// businesses must list at least one counterparty.
	if user.AccountType == "business" {
		if in.IsImporter == nil {
			return nil, fmt.Errorf("missing required KYC fields: is_importer")
		}
		if opts.NeedsNRE && len(in.Counterparties) == 0 {
			return nil, fmt.Errorf("missing required KYC fields: top_3_counterparties")
		}
	}

	// Marshal counterparties to JSON for storage (nil when none supplied).
	var counterpartiesJSON []byte
	if len(in.Counterparties) > 0 {
		if b, mErr := json.Marshal(in.Counterparties); mErr == nil {
			counterpartiesJSON = b
		}
	}

	intake, err := q.UpsertKYCIntake(ctx, db.UpsertKYCIntakeParams{
		UserID:           userID,
		AccountType:      user.AccountType,
		LegalFirstName:   ptrOrNil(values["legal_first_name"]),
		LegalLastName:    ptrOrNil(values["legal_last_name"]),
		DateOfBirth:      ptrOrNil(values["date_of_birth"]),
		Nationality:      ptrOrNil(values["nationality"]),
		Bvn:              ptrOrNil(values["bvn"]),
		AddressLine1:     ptrOrNil(values["address_line1"]),
		AddressLine2:     ptrOrNil(values["address_line2"]),
		City:             ptrOrNil(values["city"]),
		State:            ptrOrNil(values["state"]),
		PostalCode:       ptrOrNil(values["postal_code"]),
		Country:          ptrOrNil(values["country"]),
		Occupation:       ptrOrNil(values["occupation"]),
		SourceOfFunds:    ptrOrNil(values["source_of_funds"]),
		PurposeOfAccount: ptrOrNil(values["purpose_of_account"]),
		IsImporter:       in.IsImporter,
		Counterparties:   counterpartiesJSON,
		ContactEmail:     ptrOrNil(values["contact_email"]),
		ContactPhone:     ptrOrNil(values["contact_phone"]),
	})
	if err != nil {
		return nil, fmt.Errorf("save kyc intake: %w", err)
	}

	// Mirror the BVN onto the user so NGN (Nomba) provisioning can attach it.
	if bvn := values["bvn"]; bvn != "" {
		if _, err := q.SetUserBVN(ctx, db.SetUserBVNParams{ID: userID, Bvn: &bvn}); err != nil {
			s.logger.Error("mirror bvn to user on intake", zap.Error(err))
		}
	}

	s.logger.Info("kyc intake completed", zap.String("user_id", userID.String()), zap.String("account_type", user.AccountType))
	return &intake, nil
}

// GetIntake returns the user's KYC intake record (or nil if none submitted yet).
func (s *KYCService) GetIntake(ctx context.Context, userID uuid.UUID) (*db.KycIntake, error) {
	q := db.New(s.pool)
	intake, err := q.GetKYCIntake(ctx, userID)
	if err != nil {
		return nil, nil
	}
	return &intake, nil
}

// ptrOrNil returns nil for an empty string, else a pointer to it — so empty
// optional fields are stored as SQL NULL.
func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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

	// Provision fiat accounts (Nilos EUR/GBP + Nomba NGN) now that KYC is approved.
	go s.provisionFiatAccounts(context.Background(), user)
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

	// Provision fiat accounts (Nilos EUR/GBP + Nomba NGN) now that KYC is approved.
	go s.provisionFiatAccounts(context.Background(), user)

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

// ─── Fiat Account Provisioning ────────────────────────────────────────────────

// provisionFiatAccounts attaches real bank details to the user's fiat accounts
// once KYC is approved, dispatching per currency to the right provider:
//   - EUR → Nilos SEPA (IBAN / BIC)
//   - GBP → Nilos FPS  (sort code / UK account number)
//   - NGN → Nomba virtual account (real NGN bank account number)
//
// It is idempotent per account: an account that already carries provider details
// is skipped. Every customer gets an NGN account regardless of their country, so
// this runs for all users.
func (s *KYCService) provisionFiatAccounts(ctx context.Context, user db.User) {
	q := db.New(s.pool)
	accounts, err := q.GetAccountsByUserID(ctx, user.ID)
	if err != nil {
		s.logger.Error("failed to get user accounts for fiat provisioning", zap.Error(err))
		return
	}

	for _, acc := range accounts {
		switch acc.Currency {
		case "EUR", "GBP":
			s.provisionNilosAccount(ctx, user, acc)
		case "NGN":
			s.provisionNombaNGN(ctx, user, acc)
		}
	}
}

// provisionNilosAccount provisions a Nilos SEPA/FPS virtual account for a single
// EUR or GBP account (idempotent — skips accounts already linked to Nilos).
func (s *KYCService) provisionNilosAccount(ctx context.Context, user db.User, acc db.Account) {
	if s.nilosClient == nil || acc.NilosAccountID != nil {
		return
	}

	var rail string
	switch acc.Currency {
	case "EUR":
		rail = nilos.RailSEPA
	case "GBP":
		rail = nilos.RailFPS
	default:
		return
	}

	q := db.New(s.pool)
	accountName := fmt.Sprintf("DigitalFX %s %s %s", user.FirstName, user.LastName, acc.Currency)
	nilosAcc, err := s.nilosClient.CreateAccount(ctx, nilos.CreateAccountRequest{Name: accountName, Rail: rail})
	if err != nil {
		s.logger.Error("failed to provision nilos account",
			zap.String("user_id", user.ID.String()), zap.String("currency", acc.Currency), zap.Error(err))
		return
	}

	var iban, bic, sortCode, accountNumberUK *string
	if rail == nilos.RailSEPA {
		if val := nilosAcc.DetailString("iban"); val != "" {
			iban = &val
		}
		if val := nilosAcc.DetailString("bic"); val != "" {
			bic = &val
		}
	} else if rail == nilos.RailFPS {
		if val := nilosAcc.DetailString("accountNumber"); val != "" {
			accountNumberUK = &val
		}
		if val := nilosAcc.DetailString("sortCode"); val != "" {
			sortCode = &val
		}
	}

	if err := q.UpdateNilosAccountDetails(ctx, db.UpdateNilosAccountDetailsParams{
		ID:              acc.ID,
		NilosAccountID:  &nilosAcc.ID,
		Iban:            iban,
		Bic:             bic,
		SortCode:        sortCode,
		AccountNumberUk: accountNumberUK,
	}); err != nil {
		s.logger.Error("failed to save nilos account details",
			zap.String("user_id", user.ID.String()), zap.String("currency", acc.Currency), zap.Error(err))
		return
	}
	s.logger.Info("provisioned nilos virtual account",
		zap.String("user_id", user.ID.String()), zap.String("currency", acc.Currency), zap.String("nilos_id", nilosAcc.ID))
}

// provisionNombaNGN provisions a real NGN virtual bank account via Nomba for the
// user's NGN account (idempotent — skips if already provisioned). BVN is attached
// when the user has one on file; it is optional at Nomba.
func (s *KYCService) provisionNombaNGN(ctx context.Context, user db.User, acc db.Account) {
	if s.nombaClient == nil || !s.nombaClient.Configured() {
		s.logger.Warn("nomba not configured, skipping NGN virtual account", zap.String("user_id", user.ID.String()))
		return
	}
	if acc.NombaAccountRef != nil {
		return // already provisioned
	}

	q := db.New(s.pool)

	// accountRef must be 16-64 chars and stable/unique per virtual account.
	accountRef := "digitalfx-ngn-" + user.ID.String()

	// accountName (holder name) must be 8-64 chars; pad with a prefix if the raw
	// name is too short for Nomba's minimum.
	accountName := strings.TrimSpace(fmt.Sprintf("%s %s", user.FirstName, user.LastName))
	if len(accountName) < 8 {
		accountName = "DigitalFX " + accountName
	}

	req := nomba.CreateVirtualAccountRequest{AccountRef: accountRef, AccountName: accountName}
	if user.Bvn != nil && *user.Bvn != "" {
		req.BVN = *user.Bvn
	}

	va, err := s.nombaClient.CreateVirtualAccount(ctx, req)
	if err != nil {
		s.logger.Error("failed to provision nomba NGN account",
			zap.String("user_id", user.ID.String()), zap.Error(err))
		return
	}

	ref := va.AccountRef
	if ref == "" {
		ref = accountRef
	}
	holderID := &va.AccountHolderID
	bankName := &va.BankName
	acctNo := &va.BankAccountNumber
	acctName := &va.BankAccountName
	if err := q.UpdateNombaAccountDetails(ctx, db.UpdateNombaAccountDetailsParams{
		ID:                     acc.ID,
		NombaAccountRef:        &ref,
		NombaAccountHolderID:   holderID,
		NombaBankName:          bankName,
		NombaBankAccountNumber: acctNo,
		NombaBankAccountName:   acctName,
	}); err != nil {
		s.logger.Error("failed to save nomba NGN account details",
			zap.String("user_id", user.ID.String()), zap.Error(err))
		return
	}
	s.logger.Info("provisioned nomba NGN virtual account",
		zap.String("user_id", user.ID.String()),
		zap.String("bank", va.BankName),
		zap.String("account_number", va.BankAccountNumber))
}

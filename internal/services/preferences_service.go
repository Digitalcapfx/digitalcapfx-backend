package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

var (
	validLanguages = map[string]bool{
		"en": true, "fr": true, "es": true, "ar": true, "pt": true,
	}
	validDarkModes = map[string]bool{
		"always": true, "never": true, "system": true,
	}
)

type PreferencesService struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewPreferencesService(pool *pgxpool.Pool, logger *zap.Logger) *PreferencesService {
	return &PreferencesService{pool: pool, logger: logger}
}

func (s *PreferencesService) Get(ctx context.Context, userID uuid.UUID) (*db.UserPreferences, error) {
	q := db.New(s.pool)
	prefs, err := q.GetUserPreferences(ctx, userID)
	if err != nil {
		// Return sensible defaults when the row doesn't exist yet.
		return &db.UserPreferences{
			UserID:            userID,
			Language:          "en",
			DarkMode:          "always",
			BiometricsEnabled: false,
		}, nil
	}
	return &prefs, nil
}

// UpdatePreferencesInput — any field may be omitted (zero value = don't change).
type UpdatePreferencesInput struct {
	Language          string // ISO 639-1 code
	DarkMode          string // "always" | "never" | "system"
	BiometricsEnabled *bool  // nil = don't change
}

func (s *PreferencesService) Update(ctx context.Context, userID uuid.UUID, in UpdatePreferencesInput) (*db.UserPreferences, error) {
	q := db.New(s.pool)

	cur, err := q.GetUserPreferences(ctx, userID)
	if err != nil {
		cur = db.UserPreferences{Language: "en", DarkMode: "always"}
	}

	if in.Language != "" {
		if !validLanguages[in.Language] {
			return nil, fmt.Errorf("unsupported language %q — supported: en, fr, es, ar, pt", in.Language)
		}
		cur.Language = in.Language
	}
	if in.DarkMode != "" {
		if !validDarkModes[in.DarkMode] {
			return nil, fmt.Errorf("invalid dark_mode %q — must be: always | never | system", in.DarkMode)
		}
		cur.DarkMode = in.DarkMode
	}
	if in.BiometricsEnabled != nil {
		cur.BiometricsEnabled = *in.BiometricsEnabled
	}

	prefs, err := q.UpsertUserPreferences(ctx, db.UpsertUserPreferencesParams{
		UserID:            userID,
		Language:          cur.Language,
		DarkMode:          cur.DarkMode,
		BiometricsEnabled: cur.BiometricsEnabled,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert preferences: %w", err)
	}
	return &prefs, nil
}

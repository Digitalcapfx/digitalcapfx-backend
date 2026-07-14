package token

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// pending2FASubject marks a short-lived token issued after a correct
// phone+PIN when 2FA is required. It stands in for a server-side "pending"
// record, so the 2FA login handoff needs no Redis/DB state.
const pending2FASubject = "2fa_pending"

// SignPending2FA issues a short-lived token carrying just the user id, accepted
// only by ParsePending2FA. Replaces the old Redis "2fa:pending:" ref.
func SignPending2FA(userID uuid.UUID, secret string, ttl time.Duration) (string, error) {
	return sign(userID, "", "", "", secret, ttl, pending2FASubject)
}

// ParsePending2FA validates a pending-2FA token (signature + expiry + subject)
// and returns the user id.
func ParsePending2FA(tokenStr, secret string) (uuid.UUID, error) {
	claims, err := Parse(tokenStr, secret)
	if err != nil {
		return uuid.Nil, err
	}
	if claims.Subject != pending2FASubject {
		return uuid.Nil, fmt.Errorf("token is not a pending-2FA token")
	}
	return uuid.Parse(claims.UserID)
}

type Claims struct {
	UserID    string `json:"user_id"`
	Phone     string `json:"phone"`
	SessionID string `json:"session_id,omitempty"`
	Role      string `json:"role,omitempty"` // "user" | "admin"
	jwt.RegisteredClaims
}

type Pair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	SessionID    string `json:"session_id"`
	// AccountType ("individual"|"business") is surfaced on auth responses so the
	// frontend can branch on tier without a separate profile fetch. Omitted when
	// not populated (e.g. refresh where it isn't looked up).
	AccountType string `json:"account_type,omitempty"`
}

func NewPair(userID uuid.UUID, phone, sessionID, role, secret string, accessExp, refreshExp time.Duration) (*Pair, error) {
	access, err := sign(userID, phone, sessionID, role, secret, accessExp, "access")
	if err != nil {
		return nil, err
	}
	refresh, err := sign(userID, phone, sessionID, role, secret, refreshExp, "refresh")
	if err != nil {
		return nil, err
	}
	return &Pair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(accessExp.Seconds()),
		SessionID:    sessionID,
	}, nil
}

func Parse(tokenStr, secret string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	return claims, err
}

func sign(userID uuid.UUID, phone, sessionID, role, secret string, exp time.Duration, subject string) (string, error) {
	claims := Claims{
		UserID:    userID.String(),
		Phone:     phone,
		SessionID: sessionID,
		Role:      role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(exp)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

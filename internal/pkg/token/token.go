package token

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

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

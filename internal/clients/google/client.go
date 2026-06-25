package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TokenInfo is the decoded payload returned by Google's tokeninfo endpoint.
type TokenInfo struct {
	Sub           string `json:"sub"`            // stable user ID
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"` // "true" | "false"
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Aud           string `json:"aud"` // must match our client ID
}

// VerifyIDToken calls Google's tokeninfo endpoint to validate the ID token and
// returns the decoded claims. It validates that the audience matches clientID.
func VerifyIDToken(ctx context.Context, idToken, clientID string) (*TokenInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://oauth2.googleapis.com/tokeninfo?id_token="+idToken, nil)
	if err != nil {
		return nil, fmt.Errorf("build google tokeninfo request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google tokeninfo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google tokeninfo: invalid token (status %d)", resp.StatusCode)
	}

	var info TokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode google tokeninfo: %w", err)
	}

	if clientID != "" && info.Aud != clientID {
		return nil, fmt.Errorf("google tokeninfo: audience mismatch (got %q, want %q)", info.Aud, clientID)
	}

	return &info, nil
}

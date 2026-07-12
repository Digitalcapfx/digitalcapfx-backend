package sumsub

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.sumsub.com"

type Client struct {
	appToken      string
	secretKey     string
	levelName     string
	webhookSecret string
	baseURL       string
	httpClient    *http.Client
}

func New(appToken, secretKey, levelName, webhookSecret string) *Client {
	return &Client{
		appToken:      appToken,
		secretKey:     secretKey,
		levelName:     levelName,
		webhookSecret: webhookSecret,
		baseURL:       defaultBaseURL,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) LevelName() string {
	return c.levelName
}

type AccessTokenResponse struct {
	Token  string `json:"token"`
	UserId string `json:"userId"`
}

type WebhookPayload struct {
	Type           string       `json:"type"`
	ApplicantId    string       `json:"applicantId"`
	ExternalUserId string       `json:"externalUserId"`
	ReviewResult   ReviewResult `json:"reviewResult"`
}

type ReviewResult struct {
	ReviewAnswer string   `json:"reviewAnswer"`
	RejectLabels []string `json:"rejectLabels,omitempty"`
}

func (c *Client) signRequest(req *http.Request, body []byte) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	method := req.Method
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}

	dataToSign := timestamp + method + path
	if len(body) > 0 {
		dataToSign += string(body)
	}

	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(dataToSign))
	signature := hex.EncodeToString(h.Sum(nil))

	req.Header.Set("X-App-Token", c.appToken)
	req.Header.Set("X-App-Access-Sig", signature)
	req.Header.Set("X-App-Access-Ts", timestamp)
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
}

func (c *Client) GenerateAccessToken(ctx context.Context, userID string, levelName string, ttlSeconds int) (*AccessTokenResponse, error) {
	q := url.Values{}
	q.Set("userId", userID)
	q.Set("levelName", levelName)
	q.Set("ttlInSecs", strconv.Itoa(ttlSeconds))
	endpoint := c.baseURL + "/resources/accessTokens?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}

	c.signRequest(req, nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sumsub: generate token status %d: %s", resp.StatusCode, string(b))
	}

	var out AccessTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VerifyWebhookSignature checks a Sumsub webhook's HMAC digest. Sumsub declares
// the algorithm per-webhook in the X-Payload-Digest-Alg header
// (HMAC_SHA1_HEX | HMAC_SHA256_HEX | HMAC_SHA512_HEX); alg may be empty, in
// which case SHA-256 (Sumsub's default) is used.
func (c *Client) VerifyWebhookSignature(signature, alg string, body []byte) bool {
	var newHash func() hash.Hash
	switch strings.ToUpper(strings.TrimSpace(alg)) {
	case "HMAC_SHA1_HEX":
		newHash = sha1.New
	case "HMAC_SHA512_HEX":
		newHash = sha512.New
	default: // HMAC_SHA256_HEX and unspecified
		newHash = sha256.New
	}
	mac := hmac.New(newHash, []byte(c.webhookSecret))
	mac.Write(body)
	expected := mac.Sum(nil)

	got, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false
	}
	return hmac.Equal(got, expected)
}

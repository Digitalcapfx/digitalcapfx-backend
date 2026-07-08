package sumsub

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	url := fmt.Sprintf("%s/resources/accessTokens?userId=%s&levelName=%s&ttlInSecs=%d", c.baseURL, userID, levelName, ttlSeconds)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
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

func (c *Client) VerifyWebhookSignature(signature string, body []byte) bool {
	h := hmac.New(sha256.New, []byte(c.webhookSecret))
	h.Write(body)
	expected := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}
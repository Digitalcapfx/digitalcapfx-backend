// twofa-check exercises the full 2FA flow against a live server using a fresh
// throwaway account: register → setup 2FA → confirm → re-login (expect 2FA) →
// complete 2FA. Run: go run ./cmd/twofa-check <base-url>
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/pquerna/otp/totp"
)

func main() {
	base := os.Args[1]
	hc := &http.Client{Timeout: 30 * time.Second}
	phone := fmt.Sprintf("+23480%06d", rand.Intn(1000000))
	pin := "1234"

	// 1. Register a fresh account.
	var reg struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	post(hc, base+"/api/v1/auth/register", "", map[string]any{
		"phone": phone, "first_name": "TwoFA", "last_name": "Check", "pin": pin,
	}, &reg)
	tok := reg.Data.AccessToken
	fmt.Println("registered:", phone, "token?", tok != "")

	// 2. Setup 2FA — this used to 500 (Redis). Expect a secret now.
	var setup struct {
		Data struct {
			Secret string `json:"secret"`
			URI    string `json:"uri"`
		} `json:"data"`
	}
	post(hc, base+"/api/v1/security/2fa/setup", tok, nil, &setup)
	fmt.Println("setup 2fa: secret_len=", len(setup.Data.Secret), "uri?", setup.Data.URI != "")
	if setup.Data.Secret == "" {
		fmt.Println("FAIL: no secret returned from setup")
		os.Exit(1)
	}

	// 3. Confirm with a live TOTP code.
	code, _ := totp.GenerateCode(setup.Data.Secret, time.Now())
	var confirm struct {
		Message string `json:"message"`
	}
	post(hc, base+"/api/v1/security/2fa/confirm", tok, map[string]any{"code": code}, &confirm)
	fmt.Println("confirm 2fa:", confirm.Message)

	// 4. Re-login — should now require 2FA and return a ref.
	var login struct {
		Data struct {
			Requires2FA bool   `json:"requires_2fa"`
			Ref         string `json:"ref"`
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	post(hc, base+"/api/v1/auth/login", "", map[string]any{"phone": phone, "pin": pin}, &login)
	fmt.Println("re-login: requires_2fa=", login.Data.Requires2FA, "ref?", login.Data.Ref != "")
	if !login.Data.Requires2FA || login.Data.Ref == "" {
		fmt.Println("FAIL: login did not require 2FA")
		os.Exit(1)
	}

	// 5. Complete 2FA login with the stateless ref + a fresh code.
	code2, _ := totp.GenerateCode(setup.Data.Secret, time.Now())
	var final struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	post(hc, base+"/api/v1/auth/2fa/login", "", map[string]any{"ref": login.Data.Ref, "code": code2}, &final)
	fmt.Println("complete 2fa: final_token?", final.Data.AccessToken != "")
	if final.Data.AccessToken == "" {
		fmt.Println("FAIL: 2FA completion did not return a token")
		os.Exit(1)
	}
	fmt.Println("PASS: full 2FA flow works end-to-end (no Redis)")
}

func post(hc *http.Client, url, tok string, body map[string]any, out any) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(http.MethodPost, url, r)
	req.Header.Set("Content-Type", "application/json")
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := hc.Do(req)
	if err != nil {
		fmt.Println("request error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		fmt.Printf("HTTP %d from %s: %s\n", resp.StatusCode, url, string(raw))
		os.Exit(1)
	}
	if out != nil {
		_ = json.Unmarshal(raw, out)
	}
}

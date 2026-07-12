// uploadcheck exercises the live upload flow with a real HTTP client + JSON
// decoder (no shell escaping), to tell a genuine signed-URL failure apart from
// a test-harness URL-mangling artifact.
//
// Run: go run ./cmd/uploadcheck <base-url> <phone> <pin>
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	base, phone, pin := os.Args[1], os.Args[2], os.Args[3]
	hc := &http.Client{Timeout: 60 * time.Second}

	// 1. login
	lb, _ := json.Marshal(map[string]string{"phone": phone, "pin": pin})
	lr, err := hc.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(lb))
	if err != nil {
		fmt.Println("login:", err)
		os.Exit(1)
	}
	var login struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	json.NewDecoder(lr.Body).Decode(&login)
	lr.Body.Close()
	tok := login.Data.AccessToken
	if tok == "" {
		fmt.Println("no token from login")
		os.Exit(1)
	}

	// 2. multipart upload
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "check.txt")
	io.WriteString(fw, "uploadcheck "+time.Now().UTC().String())
	mw.WriteField("purpose", "avatar")
	mw.Close()

	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/uploads", &buf)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	ur, err := hc.Do(req)
	if err != nil {
		fmt.Println("upload:", err)
		os.Exit(1)
	}
	body, _ := io.ReadAll(ur.Body)
	ur.Body.Close()
	fmt.Printf("upload HTTP %d\n", ur.StatusCode)

	var up struct {
		Data struct {
			Object  string `json:"object"`
			ReadURL string `json:"read_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &up); err != nil {
		fmt.Println("decode upload resp:", err, string(body))
		os.Exit(1)
	}
	fmt.Println("object:", up.Data.Object)
	fmt.Println("read_url host+path:", up.Data.ReadURL[:strings.Index(up.Data.ReadURL, "?")])

	// 3. fetch the signed URL with a real client (properly decoded)
	rr, err := hc.Get(up.Data.ReadURL)
	if err != nil {
		fmt.Println("fetch read_url:", err)
		os.Exit(1)
	}
	rb, _ := io.ReadAll(rr.Body)
	rr.Body.Close()
	fmt.Printf("read_url fetch HTTP %d\nbody: %s\n", rr.StatusCode, strings.TrimSpace(string(rb)))
}

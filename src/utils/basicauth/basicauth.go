// Package basicauth provides basic authentication method (JWT token)
package basicauth

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gookit/slog"
)

type AuthDiagnostics struct {
	StatusCode int
	Category   string
}

// AuthForJWTTokenDetailed returns a token and sanitized diagnostics without
// exposing credentials or response bodies.
func AuthForJWTTokenDetailed(url, email, password string) (string, AuthDiagnostics) {
	var diagnostics AuthDiagnostics

	payload := struct {
		Email    string `json:"email,omitempty"`
		Password string `json:"password,omitempty"`
	}{Email: email, Password: password}

	putBody, err := json.Marshal(payload)
	if err != nil {
		diagnostics.Category = "request encoding error"
		return "", diagnostics
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(putBody))
	if err != nil {
		diagnostics.Category = "request creation error"
		return "", diagnostics
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		diagnostics.Category = "network error"
		return "", diagnostics
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	diagnostics.StatusCode = resp.StatusCode
	if err != nil {
		diagnostics.Category = "response read error"
		return "", diagnostics
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		diagnostics.Category = "HTTP error"
		return "", diagnostics
	}

	var response struct {
		Token string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		diagnostics.Category = "invalid auth response"
		return "", diagnostics
	}
	if response.Token == "" {
		diagnostics.Category = "missing access token"
		return "", diagnostics
	}
	diagnostics.Category = "success"
	return response.Token, diagnostics
}

// AuthForJWTToken authenticates against Kitsu and returns a JWT.
// It preserves the legacy empty-string failure contract.
func AuthForJWTToken(url, email, password string) string {
	token, diagnostics := AuthForJWTTokenDetailed(url, email, password)
	if diagnostics.Category != "success" {
		slog.Error("basicauth: authentication failed", "url", url, "status", diagnostics.StatusCode, "category", diagnostics.Category)
	}
	return token
}

func ValidateJWTToken(url, token string) bool {
	if strings.TrimSpace(url) == "" || strings.TrimSpace(token) == "" {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

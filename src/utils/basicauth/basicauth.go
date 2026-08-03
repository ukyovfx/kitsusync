// Package basicauth provides basic authentication method (JWT token)
package basicauth

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/gookit/slog"
)

// AuthForJWTToken authenticates against Kitsu and returns a JWT.
// Returns "" on any failure (network, bad credentials, malformed response).
// Callers must check for the empty string and react accordingly — the
// hourly refresher in main.go keeps the previous token if this returns "".
func AuthForJWTToken(url, email, password string) string {
	type Payload struct {
		Email    string `json:"email,omitempty"`
		Password string `json:"password,omitempty"`
	}

	payload := &Payload{Email: email, Password: password}

	putBody, err := json.Marshal(payload)
	if err != nil {
		slog.Error("basicauth: marshal failed", "err", err)
		return ""
	}
	requestBody := bytes.NewBuffer(putBody)

	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequest(http.MethodPost, url, requestBody)
	if err != nil {
		slog.Error("basicauth: NewRequest failed", "err", err, "url", url)
		return ""
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("basicauth: client.Do failed", "err", err, "url", url)
		return ""
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		slog.Error("basicauth: read body failed", "err", err)
		return ""
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("basicauth: non-2xx response — check Kitsu credentials in conf.toml",
			"status", resp.StatusCode)
		return ""
	}

	type Response struct {
		Token string `json:"access_token"`
	}
	var jwt Response
	if err := json.Unmarshal(respBody, &jwt); err != nil {
		slog.Error("basicauth: unmarshal failed — check Kitsu credentials in conf.toml", "err", err)
		return ""
	}

	return jwt.Token
}

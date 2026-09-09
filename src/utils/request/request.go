package request

import (
	"app/src/utils/debug"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gookit/slog"
)

type VerifiedOrigin struct {
	BaseURL   string
	PinnedIPs []netip.Addr
}

var verifiedOrigin = struct {
	sync.RWMutex
	origin VerifiedOrigin
}{}

// ConfigureVerifiedOrigin installs the authority already verified by setup.
// Credential-bearing polling fails closed when its target leaves this origin.
func ConfigureVerifiedOrigin(origin VerifiedOrigin) error {
	u, err := url.Parse(strings.TrimSpace(origin.BaseURL))
	if err != nil || u.Scheme == "" || u.Hostname() == "" || len(origin.PinnedIPs) == 0 {
		return fmt.Errorf("verified Kitsu origin is invalid")
	}
	verifiedOrigin.Lock()
	verifiedOrigin.origin = VerifiedOrigin{BaseURL: u.Scheme + "://" + u.Host, PinnedIPs: append([]netip.Addr(nil), origin.PinnedIPs...)}
	verifiedOrigin.Unlock()
	return nil
}

func configuredVerifiedOrigin() VerifiedOrigin {
	verifiedOrigin.RLock()
	defer verifiedOrigin.RUnlock()
	return VerifiedOrigin{BaseURL: verifiedOrigin.origin.BaseURL, PinnedIPs: append([]netip.Addr(nil), verifiedOrigin.origin.PinnedIPs...)}
}

// Do は JWT 付き HTTP リクエストを実行し、レスポンスボディを文字列で返す。
//
// 失敗時は slog.Error を出してから空文字列を返す（slog.Fatal は使わない）。
// 一時的なネットワーク障害・5xx・429 に対してはリトライ＋指数バックオフを行う。
// 4xx（429 を除く）は永続的エラーとみなし即座に返る。
func Do(token, method, url string, payload, unmarshal interface{}) string {
	body, _ := DoWithError(token, method, url, payload, unmarshal)
	return body
}

// DoWithError executes the same bounded-retry request as Do, while preserving
// the failure outcome for callers that must distinguish an empty response from
// an unsuccessful request.
func DoWithError(token, method, url string, payload, unmarshal interface{}) (string, error) {
	const maxAttempts = 3
	// 試行間の待機時間: 2s → 6s
	retryDelays := []time.Duration{2 * time.Second, 6 * time.Second}

	// payload の JSON 化は一度だけ行う（ループ内で bytes.NewBuffer でコピーして再利用）
	var payloadBytes []byte
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			slog.Error("request.Do: marshal payload failed", "err", err, "url", url)
			return "", fmt.Errorf("marshal payload: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := retryDelays[attempt-1]
			slog.Warn("request.Do: transient failure — retrying",
				"attempt", attempt+1,
				"maxAttempts", maxAttempts,
				"delay", delay,
				"url", url)
			time.Sleep(delay)
		}

		// body は試行ごとに新しい Reader を作成する（io.Reader は一度読むと使い切り）
		var body *bytes.Buffer
		if payloadBytes != nil {
			body = bytes.NewBuffer(payloadBytes)
		}

		result := attemptOnce(token, method, url, body, unmarshal)
		switch result.status {
		case statusSuccess:
			return result.body, nil
		case statusPermanent:
			// 4xx 等の永続エラーはリトライ不要
			return "", result.err
		case statusTransient:
			// 次のループでリトライ
			lastErr = result.err
			continue
		}
	}

	slog.Error("request.Do: all attempts exhausted", "url", url, "maxAttempts", maxAttempts)
	return "", fmt.Errorf("request failed after %d attempts: %w", maxAttempts, lastErr)
}

type attemptStatus int

const (
	statusSuccess   attemptStatus = iota
	statusTransient               // リトライすべき一時エラー
	statusPermanent               // リトライ不要な永続エラー
)

type attemptResult struct {
	status attemptStatus
	body   string
	err    error
}

func attemptOnce(token, method, url string, body *bytes.Buffer, unmarshal interface{}) attemptResult {
	origin := configuredVerifiedOrigin()
	target, err := urlpkgParse(url)
	if err != nil {
		return attemptResult{status: statusPermanent, err: fmt.Errorf("create request: invalid URL")}
	}
	if token != "" && (origin.BaseURL == "" || !sameOrigin(origin.BaseURL, target)) {
		return attemptResult{status: statusPermanent, err: fmt.Errorf("verified Kitsu origin required")}
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:           nil,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext:     pinnedDialer(origin.PinnedIPs),
		},
		// Legacy Kitsu callers must never replay bearer credentials to a
		// redirect target. Callers receive the original 3xx as a failure.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}

	var req *http.Request
	if body != nil {
		req, err = http.NewRequest(method, url, body)
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		slog.Error("request.Do: NewRequest failed", "err", err, "url", url)
		return attemptResult{status: statusPermanent, err: fmt.Errorf("create request: %w", err)}
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("request.Do: client.Do failed", "err", err, "method", method, "url", url)
		return attemptResult{status: statusTransient, err: fmt.Errorf("send request: %w", err)}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		slog.Error("request.Do: read body failed", "err", err, "url", url)
		return attemptResult{status: statusTransient, err: fmt.Errorf("read response: %w", err)}
	}

	if os.Getenv("Debug") == "true" {
		debug.Info(resp, respBody)
	}

	// 429 Too Many Requests — 一時的なレート制限
	if resp.StatusCode == 429 {
		slog.Warn("request.Do: rate limited (429)",
			"url", url,
			"retryAfter", resp.Header.Get("Retry-After"))
		return attemptResult{status: statusTransient, err: fmt.Errorf("HTTP status %d", resp.StatusCode)}
	}

	// 5xx — サーバーサイドの一時障害
	if resp.StatusCode >= 500 {
		slog.Error("request.Do: 5xx response",
			"status", resp.StatusCode,
			"method", method,
			"url", url)
		return attemptResult{status: statusTransient}
	}

	// その他の非 2xx（4xx 等）— 永続エラー
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("request.Do: non-2xx response",
			"status", resp.StatusCode,
			"method", method,
			"url", url)
		return attemptResult{status: statusPermanent, err: fmt.Errorf("HTTP status %d", resp.StatusCode)}
	}

	// 成功
	if unmarshal != nil {
		if err := json.Unmarshal(respBody, &unmarshal); err != nil {
			slog.Error("request.Do: unmarshal failed", "err", err, "url", url)
			return attemptResult{status: statusPermanent, err: fmt.Errorf("decode response: %w", err)}
		}
	}

	return attemptResult{status: statusSuccess, body: string(respBody)}
}

func urlpkgParse(raw string) (*url.URL, error) { return url.Parse(raw) }

func sameOrigin(raw string, target *url.URL) bool {
	origin, err := url.Parse(raw)
	return err == nil && origin.Scheme == target.Scheme && strings.EqualFold(origin.Host, target.Host)
}

func pinnedDialer(ips []netip.Addr) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if len(ips) == 0 {
			return nil, fmt.Errorf("verified Kitsu origin required")
		}
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
}

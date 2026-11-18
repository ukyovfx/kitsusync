package request

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// newBodyLimitFixture は指定サイズの本文を返すサーバと、短縮済み retryDelays を用意する。
func newBodyLimitFixture(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
	prev := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryDelays = prev })
	return server
}

func TestResponseBodyWithinLimitSucceeds(t *testing.T) {
	// 既定 64 MiB 未満でも、上限を小さく設定した状態で境界未満が成功すること。
	if err := ConfigureMaxResponseBodyLimit(1024); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { maxResponseBodyBytes = DefaultMaxResponseBodyBytes })

	body := `{"each":["` + strings.Repeat("a", 900) + `"]}` // 1 KiB 未満
	server := newBodyLimitFixture(t, body)

	var out struct {
		Each []string `json:"each"`
	}
	got, err := DoWithError(context.Background(), "", http.MethodGet, server.URL+"/x", nil, &out)
	if err != nil {
		t.Fatalf("under-limit response should succeed, got %v", err)
	}
	if got != body || len(out.Each) != 1 {
		t.Fatalf("body mismatch: got %d bytes, each=%d", len(got), len(out.Each))
	}
}

func TestResponseBodyOverLimitIsExplicitPermanentError(t *testing.T) {
	if err := ConfigureMaxResponseBodyLimit(1024); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { maxResponseBodyBytes = DefaultMaxResponseBodyBytes })

	// 旧来は 4 MiB で黙って切り捨てられ、decode エラーになっていた（F1）。
	// JSON として有効な先頭だけを送り、切り捨てなら decode が通ってしまう本文にする。
	body := `["` + strings.Repeat("a", 2048) + `"]`
	server := newBodyLimitFixture(t, body)

	_, err := DoWithError(context.Background(), "", http.MethodGet, server.URL+"/x", nil, nil)
	if err == nil {
		t.Fatal("over-limit response should fail")
	}
	if !strings.Contains(err.Error(), "response too large (limit=") {
		t.Fatalf("expected explicit too-large error, got %v", err)
	}
	// 上限超過は永続エラーなので再試行しない（リトライすれば 3 回送られる）。
	if strings.Contains(err.Error(), "request failed after") {
		t.Fatalf("permanent error should not be wrapped by retry exhaustion, got %v", err)
	}
}

func TestConfigureMaxResponseBodyLimitRejectsNonPositive(t *testing.T) {
	prev := maxResponseBodyBytes
	for _, bad := range []int64{0, -1} {
		if err := ConfigureMaxResponseBodyLimit(bad); err == nil {
			t.Fatalf("limit %d should be rejected", bad)
		}
	}
	if maxResponseBodyBytes != prev {
		t.Fatalf("rejected values must not change the limit")
	}
	if err := ConfigureMaxResponseBodyLimit(2048); err != nil {
		t.Fatalf("positive limit should be accepted: %v", err)
	}
	t.Cleanup(func() { maxResponseBodyBytes = DefaultMaxResponseBodyBytes })
	if maxResponseBodyBytes != 2048 {
		t.Fatalf("limit not applied, got %d", maxResponseBodyBytes)
	}
}

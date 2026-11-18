package request

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

// A shutdown cancels the poll context while a Kitsu read is hanging. The read
// must return on cancellation instead of running into the client timeout.
func TestDoWithErrorAbortsInFlightRequestOnContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		close(started)
		cancel()
	}()

	type result struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		_, err := DoWithError(ctx, "", http.MethodGet, server.URL+"/tasks", nil, nil)
		done <- result{err: err, elapsed: time.Since(start)}
	}()

	<-started
	select {
	case got := <-done:
		if got.elapsed > 5*time.Second {
			t.Fatalf("request ran %s after cancellation, want an immediate abort", got.elapsed)
		}
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("request ignored context cancellation")
	}
}

// A transient failure's retry backoff must not hold shutdown either.
func TestDoWithErrorStopsRetryBackoffOnContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
	prev := retryDelays
	retryDelays = []time.Duration{30 * time.Second}
	t.Cleanup(func() { retryDelays = prev })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := DoWithError(ctx, "", http.MethodGet, server.URL+"/tasks", nil, nil)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("retry backoff ignored cancellation and took %s", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

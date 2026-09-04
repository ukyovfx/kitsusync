package setup

import (
	"net/http/httptest"
	"testing"
)

func TestNormalizeKitsuURLPreservesPortAndNormalizesAPIRoutes(t *testing.T) {
	for _, tc := range []struct{ raw, display, api string }{
		{"kitsu.example.test:9443", "https://kitsu.example.test:9443", "https://kitsu.example.test:9443/api"},
		{"https://kitsu.example.test/studio/api", "https://kitsu.example.test/studio", "https://kitsu.example.test/studio/api"},
		{"https://kitsu.example.test/studio/api/auth/login", "https://kitsu.example.test/studio", "https://kitsu.example.test/studio/api"},
	} {
		got, err := NormalizeKitsuURL(tc.raw, APISourceExplicit)
		if err != nil || got.DisplayBaseURL != tc.display || got.ResolvedAPIBaseURL != tc.api {
			t.Fatalf("%q = %+v, %v", tc.raw, got, err)
		}
	}
}

func TestNormalizeKitsuURLRejectsUnsafeInputs(t *testing.T) {
	for _, raw := range []string{"ftp://kitsu.example.test", "https://user:pass@kitsu.example.test", "http://169.254.169.254", "https://metadata.google.internal"} {
		if _, err := NormalizeKitsuURL(raw, APISourceExplicit); err == nil {
			t.Fatalf("accepted unsafe input %q", raw)
		}
	}
}

func TestKitsuCandidateBudgetIsFixed(t *testing.T) {
	if maxKitsuCandidates != 4 {
		t.Fatalf("candidate budget = %d", maxKitsuCandidates)
	}
}

func TestSetupProbeRateLimitIsBoundedPerPeer(t *testing.T) {
	peer := httptest.NewRequest("POST", "/api/setup/test-kitsu", nil)
	peer.RemoteAddr = "198.51.100.42:1234"
	setupProbeLimiter.Lock()
	delete(setupProbeLimiter.started, "198.51.100.42")
	setupProbeLimiter.Unlock()
	for i := 0; i < 6; i++ {
		if !allowSetupProbe(peer) {
			t.Fatalf("probe %d unexpectedly rejected", i+1)
		}
	}
	if allowSetupProbe(peer) {
		t.Fatal("seventh probe was not rejected")
	}
	peer.Header.Set("X-Forwarded-For", "203.0.113.99")
	if allowSetupProbe(peer) {
		t.Fatal("forwarded header bypassed probe limit")
	}
}

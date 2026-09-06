package setup

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"app/src/model"
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

func TestSetupProbeRateLimitHasIndependentPeersAndExpires(t *testing.T) {
	setupProbeLimiter.Lock()
	setupProbeLimiter.started = make(map[string][]time.Time)
	setupProbeLimiter.Unlock()
	one := httptest.NewRequest(http.MethodPost, "/api/setup/test-kitsu", nil)
	one.RemoteAddr = net.JoinHostPort("198.51.100.10", "1")
	two := httptest.NewRequest(http.MethodPost, "/api/setup/test-kitsu", nil)
	two.RemoteAddr = net.JoinHostPort("198.51.100.11", "1")
	for i := 0; i < 6; i++ {
		if !allowSetupProbe(one) {
			t.Fatalf("peer one request %d rejected", i+1)
		}
	}
	if allowSetupProbe(one) {
		t.Fatal("peer one seventh request accepted")
	}
	if !allowSetupProbe(two) {
		t.Fatal("independent peer budget was not available")
	}
	setupProbeLimiter.Lock()
	setupProbeLimiter.started["198.51.100.10"] = []time.Time{time.Now().Add(-2 * time.Minute)}
	setupProbeLimiter.Unlock()
	if !allowSetupProbe(one) {
		t.Fatal("expired probe budget did not reset")
	}
}

func TestAuth307DoesNotReplayCredentials(t *testing.T) {
	testAuthRedirectDoesNotReplay(t, http.StatusTemporaryRedirect)
}
func TestAuth308DoesNotReplayCredentials(t *testing.T) {
	testAuthRedirectDoesNotReplay(t, http.StatusPermanentRedirect)
}

func testAuthRedirectDoesNotReplay(t *testing.T, code int) {
	var authCalls, replayCalls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			authCalls++
			w.Header().Set("Location", server.URL+"/capture")
			w.WriteHeader(code)
			return
		}
		if r.URL.Path == "/capture" {
			replayCalls++
		}
	}))
	defer server.Close()
	model, err := NormalizeKitsuURL(server.URL, APISourceExplicit)
	if err != nil {
		t.Fatal(err)
	}
	model.VerifiedIPs = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	model.TargetScope = "loopback"
	_, _, err = AuthenticateKitsuCredentials(context.Background(), model, "user@example.com", "secret")
	if err == nil || connectionErrorClass(err) != "auth_redirect_blocked" {
		t.Fatalf("redirect error = %v", err)
	}
	if authCalls != 1 || replayCalls != 0 {
		t.Fatalf("auth calls=%d replay calls=%d", authCalls, replayCalls)
	}
}

func TestDNSChangeAfterVerificationBlocksCredentials(t *testing.T) {
	old := kitsuLookupNetIP
	defer func() { kitsuLookupNetIP = old }()
	kitsuLookupNetIP = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("198.51.100.11")}, nil
	}
	model := KitsuURLModel{ResolvedAPIBaseURL: "https://kitsu.test/api", VerifiedIPs: []netip.Addr{netip.MustParseAddr("198.51.100.10")}, TargetScope: "public"}
	if _, _, err := AuthenticateKitsuCredentials(context.Background(), model, "u", "p"); connectionErrorClass(err) != "dns_scope_changed" {
		t.Fatalf("error=%v", err)
	}
}

func TestPublicToPrivateAndLoopbackRebindingFailsClosed(t *testing.T) {
	for _, ip := range []string{"10.0.0.4", "127.0.0.1"} {
		old := kitsuLookupNetIP
		kitsuLookupNetIP = func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(ip)}, nil }
		model := KitsuURLModel{ResolvedAPIBaseURL: "https://kitsu.test/api", VerifiedIPs: []netip.Addr{netip.MustParseAddr("198.51.100.10")}, TargetScope: "public"}
		if err := ensureKitsuTargetStable(context.Background(), model); connectionErrorClass(err) != "dns_scope_changed" {
			t.Fatalf("%s rebinding error=%v", ip, err)
		}
		kitsuLookupNetIP = old
	}
}

func TestExplicitPrivateAndTailscaleDNSScopesAllowed(t *testing.T) {
	for _, ip := range []string{"10.20.30.40", "100.100.20.30"} {
		scope, err := resolvedKitsuScope([]netip.Addr{netip.MustParseAddr(ip)})
		if err != nil || (scope != "private_explicit" && scope != "vpn/shared") {
			t.Fatalf("%s scope=%q err=%v", ip, scope, err)
		}
	}
}

func TestAPIOverridePersistsAndReadsBack(t *testing.T) {
	db := newSetupStateTestDB(t)
	want := "https://private.kitsu.test/studio/api"
	model.SetSetting(db, KitsuAPIBaseURLSettingKey, want)
	if got := model.GetSetting(db, KitsuAPIBaseURLSettingKey); got != want {
		t.Fatalf("API override readback = %q, want %q", got, want)
	}
}

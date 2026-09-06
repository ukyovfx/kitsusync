package setup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/src/model"
)

func TestKitsuHostDiscoveryUsesSavedEndpointBeforeLocalCandidates(t *testing.T) {
	server := httptest.NewServer(kitsuProbeHandler())
	defer server.Close()
	db := newSetupStateTestDB(t)
	model.SetSetting(db, "kitsu.hostname", server.URL+"/")
	t.Setenv("KITSU_HOSTNAME", "")
	result := DiscoverKitsuHost(db)
	if result.RuntimeHost != server.URL+"/" || result.Source != "persisted" {
		t.Fatalf("saved endpoint did not win: %+v", result)
	}
}

func TestKitsuHostDiscoveryUsesExplicitEndpointBeforePersistedEndpoint(t *testing.T) {
	explicit := httptest.NewServer(kitsuProbeHandler())
	defer explicit.Close()
	persisted := httptest.NewServer(kitsuProbeHandler())
	defer persisted.Close()
	db := newSetupStateTestDB(t)
	model.SetSetting(db, "kitsu.hostname", persisted.URL)
	t.Setenv("KITSU_HOSTNAME", explicit.URL)
	result := DiscoverKitsuHost(db)
	if result.RuntimeHost != explicit.URL+"/" || result.Source != "explicit" {
		t.Fatalf("explicit endpoint did not win: %+v", result)
	}
}

func TestKitsuHostDiscoveryIgnoresPlaceholderAndUsesLocalCandidate(t *testing.T) {
	server := httptest.NewServer(kitsuProbeHandler())
	defer server.Close()
	originalCandidates := discoveryCandidates
	originalAt, originalResult := discoveryAt, discoveryResult
	defer func() {
		discoveryCandidates = originalCandidates
		discoveryAt, discoveryResult = originalAt, originalResult
	}()
	discoveryCandidates = func() []kitsuHostProbe {
		return []kitsuHostProbe{{RuntimeHost: server.URL, DisplayHost: "local"}}
	}
	discoveryAt = time.Time{}
	discoveryResult = KitsuHostDiscoveryResult{}
	t.Setenv("KITSU_HOSTNAME", "http://YOUR_KITSU_HOST/")
	if result := DiscoverKitsuHost(nil); result.Source != "local-discovered" || result.RuntimeHost != server.URL {
		t.Fatalf("placeholder was not ignored: %+v", result)
	}
}

func TestKitsuHostDiscoveryFindsExactlyOneVerifiedLocalCandidate(t *testing.T) {
	server := httptest.NewServer(kitsuProbeHandler())
	defer server.Close()
	originalCandidates := discoveryCandidates
	originalAt, originalResult := discoveryAt, discoveryResult
	defer func() {
		discoveryCandidates = originalCandidates
		discoveryAt, discoveryResult = originalAt, originalResult
	}()
	discoveryCandidates = func() []kitsuHostProbe {
		return []kitsuHostProbe{{RuntimeHost: server.URL, DisplayHost: "http://127.0.0.1:8080"}}
	}
	discoveryAt = time.Time{}
	discoveryResult = KitsuHostDiscoveryResult{}
	t.Setenv("KITSU_HOSTNAME", "")

	result := DiscoverKitsuHost(nil)
	if result.RuntimeHost != server.URL || result.Status != hostReachableKitsu {
		t.Fatalf("expected one verified candidate, got %+v", result)
	}
}

func TestKitsuHostDiscoveryRejectsMultipleCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	originalCandidates := discoveryCandidates
	originalAt, originalResult := discoveryAt, discoveryResult
	defer func() {
		discoveryCandidates = originalCandidates
		discoveryAt, discoveryResult = originalAt, originalResult
	}()
	discoveryCandidates = func() []kitsuHostProbe {
		return []kitsuHostProbe{{RuntimeHost: server.URL, DisplayHost: "one"}, {RuntimeHost: server.URL, DisplayHost: "two"}}
	}
	discoveryAt = time.Time{}
	discoveryResult = KitsuHostDiscoveryResult{}
	t.Setenv("KITSU_HOSTNAME", "")
	if result := DiscoverKitsuHost(nil); result.RuntimeHost != "" {
		t.Fatalf("multiple candidates were selected: %+v", result)
	}
}

func TestKitsuHostDiscoveryRejectsRandomHTTP200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer server.Close()
	if probeKitsu(server.URL) {
		t.Fatal("random HTTP 200 was identified as Kitsu")
	}
}

func TestKitsuHostDiscoveryFailsClosedWithoutCandidate(t *testing.T) {
	originalCandidates := discoveryCandidates
	originalAt, originalResult := discoveryAt, discoveryResult
	defer func() {
		discoveryCandidates = originalCandidates
		discoveryAt, discoveryResult = originalAt, originalResult
	}()
	discoveryCandidates = func() []kitsuHostProbe { return nil }
	discoveryAt = time.Time{}
	discoveryResult = KitsuHostDiscoveryResult{}
	t.Setenv("KITSU_HOSTNAME", "")
	if result := DiscoverKitsuHost(nil); result.RuntimeHost != "" || result.Status != "" {
		t.Fatalf("expected fail-closed result, got %+v", result)
	}
}

func kitsuProbeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
}

func TestKitsuHostDiscoveryTimeoutIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()
	started := time.Now()
	if probeKitsu(server.URL) {
		t.Fatal("timed out candidate was accepted")
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("probe took too long: %s", elapsed)
	}
}

func TestConnectionsFormOmitsHumanCredentialControls(t *testing.T) {
	db := newSetupStateTestDB(t)
	for _, lang := range []string{"ja", "en"} {
		body := renderConnectionsEditFormWithHealth(lang, httptest.NewRequest(http.MethodGet, "/bot/admin/bot?edit=1&lang="+lang, nil), db, "Review", "warn", "Check", "http://127.0.0.1:8080", false, false, "")
		for _, forbidden := range []string{`name="kitsu_runtime_email"`, `name="kitsu_runtime_password"`, "Legacy fallback", "Runtime email", "Runtime password"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s current form rendered forbidden legacy control/text %q", lang, forbidden)
			}
		}
		for _, required := range []string{`name="kitsu_hostname"`, `name="kitsu_bot_token"`} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s current form missing %q", lang, required)
			}
		}
		if strings.Contains(body, ">Bot</dt>") || strings.Contains(body, ">Bot</dd>") {
			t.Fatalf("%s current form rendered a Bot identity row", lang)
		}
	}
}

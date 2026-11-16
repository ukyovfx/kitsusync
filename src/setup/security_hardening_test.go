package setup

import (
	"testing"
)

// TestValidateKitsuEndpointRejectsPlainHTTPToPublicHost reproduces the audit
// finding: a public Kitsu could be connected over plain http. Default must be
// reject; only loopback/LAN targets or the explicit insecure-HTTP flag may use
// http. Stored values and display-only paths are not re-gated.
func TestValidateKitsuEndpointRejectsPlainHTTPToPublicHost(t *testing.T) {
	_, err := validateKitsuEndpoint("http://kitsu.example.com")
	if err == nil {
		t.Fatal("plain http to a public host was accepted by default")
	}
	if class := connectionErrorClass(err); class != "insecure_http_rejected" {
		t.Fatalf("error class = %q, want insecure_http_rejected", class)
	}
	if _, err := validateKitsuEndpoint("https://kitsu.example.com"); err != nil {
		t.Fatalf("https was rejected: %v", err)
	}
	_, probeErr := ResolveAndProbeKitsu(t.Context(), "http://kitsu.example.com", APISourceExplicit)
	if class := connectionErrorClass(probeErr); class != "insecure_http_rejected" {
		t.Fatalf("probe error class = %q, want insecure_http_rejected", class)
	}
}

// TestValidateKitsuURLAllowsLoopbackAndLANHTTP verifies the local-development
// and LAN paths keep working without any flag.
func TestValidateKitsuURLAllowsLoopbackAndLANHTTP(t *testing.T) {
	for _, raw := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://host.docker.internal:8080", "http://192.168.1.20:8080", "http://100.64.0.5:8080"} {
		if _, err := validateKitsuEndpoint(raw); err != nil {
			t.Fatalf("local http %q was rejected: %v", raw, err)
		}
	}
}

// TestNormalizeKitsuURLInsecureFlagOverride verifies the explicit validation
// flag is the only way to reach a public host over http.
func TestNormalizeKitsuURLInsecureFlagOverride(t *testing.T) {
	t.Setenv("KITSUSYNC_ALLOW_INSECURE_KITSU_HTTP", "1")
	if _, err := validateKitsuEndpoint("http://kitsu.example.com"); err != nil {
		t.Fatalf("flagged insecure http was rejected: %v", err)
	}
	t.Setenv("KITSUSYNC_ALLOW_INSECURE_KITSU_HTTP", "no")
	if _, err := validateKitsuEndpoint("http://kitsu.example.com"); err == nil {
		t.Fatal("non-truthy flag value must not enable insecure http")
	}
}



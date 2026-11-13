package setup

import (
	"net/http/httptest"
	"strings"
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

// TestRenderFormEscapesKitsuProjectNames reproduces the round-3 stored-XSS
// finding: /bot/setup?legacy=1 rendered Kitsu project names unescaped, so a
// malicious project name could inject <script> and break out of the
// data-name attribute with <svg onload>.
func TestRenderFormEscapesKitsuProjectNames(t *testing.T) {
	db := newIAViewDB(t)
	malicious := `<script>alert(1)</script><svg onload=alert(2) data-x="`
	kitsuProjects := []KitsuProject{{ID: `p"><svg onload=alert(3)>`, Name: malicious}}

	html := renderForm(httptest.NewRequest("GET", "/bot/setup?legacy=1", nil), nil, kitsuProjects, map[string]bool{}, db, "", "", "", "", "")

	if strings.Contains(html, malicious) {
		t.Fatal("project name was rendered unescaped (script/svg injection)")
	}
	if strings.Contains(html, `p"><svg onload=alert(3)>`) {
		t.Fatal("project ID broke out of the value attribute")
	}
	if !strings.Contains(html, `&lt;script&gt;`) {
		t.Fatal("expected HTML-escaped project name in the output")
	}
}

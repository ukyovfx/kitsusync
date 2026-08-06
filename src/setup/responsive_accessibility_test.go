package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrimaryNavigationUsesContainedRailAndAccessibleTargets(t *testing.T) {
	body := adminPage("en", "Dashboard", httptest.NewRequest(http.MethodGet, "/bot/admin?lang=en", nil), "<p>content</p>")
	for _, want := range []string{`<nav class="primary-nav"`, `overflow-x:auto`, `.primary-nav .nav-chip{flex:0 0 auto}`, `summary:focus-visible`, `display:inline-flex;align-items:center;justify-content:center`, `min-height:44px`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestSetupRequiredPagesHaveOnePrimaryHeading(t *testing.T) {
	body := renderSetupRequiredPage("en", httptest.NewRequest(http.MethodGet, "/bot/setup?lang=en", nil))
	if strings.Count(body, "<h1") != 1 {
		t.Fatalf("want one H1, got %d", strings.Count(body, "<h1"))
	}
	if strings.Contains(body, ">Setup required</h2>") {
		t.Fatal("duplicate setup heading")
	}
}

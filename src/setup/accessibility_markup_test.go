package setup

import (
	"strings"
	"testing"
)

func TestLoginMarkupHasAccessibleLabelsAndErrorRegion(t *testing.T) {
	html := loginPageHTML("en", "invalid login", "/bot/admin", true, nil)
	for _, want := range []string{
		`<main id="main-content">`,
		`class="login-page"`,
		`class="page-card glass login-card"`,
		`<label for="login-email">`,
		`id="login-email"`,
		`<label for="login-password">`,
		`id="login-password"`,
		`role="alert"`,
		`aria-live="assertive"`,
		`button:focus-visible`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("login markup missing %q", want)
		}
	}
	if !strings.Contains(adminThemeCSS, `body.login-surface main{position:fixed;inset:0;z-index:1;display:grid;place-items:center`) {
		t.Fatal("login page is missing the viewport-centered composition contract")
	}
}

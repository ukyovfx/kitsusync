package setup

import (
	"strings"
	"testing"
)

func TestLoginMarkupHasAccessibleLabelsAndErrorRegion(t *testing.T) {
	html := loginPageHTML("en", "invalid login", "/bot/admin", true, nil)
	for _, want := range []string{
		`<main id="main-content">`,
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
}

package setup

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminHeaderKeepsGlobalActionsInPrimaryHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/bot/admin?lang=en", nil)
	body := adminPage("en", "KitsuSync", r, "<p>content</p>")
	topbar := strings.Index(body, `<div class="topbar">`)
	actions := strings.Index(body, `<div class="top-actions">`)
	nav := strings.Index(body, `<nav aria-label="Primary navigation">`)
	main := strings.Index(body, `<main id="main-content">`)
	if topbar < 0 || actions < topbar || nav < actions || main < nav {
		t.Fatalf("global navigation is not inside the header: topbar=%d actions=%d nav=%d main=%d", topbar, actions, nav, main)
	}
	if strings.Count(body[:main], `<nav aria-label="Primary navigation">`) != 1 {
		t.Fatal("unexpected duplicate primary navigation")
	}
	for _, label := range []string{"Dashboard", "Productions", "New Production Connection", "User Linking", "Connections", "System Status", "Audit Log", "JP", "EN"} {
		if !strings.Contains(body, label) {
			t.Fatalf("header is missing %q", label)
		}
	}
}

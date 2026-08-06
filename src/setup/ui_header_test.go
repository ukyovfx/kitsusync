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
	nav := strings.Index(body, `<nav class="primary-nav" aria-label="Primary navigation">`)
	main := strings.Index(body, `<main id="main-content">`)
	if topbar < 0 || actions < topbar || nav < actions || main < nav {
		t.Fatalf("global navigation is not inside the header: topbar=%d actions=%d nav=%d main=%d", topbar, actions, nav, main)
	}
	if strings.Count(body[:main], `<nav class="primary-nav" aria-label="Primary navigation">`) != 1 {
		t.Fatal("unexpected duplicate primary navigation")
	}
	for _, label := range []string{"Productions", "User Linking", "Connections", "System Status", "Audit Log", "JP", "EN"} {
		if !strings.Contains(body, label) {
			t.Fatalf("header is missing %q", label)
		}
	}
	navEnd := strings.Index(body[nav:], `</nav>`)
	if navEnd >= 0 && strings.Contains(body[nav:nav+navEnd], ">Dashboard<") {
		t.Fatal("Dashboard remains a dedicated primary-navigation item")
	}
	if navEnd >= 0 && strings.Contains(body[nav:nav+navEnd], "New Production Connection") {
		t.Fatal("New Production Connection remains a primary-navigation item")
	}
}

func TestLanguageToggleCentersBothLabels(t *testing.T) {
	body := adminPage("ja", "KitsuSync", httptest.NewRequest("GET", "/bot/admin?lang=ja", nil), "<p>content</p>")
	for _, want := range []string{".lang-toggle{", "align-items:center;", "min-height:56px;", ".lang-thumb{", "top:6px;", "bottom:6px;", ".lang-option{", "display:flex;", "justify-content:center;", "line-height:1;", "min-height:44px;"} {
		if !strings.Contains(body, want) {
			t.Fatalf("language toggle is missing centered-label styling %q", want)
		}
	}
}

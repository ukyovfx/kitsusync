package setup

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestFaviconRouteServesICO(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /favicon.ico", FaviconHandler())

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /favicon.ico status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/x-icon" {
		t.Fatalf("favicon Content-Type = %q, want image/x-icon", got)
	}
	body := recorder.Body.Bytes()
	if len(body) < 22 || body[0] != 0 || body[1] != 0 || body[2] != 1 || body[3] != 0 || body[4] != 1 || body[5] != 0 {
		t.Fatalf("favicon response is not a valid ICO resource: %v", body[:min(len(body), 8)])
	}
	if got := binary.LittleEndian.Uint32(body[14:18]); got != uint32(len(body)-22) {
		t.Fatalf("ICO image size = %d, response payload after directory = %d", got, len(body)-22)
	}
	if got := binary.LittleEndian.Uint32(body[18:22]); got != 22 {
		t.Fatalf("ICO image offset = %d, want 22", got)
	}
	if got := recorder.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Fatalf("favicon Content-Length = %q, want %d", got, len(body))
	}

	head := httptest.NewRecorder()
	mux.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/favicon.ico", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD /favicon.ico status/body = %d/%d, want 200/0", head.Code, head.Body.Len())
	}
}

func TestKitsuSyncIconRouteServesEmbeddedSVG(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /kitsusync.svg", KitsuSyncIconHandler())
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/kitsusync.svg", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("KitsuSync SVG status/type = %d/%q", response.Code, response.Header().Get("Content-Type"))
	}
	if !strings.Contains(response.Body.String(), `<svg id="Layer_1"`) || !strings.Contains(response.Body.String(), `fill: #fc8742`) {
		t.Fatal("KitsuSync SVG response does not contain the supplied brand artwork")
	}
}

func TestFaviconMetadataPreservesLoginAndAdminPages(t *testing.T) {
	login := loginPageHTML("en", "", "", false, httptest.NewRequest(http.MethodGet, "/bot/login", nil))
	if !strings.Contains(login, `<form method="POST" class="section-stack">`) || !strings.Contains(login, `type="password"`) {
		t.Fatal("login page lost its existing sign-in form")
	}
	if !strings.Contains(login, `<link rel="icon" href="/favicon.ico" type="image/x-icon">`) {
		t.Fatal("login page does not reference the served favicon")
	}
	if !strings.Contains(login, `<link rel="icon" href="/kitsusync.svg" type="image/svg+xml">`) {
		t.Fatal("login page does not reference the supplied KitsuSync icon")
	}

	admin := appShell("Dashboard", "", "en", httptest.NewRequest(http.MethodGet, "/bot/admin", nil), `<a href="/bot/admin">Admin</a>`, `<h1>Dashboard</h1>`)
	if !strings.Contains(admin, `<h1>Dashboard</h1>`) || !strings.Contains(admin, `class="admin-surface"`) {
		t.Fatal("admin page content or surface changed")
	}
	if !strings.Contains(admin, `<link rel="icon" href="/favicon.ico" type="image/x-icon">`) {
		t.Fatal("admin page does not reference the served favicon")
	}
	if !strings.Contains(admin, `<link rel="icon" href="/kitsusync.svg" type="image/svg+xml">`) {
		t.Fatal("admin page does not reference the supplied KitsuSync icon")
	}
}

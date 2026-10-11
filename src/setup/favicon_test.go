package setup

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
	if len(body) < 6+3*16 || body[0] != 0 || body[1] != 0 || body[2] != 1 || body[3] != 0 || body[4] != 3 || body[5] != 0 {
		t.Fatalf("favicon response is not a valid ICO resource: %v", body[:min(len(body), 8)])
	}
	for i, size := range []byte{16, 32, 48} {
		entry := body[6+i*16 : 6+(i+1)*16]
		if entry[0] != size || entry[1] != size || binary.LittleEndian.Uint16(entry[4:6]) != 1 || binary.LittleEndian.Uint16(entry[6:8]) != 32 {
			t.Fatalf("ICO entry %d does not describe a %dx%d 32-bit image: %v", i, size, size, entry)
		}
		resourceSize := binary.LittleEndian.Uint32(entry[8:12])
		resourceOffset := binary.LittleEndian.Uint32(entry[12:16])
		resourceEnd := uint64(resourceOffset) + uint64(resourceSize)
		if resourceOffset < uint32(6+3*16) || resourceEnd > uint64(len(body)) || resourceSize < 8 {
			t.Fatalf("ICO entry %d resource bounds invalid: offset=%d size=%d response=%d", i, resourceOffset, resourceSize, len(body))
		}
		if string(body[resourceOffset:resourceOffset+8]) != "\x89PNG\r\n\x1a\n" {
			t.Fatalf("ICO entry %d is not a PNG raster of the source SVG", i)
		}
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
	digest := sha256.Sum256(kitsusyncSVG)
	if got := strings.ToUpper(hex.EncodeToString(digest[:])); got != "76E19961545AA1B4061DD70D2187D95CF830F674B89C1F24AAE8FF35BE97DB38" {
		t.Fatalf("KitsuSync SVG does not byte-match the supplied kitsusync2.svg artwork: SHA-256 %s", got)
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

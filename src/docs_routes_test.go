package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsRoutesServeEntryPointAndSameOriginAsset(t *testing.T) {
	mux := http.NewServeMux()
	registerDocsRoutes(mux)

	for _, path := range []string{"/bot/docs", "/bot/docs/", "/docs", "/docs/"} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "KitsuSync Documentation") {
			t.Fatalf("%s: status=%d body prefix=%q", path, rr.Code, rr.Body.String()[:min(40, len(rr.Body.String()))])
		}
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/bot/docs/site.jsx", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "sections") {
		t.Fatalf("site asset: status=%d", rr.Code)
	}
}

func TestDocsRoutesRejectMutation(t *testing.T) {
	mux := http.NewServeMux()
	registerDocsRoutes(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/bot/docs", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

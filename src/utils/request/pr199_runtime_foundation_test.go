package request

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestPR199Transient5xxPreservesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}

	result := attemptOnce("runtime-token", http.MethodGet, server.URL, nil, nil)
	if result.status != statusTransient {
		t.Fatalf("status=%v, want transient", result.status)
	}
	if result.err == nil {
		t.Fatal("5xx transient result lost the concrete error")
	}
}

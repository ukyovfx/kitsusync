package request

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestPinnedTransportIsReusedForSameOrigin(t *testing.T) {
	ips := []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	first := pinnedTransport(ips)
	if second := pinnedTransport([]netip.Addr{netip.MustParseAddr("127.0.0.1")}); second != first {
		t.Fatal("same pinned IPs must reuse the Transport")
	}
	if first.IdleConnTimeout != pinnedTransportIdleTimeout || first.IdleConnTimeout <= 0 {
		t.Fatalf("IdleConnTimeout = %v", first.IdleConnTimeout)
	}
	if other := pinnedTransport([]netip.Addr{netip.MustParseAddr("127.0.0.2")}); other == first {
		t.Fatal("different pinned IPs must not share a Transport")
	}
}

func TestRepeatedRequestsDoNotCreateTransports(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(server.Close)
	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
	prev := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryDelays = prev })

	attemptOnce("tok", http.MethodGet, server.URL+"/x", nil, nil)
	pinnedTransportCache.Lock()
	before := pinnedTransportCache.transport
	pinnedTransportCache.Unlock()
	for i := 0; i < 5; i++ {
		attemptOnce("tok", http.MethodGet, server.URL+"/x", nil, nil)
	}
	pinnedTransportCache.Lock()
	after := pinnedTransportCache.transport
	pinnedTransportCache.Unlock()
	if before == nil || before != after {
		t.Fatal("Transport was recreated across repeated requests")
	}
}

package setup

import (
	"net/http"
	"net/netip"
	"testing"
)

func TestSafeKitsuClientReusesTransport(t *testing.T) {
	model := KitsuURLModel{TargetScope: "private"}
	ips := []netip.Addr{netip.MustParseAddr("10.0.0.5")}
	a := safeKitsuClient(model, ips).Transport.(*http.Transport)
	b := safeKitsuClient(model, []netip.Addr{netip.MustParseAddr("10.0.0.5")}).Transport.(*http.Transport)
	if a != b {
		t.Fatal("same scope and pinned IPs must reuse the Transport")
	}
	if a.IdleConnTimeout != safeKitsuTransportIdleTimeout {
		t.Fatalf("IdleConnTimeout = %v", a.IdleConnTimeout)
	}
	if c := safeKitsuClient(KitsuURLModel{TargetScope: "public"}, ips).Transport.(*http.Transport); c == a {
		t.Fatal("different target scope must not share a Transport")
	}
}

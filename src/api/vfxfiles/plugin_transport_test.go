package vfxfiles

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

func TestPinnedPluginDialerRejectsOriginChangeAndUsesPinnedAddresses(t *testing.T) {
	var dialed string
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		dialed = network + " " + address
		return nil, net.ErrClosed
	}
	transport := newPinnedPluginTransport("files.example.test", "443", []netip.Addr{netip.MustParseAddr("8.8.8.8")}, dial)
	if transport.Proxy != nil {
		t.Fatal("Plugin bearer transport must not inherit a proxy")
	}
	if _, err := transport.DialContext(context.Background(), "tcp", "files.example.test:443"); err == nil {
		t.Fatal("test dial unexpectedly succeeded")
	}
	if dialed != "tcp 8.8.8.8:443" {
		t.Fatalf("transport did not use the construction-time pinned address: %q", dialed)
	}
	if _, err := transport.DialContext(context.Background(), "tcp", "other.example.test:443"); err == nil {
		t.Fatal("dialer accepted a different host")
	}
	if _, err := transport.DialContext(context.Background(), "tcp", "files.example.test:444"); err == nil {
		t.Fatal("dialer accepted a different port")
	}
}

func TestNewPluginClientUsesPinnedTransportByDefault(t *testing.T) {
	base := "https://files.example.test" + ServiceAudienceSuffix
	oldLookup := lookupPluginOrigin
	lookupPluginOrigin = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	t.Cleanup(func() { lookupPluginOrigin = oldLookup })
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewPluginClient(base, nil, ServiceTokenConfig{Issuer: "https://sync.example.test", Audience: base, Subject: "sync", ClientID: "instance", KeyID: "k1", PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.DialContext == nil {
		t.Fatal("default Plugin client does not use its direct pinned transport")
	}
}

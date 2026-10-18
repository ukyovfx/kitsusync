package vfxfiles

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPinnedPluginDialerRejectsOriginChangeAndUsesPinnedAddresses(t *testing.T) {
	var dialed string
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		dialed = network + " " + address
		return nil, net.ErrClosed
	}
	transport := newPinnedPluginTransport("files.example.test", "443", []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil, dial)
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

func TestPluginAddressPolicyRejectsPrivateUnlessExactlyPinned(t *testing.T) {
	private := netip.MustParseAddr("172.28.0.5")
	linkLocal := netip.MustParseAddr("169.254.169.254")
	if validPluginAddress(private) {
		t.Fatal("RFC1918 address must not pass the default SSRF policy")
	}
	if validPluginAddress(netip.MustParseAddr("fd00::5")) {
		t.Fatal("IPv6 ULA address must not pass the default SSRF policy")
	}
	if !trustedPrivatePluginAddress(private, []netip.Addr{private}) {
		t.Fatal("exactly pinned private Plugin address was rejected")
	}
	if trustedPrivatePluginAddress(private, []netip.Addr{netip.MustParseAddr("172.28.0.6")}) {
		t.Fatal("private Plugin address was accepted without an exact pin")
	}
	if trustedPrivatePluginAddress(linkLocal, []netip.Addr{linkLocal}) {
		t.Fatal("link-local metadata address was accepted by the private endpoint exception")
	}
}

func TestResolvePluginOriginRequiresExactPrivatePin(t *testing.T) {
	private := netip.MustParseAddr("172.28.0.5")
	oldLookup := lookupPluginOrigin
	lookupPluginOrigin = func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{private}, nil }
	t.Cleanup(func() { lookupPluginOrigin = oldLookup })
	if _, err := resolvePluginOrigin(context.Background(), "files.test", nil); err == nil {
		t.Fatal("untrusted private DNS result was accepted")
	}
	resolved, err := resolvePluginOrigin(context.Background(), "files.test", []netip.Addr{private})
	if err != nil || len(resolved) != 1 || resolved[0] != private {
		t.Fatalf("exact private IP pin did not allow the configured Plugin origin: %v %v", resolved, err)
	}
	if _, err := resolvePluginOrigin(context.Background(), "files.test", []netip.Addr{netip.MustParseAddr("172.28.0.6")}); err == nil {
		t.Fatal("DNS result outside the exact private IP pin was accepted")
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

func TestNewPluginClientAllowsOnlyConfiguredPrivateOriginPin(t *testing.T) {
	base := "https://files.test" + ServiceAudienceSuffix
	private := netip.MustParseAddr("172.28.0.5")
	oldLookup := lookupPluginOrigin
	lookupPluginOrigin = func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{private}, nil }
	t.Cleanup(func() { lookupPluginOrigin = oldLookup })
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	config := ServiceTokenConfig{Issuer: "https://sync.example.test", Audience: base, Subject: "sync", ClientID: "instance", KeyID: "k1", PrivateKey: key}
	if _, err := NewPluginClient(base, nil, config); err == nil {
		t.Fatal("private service origin was accepted without explicit pinning")
	}
	client, err := newPluginClient(base, nil, config, []netip.Addr{private})
	if err != nil {
		t.Fatalf("explicit private service pin rejected: %v", err)
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.DialContext == nil {
		t.Fatal("private Plugin client did not retain pinned direct transport")
	}
}

func TestPluginClientUsesPinnedPrivateHTTPSOriginWithNormalTLSVerification(t *testing.T) {
	serviceIP := privateContainerAddress(t)
	certificate, roots := pluginServiceTestCertificate(t, "files.test")
	var gotPath, gotActor, gotAuthorization string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotActor = r.Header.Get("X-KitsuSync-Actor-ID")
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"contract_version":"1.0","state":"connected"}`))
	}))
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	base := "https://files.test:" + strconv.Itoa(port) + ServiceAudienceSuffix

	oldLookup := lookupPluginOrigin
	lookupPluginOrigin = func(_ context.Context, host string) ([]netip.Addr, error) {
		if host != "files.test" {
			return nil, ErrPluginRequestRejected
		}
		return []netip.Addr{serviceIP}, nil
	}
	t.Cleanup(func() { lookupPluginOrigin = oldLookup })
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	client, err := newPluginClient(base, nil, ServiceTokenConfig{
		Issuer: "https://sync.test", Audience: base, Subject: "sync", ClientID: "instance", KeyID: "k1", PrivateKey: key,
	}, []netip.Addr{serviceIP})
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
	productionID := "123e4567-e89b-42d3-a456-426614174001"
	actorID := "123e4567-e89b-42d3-a456-426614174000"
	response, err := client.Do(context.Background(), PluginRequest{
		Method: http.MethodGet, Path: "/drive-connections", Query: map[string][]string{"production_id": {productionID}},
		ProductionID: productionID, Authorizer: testProductionAdminAuthorizer{actorID: actorID, allowed: true},
		Scope: "vfx.connections.read", Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("pinned private HTTPS service request failed TLS or HTTP: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || gotPath != ServiceAudienceSuffix+"/drive-connections" || gotActor != actorID || !strings.HasPrefix(gotAuthorization, "Bearer ") {
		t.Fatalf("unexpected private HTTPS API request: status=%d path=%q actor=%q has_bearer=%t", response.StatusCode, gotPath, gotActor, strings.HasPrefix(gotAuthorization, "Bearer "))
	}
}

func privateContainerAddress(t *testing.T) netip.Addr {
	t.Helper()
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, raw := range addresses {
			prefix, err := netip.ParsePrefix(raw.String())
			if err != nil {
				continue
			}
			address := prefix.Addr().Unmap()
			if address.Is4() && address.IsPrivate() && !address.IsLoopback() {
				return address
			}
		}
	}
	t.Fatal("isolated test container has no private IPv4 interface")
	return netip.Addr{}
}

func pluginServiceTestCertificate(t *testing.T, dnsName string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	now := time.Now().UTC()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic Plugin API Test CA"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: dnsName},
		DNSNames: []string{dnsName}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})...)
	keyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})) {
		t.Fatal("could not install synthetic CA in test trust pool")
	}
	return certificate, roots
}

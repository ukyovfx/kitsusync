package request

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func startRequestFixture(t *testing.T, ip string, handler http.Handler) (port int, hits *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Fatalf("listen on %s: %v", ip, err)
	}
	server := &http.Server{Handler: handler}
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	hits = &atomic.Int32{}
	go func() { _ = server.Serve(listener) }()
	return listener.Addr().(*net.TCPAddr).Port, hits
}

func TestDoWithErrorDoesNotFollowRedirectsOrReplayBearerToken(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			redirectTargetHit := false
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirectTargetHit = true
				if r.Header.Get("Authorization") != "" {
					t.Error("bearer token reached redirect target")
				}
			}))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, status)
			}))
			defer source.Close()
			if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: source.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
				t.Fatal(err)
			}

			if _, err := DoWithError("synthetic-secret", http.MethodGet, source.URL, nil, nil); err == nil {
				t.Fatal("redirect was accepted")
			}
			if redirectTargetHit {
				t.Fatal("redirect target was requested")
			}
		})
	}
}

func TestDoWithErrorUsesPinnedIPWithoutChangingHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "127.0.0.1" || r.Host == "" {
			t.Fatalf("pinned dial replaced Host header: %q", r.Host)
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("credential missing at verified origin")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	verified := "http://verified.kitsu.test:" + u.Port()
	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: verified, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := DoWithError("synthetic-token", http.MethodGet, verified+"/api/data/tasks", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDoWithErrorDoesNotFollowDNSReplacementOrSendBearerToReplacementIP(t *testing.T) {
	var pinnedHits, replacementHits atomic.Int32
	var pinnedBearer atomic.Value
	port, _ := startRequestFixture(t, "127.0.0.1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pinnedHits.Add(1)
		pinnedBearer.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	}))
	_, _ = startRequestFixture(t, "127.0.0.2", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replacementHits.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Errorf("bearer token reached replacement IP")
		}
	}))
	origin := "http://dns-rebind.kitsu.test:" + strconv.Itoa(port)
	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: origin, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := DoWithError("replacement-canary-token", http.MethodGet, origin+"/api/data/tasks", nil, nil); err != nil {
		t.Fatalf("pinned request failed: %v", err)
	}
	if pinnedHits.Load() != 1 || replacementHits.Load() != 0 {
		t.Fatalf("unexpected fixture hits: pinned=%d replacement=%d", pinnedHits.Load(), replacementHits.Load())
	}
	if got, _ := pinnedBearer.Load().(string); got != "Bearer replacement-canary-token" {
		t.Fatalf("pinned authority did not receive the expected bearer: %q", got)
	}
}

func TestDoWithErrorAcceptsPinnedPrivateAddress(t *testing.T) {
	var hits atomic.Int32
	port, _ := startRequestFixture(t, "127.0.0.1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer private-canary-token" {
			t.Errorf("private pinned request lost bearer token")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	origin := "http://100.114.77.117:" + strconv.Itoa(port)
	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: origin, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := DoWithError("private-canary-token", http.MethodGet, origin+"/api/data/tasks", nil, nil); err != nil {
		t.Fatalf("pinned private authority failed: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected one private authority request, got %d", hits.Load())
	}
}

func TestDoWithErrorUsesVerifiedHostnameForTLSServerNameWhenDialingPinnedIP(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "verified.kitsu.test"},
		DNSNames:     []string{"verified.kitsu.test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	var observedSNI atomic.Value
	config := &tls.Config{Certificates: []tls.Certificate{certificate}}
	config.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		observedSNI.Store(hello.ServerName)
		return config, nil
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", config)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = conn.(*tls.Conn).Handshake()
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	origin := "https://verified.kitsu.test:" + strconv.Itoa(port)
	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: origin, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
	result := attemptOnce("tls-sni-canary-token", http.MethodGet, origin+"/api/data/tasks", nil, nil)
	if result.err == nil {
		t.Fatal("expected local self-signed certificate verification failure")
	}
	if got, _ := observedSNI.Load().(string); got != "verified.kitsu.test" {
		t.Fatalf("TLS ServerName = %q, want verified hostname", got)
	}
}

func TestDoWithErrorRejectsConfiguredOriginMismatchBeforeDial(t *testing.T) {
	var hits atomic.Int32
	port, _ := startRequestFixture(t, "127.0.0.1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	origin := "http://trusted.kitsu.test:" + strconv.Itoa(port)
	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: origin, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := DoWithError("override-canary-token", http.MethodGet, "http://attacker.kitsu.test:"+strconv.Itoa(port)+"/api/data/tasks", nil, nil); err == nil {
		t.Fatal("untrusted configured target was accepted")
	}
	if hits.Load() != 0 {
		t.Fatal("origin mismatch was dialed")
	}
}

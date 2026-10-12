package vfxfiles

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestIssueServiceTokenUsesBoundRS256AccessTokenProfile(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	config := ServiceTokenConfig{
		Issuer: "https://sync.example.test", Audience: "https://zou.example.test/api/plugins/vfx_files/integrations/v1",
		Subject: "kitsusync-service", ClientID: "installation-test", KeyID: "key-2026-10", PrivateKey: key,
	}
	actor := mustAuthorizeTestActor(t, "123e4567-e89b-42d3-a456-426614174001")
	now := time.Date(2026, 10, 12, 3, 4, 5, 0, time.UTC)
	token, err := IssueServiceToken(config, actor, []string{"vfx.config.write", "vfx.connections.read", "vfx.config.write"}, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT compact serialization has %d segments", len(parts))
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]any
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "RS256" || header["typ"] != "at+JWT" || header["kid"] != config.KeyID {
		t.Fatalf("unexpected protected header: %#v", header)
	}
	claimBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimBytes, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != config.Issuer || claims["aud"] != config.Audience || claims["sub"] != config.Subject || claims["client_id"] != config.ClientID || claims["production_id"] != actor.productionID || claims["actor_id"] != actor.actorID || claims["actor_role"] != "production_admin" {
		t.Fatalf("token is missing a required binding: %#v", claims)
	}
	if claims["scope"] != "vfx.config.write vfx.connections.read" || claims["exp"].(float64)-claims["iat"].(float64) != ServiceTokenMaxLifetime.Seconds() {
		t.Fatalf("token scope or lifetime is not canonical: %#v", claims)
	}
	if claims["jti"] == "" || claims["nbf"] != claims["iat"] {
		t.Fatalf("token replay/time claims missing: %#v", claims)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("service access token signature did not verify: %v", err)
	}
}

func TestIssueServiceTokenFailsClosedForUnverifiedActorScopeOrAudience(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	config := ServiceTokenConfig{Issuer: "https://sync.example.test", Audience: "https://zou.example.test/api/plugins/vfx_files/integrations/v1", Subject: "sync", ClientID: "instance", KeyID: "k1", PrivateKey: key}
	actor := mustAuthorizeTestActor(t, "123e4567-e89b-42d3-a456-426614174001")
	tests := []struct {
		name   string
		config ServiceTokenConfig
		actor  AuthorizedProductionActor
		scopes []string
	}{
		{name: "actor was not authorized", config: config, scopes: []string{"vfx.config.read"}},
		{name: "unknown scope", config: config, actor: actor, scopes: []string{"admin"}},
		{name: "non HTTPS issuer", config: ServiceTokenConfig{Issuer: "http://sync.example.test", Audience: config.Audience, Subject: config.Subject, ClientID: config.ClientID, KeyID: config.KeyID, PrivateKey: key}, actor: actor, scopes: []string{"vfx.config.read"}},
		{name: "wrong route audience", config: ServiceTokenConfig{Issuer: config.Issuer, Audience: "https://zou.example.test/api/integrations/v1", Subject: config.Subject, ClientID: config.ClientID, KeyID: config.KeyID, PrivateKey: key}, actor: actor, scopes: []string{"vfx.config.read"}},
		{name: "weak signing key", config: ServiceTokenConfig{Issuer: config.Issuer, Audience: config.Audience, Subject: config.Subject, ClientID: config.ClientID, KeyID: config.KeyID, PrivateKey: &rsa.PrivateKey{PublicKey: rsa.PublicKey{N: bigIntWithBits(1024), E: 65537}}}, actor: actor, scopes: []string{"vfx.config.read"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if token, err := IssueServiceToken(tc.config, tc.actor, tc.scopes, time.Now()); err == nil || token != "" {
				t.Fatal("invalid token request was accepted")
			}
		})
	}
}

func TestIssueServiceTokenAllowsCanonicalSnapshotScopeOnlyFromAllowlist(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	config := ServiceTokenConfig{Issuer: "https://sync.example.test", Audience: "https://zou.example.test/api/plugins/vfx_files/integrations/v1", Subject: "sync", ClientID: "instance", KeyID: "k1", PrivateKey: key}
	actor := mustAuthorizeTestActor(t, "123e4567-e89b-42d3-a456-426614174001")
	if _, err := IssueServiceToken(config, actor, []string{"vfx.sync.snapshot.write"}, time.Now()); err != nil {
		t.Fatalf("canonical snapshot scope was rejected: %v", err)
	}
	if _, err := IssueServiceToken(config, actor, []string{"vfx.sync.snapshot.delete"}, time.Now()); err == nil {
		t.Fatal("unlisted four-component scope was accepted")
	}
}

type testProductionAdminAuthorizer struct {
	actorID string
	allowed bool
	err     error
}

func (a testProductionAdminAuthorizer) AuthorizeProductionAdmin(context.Context, string) (string, bool, error) {
	return a.actorID, a.allowed, a.err
}

func mustAuthorizeTestActor(t *testing.T, productionID string) AuthorizedProductionActor {
	t.Helper()
	actor, err := AuthorizeProductionActor(context.Background(), testProductionAdminAuthorizer{actorID: "123e4567-e89b-42d3-a456-426614174000", allowed: true}, productionID)
	if err != nil {
		t.Fatal("synthetic Production administrator was not authorized")
	}
	return actor
}

func TestProductionActorAuthorizationFailsClosed(t *testing.T) {
	productionID := "123e4567-e89b-42d3-a456-426614174001"
	for _, authorizer := range []ProductionAdminAuthorizer{
		testProductionAdminAuthorizer{actorID: "123e4567-e89b-42d3-a456-426614174000", allowed: false},
		testProductionAdminAuthorizer{actorID: "not-a-uuid", allowed: true},
		testProductionAdminAuthorizer{actorID: "123e4567-e89b-42d3-a456-426614174000", allowed: true, err: errors.New("authorization backend unavailable")},
	} {
		if actor, err := AuthorizeProductionActor(context.Background(), authorizer, productionID); err == nil || actor != (AuthorizedProductionActor{}) {
			t.Fatal("denied or indeterminate Production authorization was accepted")
		}
	}
	if _, err := AuthorizeProductionActor(context.Background(), testProductionAdminAuthorizer{actorID: "123e4567-e89b-42d3-a456-426614174000", allowed: true}, "not-a-uuid"); err == nil {
		t.Fatal("invalid Production ID was accepted")
	}
}

func TestJWKSHandlerPublishesOnlyBoundedPublicSigningKeys(t *testing.T) {
	oldKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	handler := JWKSHandler(func() ([]VerificationKey, error) {
		return []VerificationKey{{KeyID: "old", PublicKey: &oldKey.PublicKey}, {KeyID: "current", PublicKey: &newKey.PublicKey}}, nil
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, ServiceJWKSPath, nil))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "public, max-age=300, must-revalidate" || response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected public key endpoint behavior: status=%d headers=%v", response.Code, response.Header())
	}
	var document jwksDocument
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Keys) != 2 || document.Keys[0].KeyID != "current" || document.Keys[1].KeyID != "old" {
		t.Fatalf("overlap key set was not sorted/stable: %#v", document.Keys)
	}
	for _, key := range document.Keys {
		if key.Alg != "RS256" || key.KeyType != "RSA" || key.Use != "sig" || key.Modulus == "" || key.Exponent == "" || strings.Contains(response.Body.String(), "PRIVATE KEY") {
			t.Fatalf("JWK contains invalid or private key material: %#v", key)
		}
	}
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, ServiceJWKSPath, nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("JWKS must be read-only, got %d", post.Code)
	}
}

func TestLoadServiceVerificationKeysSupportsExplicitRotationOverlap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the current Windows filesystem does not provide the Unix permission proof required for trusted verification-key storage")
	}
	oldKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	currentKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	for kid, key := range map[string]*rsa.PrivateKey{"old": oldKey, "current": currentKey} {
		encoded, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, kid+".pem")
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := LoadServiceVerificationKeys(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected active and overlap public keys, got %d", len(keys))
	}
	if _, err := EncodeVerificationKeySet(keys); err != nil {
		t.Fatalf("rotation overlap could not be served as JWKS: %v", err)
	}
}

func TestLoadRS256PrivateKeyRejectsBroadPermissionsAndSymlinks(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "signing.pem")
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if _, err := LoadRS256PrivateKey(privatePath); err == nil {
			t.Fatal("key loading must fail closed when the current platform cannot prove restrictive file permissions")
		}
		return
	}
	if loaded, err := LoadRS256PrivateKey(privatePath); err != nil || loaded.N.Cmp(key.N) != 0 {
		t.Fatalf("protected PKCS8 key could not be loaded: err=%v", err)
	}
	if err := os.Chmod(privatePath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRS256PrivateKey(privatePath); err == nil {
		t.Fatal("group/world-readable signing key was accepted")
	}
	linkPath := filepath.Join(dir, "signing-link.pem")
	if err := os.Symlink(privatePath, linkPath); err != nil {
		t.Skipf("symlink creation unavailable on this runner: %v", err)
	}
	if _, err := LoadRS256PrivateKey(linkPath); err == nil {
		t.Fatal("symlink signing key was accepted")
	}
}

func bigIntWithBits(bits int) *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
}

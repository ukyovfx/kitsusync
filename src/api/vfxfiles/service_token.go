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
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	ServiceTokenMaxLifetime = 5 * time.Minute
	ServiceTokenClockSkew   = 30 * time.Second
	ServiceAudienceSuffix   = "/api/plugins/vfx_files/integrations/v1"
	ServiceJWKSPath         = "/.well-known/vfx-files-jwks.json"
)

var (
	serviceKeyIDPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	serviceUUIDPattern   = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	serviceScopePattern  = regexp.MustCompile(`^vfx\.[a-z]+\.[a-z]+$`)
	allowedServiceScopes = map[string]struct{}{
		"vfx.connections.read": {}, "vfx.connections.authorize": {}, "vfx.connections.revoke": {},
		"vfx.roots.read": {}, "vfx.roots.verify": {}, "vfx.templates.read": {},
		"vfx.config.read": {}, "vfx.config.write": {}, "vfx.activation.write": {},
		"vfx.sync.preview": {}, "vfx.sync.apply": {}, "vfx.sync.read": {},
		"vfx.sync.retry": {}, "vfx.sync.reconcile": {}, "vfx.sync.snapshot.write": {},
	}
)

var (
	ErrServiceTokenDisabled = errors.New("vfx files service token issuer is not configured")
	ErrInvalidServiceToken  = errors.New("invalid vfx files service token request")
)

// ServiceTokenConfig is operator-managed process configuration. The private
// key must be loaded from a protected secret file, never from a browser or DB.
type ServiceTokenConfig struct {
	Issuer     string
	Audience   string
	Subject    string
	ClientID   string
	KeyID      string
	PrivateKey *rsa.PrivateKey
}

// ProductionAdminAuthorizer adapts the authenticated KitsuSync session to the
// exact Production being accessed. Implementations must derive actor identity
// from the server-side session and check the current Production-admin grant.
type ProductionAdminAuthorizer interface {
	AuthorizeProductionAdmin(ctx context.Context, productionID string) (actorID string, allowed bool, err error)
}

// AuthorizedProductionActor is an unforgeable in-process authorization result.
// Its fields are private so callers cannot mint a token by setting a role flag.
type AuthorizedProductionActor struct {
	actorID      string
	productionID string
}

// AuthorizeProductionActor performs a fresh permission check for one request.
func AuthorizeProductionActor(ctx context.Context, authorizer ProductionAdminAuthorizer, productionID string) (AuthorizedProductionActor, error) {
	if ctx == nil || authorizer == nil || !serviceUUIDPattern.MatchString(productionID) {
		return AuthorizedProductionActor{}, ErrInvalidServiceToken
	}
	actorID, allowed, err := authorizer.AuthorizeProductionAdmin(ctx, productionID)
	if err != nil || !allowed || !serviceUUIDPattern.MatchString(actorID) {
		return AuthorizedProductionActor{}, ErrInvalidServiceToken
	}
	return AuthorizedProductionActor{actorID: actorID, productionID: productionID}, nil
}

// IssueServiceToken mints an RFC 9068-style RS256 JWT bound to one authorized
// actor and one Production. Tokens are short-lived and carry an exact scope set.
func IssueServiceToken(config ServiceTokenConfig, actor AuthorizedProductionActor, scopes []string, now time.Time) (string, error) {
	if err := validateServiceTokenConfig(config); err != nil || !serviceUUIDPattern.MatchString(actor.actorID) || !serviceUUIDPattern.MatchString(actor.productionID) {
		return "", ErrInvalidServiceToken
	}
	if len(scopes) == 0 {
		return "", ErrInvalidServiceToken
	}
	scopeSet := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if !serviceScopePattern.MatchString(scope) {
			return "", ErrInvalidServiceToken
		}
		if _, ok := allowedServiceScopes[scope]; !ok {
			return "", ErrInvalidServiceToken
		}
		scopeSet[scope] = struct{}{}
	}
	orderedScopes := make([]string, 0, len(scopeSet))
	for scope := range scopeSet {
		orderedScopes = append(orderedScopes, scope)
	}
	sort.Strings(orderedScopes)
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	issuedAt := now.Unix()
	expiresAt := now.Add(ServiceTokenMaxLifetime).Unix()
	jti, err := newServiceJTI()
	if err != nil {
		return "", fmt.Errorf("generate service token identity: %w", err)
	}
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "at+JWT", "kid": config.KeyID})
	if err != nil {
		return "", ErrInvalidServiceToken
	}
	claims, err := json.Marshal(map[string]any{
		"iss": config.Issuer, "aud": config.Audience, "sub": config.Subject,
		"client_id": config.ClientID, "iat": issuedAt, "nbf": issuedAt,
		"exp": expiresAt, "jti": jti, "scope": strings.Join(orderedScopes, " "),
		"production_id": actor.productionID, "actor_id": actor.actorID,
		"actor_role": "production_admin",
	})
	if err != nil {
		return "", ErrInvalidServiceToken
	}
	encoded := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(encoded))
	signature, err := rsa.SignPKCS1v15(rand.Reader, config.PrivateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign service token: %w", err)
	}
	return encoded + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func validateServiceTokenConfig(config ServiceTokenConfig) error {
	if config.PrivateKey == nil || config.PrivateKey.N == nil || config.PrivateKey.N.BitLen() < 2048 || !serviceKeyIDPattern.MatchString(config.KeyID) {
		return ErrServiceTokenDisabled
	}
	issuer, err := url.Parse(config.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" || (issuer.Path != "" && issuer.Path != "/") {
		return ErrServiceTokenDisabled
	}
	audience, err := url.Parse(config.Audience)
	if err != nil || audience.Scheme != "https" || audience.Host == "" || audience.User != nil || audience.RawQuery != "" || audience.Fragment != "" || audience.Path != ServiceAudienceSuffix || audience.RawPath != "" {
		return ErrServiceTokenDisabled
	}
	if strings.TrimSpace(config.Subject) == "" || strings.TrimSpace(config.ClientID) == "" {
		return ErrServiceTokenDisabled
	}
	return nil
}

func newServiceJTI() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
	return encoded, nil
}

// LoadRS256PrivateKey reads a PEM RSA signing key from an operator-provisioned
// regular file. Symlinks and group/world-readable files are rejected.
func LoadRS256PrivateKey(path string) (*rsa.PrivateKey, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrServiceTokenDisabled
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, ErrServiceTokenDisabled
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, ErrServiceTokenDisabled
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrServiceTokenDisabled
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, ErrServiceTokenDisabled
	}
	var key *rsa.PrivateKey
	if parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes); parseErr == nil {
		key, _ = parsed.(*rsa.PrivateKey)
	} else if parsed, parseErr := x509.ParsePKCS1PrivateKey(block.Bytes); parseErr == nil {
		key = parsed
	}
	if key == nil || key.N == nil || key.N.BitLen() < 2048 {
		return nil, ErrServiceTokenDisabled
	}
	return key, nil
}

// LoadServiceVerificationKeys reads the current and overlap public keys from
// one operator-managed directory. Each file is named <kid>.pem; key ids are
// validated and at most eight keys may be published during rotation.
func LoadServiceVerificationKeys(directory string) ([]VerificationKey, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, ErrServiceTokenDisabled
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return nil, ErrServiceTokenDisabled
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) == 0 || len(entries) > 8 {
		return nil, ErrServiceTokenDisabled
	}
	keys := make([]VerificationKey, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".pem" {
			return nil, ErrServiceTokenDisabled
		}
		keyID := strings.TrimSuffix(entry.Name(), ".pem")
		if !serviceKeyIDPattern.MatchString(keyID) {
			return nil, ErrServiceTokenDisabled
		}
		path := filepath.Join(directory, entry.Name())
		fileInfo, statErr := os.Lstat(path)
		if statErr != nil || !fileInfo.Mode().IsRegular() || fileInfo.Mode()&os.ModeSymlink != 0 || fileInfo.Mode().Perm()&0o022 != 0 {
			return nil, ErrServiceTokenDisabled
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, ErrServiceTokenDisabled
		}
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, ErrServiceTokenDisabled
		}
		publicKey, parseErr := x509.ParsePKIXPublicKey(block.Bytes)
		if parseErr != nil {
			publicKey, parseErr = x509.ParsePKCS1PublicKey(block.Bytes)
		}
		rsaKey, ok := publicKey.(*rsa.PublicKey)
		if parseErr != nil || !ok || rsaKey.N == nil || rsaKey.N.BitLen() < 2048 {
			return nil, ErrServiceTokenDisabled
		}
		keys = append(keys, VerificationKey{KeyID: keyID, PublicKey: rsaKey})
	}
	if _, err := EncodeVerificationKeySet(keys); err != nil {
		return nil, err
	}
	return keys, nil
}

type VerificationKey struct {
	KeyID     string
	PublicKey *rsa.PublicKey
}

type jwk struct {
	KeyType  string `json:"kty"`
	Use      string `json:"use"`
	Alg      string `json:"alg"`
	KeyID    string `json:"kid"`
	Modulus  string `json:"n"`
	Exponent string `json:"e"`
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

// EncodeVerificationKeySet exports only public keys for the Plugin's fixed
// JWKS discovery path. Retain overlapping public keys while old tokens expire.
func EncodeVerificationKeySet(keys []VerificationKey) ([]byte, error) {
	if len(keys) == 0 || len(keys) > 8 {
		return nil, ErrServiceTokenDisabled
	}
	seen := make(map[string]struct{}, len(keys))
	result := jwksDocument{Keys: make([]jwk, 0, len(keys))}
	for _, key := range keys {
		if !serviceKeyIDPattern.MatchString(key.KeyID) || key.PublicKey == nil || key.PublicKey.N == nil || key.PublicKey.N.BitLen() < 2048 {
			return nil, ErrServiceTokenDisabled
		}
		if _, duplicate := seen[key.KeyID]; duplicate {
			return nil, ErrServiceTokenDisabled
		}
		seen[key.KeyID] = struct{}{}
		exponent := make([]byte, 0, 4)
		for value := key.PublicKey.E; value > 0; value >>= 8 {
			exponent = append([]byte{byte(value)}, exponent...)
		}
		result.Keys = append(result.Keys, jwk{
			KeyType: "RSA", Use: "sig", Alg: "RS256", KeyID: key.KeyID,
			Modulus:  base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			Exponent: base64.RawURLEncoding.EncodeToString(exponent),
		})
	}
	sort.Slice(result.Keys, func(i, j int) bool { return result.Keys[i].KeyID < result.Keys[j].KeyID })
	return json.Marshal(result)
}

// JWKSHandler serves public signing keys only. It deliberately has no CORS
// policy and does not serve token issuance or management API operations.
func JWKSHandler(keys func() ([]VerificationKey, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if keys == nil {
			http.Error(w, "service keys unavailable", http.StatusServiceUnavailable)
			return
		}
		active, err := keys()
		body, encodeErr := EncodeVerificationKeySet(active)
		if err != nil || encodeErr != nil {
			http.Error(w, "service keys unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/jwk-set+json")
		w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

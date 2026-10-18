package vfxfiles

import (
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"os"
	"strings"
)

const (
	apiBaseURLEnv        = "KITSUSYNC_VFX_FILES_API_URL"
	issuerEnv            = "KITSUSYNC_VFX_FILES_ISSUER"
	subjectEnv           = "KITSUSYNC_VFX_FILES_SUBJECT"
	clientIDEnv          = "KITSUSYNC_VFX_FILES_CLIENT_ID"
	keyIDEnv             = "KITSUSYNC_VFX_FILES_KEY_ID"
	signingKeyPathEnv    = "KITSUSYNC_VFX_FILES_SIGNING_KEY_PATH"
	verificationDirEnv   = "KITSUSYNC_VFX_FILES_JWKS_DIR"
	eventHMACKeysEnv     = "KITSUSYNC_VFX_FILES_EVENT_HMAC_KEYS"
	trustedPrivateIPsEnv = "KITSUSYNC_VFX_FILES_TRUSTED_PRIVATE_IPS"
)

var keyIDRE = serviceKeyIDPattern

// RuntimeConfig contains the server-side Plugin client and the independent
// Plugin-to-KitsuSync event trust. It contains no Google credential or token.
type RuntimeConfig struct {
	Client             *PluginClient
	Token              ServiceTokenConfig
	VerificationKeys   []VerificationKey
	VerificationKeyDir string
	EventKeys          map[string][]byte
}

// LoadRuntimeConfig treats an absent configuration as disabled. Partial or
// inconsistent configuration fails closed; it never falls back to an HMAC
// service token or an unscoped API client.
func LoadRuntimeConfig(getenv func(string) string, httpClient *http.Client) (*RuntimeConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	base := strings.TrimSpace(getenv(apiBaseURLEnv))
	issuer := strings.TrimSpace(getenv(issuerEnv))
	subject := strings.TrimSpace(getenv(subjectEnv))
	clientID := strings.TrimSpace(getenv(clientIDEnv))
	keyID := strings.TrimSpace(getenv(keyIDEnv))
	privatePath := strings.TrimSpace(getenv(signingKeyPathEnv))
	verificationDir := strings.TrimSpace(getenv(verificationDirEnv))
	eventRaw := strings.TrimSpace(getenv(eventHMACKeysEnv))
	trustedPrivateRaw := strings.TrimSpace(getenv(trustedPrivateIPsEnv))
	values := []string{base, issuer, subject, clientID, keyID, privatePath, verificationDir, eventRaw}
	configured := trustedPrivateRaw != ""
	for _, value := range values {
		configured = configured || value != ""
	}
	if !configured {
		return nil, nil
	}
	for _, value := range values {
		if value == "" {
			return nil, errors.New("VFX Files integration configuration is incomplete")
		}
	}
	privateKey, err := LoadRS256PrivateKey(privatePath)
	if err != nil {
		return nil, errors.New("VFX Files signing key is unavailable or unsafe")
	}
	verificationKeys, err := LoadServiceVerificationKeys(verificationDir)
	if err != nil {
		return nil, errors.New("VFX Files verification key set is unavailable or unsafe")
	}
	if !verificationKeyMatches(verificationKeys, keyID, &privateKey.PublicKey) {
		return nil, errors.New("VFX Files signing key does not match the published key set")
	}
	var encodedKeys map[string]string
	if err := json.Unmarshal([]byte(eventRaw), &encodedKeys); err != nil || len(encodedKeys) == 0 || len(encodedKeys) > 16 {
		return nil, errors.New("VFX Files event key configuration is invalid")
	}
	eventKeys := make(map[string][]byte, len(encodedKeys))
	for id, encoded := range encodedKeys {
		key, decodeErr := hex.DecodeString(encoded)
		if !keyIDRE.MatchString(id) || decodeErr != nil || len(key) < 32 {
			return nil, errors.New("VFX Files event key configuration is invalid")
		}
		eventKeys[id] = key
	}
	trustedPrivate, err := parseTrustedPrivatePluginIPs(trustedPrivateRaw)
	if err != nil {
		return nil, errors.New("VFX Files trusted private endpoint configuration is invalid")
	}
	token := ServiceTokenConfig{Issuer: issuer, Audience: base, Subject: subject, ClientID: clientID, KeyID: keyID, PrivateKey: privateKey}
	client, err := newPluginClient(base, httpClient, token, trustedPrivate)
	if err != nil {
		return nil, errors.New("VFX Files Plugin endpoint or service identity is invalid")
	}
	return &RuntimeConfig{Client: client, Token: token, VerificationKeys: verificationKeys, VerificationKeyDir: verificationDir, EventKeys: eventKeys}, nil
}

func parseTrustedPrivatePluginIPs(raw string) ([]netip.Addr, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 16 {
		return nil, errors.New("too many trusted endpoint addresses")
	}
	addresses := make([]netip.Addr, 0, len(parts))
	seen := make(map[netip.Addr]struct{}, len(parts))
	for _, part := range parts {
		address, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			return nil, errors.New("invalid trusted endpoint address")
		}
		address = address.Unmap()
		if !address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
			return nil, errors.New("trusted endpoint address is not an allowed private address")
		}
		if _, exists := seen[address]; exists {
			return nil, errors.New("duplicate trusted endpoint address")
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	return addresses, nil
}

// ManagementConfigured reports whether the server-side Plugin API has been
// configured. The application validates the complete identity/key set during
// startup; this is only a presentation gate and never represents Plugin health.
func ManagementConfigured() bool {
	return strings.TrimSpace(os.Getenv(apiBaseURLEnv)) != ""
}

func verificationKeyMatches(keys []VerificationKey, keyID string, publicKey *rsa.PublicKey) bool {
	if publicKey == nil {
		return false
	}
	for _, candidate := range keys {
		if candidate.KeyID == keyID && candidate.PublicKey != nil && candidate.PublicKey.E == publicKey.E && candidate.PublicKey.N.Cmp(publicKey.N) == 0 {
			return true
		}
	}
	return false
}

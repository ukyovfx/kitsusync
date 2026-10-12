package vfxfiles

import (
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
)

const (
	apiBaseURLEnv      = "KITSUSYNC_VFX_FILES_API_URL"
	issuerEnv          = "KITSUSYNC_VFX_FILES_ISSUER"
	subjectEnv         = "KITSUSYNC_VFX_FILES_SUBJECT"
	clientIDEnv        = "KITSUSYNC_VFX_FILES_CLIENT_ID"
	keyIDEnv           = "KITSUSYNC_VFX_FILES_KEY_ID"
	signingKeyPathEnv  = "KITSUSYNC_VFX_FILES_SIGNING_KEY_PATH"
	verificationDirEnv = "KITSUSYNC_VFX_FILES_JWKS_DIR"
	eventHMACKeysEnv   = "KITSUSYNC_VFX_FILES_EVENT_HMAC_KEYS"
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
	values := []string{base, issuer, subject, clientID, keyID, privatePath, verificationDir, eventRaw}
	configured := false
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
	token := ServiceTokenConfig{Issuer: issuer, Audience: base, Subject: subject, ClientID: clientID, KeyID: keyID, PrivateKey: privateKey}
	client, err := NewPluginClient(base, httpClient, token)
	if err != nil {
		return nil, errors.New("VFX Files Plugin endpoint or service identity is invalid")
	}
	return &RuntimeConfig{Client: client, Token: token, VerificationKeys: verificationKeys, VerificationKeyDir: verificationDir, EventKeys: eventKeys}, nil
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

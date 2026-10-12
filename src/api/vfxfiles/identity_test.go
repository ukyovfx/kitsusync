package vfxfiles

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeConfigAbsentOrPartialFailsClosed(t *testing.T) {
	values := map[string]string{}
	get := func(name string) string { return values[name] }
	if cfg, err := LoadRuntimeConfig(get, nil); cfg != nil || err != nil {
		t.Fatalf("empty optional configuration = %#v, %v", cfg, err)
	}
	values[apiBaseURLEnv] = "https://plugin.example.invalid" + ServiceAudienceSuffix
	if cfg, err := LoadRuntimeConfig(get, nil); cfg != nil || err == nil {
		t.Fatal("partial configuration did not fail closed")
	}
	// Legacy shared bearer and email-derived actor configuration is not an
	// accepted fallback to the RS256 actor/Production-bound contract.
	values["KITSUSYNC_VFX_FILES_SERVICE_TOKEN"] = strings.Repeat("s", 32)
	values["KITSUSYNC_VFX_FILES_ACTOR_HMAC_KEY"] = strings.Repeat("a", 64)
	if _, err := LoadRuntimeConfig(get, nil); err == nil {
		t.Fatal("legacy bearer identity configuration was accepted")
	}
}

func TestRuntimeConfigRequiresSigningKeyToBePublishedForExactKeyID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("current Windows filesystem cannot prove the restrictive key permissions required by the runtime contract")
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	privatePath := filepath.Join(directory, "signing.pem")
	privateBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateBytes}), 0o600); err != nil {
		t.Fatal(err)
	}
	publicDirectory := filepath.Join(directory, "jwks")
	if err := os.Mkdir(publicDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	publicBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDirectory, "active.pem"), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicBytes}), 0o600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		apiBaseURLEnv: "https://plugin.example.invalid" + ServiceAudienceSuffix,
		issuerEnv:     "https://kitsusync.example.invalid", subjectEnv: "kitsusync", clientIDEnv: "instance-1",
		keyIDEnv: "active", signingKeyPathEnv: privatePath, verificationDirEnv: publicDirectory,
		eventHMACKeysEnv: `{"events-1":"` + hex.EncodeToString([]byte(strings.Repeat("e", 32))) + `"}`,
	}
	get := func(name string) string { return values[name] }
	client := &http.Client{Transport: http.DefaultTransport}
	cfg, err := LoadRuntimeConfig(get, client)
	if err != nil || cfg == nil || cfg.Client == nil || len(cfg.EventKeys["events-1"]) != 32 || len(cfg.VerificationKeys) != 1 {
		t.Fatalf("valid paired runtime keys were rejected: cfg=%#v err=%v", cfg, err)
	}
	values[keyIDEnv] = "not-published"
	if cfg, err := LoadRuntimeConfig(get, client); cfg != nil || err == nil {
		t.Fatal("signing with a kid absent from published JWKS was accepted")
	}
}

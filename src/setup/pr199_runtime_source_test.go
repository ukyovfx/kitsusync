package setup

import (
	"app/src/model"
	"path/filepath"
	"testing"
)

func TestPR199RuntimeKitsuDataSourcePrefersPersistedToken(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	t.Setenv("KitsuJWTToken", "stale-env-token")
	db := newRuntimeCredentialTestDB(t)
	model.SetSetting(db, KitsuAPIBaseURLSettingKey, "https://kitsu.example.test/api")
	if err := setRuntimeKitsuToken(db, "persisted-runtime-token"); err != nil {
		t.Fatal(err)
	}

	base, token, ok := runtimeKitsuDataSource(db)
	if !ok || base != "https://kitsu.example.test/api" || token != "persisted-runtime-token" {
		t.Fatalf("base=%q token=%q ok=%t", base, token, ok)
	}
}

func TestPR199RuntimeKitsuDataSourceFailsClosedForUnreadablePersistedToken(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	t.Setenv("KitsuJWTToken", "stale-env-token")
	db := newRuntimeCredentialTestDB(t)
	model.SetSetting(db, KitsuAPIBaseURLSettingKey, "https://kitsu.example.test/api")
	if err := model.SetSecretSettingWithError(db, RuntimeKitsuTokenSettingKey, "v1:not-decryptable"); err != nil {
		t.Fatal(err)
	}

	base, token, ok := runtimeKitsuDataSource(db)
	if ok || base != "" || token != "" {
		t.Fatalf("unreadable persisted token did not fail closed: base=%q token_present=%t ok=%t", base, token != "", ok)
	}
}

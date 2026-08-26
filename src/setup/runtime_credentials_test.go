package setup

import (
	"path/filepath"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRuntimeKitsuPasswordIsEncryptedAndReloadable(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	t.Setenv(RuntimeKitsuPasswordEnv, "")
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}

	const password = "local-runtime-password"
	if err := setRuntimeKitsuPassword(db, password); err != nil {
		t.Fatalf("store runtime password: %v", err)
	}
	stored := model.GetSetting(db, RuntimeKitsuPasswordSettingKey)
	if stored == "" || strings.Contains(stored, password) {
		t.Fatal("runtime password was not stored as encrypted data")
	}
	t.Setenv(RuntimeKitsuPasswordEnv, "")
	if got := StoredRuntimeKitsuPassword(db); got != password {
		t.Fatal("encrypted runtime password did not survive reload")
	}
}

func TestInspectRuntimeKitsuCredentialReportsMissingKeyWithoutSecrets(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "missing-runtime-secret.key"))
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeKitsuEmailSettingKey, "manager@example.invalid")
	if err := model.SetSecretSettingWithError(db, RuntimeKitsuPasswordSettingKey, "v1:stored-ciphertext"); err != nil {
		t.Fatal(err)
	}

	state := InspectRuntimeKitsuCredential(db)
	if !state.EmailPresent || !state.CiphertextPresent || state.Decryptable || state.ErrorClass != "key_unavailable" {
		t.Fatalf("unexpected safe credential state: %+v", state)
	}
}

func TestInspectRuntimeKitsuCredentialReportsDecryptableState(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeKitsuEmailSettingKey, "manager@example.invalid")
	if err := setRuntimeKitsuPassword(db, "runtime-password"); err != nil {
		t.Fatal(err)
	}
	state := InspectRuntimeKitsuCredential(db)
	if !state.EmailPresent || !state.CiphertextPresent || !state.Decryptable || state.ErrorClass != "" {
		t.Fatalf("unexpected safe credential state: %+v", state)
	}
}

package setup

import (
	"os"
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

func TestDiscordRuntimeTokenIsEncryptedAndReloadable(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	db := newRuntimeCredentialTestDB(t)
	if err := SetRuntimeDiscordBotToken(db, "discord-runtime-token"); err != nil {
		t.Fatal(err)
	}
	if got := model.GetSetting(db, RuntimeDiscordBotTokenSettingKey); got == "" || strings.Contains(got, "discord-runtime-token") {
		t.Fatal("Discord token was not stored as encrypted data")
	}
	if model.GetSetting(db, "discord.runtime_bot_token") != "" {
		t.Fatal("new Discord token must not create a plaintext legacy setting")
	}
	if got := StoredRuntimeDiscordBotToken(db); got != "discord-runtime-token" {
		t.Fatalf("encrypted Discord token did not reload: %q", got)
	}
	if err := os.Unsetenv("DISCORD_BOT_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if got := StoredRuntimeDiscordBotToken(db); got != "discord-runtime-token" {
		t.Fatalf("encrypted Discord token did not survive environment reset: %q", got)
	}
}

func TestLegacyDiscordRuntimeTokenMigratesOnlyAfterReadBack(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	db := newRuntimeCredentialTestDB(t)
	const token = "legacy-discord-token"
	if err := model.SetSecretSettingWithError(db, "discord.runtime_bot_token", token); err != nil {
		t.Fatal(err)
	}
	if err := MigrateRuntimeDiscordBotToken(db); err != nil {
		t.Fatal(err)
	}
	if model.GetSetting(db, "discord.runtime_bot_token") != "" {
		t.Fatal("legacy plaintext Discord token remained after migration")
	}
	if got := StoredRuntimeDiscordBotToken(db); got != token {
		t.Fatalf("migrated Discord token mismatch: %q", got)
	}
}

func TestLegacyDiscordRuntimeTokenIsKeptWhenEncryptionFails(t *testing.T) {
	db := newRuntimeCredentialTestDB(t)
	const token = "legacy-discord-token"
	if err := model.SetSecretSettingWithError(db, "discord.runtime_bot_token", token); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(t.TempDir(), "runtime-secret.key")
	if err := os.Mkdir(keyDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RuntimeSecretKeyFileEnv, keyDir)
	if err := MigrateRuntimeDiscordBotToken(db); err == nil {
		t.Fatal("expected migration encryption failure")
	}
	if got := model.GetSetting(db, "discord.runtime_bot_token"); got != token {
		t.Fatal("legacy token must remain after encryption failure")
	}
	if model.GetSetting(db, RuntimeDiscordBotTokenSettingKey) != "" {
		t.Fatal("failed migration must not leave encrypted token")
	}
}

func TestEncryptedDiscordRuntimeTokenFailsClosedWhenKeyIsMissing(t *testing.T) {
	db := newRuntimeCredentialTestDB(t)
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	if err := SetRuntimeDiscordBotToken(db, "discord-runtime-token"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "missing.key"))
	if got := StoredRuntimeDiscordBotToken(db); got != "" {
		t.Fatal("encrypted Discord token must fail closed when the key is missing")
	}
}

func TestEncryptedDiscordRuntimeTokenDoesNotFallBackToEnvironment(t *testing.T) {
	db := newRuntimeCredentialTestDB(t)
	if err := model.SetSecretSettingWithError(db, RuntimeDiscordBotTokenSettingKey, "v1:not-valid-ciphertext"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISCORD_BOT_TOKEN", "stale-environment-token")
	if got := RuntimeDiscordBotToken(db); got != "" {
		t.Fatal("unreadable encrypted token must not fall back to the environment")
	}
}

func TestEncryptedDiscordRuntimeTokenFailsClosedWhenCiphertextIsCorrupt(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	db := newRuntimeCredentialTestDB(t)
	if err := model.SetSecretSettingWithError(db, RuntimeDiscordBotTokenSettingKey, "v1:not-valid-ciphertext"); err != nil {
		t.Fatal(err)
	}
	if got := StoredRuntimeDiscordBotToken(db); got != "" {
		t.Fatal("corrupt encrypted Discord token must fail closed")
	}
	if err := MigrateRuntimeDiscordBotToken(db); err == nil {
		t.Fatal("corrupt encrypted Discord token must fail migration")
	}
}

func TestLegacyDiscordRuntimeTokenIsKeptWhenDatabaseMigrationFails(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	db := newRuntimeCredentialTestDB(t)
	const token = "legacy-discord-token"
	if err := model.SetSecretSettingWithError(db, "discord.runtime_bot_token", token); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_legacy_discord_delete BEFORE DELETE ON settings WHEN OLD.key = 'discord.runtime_bot_token' BEGIN SELECT RAISE(ABORT, 'migration delete rejected'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateRuntimeDiscordBotToken(db); err == nil {
		t.Fatal("expected database migration failure")
	}
	if got := model.GetSetting(db, "discord.runtime_bot_token"); got != token {
		t.Fatal("legacy token must remain after database migration failure")
	}
	if model.GetSetting(db, RuntimeDiscordBotTokenSettingKey) != "" {
		t.Fatal("failed database migration must roll back encrypted token")
	}
}

func TestEncryptedDiscordRuntimeTokenSurvivesDatabaseReopen(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	dbPath := filepath.Join(t.TempDir(), "runtime.sqlite")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	const token = "discord-runtime-token"
	if err := SetRuntimeDiscordBotToken(db, token); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	reopenedSQLDB, err := reopened.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedSQLDB.Close()
	if got := StoredRuntimeDiscordBotToken(reopened); got != token {
		t.Fatalf("encrypted Discord token did not survive database reopen: %q", got)
	}
}

func TestProjectWebhookURLIsEncryptedAtRestAndReloadable(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ProjectWebhook{}); err != nil {
		t.Fatal(err)
	}

	const webhookURL = "https://discord.com/api/webhooks/123456789012345678/secret-token-value"
	if err := model.CreateProjectWebhook(db, "proj-1", "anim", "Animation", webhookURL, "chan-1"); err != nil {
		t.Fatalf("CreateProjectWebhook: %v", err)
	}

	var rawStored string
	if err := db.Table("project_webhooks").Select("webhook_url").Where("kitsu_project_id = ?", "proj-1").Scan(&rawStored).Error; err != nil {
		t.Fatal(err)
	}
	if rawStored == "" || rawStored == webhookURL || strings.Contains(rawStored, "secret-token-value") || !strings.HasPrefix(rawStored, "v1:") {
		t.Fatalf("expected encrypted webhook_url in DB, got %q", rawStored)
	}

	rows := model.ListProjectWebhooks(db, "proj-1")
	if len(rows) != 1 || rows[0].WebhookURL != webhookURL {
		t.Fatalf("ListProjectWebhooks returned %+v, want %q", rows, webhookURL)
	}
	if got := model.FindProjectWebhookByID(db, rows[0].ID); got == nil || got.WebhookURL != webhookURL {
		t.Fatalf("FindProjectWebhookByID returned %+v, want %q", got, webhookURL)
	}
	if ch := model.FindChannelNameByWebhookURL(db, webhookURL); ch != "anim" {
		t.Fatalf("FindChannelNameByWebhookURL = %q, want anim", ch)
	}

	const updatedURL = "https://discord.com/api/webhooks/123456789012345678/rotated-token"
	if err := model.UpdateProjectWebhookURL(db, rows[0].ID, updatedURL); err != nil {
		t.Fatalf("UpdateProjectWebhookURL: %v", err)
	}
	var rawUpdated string
	if err := db.Table("project_webhooks").Select("webhook_url").Where("id = ?", rows[0].ID).Scan(&rawUpdated).Error; err != nil {
		t.Fatal(err)
	}
	if rawUpdated == updatedURL || strings.Contains(rawUpdated, "rotated-token") || !strings.HasPrefix(rawUpdated, "v1:") {
		t.Fatalf("expected encrypted updated webhook_url in DB, got %q", rawUpdated)
	}
	if got := model.FindProjectWebhookByID(db, rows[0].ID); got == nil || got.WebhookURL != updatedURL {
		t.Fatalf("FindProjectWebhookByID after update = %+v, want %q", got, updatedURL)
	}
}

func TestLegacyPlaintextProjectWebhookMigratesAndReadsBack(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ProjectWebhook{}); err != nil {
		t.Fatal(err)
	}

	const legacyURL = "https://discord.com/api/webhooks/987654321098765432/legacy-plaintext-token"
	if err := db.Table("project_webhooks").Create(map[string]any{
		"kitsu_project_id":   "proj-legacy",
		"channel_name":       "comp",
		"task_type":          "Compositing",
		"webhook_url":        legacyURL,
		"discord_channel_id": "chan-legacy",
	}).Error; err != nil {
		t.Fatal(err)
	}

	// Pre-existing plaintext row reads back before migration.
	rows := model.ListProjectWebhooks(db, "proj-legacy")
	if len(rows) != 1 || rows[0].WebhookURL != legacyURL {
		t.Fatalf("pre-migration read = %+v, want %q", rows, legacyURL)
	}

	if err := MigrateProjectWebhookURLs(db); err != nil {
		t.Fatalf("MigrateProjectWebhookURLs: %v", err)
	}

	var rawAfter string
	if err := db.Table("project_webhooks").Select("webhook_url").Where("kitsu_project_id = ?", "proj-legacy").Scan(&rawAfter).Error; err != nil {
		t.Fatal(err)
	}
	if rawAfter == legacyURL || strings.Contains(rawAfter, "legacy-plaintext-token") || !strings.HasPrefix(rawAfter, "v1:") {
		t.Fatalf("expected migrated encrypted webhook_url in DB, got %q", rawAfter)
	}

	rowsAfter := model.ListProjectWebhooks(db, "proj-legacy")
	if len(rowsAfter) != 1 || rowsAfter[0].WebhookURL != legacyURL {
		t.Fatalf("post-migration read = %+v, want %q", rowsAfter, legacyURL)
	}
}

func newRuntimeCredentialTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	return db
}

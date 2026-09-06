package setup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"app/src/model"
	"gorm.io/gorm"
)

const (
	RuntimeKitsuPasswordSettingKey   = "kitsu.runtime_password_encrypted"
	RuntimeKitsuTokenSettingKey      = "kitsu.runtime_token_encrypted"
	RuntimeDiscordBotTokenSettingKey = "discord.runtime_bot_token_encrypted"
	RuntimeSecretKeyFileEnv          = "KITSUSYNC_SECRET_KEY_FILE"
	defaultRuntimeSecretKeyFile      = "data/runtime-secret.key"
)

// StoredRuntimeDiscordBotToken reads the encrypted Discord token. The legacy
// setting is consulted only when no encrypted value exists, allowing existing
// installations to migrate without making the plaintext value authoritative.
func StoredRuntimeDiscordBotToken(db *gorm.DB) string {
	if db == nil {
		return ""
	}
	if ciphertext := strings.TrimSpace(model.GetSetting(db, RuntimeDiscordBotTokenSettingKey)); ciphertext != "" {
		token, err := decryptRuntimeSecret(ciphertext)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(token)
	}
	return strings.TrimSpace(model.GetSetting(db, "discord.runtime_bot_token"))
}

// RuntimeDiscordBotToken returns the configured runtime token without falling
// back to another source when an encrypted setting exists but cannot be read.
// That fail-closed behavior prevents a stale environment/config token from
// masking a broken persisted credential.
func RuntimeDiscordBotToken(db *gorm.DB) string {
	if db != nil && strings.TrimSpace(model.GetSetting(db, RuntimeDiscordBotTokenSettingKey)) != "" {
		return StoredRuntimeDiscordBotToken(db)
	}
	if db != nil {
		if token := strings.TrimSpace(model.GetSetting(db, "discord.runtime_bot_token")); token != "" {
			return token
		}
	}
	return strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN"))
}

// MigrateRuntimeDiscordBotToken upgrades the legacy plaintext setting only
// after encryption and a decrypt/read-back check have succeeded. The old
// value remains intact if any database or cryptographic step fails.
func MigrateRuntimeDiscordBotToken(db *gorm.DB) error {
	if db == nil {
		return errors.New("database unavailable")
	}
	if ciphertext := strings.TrimSpace(model.GetSetting(db, RuntimeDiscordBotTokenSettingKey)); ciphertext != "" {
		if _, err := decryptRuntimeSecret(ciphertext); err != nil {
			return fmt.Errorf("encrypted Discord token is unavailable: %w", err)
		}
		return nil
	}
	legacy := strings.TrimSpace(model.GetSetting(db, "discord.runtime_bot_token"))
	if legacy == "" {
		return nil
	}
	ciphertext, err := encryptRuntimeSecret(legacy)
	if err != nil {
		return fmt.Errorf("encrypt legacy Discord token: %w", err)
	}
	verified, err := decryptRuntimeSecret(ciphertext)
	if err != nil || verified != legacy {
		return errors.New("encrypted Discord token read-back verification failed")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := model.SetSecretSettingWithError(tx, RuntimeDiscordBotTokenSettingKey, ciphertext); err != nil {
			return fmt.Errorf("save encrypted Discord token: %w", err)
		}
		stored := strings.TrimSpace(model.GetSetting(tx, RuntimeDiscordBotTokenSettingKey))
		readBack, err := decryptRuntimeSecret(stored)
		if err != nil || readBack != legacy {
			return errors.New("encrypted Discord token database read-back failed")
		}
		return tx.Where("key = ?", "discord.runtime_bot_token").Delete(&model.Setting{}).Error
	}); err != nil {
		return err
	}
	if strings.TrimSpace(model.GetSetting(db, "discord.runtime_bot_token")) != "" {
		return errors.New("legacy Discord token remained after migration")
	}
	stored, err := decryptRuntimeSecret(strings.TrimSpace(model.GetSetting(db, RuntimeDiscordBotTokenSettingKey)))
	if err != nil || stored != legacy {
		return errors.New("encrypted Discord token post-migration verification failed")
	}
	return nil
}

func SetRuntimeDiscordBotToken(db *gorm.DB, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("Discord Bot Token is empty")
	}
	ciphertext, err := encryptRuntimeSecret(token)
	if err != nil {
		return err
	}
	if db != nil {
		if err := model.SetSecretSettingWithError(db, RuntimeDiscordBotTokenSettingKey, ciphertext); err != nil {
			return fmt.Errorf("save encrypted Discord token: %w", err)
		}
		stored := StoredRuntimeDiscordBotToken(db)
		if stored != token {
			return errors.New("encrypted Discord token persistence verification failed")
		}
	}
	if err := os.Setenv("DISCORD_BOT_TOKEN", token); err != nil {
		return fmt.Errorf("set Discord runtime token: %w", err)
	}
	return nil
}

type RuntimeKitsuCredentialState struct {
	EmailPresent      bool
	CiphertextPresent bool
	Decryptable       bool
	ErrorClass        string
}

// InspectRuntimeKitsuCredential reports only safe credential state. It never
// returns or logs the email, ciphertext, key, or decrypted password.
func InspectRuntimeKitsuCredential(db *gorm.DB) RuntimeKitsuCredentialState {
	state := RuntimeKitsuCredentialState{}
	if db == nil {
		state.ErrorClass = "database_unavailable"
		return state
	}
	state.EmailPresent = strings.TrimSpace(model.GetSetting(db, RuntimeKitsuEmailSettingKey)) != ""
	ciphertext := strings.TrimSpace(model.GetSetting(db, RuntimeKitsuPasswordSettingKey))
	state.CiphertextPresent = ciphertext != ""
	if !state.CiphertextPresent {
		state.ErrorClass = "ciphertext_missing"
		return state
	}
	if _, err := decryptRuntimeSecret(ciphertext); err != nil {
		state.ErrorClass = runtimeSecretErrorClass(err)
		return state
	}
	state.Decryptable = true
	return state
}

func runtimeSecretErrorClass(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case strings.Contains(err.Error(), "read runtime secret key"):
		return "key_unavailable"
	case strings.Contains(err.Error(), "invalid length"):
		return "key_invalid"
	case strings.Contains(err.Error(), "malformed"):
		return "ciphertext_malformed"
	case strings.Contains(err.Error(), "could not be decrypted"):
		return "ciphertext_undecryptable"
	default:
		return "decryption_failed"
	}
}

func runtimeSecretKeyPath() string {
	if value := strings.TrimSpace(os.Getenv(RuntimeSecretKeyFileEnv)); value != "" {
		return value
	}
	return defaultRuntimeSecretKeyFile
}

func loadRuntimeSecretKey() ([]byte, error) {
	key, err := os.ReadFile(runtimeSecretKeyPath())
	if err != nil {
		return nil, fmt.Errorf("read runtime secret key: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("runtime secret key has an invalid length")
	}
	return key, nil
}

func loadOrCreateRuntimeSecretKey() ([]byte, error) {
	path := runtimeSecretKeyPath()
	if key, err := os.ReadFile(path); err == nil {
		if len(key) != 32 {
			return nil, errors.New("runtime secret key has an invalid length")
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read runtime secret key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create runtime secret directory: %w", err)
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate runtime secret key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreateRuntimeSecretKey()
	}
	if err != nil {
		return nil, fmt.Errorf("create runtime secret key: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("write runtime secret key: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("close runtime secret key: %w", err)
	}
	_ = os.Chmod(path, 0600)
	return key, nil
}

func encryptRuntimeSecret(plaintext string) (string, error) {
	key, err := loadOrCreateRuntimeSecretKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return "v1:" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func decryptRuntimeSecret(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, "v1:") {
		return "", errors.New("unsupported runtime secret format")
	}
	key, err := loadRuntimeSecretKey()
	if err != nil {
		return "", err
	}
	encoded := strings.TrimPrefix(ciphertext, "v1:")
	sealed, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("runtime secret is malformed")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("runtime secret is too short")
	}
	nonce, data := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", errors.New("runtime secret could not be decrypted")
	}
	return string(plaintext), nil
}

func setRuntimeKitsuPassword(db *gorm.DB, password string) error {
	if password == "" {
		return nil
	}
	ciphertext, err := encryptRuntimeSecret(password)
	if err != nil {
		return err
	}
	if db != nil {
		if err := model.SetSecretSettingWithError(db, RuntimeKitsuPasswordSettingKey, ciphertext); err != nil {
			return fmt.Errorf("save encrypted runtime credential: %w", err)
		}
	}
	os.Setenv(RuntimeKitsuPasswordEnv, password)
	os.Unsetenv("KITSU_PASSWORD")
	return nil
}

func StoredRuntimeKitsuPassword(db *gorm.DB) string {
	if db != nil {
		ciphertext := strings.TrimSpace(model.GetSetting(db, RuntimeKitsuPasswordSettingKey))
		if ciphertext != "" {
			if password, err := decryptRuntimeSecret(ciphertext); err == nil {
				return password
			}
		}
	}
	return strings.TrimSpace(os.Getenv(RuntimeKitsuPasswordEnv))
}

func setRuntimeKitsuToken(db *gorm.DB, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("runtime Kitsu token is empty")
	}
	ciphertext, err := encryptRuntimeSecret(token)
	if err != nil {
		return err
	}
	if db != nil {
		if err := model.SetSecretSettingWithError(db, RuntimeKitsuTokenSettingKey, ciphertext); err != nil {
			return fmt.Errorf("save encrypted runtime Kitsu token: %w", err)
		}
	}
	return nil
}

func StoredRuntimeKitsuToken(db *gorm.DB) string {
	if db == nil {
		return ""
	}
	ciphertext := strings.TrimSpace(model.GetSetting(db, RuntimeKitsuTokenSettingKey))
	if ciphertext == "" {
		return ""
	}
	token, err := decryptRuntimeSecret(ciphertext)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(token)
}

func StoreValidatedKitsuBotMetadata(db *gorm.DB, result BotTokenValidationResult) error {
	if db == nil || !result.Compatible() {
		return errors.New("Kitsu Bot validation is not successful")
	}
	model.SetSetting(db, RuntimeKitsuAuthModeSettingKey, "bot_token")
	model.SetSetting(db, RuntimeKitsuBotIDSettingKey, strings.TrimSpace(result.IdentityID))
	model.SetSetting(db, RuntimeKitsuBotNameSettingKey, strings.TrimSpace(result.IdentityName))
	model.SetSetting(db, RuntimeKitsuTokenValidatedAtSettingKey, time.Now().UTC().Format(time.RFC3339))
	model.SetSetting(db, RuntimeKitsuTokenErrorSettingKey, "")
	return nil
}

func RecordKitsuRuntimeAuthMode(db *gorm.DB, mode, errorClass string) {
	if db == nil {
		return
	}
	model.SetSetting(db, RuntimeKitsuAuthModeSettingKey, strings.TrimSpace(mode))
	model.SetSetting(db, RuntimeKitsuTokenErrorSettingKey, strings.TrimSpace(errorClass))
}

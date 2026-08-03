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

	"app/src/model"
	"gorm.io/gorm"
)

const (
	RuntimeKitsuPasswordSettingKey = "kitsu.runtime_password_encrypted"
	RuntimeKitsuTokenSettingKey    = "kitsu.runtime_token_encrypted"
	RuntimeSecretKeyFileEnv        = "KITSUSYNC_SECRET_KEY_FILE"
	defaultRuntimeSecretKeyFile    = "data/runtime-secret.key"
)

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

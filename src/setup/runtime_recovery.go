package setup

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"app/src/model"
	"app/src/utils/basicauth"
	"gorm.io/gorm"
)

// RecoverRuntimeCredentials verifies the bot password before persisting it.
// The caller owns the password in memory and must not log or persist it.
func RecoverRuntimeCredentials(db *gorm.DB, kitsuHost, email, password string) error {
	if db == nil {
		return errors.New("runtime recovery database is unavailable")
	}
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return errors.New("runtime recovery credentials are incomplete")
	}
	authURL := strings.TrimRight(kitsuHost, "/") + "/api/auth/login"
	if token, diagnostics := basicauth.AuthForJWTTokenDetailed(authURL, email, password); token == "" {
		_ = diagnostics
		return errors.New("Kitsu connection could not be verified")
	}
	ciphertext, err := encryptRuntimeSecret(password)
	if err != nil {
		return err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		model.SetSetting(tx, RuntimeKitsuEmailSettingKey, email)
		return model.SetSecretSettingWithError(tx, RuntimeKitsuPasswordSettingKey, ciphertext)
	}); err != nil {
		return fmt.Errorf("runtime credential persistence failed: %w", err)
	}
	os.Setenv(RuntimeKitsuEmailEnv, email)
	os.Setenv(RuntimeKitsuPasswordEnv, password)
	if strings.TrimSpace(model.GetSetting(db, RuntimeKitsuEmailSettingKey)) != email {
		return errors.New("runtime email persistence verification failed")
	}
	if StoredRuntimeKitsuPassword(db) == "" {
		return errors.New("runtime password persistence verification failed")
	}
	return nil
}

func RecoverRuntimeToken(db *gorm.DB, kitsuHost, email, token string) error {
	if db == nil {
		return errors.New("runtime recovery database is unavailable")
	}
	email = strings.TrimSpace(email)
	token = strings.TrimSpace(token)
	if email == "" || token == "" {
		return errors.New("runtime token credentials are incomplete")
	}
	if !basicauth.ValidateJWTToken(strings.TrimRight(kitsuHost, "/")+"/api/auth/authenticated", token) {
		return errors.New("runtime bot token authentication failed")
	}
	ciphertext, err := encryptRuntimeSecret(token)
	if err != nil {
		return err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		model.SetSetting(tx, RuntimeKitsuEmailSettingKey, email)
		return model.SetSecretSettingWithError(tx, RuntimeKitsuTokenSettingKey, ciphertext)
	}); err != nil {
		return fmt.Errorf("runtime token persistence failed: %w", err)
	}
	if StoredRuntimeKitsuToken(db) == "" {
		return errors.New("runtime token persistence verification failed")
	}
	return nil
}

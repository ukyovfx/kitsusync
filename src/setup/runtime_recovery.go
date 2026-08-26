package setup

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"app/src/model"
	"app/src/utils/basicauth"
	"gorm.io/gorm"
)

// RecoverRuntimeCredentials verifies the bot password before persisting it.
// The caller owns the password in memory and must not log or persist it.
func RecoverRuntimeCredentials(db *gorm.DB, kitsuHost, email, password string) error {
	slog.Debug("Kitsu credential recovery started",
		"hostname_present", strings.TrimSpace(kitsuHost) != "",
		"email_present", strings.TrimSpace(email) != "",
		"password_present", strings.TrimSpace(password) != "",
	)
	if db == nil {
		slog.Warn("Kitsu credential recovery failed", "stage", "database_available", "success", false)
		return errors.New("runtime recovery database is unavailable")
	}
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		slog.Warn("Kitsu credential recovery failed", "stage", "credential_validation", "success", false)
		return errors.New("runtime recovery credentials are incomplete")
	}
	authURL := strings.TrimRight(kitsuHost, "/") + "/api/auth/login"
	slog.Debug("Kitsu credential authentication attempted", "attempted", true)
	token, diagnostics := basicauth.AuthForJWTTokenDetailed(authURL, email, password)
	slog.Debug("Kitsu credential authentication result", "success", token != "", "status_code", diagnostics.StatusCode, "error_class", diagnostics.Category)
	if token == "" {
		return errors.New("Kitsu connection could not be verified")
	}
	slog.Debug("Kitsu credential encryption attempted", "attempted", true)
	ciphertext, err := encryptRuntimeSecret(password)
	if err != nil {
		slog.Warn("Kitsu credential encryption result", "success", false, "error_class", "encryption_error")
		return err
	}
	slog.Debug("Kitsu credential encryption result", "success", true)
	slog.Debug("Kitsu credential SQLite write attempted", "attempted", true)
	if err := db.Transaction(func(tx *gorm.DB) error {
		model.SetSetting(tx, RuntimeKitsuEmailSettingKey, email)
		return model.SetSecretSettingWithError(tx, RuntimeKitsuPasswordSettingKey, ciphertext)
	}); err != nil {
		slog.Warn("Kitsu credential SQLite write result", "success", false, "error_class", "database_write_error")
		return fmt.Errorf("runtime credential persistence failed: %w", err)
	}
	slog.Debug("Kitsu credential SQLite write result", "success", true)
	os.Setenv(RuntimeKitsuEmailEnv, email)
	os.Setenv(RuntimeKitsuPasswordEnv, password)
	if strings.TrimSpace(model.GetSetting(db, RuntimeKitsuEmailSettingKey)) != email {
		slog.Warn("Kitsu credential persistence verification", "success", false, "field", "email")
		return errors.New("runtime email persistence verification failed")
	}
	if StoredRuntimeKitsuPassword(db) == "" {
		slog.Warn("Kitsu credential persistence verification", "success", false, "field", "password")
		return errors.New("runtime password persistence verification failed")
	}
	slog.Debug("Kitsu credential persistence verification", "success", true)
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

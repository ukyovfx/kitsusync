package setup

import (
	"errors"
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
	if basicauth.AuthForJWTToken(strings.TrimRight(kitsuHost, "/")+"/api/auth/login", email, password) == "" {
		return errors.New("runtime bot authentication failed")
	}
	ciphertext, err := encryptRuntimeSecret(password)
	if err != nil {
		return err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		model.SetSetting(tx, RuntimeKitsuEmailSettingKey, email)
		return model.SetSecretSettingWithError(tx, RuntimeKitsuPasswordSettingKey, ciphertext)
	}); err != nil {
		return err
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

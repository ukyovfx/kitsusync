package setup

import (
	"errors"
	"testing"

	"app/src/model"
	"gorm.io/gorm"
)

func TestPersistKitsuConnectionRollsBackEveryWriteBoundary(t *testing.T) {
	for failAt := 1; failAt <= 10; failAt++ {
		t.Run("write_boundary", func(t *testing.T) {
			db := newSetupStateTestDB(t)
			writes := 0
			fail := func() error {
				writes++
				if writes == failAt {
					return errors.New("injected persistence failure")
				}
				return nil
			}
			p := kitsuConnectionPersistence{}
			p.set = func(tx *gorm.DB, key, value string) error {
				if err := fail(); err != nil {
					return err
				}
				return model.SetSettingWithError(tx, key, value)
			}
			p.delete = func(tx *gorm.DB, key string) error {
				if err := fail(); err != nil {
					return err
				}
				return model.DeleteSettingWithError(tx, key)
			}
			p.storeToken = func(tx *gorm.DB, _ string) error { return p.set(tx, RuntimeKitsuTokenSettingKey, "test-ciphertext") }
			p.storeMetadata = func(tx *gorm.DB, _ BotTokenValidationResult) error {
				for _, k := range []string{RuntimeKitsuAuthModeSettingKey, RuntimeKitsuBotIDSettingKey, RuntimeKitsuBotNameSettingKey, RuntimeKitsuTokenValidatedAtSettingKey, RuntimeKitsuTokenErrorSettingKey} {
					if err := p.set(tx, k, "value"); err != nil {
						return err
					}
				}
				return nil
			}
			err := persistKitsuConnectionWith(db, p, "token", BotTokenValidationResult{Classification: BotTokenFullyCompatible}, true, "https://external.test", "https://display.test", "https://runtime.test", "https://runtime.test/api")
			if err == nil {
				t.Fatal("expected injected failure")
			}
			if count := db.Model(&model.Setting{}).Count(new(int64)); count.Error != nil {
				t.Fatal(count.Error)
			}
			var settings []model.Setting
			if err := db.Find(&settings).Error; err != nil || len(settings) != 0 {
				t.Fatalf("partial state committed: %v %v", len(settings), err)
			}
		})
	}
}

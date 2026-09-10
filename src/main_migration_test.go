package main

import (
	"path/filepath"
	"testing"
	"time"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openLegacyV043Database(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy-v043.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get legacy database handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	// This reflects the v0.4.3-era application schema: it intentionally has no
	// AdminSession table, which was introduced later in v0.4.4.
	if err := db.AutoMigrate(
		&model.Task{}, &model.Project{}, &model.ProjectWebhook{},
		&model.ProductionChannelMapping{}, &model.ProductionNotificationConfig{},
		&model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{},
		&model.UserMap{}, &model.CheckerMap{}, &model.Setting{}, &model.AuditLog{},
		&model.ProjectUserMap{}, &model.ProjectCheckerMap{}, &model.ProjectSetting{},
	); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if db.Migrator().HasTable(&model.AdminSession{}) {
		t.Fatal("legacy fixture unexpectedly contains admin_sessions")
	}
	return db
}

func TestMigrateApplicationSchemaUpgradesLegacyV043DatabaseForSessions(t *testing.T) {
	db := openLegacyV043Database(t)
	if err := migrateApplicationSchema(db); err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	if !db.Migrator().HasTable(&model.AdminSession{}) {
		t.Fatal("admin_sessions was not created during legacy upgrade")
	}
	session := model.AdminSession{TokenHash: "legacy-upgrade-session", Email: "manager@example.test", Role: "manager", Expiry: time.Now().Add(time.Hour)}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("insert admin session after upgrade: %v", err)
	}
	if err := migrateApplicationSchema(db); err != nil {
		t.Fatalf("repeat migration must remain safe: %v", err)
	}
	var got model.AdminSession
	if err := db.Where("token_hash = ?", session.TokenHash).First(&got).Error; err != nil {
		t.Fatalf("session was not preserved across repeated migration: %v", err)
	}
}

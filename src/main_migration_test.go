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

func TestMigrateApplicationSchemaAddsNullableCheckerTaskTypeIDs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy-checker-maps.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get legacy database handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatalf("create project table: %v", err)
	}
	legacySQL := []string{
		`CREATE TABLE checker_maps (id integer primary key autoincrement, task_type text, kitsu_name text, kitsu_email text, discord_id text, override_discord_id text)`,
		`CREATE TABLE project_checker_maps (id integer primary key autoincrement, project_id integer not null, task_type text not null, kitsu_name text, kitsu_email text, discord_user_id text, override_discord_id text, created_at datetime, CONSTRAINT idx_projcheckermap UNIQUE (project_id, task_type))`,
		`INSERT INTO checker_maps (task_type, kitsu_name, discord_id) VALUES ('Animation', 'Legacy', 'global-reviewer')`,
		`INSERT INTO project_checker_maps (project_id, task_type, kitsu_name, override_discord_id) VALUES (1, 'Animation', 'Legacy', 'production-reviewer')`,
	}
	for _, statement := range legacySQL {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create legacy checker schema: %v", err)
		}
	}
	if err := migrateApplicationSchema(db); err != nil {
		t.Fatalf("migrate legacy checker schema: %v", err)
	}
	var global model.CheckerMap
	if err := db.Where("task_type = ?", "Animation").First(&global).Error; err != nil {
		t.Fatal(err)
	}
	var project model.ProjectCheckerMap
	if err := db.Where("project_id = ? AND task_type = ?", 1, "Animation").First(&project).Error; err != nil {
		t.Fatal(err)
	}
	if global.TaskTypeID != "" || project.TaskTypeID != "" || global.DiscordID != "global-reviewer" || project.OverrideDiscordID != "production-reviewer" {
		t.Fatalf("legacy checker rows were not preserved: global=%+v project=%+v", global, project)
	}
	if !db.Migrator().HasIndex(&model.ProjectCheckerMap{}, "idx_projcheckermap") {
		t.Fatal("existing Production + Task Type name uniqueness index was removed")
	}
	if err := db.Create(&model.ProjectCheckerMap{ProjectID: 1, TaskTypeID: "tt-1", TaskType: "Animation"}).Error; err == nil {
		t.Fatal("existing Production + Task Type name constraint no longer rejects duplicates")
	}
}

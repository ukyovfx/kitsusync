package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestMigrateApplicationSchemaRehearsesSyntheticV049UpgradeToV051(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "synthetic-v049.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	// Table names and the legacy fields below are from tag v0.4.9
	// (90a09084bf24f67ead35614cf7c54af00b0c3149). The fixture intentionally
	// omits v0.5.0-v0.5.1 delivery-journal and mutation-barrier tables.
	legacyTables := []string{
		"CREATE TABLE `tasks` (id integer primary key, task_id text, task_status text)",
		"CREATE TABLE `projects` (id integer primary key, kitsu_project_id text, name text)",
		"CREATE TABLE `project_webhooks` (id integer primary key, kitsu_project_id text, webhook_url text)",
		// Match SQLite's quoted table-name form emitted by the v0.4.9 GORM
		// migrator; the pinned SQLite driver uses that form when rebuilding a
		// legacy table to add current columns.
		"CREATE TABLE `production_channel_mappings` (id integer primary key, production_id text, task_type_id text)",
		"CREATE TABLE `production_notification_configs` (id integer primary key, production_id text)",
		"CREATE TABLE `production_notification_routes` (id integer primary key, production_id text, task_type_id text)",
		"CREATE TABLE `notification_routing_diagnoses` (id integer primary key, production_id text)",
		"CREATE TABLE `user_maps` (id integer primary key, kitsu_id text, discord_id text)",
		"CREATE TABLE `checker_maps` (id integer primary key, task_type text, discord_id text)",
		"CREATE TABLE `settings` (key text primary key, value text)",
		"CREATE TABLE `admin_sessions` (id integer primary key, token_hash text, expiry datetime)",
		"CREATE TABLE `audit_logs` (id integer primary key, action text)",
		"CREATE TABLE `project_user_maps` (id integer primary key, project_id integer, kitsu_email text)",
		"CREATE TABLE `project_checker_maps` (id integer primary key, project_id integer not null, task_type text not null, kitsu_name text, kitsu_email text, discord_user_id text, override_discord_id text, created_at datetime)",
		`CREATE UNIQUE INDEX idx_projcheckermap ON project_checker_maps(project_id, task_type)`,
		"CREATE TABLE `project_settings` (id integer primary key, project_id integer, key text, value text)",
		`INSERT INTO projects(id,kitsu_project_id,name) VALUES (7,'kitsu-production-7','Synthetic v0.4.9')`,
		`INSERT INTO project_checker_maps(id,project_id,task_type,override_discord_id) VALUES (9,7,'Animation','discord-user-legacy')`,
		`INSERT INTO user_maps(id,kitsu_id,discord_id) VALUES (11,'kitsu-person-11','discord-user-11')`,
	}
	for _, statement := range legacyTables {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create v0.4.9 fixture: %v\nSQL: %s", err, statement)
		}
	}
	if err := migrateApplicationSchema(db); err != nil {
		t.Fatalf("v0.4.9 -> v0.5.1 migration failed: %v", err)
	}
	for _, table := range []any{&model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{}, &model.ProductionNotificationWriteLock{}, &model.VFXFilesProductionConfig{}, &model.VFXFilesEventInbox{}, &model.VFXFilesEventNonce{}, &model.VFXFilesEventConflict{}} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("upgrade omitted v0.5.1 additive table %T", table)
		}
	}
	if !db.Migrator().HasColumn(&model.NotificationDeliveryEvent{}, "RetryAfterAt") || !db.Migrator().HasColumn(&model.VFXFilesProductionConfig{}, "PendingActivationEpoch") {
		t.Fatal("upgrade omitted additive retry-after or pending-activation recovery columns")
	}
	var project model.Project
	if err := db.First(&project, 7).Error; err != nil || project.KitsuProjectID != "kitsu-production-7" {
		t.Fatalf("existing Production identity changed during upgrade: %+v err=%v", project, err)
	}
	var reviewer model.ProjectCheckerMap
	if err := db.First(&reviewer, 9).Error; err != nil || reviewer.OverrideDiscordID != "discord-user-legacy" || reviewer.TaskType != "Animation" {
		t.Fatalf("legacy Reviewer mapping changed during upgrade: %+v err=%v", reviewer, err)
	}
	var linked model.UserMap
	if err := db.First(&linked, 11).Error; err != nil || linked.DiscordID != "discord-user-11" {
		t.Fatalf("global User Linking row changed during upgrade: %+v err=%v", linked, err)
	}
	if err := migrateApplicationSchema(db); err != nil {
		t.Fatalf("repeat v0.5.1 startup migration failed: %v", err)
	}
	var count int64
	if err := db.Model(&model.ProjectCheckerMap{}).Where("id = ?", 9).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("repeat startup migration duplicated/lost legacy rows: count=%d err=%v", count, err)
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
		`CREATE TABLE "project_checker_maps" (id integer primary key autoincrement, project_id integer not null, task_type text not null, kitsu_name text, kitsu_email text, discord_user_id text, override_discord_id text, created_at datetime)`,
		`CREATE UNIQUE INDEX idx_projcheckermap ON project_checker_maps(project_id, task_type)`,
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
	if !db.Migrator().HasTable(&model.ProjectReviewerTarget{}) {
		t.Fatal("additive Production Reviewer target table was not created")
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

func TestMigrateApplicationSchemaAddsDeliveryJournalIdempotentlyAndSurvivesBackupRestore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-delivery.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	if err := migrateApplicationSchema(db); err != nil {
		t.Fatalf("first additive migration failed: %v", err)
	}
	for _, table := range []any{&model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{}, &model.ProductionNotificationWriteLock{}} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("migration omitted additive table %T", table)
		}
	}
	for _, check := range []struct {
		model any
		name  string
	}{{&model.NotificationDeliveryEvent{}, "prior_cleanup_in_flight"}, {&model.NotificationDeliveryEvent{}, "payload_redacted_at"}, {&model.NotificationDeliveryEvent{}, "identity_conflict_count"}, {&model.NotificationDeliveryEvent{}, "identity_conflict_at"}, {&model.ProductionNotificationWriteLock{}, "mutation_state"}, {&model.ProductionNotificationWriteLock{}, "mutation_token"}, {&model.ProductionNotificationWriteLock{}, "mutation_at"}} {
		if !db.Migrator().HasColumn(check.model, check.name) {
			t.Fatalf("additive migration omitted column %s on %T", check.name, check.model)
		}
	}
	var indexes []struct {
		Name   string `gorm:"column:name"`
		Unique int    `gorm:"column:unique"`
	}
	if err := db.Raw("PRAGMA index_list('notification_delivery_events')").Scan(&indexes).Error; err != nil {
		t.Fatal(err)
	}
	uniqueEventIdentity := false
	for _, index := range indexes {
		if index.Unique == 1 && strings.Contains(index.Name, "event_id") {
			uniqueEventIdentity = true
		}
	}
	if !uniqueEventIdentity {
		t.Fatalf("delivery event identity lacks a unique SQLite index: %#v", indexes)
	}
	event := model.NotificationDeliveryEvent{EventID: "durable-event-1", TaskID: "task-1", ProductionID: "production-1", TaskTypeID: "tt-1", TaskStatus: "WFA", PayloadJSON: `{"content":"synthetic"}`, PayloadSHA256: "payload-hash", WebhookRowID: 4, DestinationGuildID: "guild-1", DestinationChannelID: "channel-1", DestinationWebhookID: "webhook-1", DestinationFingerprint: "fingerprint", Disposition: model.DeliveryInFlight}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	duplicateEvent := event
	duplicateEvent.ID = 0
	if err := db.Create(&duplicateEvent).Error; err == nil {
		t.Fatal("database did not enforce deterministic event identity uniqueness")
	}
	if err := db.Create(&model.ProductionNotificationWriteLock{ProductionID: "deleting-production", MutationState: model.NotificationMutationDeleting, MutationToken: "deletion-token"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationWriteLock{ProductionID: "reordering-production", MutationState: model.NotificationMutationReordering, MutationToken: "reorder-token"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateApplicationSchema(db); err != nil {
		t.Fatalf("repeated migration failed: %v", err)
	}
	var quarantined model.NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&quarantined).Error; err != nil || quarantined.Disposition != model.DeliveryUnknown || quarantined.PayloadJSON != event.PayloadJSON || quarantined.PayloadRedactedAt != nil || quarantined.IdentityConflictCount != 0 || quarantined.IdentityConflictAt != nil {
		t.Fatalf("repeated startup did not quarantine in-flight delivery: row=%+v err=%v", quarantined, err)
	}
	var deletionLock, reorderLock model.ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", "deleting-production").First(&deletionLock).Error; err != nil || deletionLock.MutationState != model.NotificationMutationDeletionHelp || deletionLock.MutationToken != "deletion-token" {
		t.Fatalf("startup did not preserve interrupted teardown for explicit retry: lock=%+v err=%v", deletionLock, err)
	}
	if err := db.Where("production_id = ?", "reordering-production").First(&reorderLock).Error; err != nil || reorderLock.MutationState != "reorder_needs_help" || reorderLock.MutationToken != "reorder-token" {
		t.Fatalf("startup did not quarantine interrupted external reorder: lock=%+v err=%v", reorderLock, err)
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(t.TempDir(), "restored-delivery.db")
	contents, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	restoredSQL, err := restored.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoredSQL.Close() })
	if err := migrateApplicationSchema(restored); err != nil {
		t.Fatalf("restored database startup migration failed: %v", err)
	}
	var restoredEvent model.NotificationDeliveryEvent
	if err := restored.Where("event_id = ?", event.EventID).First(&restoredEvent).Error; err != nil || restoredEvent.Disposition != model.DeliveryUnknown || restoredEvent.PayloadJSON != event.PayloadJSON || restoredEvent.PayloadRedactedAt != nil {
		t.Fatalf("backup/restore lost journal disposition: row=%+v err=%v", restoredEvent, err)
	}
}

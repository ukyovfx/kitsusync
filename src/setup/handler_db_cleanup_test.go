package setup

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSetupHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Setting{},
		&model.Project{},
		&model.ProjectWebhook{},
		&model.ProductionChannelMapping{},
		&model.ProductionNotificationConfig{},
		&model.ProductionNotificationRoute{},
		&model.ProjectUserMap{},
		&model.ProjectCheckerMap{},
		&model.ProjectReviewerTarget{},
		&model.ProductionNotificationWriteLock{},
		&model.ProductionNotificationWriteLock{},
		&model.ProjectSetting{},
		&model.UserMap{},
		&model.CheckerMap{},
		&model.NotificationDeliveryEvent{},
		&model.ManagedChannelDeletion{},
	); err != nil {
		t.Fatalf("failed to migrate setup schema: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys=ON").Error; err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}
	return db
}

func TestDeleteProjectConnectionOnlyRejectsUnresolvedDeletionTombstone(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	if err := model.CreateProject(db, "tombstone-cleanup", "Tombstone Cleanup", "kitsu", "guild-1", "category-1", "en"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "tombstone-cleanup", "channel", "task-type-1", "https://discord.com/api/webhooks/12345678901234567/synthetic-token", "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "tombstone-cleanup")[0]
	if err := db.Create(&model.ProductionNotificationRoute{ProductionID: "tombstone-cleanup", TaskTypeID: "task-type-1", DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.BeginManagedChannelDeletion(db, "tombstone-cleanup", webhook.ID, "guild-1", "channel-1", "12345678901234567", time.Now()); err == nil {
		t.Fatal("last route deletion must remain forbidden in the single-channel path")
	}
	if err := db.Create(&model.ManagedChannelDeletion{WebhookRowID: webhook.ID, ProductionID: "tombstone-cleanup", GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "12345678901234567", Disposition: model.ChannelDeletionNeedsHelp}).Error; err != nil {
		t.Fatal(err)
	}
	if err := DeleteProjectConnectionOnly("tombstone-cleanup", db); !errors.Is(err, model.ErrChannelDeletionConflict) {
		t.Fatalf("connection cleanup erased unresolved deletion state: %v", err)
	}
	if model.FindProjectByKitsuID(db, "tombstone-cleanup") == nil {
		t.Fatal("project was removed despite unresolved channel tombstone")
	}
}

func TestDeleteProjectConnectionOnly_RemovesOnlyKitsuSyncRecords(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	if err := model.CreateProject(db, "kitsu-proj-unlink", "Project Unlink", "cg", "guild-123", "cat-123", "ja"); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}
	project := model.FindProjectByKitsuID(db, "kitsu-proj-unlink")
	if project == nil {
		t.Fatalf("expected project row")
	}
	if err := model.CreateProjectWebhook(db, "kitsu-proj-unlink", "general", "*", "https://discord.invalid/general", "channel-1"); err != nil {
		t.Fatalf("failed to create webhook: %v", err)
	}
	db.Create(&model.ProductionChannelMapping{ProductionID: "kitsu-proj-unlink", GuildID: "guild-123", TaskTypeID: "tt-1", ChannelID: "channel-1", Active: true, State: model.ChannelMappingStateCurrent, MigrationState: model.ChannelMappingStateCurrent})
	db.Create(&model.ProductionNotificationConfig{ProductionID: "kitsu-proj-unlink", ProductionName: "Project Unlink", Enabled: true})
	db.Create(&model.ProductionNotificationRoute{ProductionID: "kitsu-proj-unlink", TaskTypeID: "tt-1", DestinationWebhookID: 1, DestinationChannelID: "channel-1"})
	model.UpsertProjectUserMap(db, project.ID, "Artist A", "artist@example.com", "discord-user-1")
	model.UpsertProjectCheckerMap(db, project.ID, "Animation", "discord-reviewer-1")
	db.Create(&model.ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-animation", TaskTypeName: "Animation", TargetKind: model.ReviewerTargetRole, DiscordID: "123456789012345678"})
	db.Create(&model.ProjectSetting{ProjectID: project.ID, Key: "storage_url", Value: "s3://bucket/project"})
	db.Create(&model.UserMap{KitsuName: "Global User", KitsuEmail: "global@example.com", DiscordID: "global-discord"})
	db.Create(&model.CheckerMap{TaskType: "GlobalTask", KitsuName: "Global Reviewer", KitsuEmail: "reviewer@example.com", DiscordID: "global-reviewer"})

	if err := DeleteProjectConnectionOnly("kitsu-proj-unlink", db); err != nil {
		t.Fatalf("expected unlink delete to succeed, got %v", err)
	}
	if got := model.FindProjectByKitsuID(db, "kitsu-proj-unlink"); got != nil {
		t.Fatalf("expected project row to be deleted")
	}
	if got := model.ListProjectWebhooks(db, "kitsu-proj-unlink"); len(got) != 0 {
		t.Fatalf("expected project webhooks to be deleted, got %d", len(got))
	}
	if got := model.ListProductionChannelMappings(db, "kitsu-proj-unlink"); len(got) != 0 {
		t.Fatalf("expected production channel mappings to be deleted, got %d", len(got))
	}
	if got := model.ListProductionNotificationRoutes(db, "kitsu-proj-unlink"); len(got) != 0 {
		t.Fatalf("expected notification routes to be deleted, got %d", len(got))
	}
	if got := model.FindProductionNotificationConfig(db, "kitsu-proj-unlink"); got != nil {
		t.Fatal("expected notification config to be deleted")
	}
	if got := model.ListProjectUserMaps(db, project.ID); len(got) != 0 {
		t.Fatalf("expected project user maps to be deleted, got %d", len(got))
	}
	if got := model.ListProjectCheckerMaps(db, project.ID); len(got) != 0 {
		t.Fatalf("expected project checker maps to be deleted, got %d", len(got))
	}
	if got := model.ListProjectReviewerTargets(db, project.ID); len(got) != 0 {
		t.Fatalf("expected project Reviewer targets to be deleted, got %d", len(got))
	}

	var projectSettings []model.ProjectSetting
	db.Where("project_id = ?", project.ID).Find(&projectSettings)
	if len(projectSettings) != 0 {
		t.Fatalf("expected project settings to be deleted, got %d", len(projectSettings))
	}

	var globalUsers []model.UserMap
	db.Find(&globalUsers)
	if len(globalUsers) != 1 {
		t.Fatalf("expected global user mappings to remain, got %d", len(globalUsers))
	}
	var globalCheckers []model.CheckerMap
	db.Find(&globalCheckers)
	if len(globalCheckers) != 1 {
		t.Fatalf("expected global checker mappings to remain, got %d", len(globalCheckers))
	}
}

func TestDeleteProjectConnectionOnlyWaitsForApplyBeforeProjectRowExists(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	const productionID = "connection-delete-before-row"
	unlockApply := lockProductionNotificationApply(productionID)
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- DeleteProjectConnectionOnly(productionID, db) }()

	select {
	case err := <-deleteDone:
		unlockApply()
		t.Fatalf("connection deletion bypassed the Apply lock before the Project row existed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := db.Create(&model.Project{KitsuProjectID: productionID, Name: "Created while Apply held lock"}).Error; err != nil {
		unlockApply()
		t.Fatal(err)
	}
	unlockApply()
	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatalf("connection deletion failed after Apply released the lock: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("connection deletion did not resume after Apply released the lock")
	}
	if model.FindProjectByKitsuID(db, productionID) != nil {
		t.Fatal("connection deletion did not remove the Project created while it waited")
	}
}

func TestDeleteProjectWaitsForApplyBeforeProjectRowExists(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	const productionID = "project-delete-before-row"
	unlockApply := lockProductionNotificationApply(productionID)
	deleteDone := make(chan error, 1)
	go func() {
		_, err := DeleteProject(productionID, "bot-token", db)
		deleteDone <- err
	}()

	select {
	case err := <-deleteDone:
		unlockApply()
		t.Fatalf("project deletion bypassed the Apply lock before the Project row existed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := db.Create(&model.Project{KitsuProjectID: productionID, Name: "Created while Apply held lock"}).Error; err != nil {
		unlockApply()
		t.Fatal(err)
	}
	unlockApply()
	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatalf("project deletion failed after Apply released the lock: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("project deletion did not resume after Apply released the lock")
	}
	if model.FindProjectByKitsuID(db, productionID) != nil {
		t.Fatal("project deletion did not remove the Project created while it waited")
	}
}

func TestCreateProjectDoesNotInheritOrphanRoutingState(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	const productionID = "kitsu-proj-reconnect"
	db.Create(&model.ProductionChannelMapping{
		ProductionID: productionID, GuildID: "guild-123", TaskTypeID: "tt-1", ChannelID: "channel-1",
		Active: true, State: model.ChannelMappingStateCurrent, MigrationState: model.ChannelMappingStateCurrent,
	})
	db.Create(&model.ProductionNotificationConfig{ProductionID: productionID, ProductionName: "Stale", Enabled: true})
	db.Create(&model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: "tt-1", DestinationWebhookID: 99})

	if err := model.CreateProject(db, productionID, "Reconnected", "kitsu", "guild-123", "category-1", "en"); err != nil {
		t.Fatalf("expected reconnect project creation to succeed, got %v", err)
	}
	if got := model.ListProductionChannelMappings(db, productionID); len(got) != 0 {
		t.Fatalf("stale channel mappings were inherited: %d", len(got))
	}
	if got := model.ListProductionNotificationRoutes(db, productionID); len(got) != 0 {
		t.Fatalf("stale notification routes were inherited: %d", len(got))
	}
	if got := model.FindProductionNotificationConfig(db, productionID); got != nil {
		t.Fatal("stale notification config was inherited")
	}
}
func TestSetupSQLiteFixtureEnablesForeignKeysAndHasNoViolations(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	var enabled int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&enabled).Error; err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", enabled)
	}
	var violations []struct {
		Table string
	}
	if err := db.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected no foreign key violations, got %d", len(violations))
	}
}

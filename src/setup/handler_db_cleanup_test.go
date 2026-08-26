package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type setupDiscordProvisioningStub struct {
	t                *testing.T
	categoryID       string
	createdChannelID []string
	deletedIDs       []string
	nextChannelNum   int
}

func newSetupHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Project{},
		&model.ProjectWebhook{},
		&model.ProductionChannelMapping{},
		&model.ProductionNotificationConfig{},
		&model.ProductionNotificationRoute{},
		&model.ProjectUserMap{},
		&model.ProjectCheckerMap{},
		&model.ProjectSetting{},
		&model.UserMap{},
		&model.CheckerMap{},
	); err != nil {
		t.Fatalf("failed to migrate setup schema: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys=ON").Error; err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}
	return db
}

func installSetupDiscordProvisioningStub(t *testing.T) *setupDiscordProvisioningStub {
	t.Helper()
	stub := &setupDiscordProvisioningStub{
		t:          t,
		categoryID: "cat-setup-test",
	}

	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "discord.com" {
			return nil, fmt.Errorf("unexpected host: %s", req.URL.Host)
		}
		if !strings.HasPrefix(req.URL.Path, "/api/v10/") {
			return nil, fmt.Errorf("unexpected path: %s", req.URL.Path)
		}

		switch {
		case req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/api/v10/guilds/") && strings.HasSuffix(req.URL.Path, "/channels"):
			var payload struct {
				Type int `json:"type"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload.Type == 4 {
				return jsonResp(http.StatusOK, `{"id":"`+stub.categoryID+`"}`), nil
			}
			if payload.Type == 0 {
				stub.nextChannelNum++
				channelID := fmt.Sprintf("ch-%d", stub.nextChannelNum)
				stub.createdChannelID = append(stub.createdChannelID, channelID)
				return jsonResp(http.StatusOK, `{"id":"`+channelID+`"}`), nil
			}
			return jsonResp(http.StatusBadRequest, `{"message":"unexpected channel type"}`), nil

		case req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/api/v10/channels/") && strings.HasSuffix(req.URL.Path, "/webhooks"):
			parts := strings.Split(req.URL.Path, "/")
			channelID := parts[len(parts)-2]
			return jsonResp(http.StatusOK, `{"id":"wh-`+channelID+`","token":"tok-`+channelID+`"}`), nil

		case req.Method == http.MethodDelete && strings.HasPrefix(req.URL.Path, "/api/v10/channels/"):
			parts := strings.Split(req.URL.Path, "/")
			channelID := parts[len(parts)-1]
			stub.deletedIDs = append(stub.deletedIDs, channelID)
			return jsonResp(http.StatusOK, `{"id":"`+channelID+`"}`), nil
		}

		return jsonResp(http.StatusNotFound, `{"message":"not found"}`), nil
	})
	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
	})
	return stub
}

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func registerWebhookCreateFailure(t *testing.T, db *gorm.DB) {
	t.Helper()
	const callbackName = "test:fail_project_webhook_create"
	err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "project_webhooks" {
			tx.AddError(errors.New("forced webhook insert failure"))
		}
	})
	if err != nil {
		t.Fatalf("failed to register callback: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(callbackName)
	})
}

func TestRunProjectSetup_CleansUpDiscordResourcesWhenDBTransactionFails(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	stub := installSetupDiscordProvisioningStub(t)
	registerWebhookCreateFailure(t, db)

	res := RunProjectSetup(
		"kitsu-proj-db-fail",
		"Project DB Fail",
		"cg",
		"en",
		"http://kitsu.local/",
		"fallback-guild",
		"guild-123",
		"bot-token",
		db,
	)

	if res.OK {
		t.Fatalf("expected setup failure when DB transaction fails")
	}
	if !res.SafeToRetry {
		t.Fatalf("expected SafeToRetry=true when cleanup succeeds")
	}
	if len(stub.createdChannelID) == 0 {
		t.Fatalf("expected stub to create channels")
	}
	expectedDeleteCount := len(stub.createdChannelID) + 1
	if len(stub.deletedIDs) != expectedDeleteCount {
		t.Fatalf("expected %d cleanup deletes, got %d (%v)", expectedDeleteCount, len(stub.deletedIDs), stub.deletedIDs)
	}
	for _, channelID := range stub.createdChannelID {
		if !containsString(stub.deletedIDs, channelID) {
			t.Fatalf("expected cleanup delete for channel %s; got %v", channelID, stub.deletedIDs)
		}
	}
	if !containsString(stub.deletedIDs, stub.categoryID) {
		t.Fatalf("expected cleanup delete for category %s; got %v", stub.categoryID, stub.deletedIDs)
	}
	if got := model.FindProjectByKitsuID(db, "kitsu-proj-db-fail"); got != nil {
		t.Fatalf("project should not persist after tx rollback")
	}
	if got := model.ListProjectWebhooks(db, "kitsu-proj-db-fail"); len(got) != 0 {
		t.Fatalf("webhooks should not persist after tx rollback, got %d", len(got))
	}
}

func TestCleanupSetupArtifactsRemovesOperationalState(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	installSetupDiscordProvisioningStub(t)
	if err := model.CreateProject(db, "kitsu-proj-cleanup", "Cleanup", "cg", "guild-123", "cat-setup-test", "en"); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}
	if err := model.CreateProjectWebhook(db, "kitsu-proj-cleanup", "concept", "Concept", "https://discord.invalid/concept", "channel-concept"); err != nil {
		t.Fatalf("failed to create webhook: %v", err)
	}
	db.Create(&model.ProductionChannelMapping{ProductionID: "kitsu-proj-cleanup", GuildID: "guild-123", TaskTypeID: "tt-1", ChannelID: "channel-concept", Active: true, State: model.ChannelMappingStateCurrent, MigrationState: model.ChannelMappingStateCurrent})
	db.Create(&model.ProductionNotificationConfig{ProductionID: "kitsu-proj-cleanup", ProductionName: "Cleanup", Enabled: true})
	db.Create(&model.ProductionNotificationRoute{ProductionID: "kitsu-proj-cleanup", TaskTypeID: "tt-1", DestinationWebhookID: 1, DestinationChannelID: "channel-concept"})

	cleanupSetupArtifacts("kitsu-proj-cleanup", "cat-setup-test", "bot-token", []createdSetupChannel{{ID: "channel-concept", Name: "concept"}}, db, &SetupResult{})
	if model.FindProjectByKitsuID(db, "kitsu-proj-cleanup") != nil || len(model.ListProjectWebhooks(db, "kitsu-proj-cleanup")) != 0 || len(model.ListProductionChannelMappings(db, "kitsu-proj-cleanup")) != 0 || len(model.ListProductionNotificationRoutes(db, "kitsu-proj-cleanup")) != 0 || model.FindProductionNotificationConfig(db, "kitsu-proj-cleanup") != nil {
		t.Fatal("setup cleanup left operational state behind")
	}
}

func TestRunProjectSetup_DoesNotCleanupOnSuccess(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	stub := installSetupDiscordProvisioningStub(t)

	res := RunProjectSetup(
		"kitsu-proj-success",
		"Project Success",
		"cg",
		"en",
		"http://kitsu.local/",
		"fallback-guild",
		"guild-123",
		"bot-token",
		db,
	)

	if !res.OK {
		t.Fatalf("expected setup success, got lines: %v", res.Lines)
	}
	if len(stub.deletedIDs) != 0 {
		t.Fatalf("expected no cleanup deletes on success path, got %v", stub.deletedIDs)
	}
	if got := model.FindProjectByKitsuID(db, "kitsu-proj-success"); got == nil {
		t.Fatalf("project should persist on success")
	}
	if got := model.ListProjectWebhooks(db, "kitsu-proj-success"); len(got) == 0 {
		t.Fatalf("expected persisted webhooks on success")
	}
}

func TestRunProjectSetup_PersistsRequestedGuildIDOnProjectRecord(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	installSetupDiscordProvisioningStub(t)

	res := RunProjectSetup(
		"kitsu-proj-requested-guild",
		"Project Requested Guild",
		"cg",
		"en",
		"http://kitsu.local/",
		"fallback-guild",
		"requested-guild",
		"bot-token",
		db,
	)

	if !res.OK {
		t.Fatalf("expected setup success, got lines: %v", res.Lines)
	}
	project := model.FindProjectByKitsuID(db, "kitsu-proj-requested-guild")
	if project == nil {
		t.Fatalf("expected project to persist on success")
	}
	if got := strings.TrimSpace(project.DiscordGuildID); got != "requested-guild" {
		t.Fatalf("expected requested guild to persist, got %q", got)
	}
}

func TestRunProjectSetup_UsesFallbackGuildIDWhenProjectInputIsBlank(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	installSetupDiscordProvisioningStub(t)

	res := RunProjectSetup(
		"kitsu-proj-fallback-guild",
		"Project Fallback Guild",
		"cg",
		"en",
		"http://kitsu.local/",
		"fallback-guild",
		"",
		"bot-token",
		db,
	)

	if !res.OK {
		t.Fatalf("expected setup success, got lines: %v", res.Lines)
	}
	project := model.FindProjectByKitsuID(db, "kitsu-proj-fallback-guild")
	if project == nil {
		t.Fatalf("expected project to persist on success")
	}
	if got := strings.TrimSpace(project.DiscordGuildID); got != "fallback-guild" {
		t.Fatalf("expected fallback guild to persist, got %q", got)
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

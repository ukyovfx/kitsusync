package setup

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

func TestSameProductionNotificationRoutesSkipsDiscordReorderForReviewerOnlyApply(t *testing.T) {
	before := []model.ProductionNotificationRoute{{TaskTypeID: "task-comp", TaskTypeName: "Old Name", DestinationWebhookID: 7}}
	after := []model.ProductionNotificationRoute{{TaskTypeID: "task-comp", TaskTypeName: "New Name", DestinationWebhookID: 7}}
	if !sameProductionNotificationRoutes(before, after) {
		t.Fatal("Task Type display-name refresh should not require a Discord reorder")
	}
	after[0].DestinationWebhookID = 8
	if sameProductionNotificationRoutes(before, after) {
		t.Fatal("destination change must require a Discord reorder")
	}
}

func TestProductionNotificationCompensationAttemptsDatabaseAndDiscordRestores(t *testing.T) {
	dbErr := errors.New("database restore failed")
	discordErr := errors.New("discord restore failed")
	dbCalled, discordCalled := false, false
	gotDB, gotDiscord := compensateProductionNotificationApply(func() error { dbCalled = true; return dbErr }, func() error { discordCalled = true; return discordErr })
	if !dbCalled || !discordCalled {
		t.Fatalf("both compensations must be attempted: database=%v discord=%v", dbCalled, discordCalled)
	}
	if !errors.Is(gotDB, dbErr) || !errors.Is(gotDiscord, discordErr) {
		t.Fatalf("compensation failures not reported independently: database=%v discord=%v", gotDB, gotDiscord)
	}
}

func TestProductionNotificationCompensationStillAttemptsDiscordAfterDatabaseRestoreFailure(t *testing.T) {
	dbErr := errors.New("database restore failed")
	discordCalled := false
	gotDB, gotDiscord := compensateProductionNotificationApply(func() error { return dbErr }, func() error { discordCalled = true; return nil })
	if !discordCalled || !errors.Is(gotDB, dbErr) || gotDiscord != nil {
		t.Fatalf("unexpected compensation result: database=%v discord=%v called=%v", gotDB, gotDiscord, discordCalled)
	}
}

func TestProductionNotificationRecoveryFailuresReturnExplicitNonSuccessDiagnostics(t *testing.T) {
	databaseErr := errors.New("db recovery failed")
	discordErr := errors.New("discord recovery failed")
	for _, tc := range []struct {
		name    string
		db      error
		discord error
		key     string
	}{
		{name: "database", db: databaseErr, key: "database_recovery"},
		{name: "Discord order", discord: discordErr, key: "discord_order_recovery"},
		{name: "both", db: databaseErr, discord: discordErr, key: "database_recovery"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := productionNotificationRecoveryDiagnostic(tc.db, tc.discord)
			if status < 400 || body["recovery"] != "incomplete" || body[tc.key] != "failed" || body["error"] == "" {
				t.Fatalf("recovery failure was not a fail-closed diagnostic: status=%d body=%#v", status, body)
			}
		})
	}
}

func TestProductionNotificationRecoveryDefersNewResourcesWhenDatabaseRestoreFails(t *testing.T) {
	databaseErr := errors.New("db recovery failed")
	status, body := productionNotificationRecoveryDiagnostic(databaseErr, nil)
	body["resource_cleanup"] = "deferred_database_restore_failed"
	if status < 400 || body["recovery"] != "incomplete" || body["database_recovery"] != "failed" || body["resource_cleanup"] != "deferred_database_restore_failed" {
		t.Fatalf("recovery must identify retained external resources when DB restore failed: status=%d body=%#v", status, body)
	}
}

func TestCreatedRoutingResourceCleanupAttemptsBothDiscordDeletes(t *testing.T) {
	oldWebhook, oldChannel := currentRoutingDeleteWebhook, currentRoutingDeleteChannel
	defer func() { currentRoutingDeleteWebhook, currentRoutingDeleteChannel = oldWebhook, oldChannel }()
	var deleted []string
	currentRoutingDeleteWebhook = func(id, _ string) error {
		deleted = append(deleted, "webhook:"+id)
		return errors.New("simulated webhook delete failure")
	}
	currentRoutingDeleteChannel = func(id, _ string) error { deleted = append(deleted, "channel:"+id); return nil }
	err := cleanupCreatedRoutingResources([]createdRoutingDiscordResource{{ChannelID: "channel-1", WebhookID: "webhook-1"}}, "synthetic-token")
	if err == nil || strings.Join(deleted, ",") != "webhook:webhook-1,channel:channel-1" {
		t.Fatalf("cleanup must attempt both deletes and report incomplete cleanup: deleted=%v err=%v", deleted, err)
	}
}

func TestDiscordWebhookIDAcceptsOnlyDiscordWebhookURLWithoutEchoingToken(t *testing.T) {
	if got, err := discordWebhookID("https://discord.com/api/webhooks/123456789012345678/not-a-real-token"); err != nil || got != "123456789012345678" {
		t.Fatalf("valid Discord webhook identity rejected: id=%q err=%v", got, err)
	}
	if _, err := discordWebhookID("https://example.invalid/api/webhooks/123/secret"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("untrusted webhook URL was not rejected safely: %v", err)
	}
}

func TestChannelCreateFailureReportsUnknownOutcome(t *testing.T) {
	response := httptest.NewRecorder()
	writeRoutingChannelCreateFailure(response, nil)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"channel_creation":"outcome_unknown"`) || !strings.Contains(response.Body.String(), `"resource_cleanup":"known_resources_complete"`) {
		t.Fatalf("failed create response must report its unknown remote outcome without claiming a route was applied: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProductionNotificationApplyCreatesRequestedChannelOnlyOnApply(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "apply-new-discord-channel", Name: "Apply New Channel", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	revision := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	oldTaskReader, oldCheck, oldList := reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels
	oldCreateChannel, oldCreateWebhook, oldDeleteWebhook, oldDeleteChannel, oldSetPositions := currentRoutingCreateChannel, currentRoutingCreateWebhook, currentRoutingDeleteWebhook, currentRoutingDeleteChannel, currentRoutingSetPositions
	defer func() {
		reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels = oldTaskReader, oldCheck, oldList
		currentRoutingCreateChannel, currentRoutingCreateWebhook, currentRoutingDeleteWebhook, currentRoutingDeleteChannel, currentRoutingSetPositions = oldCreateChannel, oldCreateWebhook, oldDeleteWebhook, oldDeleteChannel, oldSetPositions
	}()
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "task-comp", Name: "Compositing"}}
	}
	status := DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true, ManageWebhooks: true}}
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo { return status }
	channels := []DiscordGuildChannel{{ID: "channel-existing", Name: "already-managed", Type: 0, ParentID: "category-1", Position: 1}}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return append([]DiscordGuildChannel(nil), channels...), nil
	}
	createdChannels, createdWebhooks, reordered := 0, 0, false
	currentRoutingCreateChannel = func(guild, category, name, _ string) (string, error) {
		createdChannels++
		if guild != "guild-1" || category != "category-1" || name != "compositing" {
			t.Fatalf("unexpected Discord channel create arguments: guild=%s category=%s name=%s", guild, category, name)
		}
		channels = append(channels, DiscordGuildChannel{ID: "channel-new", Name: name, Type: 0, ParentID: category, Position: 2})
		return "channel-new", nil
	}
	currentRoutingCreateWebhook = func(channel, name, _ string) (string, error) {
		createdWebhooks++
		if channel != "channel-new" || name != "compositing" {
			t.Fatalf("unexpected Discord webhook create arguments: channel=%s name=%s", channel, name)
		}
		return "https://discord.com/api/webhooks/123456789012345678/synthetic-token", nil
	}
	currentRoutingSetPositions = func(_ string, positions []DiscordChannelPosition, _ string) error {
		reordered = len(positions) == 1 && positions[0].ID == "channel-new"
		return nil
	}
	currentRoutingDeleteWebhook = func(string, string) error { return nil }
	currentRoutingDeleteChannel = func(string, string) error { return nil }
	payload := `{"project_id":"apply-new-discord-channel","expected_revision":"` + revision + `","routes":[{"task_type_id":"task-comp","create_channel_name":"Compositing"}],"reviewer_changes":[]}`
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	if !handleProductionNotificationApply(response, request, "en", db, "synthetic-token") {
		t.Fatal("Apply endpoint did not handle the request")
	}
	if response.Code != http.StatusOK || createdChannels != 1 || createdWebhooks != 1 || !reordered {
		t.Fatalf("channel Apply did not complete the expected one-time resource flow: status=%d channels=%d webhooks=%d reordered=%v body=%s", response.Code, createdChannels, createdWebhooks, reordered, response.Body.String())
	}
	routes := model.ListProductionNotificationRoutes(db, project.KitsuProjectID)
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	if len(routes) != 1 || len(webhooks) != 1 || routes[0].DestinationWebhookID != webhooks[0].ID || webhooks[0].DiscordChannelID != "channel-new" {
		t.Fatalf("new managed channel and route were not persisted together: routes=%#v webhooks=%#v", routes, webhooks)
	}
}

func TestProductionNotificationApplyRejectsReviewerDeltaForDeletedRouteBeforeExternalReads(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "apply-deleted-delta", Name: "Apply Deleted Delta"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	oldTaskReader := reviewerTaskTypesForProduction
	reads := 0
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType { reads++; return nil }
	defer func() { reviewerTaskTypesForProduction = oldTaskReader }()
	body := []byte(`{"project_id":"apply-deleted-delta","expected_revision":"stale-but-present","routes":[],"reviewer_changes":[{"task_type_id":"removed-task","add_user_ids":["123456789012345678"]}]}`)
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	if !handleProductionNotificationApply(response, request, "en", db) {
		t.Fatal("Apply endpoint did not handle the request")
	}
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("reviewer_delta_for_removed_route")) {
		t.Fatalf("deleted-route reviewer delta was not explicitly rejected: status=%d body=%s", response.Code, response.Body.String())
	}
	if reads != 0 || len(model.ListProductionNotificationRoutes(db, project.KitsuProjectID)) != 0 || len(model.ListProjectReviewerTargets(db, project.ID)) != 0 {
		t.Fatalf("invalid delta caused external reads or writes: reads=%d routes=%#v targets=%#v", reads, model.ListProductionNotificationRoutes(db, project.KitsuProjectID), model.ListProjectReviewerTargets(db, project.ID))
	}
}

package setup

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	oldCreateChannel, oldCreateWebhook, oldDeleteWebhook, oldDeleteChannel, oldSetPositions, oldInspect := currentRoutingCreateChannel, currentRoutingCreateWebhook, currentRoutingDeleteWebhook, currentRoutingDeleteChannel, currentRoutingSetPositions, currentRoutingInspectWebhook
	defer func() {
		reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels = oldTaskReader, oldCheck, oldList
		currentRoutingCreateChannel, currentRoutingCreateWebhook, currentRoutingDeleteWebhook, currentRoutingDeleteChannel, currentRoutingSetPositions = oldCreateChannel, oldCreateWebhook, oldDeleteWebhook, oldDeleteChannel, oldSetPositions
		currentRoutingInspectWebhook = oldInspect
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
	currentRoutingInspectWebhook = func(string) (routingWebhookIdentity, bool, error) {
		return routingWebhookIdentity{ID: "123456789012345678", Type: 1, GuildID: "guild-1", ChannelID: "channel-new"}, false, nil
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

func TestProductionNotificationApplyPreservesOrRepairsManagedWebhook(t *testing.T) {
	for _, tc := range []struct {
		name            string
		reorder         bool
		missingWebhook  bool
		wrongChannel    bool
		createFailure   bool
		verifyFailure   bool
		inspectFailure  bool
		staleRevision   bool
		wantStatus      int
		wantNewWebhooks int
	}{
		{name: "reorder preserves destinations", reorder: true, wantStatus: http.StatusOK},
		{name: "destination switch reuses verified webhook", wantStatus: http.StatusOK},
		{name: "missing destination webhook is repaired on Apply", missingWebhook: true, wantStatus: http.StatusOK, wantNewWebhooks: 1},
		{name: "moved webhook is rejected without repair", wrongChannel: true, wantStatus: http.StatusBadRequest},
		{name: "webhook repair failure leaves routing unchanged", missingWebhook: true, createFailure: true, wantStatus: http.StatusBadGateway},
		{name: "webhook verification outage is reported as unavailable", inspectFailure: true, wantStatus: http.StatusBadGateway},
		{name: "unverified repair removes only its new webhook", missingWebhook: true, verifyFailure: true, wantStatus: http.StatusBadGateway},
		{name: "stale revision never creates repair webhook", missingWebhook: true, staleRevision: true, wantStatus: http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newIAViewDB(t)
			project := model.Project{KitsuProjectID: "apply-webhook-identity", Name: "Webhook Identity", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
			if err := db.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
			urls := []string{"https://discord.com/api/webhooks/111111111111111111/token-a", "https://discord.com/api/webhooks/222222222222222222/token-b", "https://discord.com/api/webhooks/333333333333333333/token-c"}
			for i, name := range []string{"first", "second", "third"} {
				if err := model.CreateProjectWebhook(db, project.KitsuProjectID, name, "", urls[i], "channel-"+name); err != nil {
					t.Fatal(err)
				}
			}
			webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
			original := []model.ProductionNotificationRoute{{TaskTypeID: "task-a", DestinationWebhookID: webhooks[0].ID}, {TaskTypeID: "task-b", DestinationWebhookID: webhooks[1].ID}}
			if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, original); err != nil {
				t.Fatal(err)
			}
			revision := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
			oldTasks, oldCheck, oldList, oldInspect := reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook
			oldCreate, oldCreateChannel, oldDelete, oldDeleteChannel, oldPositions := currentRoutingCreateWebhook, currentRoutingCreateChannel, currentRoutingDeleteWebhook, currentRoutingDeleteChannel, currentRoutingSetPositions
			t.Cleanup(func() {
				reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook = oldTasks, oldCheck, oldList, oldInspect
				currentRoutingCreateWebhook, currentRoutingCreateChannel, currentRoutingDeleteWebhook, currentRoutingDeleteChannel, currentRoutingSetPositions = oldCreate, oldCreateChannel, oldDelete, oldDeleteChannel, oldPositions
			})
			reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType {
				return []kitsu.TaskType{{ID: "task-a", Name: "First"}, {ID: "task-b", Name: "Second"}}
			}
			currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
				return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true, ManageWebhooks: true}}
			}
			currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
				return []DiscordGuildChannel{{ID: "channel-first", Name: "first", Type: 0, ParentID: "category-1", Position: 1}, {ID: "channel-second", Name: "second", Type: 0, ParentID: "category-1", Position: 2}, {ID: "channel-third", Name: "third", Type: 0, ParentID: "category-1", Position: 3}}, nil
			}
			currentRoutingInspectWebhook = func(raw string) (routingWebhookIdentity, bool, error) {
				if tc.inspectFailure {
					return routingWebhookIdentity{}, false, errors.New("synthetic Discord API outage")
				}
				if raw == urls[2] && tc.missingWebhook {
					return routingWebhookIdentity{}, true, nil
				}
				if raw == urls[2] && tc.wrongChannel {
					return routingWebhookIdentity{ID: "333333333333333333", Type: 1, GuildID: "guild-1", ChannelID: "unrelated-channel"}, false, nil
				}
				if raw == "https://discord.com/api/webhooks/444444444444444444/repaired" && tc.verifyFailure {
					return routingWebhookIdentity{ID: "444444444444444444", Type: 1, GuildID: "guild-1", ChannelID: "unrelated-channel"}, false, nil
				}
				for i, known := range append(urls, "https://discord.com/api/webhooks/444444444444444444/repaired") {
					if raw == known {
						channels := []string{"channel-first", "channel-second", "channel-third", "channel-third"}
						id, _ := discordWebhookID(raw)
						return routingWebhookIdentity{ID: id, Type: 1, GuildID: "guild-1", ChannelID: channels[i]}, false, nil
					}
				}
				return routingWebhookIdentity{}, false, errors.New("unexpected webhook read")
			}
			createCount, reorderCount, deleteCount := 0, 0, 0
			currentRoutingCreateWebhook = func(channel, name, _ string) (string, error) {
				createCount++
				if channel != "channel-third" || name != "third" || !tc.missingWebhook {
					t.Fatalf("unexpected webhook creation for %q/%q", channel, name)
				}
				if tc.createFailure {
					return "", errors.New("synthetic creation failure")
				}
				return "https://discord.com/api/webhooks/444444444444444444/repaired", nil
			}
			currentRoutingCreateChannel = func(string, string, string, string) (string, error) {
				t.Fatal("existing-channel Apply must not create a channel")
				return "", nil
			}
			currentRoutingDeleteWebhook = func(id, _ string) error {
				if id != "444444444444444444" {
					t.Fatalf("preexisting webhook %s must never be deleted", id)
				}
				deleteCount++
				return nil
			}
			currentRoutingDeleteChannel = func(string, string) error { t.Fatal("preexisting channel must never be deleted"); return nil }
			currentRoutingSetPositions = func(string, []DiscordChannelPosition, string) error { reorderCount++; return nil }
			firstID, secondID := webhooks[2].ID, webhooks[1].ID
			if tc.reorder {
				firstID, secondID = webhooks[1].ID, webhooks[0].ID
			}
			requested := `{"project_id":"apply-webhook-identity","expected_revision":"` + revision + `","routes":[{"task_type_id":"task-a","destination_webhook_id":` + strconv.FormatUint(uint64(firstID), 10) + `},{"task_type_id":"task-b","destination_webhook_id":` + strconv.FormatUint(uint64(secondID), 10) + `}],"reviewer_changes":[]}`
			if tc.reorder {
				requested = `{"project_id":"apply-webhook-identity","expected_revision":"` + revision + `","routes":[{"task_type_id":"task-b","destination_webhook_id":` + strconv.FormatUint(uint64(firstID), 10) + `},{"task_type_id":"task-a","destination_webhook_id":` + strconv.FormatUint(uint64(secondID), 10) + `}],"reviewer_changes":[]}`
			}
			if tc.staleRevision {
				requested = strings.Replace(requested, revision, "stale-revision", 1)
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", strings.NewReader(requested))
			if !handleProductionNotificationApply(response, request, "en", db) || response.Code != tc.wantStatus {
				t.Fatalf("Apply status=%d want=%d body=%s", response.Code, tc.wantStatus, response.Body.String())
			}
			if tc.createFailure && !strings.Contains(response.Body.String(), `"webhook_creation":"outcome_unknown"`) {
				t.Fatalf("webhook creation failure concealed an unknown external outcome: %s", response.Body.String())
			}
			if tc.inspectFailure && !strings.Contains(response.Body.String(), `"error":"webhook_verification_unavailable"`) {
				t.Fatalf("webhook verification failure was not reported as unavailable: %s", response.Body.String())
			}
			expectedAttempts := tc.wantNewWebhooks
			if tc.createFailure || tc.verifyFailure {
				expectedAttempts = 1
			}
			expectedDeletes := 0
			if tc.verifyFailure {
				expectedDeletes = 1
			}
			if createCount != expectedAttempts || len(model.ListProjectWebhooks(db, project.KitsuProjectID)) != len(webhooks)+tc.wantNewWebhooks || deleteCount != expectedDeletes {
				t.Fatalf("unexpected resource counts: created=%d persisted=%d deleted=%d", createCount, len(model.ListProjectWebhooks(db, project.KitsuProjectID)), deleteCount)
			}
			got := model.ListProductionNotificationRoutes(db, project.KitsuProjectID)
			if tc.createFailure || tc.verifyFailure || tc.inspectFailure || tc.staleRevision || tc.wrongChannel {
				if len(got) != 2 || got[0].DestinationWebhookID != webhooks[0].ID || got[1].DestinationWebhookID != webhooks[1].ID {
					t.Fatalf("failed Apply changed routes: %#v", got)
				}
				return
			}
			if len(got) != 2 || reorderCount != 1 {
				t.Fatalf("saved routing/order incomplete: routes=%#v reorder=%d", got, reorderCount)
			}
			if tc.reorder {
				if got[0].TaskTypeID != "task-b" || got[0].DestinationWebhookID != webhooks[1].ID || got[1].TaskTypeID != "task-a" || got[1].DestinationWebhookID != webhooks[0].ID {
					t.Fatalf("reorder redirected a route: %#v", got)
				}
			} else if tc.missingWebhook {
				if got[0].DestinationWebhookID == webhooks[2].ID || got[0].DestinationWebhookID == 0 {
					t.Fatalf("missing webhook was not repaired: %#v", got)
				}
			} else if got[0].DestinationWebhookID != webhooks[2].ID {
				t.Fatalf("destination switch lost selected webhook: %#v", got)
			}
		})
	}
}

func TestProductionNotificationApplyCleansWebhookRepairWhenLaterChannelPreflightFails(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cleanupFails bool
		wantStatus   int
		wantCleanup  string
	}{
		{name: "cleanup succeeds", wantStatus: http.StatusBadGateway, wantCleanup: `"resource_cleanup":"complete"`},
		{name: "cleanup fails closed", cleanupFails: true, wantStatus: http.StatusInternalServerError, wantCleanup: `"resource_cleanup":"incomplete"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newIAViewDB(t)
			project := model.Project{KitsuProjectID: "apply-repair-then-channel-failure", Name: "Repair Then Channel Failure", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
			if err := db.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
			oldURL := "https://discord.com/api/webhooks/333333333333333333/old-token"
			if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "existing-channel", "", oldURL, "channel-existing"); err != nil {
				t.Fatal(err)
			}
			webhook := model.FindChannelRecord(db, project.KitsuProjectID, "existing-channel")
			if webhook == nil {
				t.Fatal("existing managed webhook fixture was not persisted")
			}
			originalRoutes := []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-existing", TaskTypeName: "Existing", DestinationWebhookID: webhook.ID}}
			if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, originalRoutes); err != nil {
				t.Fatal(err)
			}
			revision := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
			oldTasks, oldCheck, oldList := reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels
			oldCreateChannel, oldCreateWebhook, oldDeleteWebhook, oldDeleteChannel, oldSetPositions, oldInspect := currentRoutingCreateChannel, currentRoutingCreateWebhook, currentRoutingDeleteWebhook, currentRoutingDeleteChannel, currentRoutingSetPositions, currentRoutingInspectWebhook
			t.Cleanup(func() {
				reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels = oldTasks, oldCheck, oldList
				currentRoutingCreateChannel, currentRoutingCreateWebhook, currentRoutingDeleteWebhook, currentRoutingDeleteChannel, currentRoutingSetPositions, currentRoutingInspectWebhook = oldCreateChannel, oldCreateWebhook, oldDeleteWebhook, oldDeleteChannel, oldSetPositions, oldInspect
			})
			reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType {
				return []kitsu.TaskType{{ID: "task-existing", Name: "Existing"}, {ID: "task-new", Name: "New"}}
			}
			repairCreated := false
			channelPreflightFailed := false
			currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
				if repairCreated {
					channelPreflightFailed = true
					return DiscordStatusInfo{}
				}
				return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true, ManageWebhooks: true}}
			}
			currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
				return []DiscordGuildChannel{{ID: "channel-existing", Name: "existing-channel", Type: 0, ParentID: "category-1", Position: 1}}, nil
			}
			currentRoutingInspectWebhook = func(raw string) (routingWebhookIdentity, bool, error) {
				if raw == oldURL {
					return routingWebhookIdentity{}, true, nil
				}
				if raw == "https://discord.com/api/webhooks/444444444444444444/repaired" {
					return routingWebhookIdentity{ID: "444444444444444444", Type: 1, GuildID: "guild-1", ChannelID: "channel-existing"}, false, nil
				}
				return routingWebhookIdentity{}, false, errors.New("unexpected webhook inspection")
			}
			currentRoutingCreateWebhook = func(channel, name, _ string) (string, error) {
				if channel != "channel-existing" || name != "existing-channel" {
					t.Fatalf("unexpected repair target: channel=%q name=%q", channel, name)
				}
				repairCreated = true
				return "https://discord.com/api/webhooks/444444444444444444/repaired", nil
			}
			currentRoutingCreateChannel = func(string, string, string, string) (string, error) {
				t.Fatal("channel creation must not start after the second Discord preflight fails")
				return "", nil
			}
			deletedWebhooks := []string{}
			currentRoutingDeleteWebhook = func(id, _ string) error {
				deletedWebhooks = append(deletedWebhooks, id)
				if tc.cleanupFails {
					return errors.New("synthetic webhook delete failure")
				}
				return nil
			}
			currentRoutingDeleteChannel = func(string, string) error {
				t.Fatal("no newly created channel should be present to clean up")
				return nil
			}
			currentRoutingSetPositions = func(string, []DiscordChannelPosition, string) error {
				t.Fatal("Discord reorder must not run before a failed preflight")
				return nil
			}
			body := `{"project_id":"apply-repair-then-channel-failure","expected_revision":"` + revision + `","routes":[{"task_type_id":"task-existing","destination_webhook_id":` + strconv.FormatUint(uint64(webhook.ID), 10) + `},{"task_type_id":"task-new","create_channel_name":"new-channel"}],"reviewer_changes":[]}`
			request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			if !handleProductionNotificationApply(response, request, "en", db) {
				t.Fatal("Apply endpoint did not handle the request")
			}
			if !repairCreated || !channelPreflightFailed {
				t.Fatalf("expected channel preflight to fail after the repair webhook was created: repair_created=%t channel_preflight_failed=%t", repairCreated, channelPreflightFailed)
			}
			if response.Code != tc.wantStatus || !strings.Contains(response.Body.String(), `"error":"discord_preflight_failed"`) || !strings.Contains(response.Body.String(), tc.wantCleanup) {
				t.Fatalf("failed post-repair channel preflight must report failure and cleanup outcome: status=%d body=%s", response.Code, response.Body.String())
			}
			if len(deletedWebhooks) != 1 || deletedWebhooks[0] != "444444444444444444" {
				t.Fatalf("the newly repaired webhook must be cleaned up exactly once: deleted=%v", deletedWebhooks)
			}
			if got := model.ListProductionNotificationRoutes(db, project.KitsuProjectID); len(got) != 1 || got[0].DestinationWebhookID != webhook.ID {
				t.Fatalf("failed Apply changed persisted routes: %#v", got)
			}
			if got := model.ListProjectWebhooks(db, project.KitsuProjectID); len(got) != 1 || got[0].WebhookURL != oldURL {
				t.Fatalf("failed Apply changed persisted webhook state: %#v", got)
			}
		})
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

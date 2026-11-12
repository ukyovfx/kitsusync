package setup

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

// TestProductionNotificationApplySavesDestinationKind reproduces the P2
// finding: the Apply contract dropped the submitted destination kind, so dm /
// record_only lanes could not be saved from the admin screen (they always came
// back as channel). Apply must persist the kind and the routing state must
// re-display it.
func TestProductionNotificationApplySavesDestinationKind(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "apply-lane-kind", Name: "Lane Kind", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "first", "", "https://discord.com/api/webhooks/111111111111111111/token-a", "channel-first"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "task-a", DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-first"}}); err != nil {
		t.Fatal(err)
	}
	revision := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)

	oldTasks, oldCheck, oldList, oldInspect := reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook
	t.Cleanup(func() {
		reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook = oldTasks, oldCheck, oldList, oldInspect
	})
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "task-a", Name: "First"}, {ID: "task-b", Name: "Second"}}
	}
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true, ManageWebhooks: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-first", Name: "first", Type: 0, ParentID: "category-1", Position: 1}}, nil
	}
	currentRoutingInspectWebhook = func(raw string) (routingWebhookIdentity, bool, error) {
		id, _ := discordWebhookID(raw)
		return routingWebhookIdentity{ID: id, Type: 1, GuildID: "guild-1", ChannelID: "channel-first"}, false, nil
	}
	oldPositions := currentRoutingSetPositions
	currentRoutingSetPositions = func(string, []DiscordChannelPosition, string) error { return nil }
	t.Cleanup(func() { currentRoutingSetPositions = oldPositions })

	requested := `{"project_id":"apply-lane-kind","expected_revision":"` + revision + `","routes":[` +
		`{"task_type_id":"task-a","destination_kind":"dm","destination_webhook_id":` + strconv.FormatUint(uint64(webhook.ID), 10) + `},` +
		`{"task_type_id":"task-b","destination_kind":"record_only"}` +
		`],"reviewer_changes":[]}`
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", strings.NewReader(requested))
	if !handleProductionNotificationApply(response, request, "en", db) || response.Code != http.StatusOK {
		t.Fatalf("Apply with destination kinds failed: status=%d body=%s", response.Code, response.Body.String())
	}

	routes := model.ListProductionNotificationRoutes(db, project.KitsuProjectID)
	byTaskType := map[string]model.ProductionNotificationRoute{}
	for _, route := range routes {
		byTaskType[route.TaskTypeID] = route
	}
	if got := byTaskType["task-a"].DestinationKind; got != model.LaneDM {
		t.Fatalf("task-a lane kind = %q, want dm", got)
	}
	if got := byTaskType["task-b"].DestinationKind; got != model.LaneRecordOnly {
		t.Fatalf("task-b lane kind = %q, want record_only", got)
	}
	if got := byTaskType["task-b"].DestinationWebhookID; got != 0 {
		t.Fatalf("record-only route must not require a webhook, got %d", got)
	}
	// The routing state renderer re-displays the saved lane kind.
	rendered := renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang=en", nil), project, "en", "", productionNotificationReviewerView{})
	if !strings.Contains(rendered, `data-lane-kind="dm"`) || !strings.Contains(rendered, `data-lane-kind="record_only"`) {
		t.Fatal("routing editor does not re-display the saved lane kinds")
	}
}

// TestProductionNotificationApplyDefaultsEmptyDestinationKindToChannel guards
// the legacy-client path: a route submitted without destination_kind keeps the
// channel lane.
func TestProductionNotificationApplyDefaultsEmptyDestinationKindToChannel(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "apply-lane-default", Name: "Lane Default", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "first", "", "https://discord.com/api/webhooks/111111111111111111/token-a", "channel-first"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "task-a", DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-first"}}); err != nil {
		t.Fatal(err)
	}
	revision := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)

	oldTasks, oldCheck, oldList, oldInspect := reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook
	t.Cleanup(func() {
		reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook = oldTasks, oldCheck, oldList, oldInspect
	})
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "task-a", Name: "First"}}
	}
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true, ManageWebhooks: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-first", Name: "first", Type: 0, ParentID: "category-1", Position: 1}}, nil
	}
	currentRoutingInspectWebhook = func(raw string) (routingWebhookIdentity, bool, error) {
		id, _ := discordWebhookID(raw)
		return routingWebhookIdentity{ID: id, Type: 1, GuildID: "guild-1", ChannelID: "channel-first"}, false, nil
	}

	requested := `{"project_id":"apply-lane-default","expected_revision":"` + revision + `","routes":[{"task_type_id":"task-a","destination_webhook_id":` + strconv.FormatUint(uint64(webhook.ID), 10) + `}],"reviewer_changes":[]}`
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", strings.NewReader(requested))
	if !handleProductionNotificationApply(response, request, "en", db) || response.Code != http.StatusOK {
		t.Fatalf("legacy Apply without destination_kind failed: status=%d body=%s", response.Code, response.Body.String())
	}
	routes := model.ListProductionNotificationRoutes(db, project.KitsuProjectID)
	if len(routes) != 1 || routes[0].DestinationKind != model.LaneChannel {
		t.Fatalf("legacy route lane kind = %#v, want channel", routes)
	}
}

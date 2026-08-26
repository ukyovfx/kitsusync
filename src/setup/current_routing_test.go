package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/model"
)

func TestSyncCurrentRoutingDiscordOrderUsesVerifiedOwnedChannels(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-sync", Name: "Routing Sync", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	db.Create(&project)
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "test-token")
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "first", "", "https://example.invalid/1", "channel-1"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "second", "", "https://example.invalid/2", "channel-2"); err != nil {
		t.Fatal(err)
	}
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	routes := []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "two", DestinationWebhookID: webhooks[1].ID}, {ProductionID: project.KitsuProjectID, TaskTypeID: "one", DestinationWebhookID: webhooks[0].ID}}
	oldCheck, oldList, oldSet := currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingSetPositions
	defer func() {
		currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingSetPositions = oldCheck, oldList, oldSet
	}()
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-1", Type: 0, ParentID: "category-1", Position: 4}, {ID: "channel-2", Type: 0, ParentID: "category-1", Position: 5}}, nil
	}
	var gotGuild string
	var got []DiscordChannelPosition
	currentRoutingSetPositions = func(guild string, positions []DiscordChannelPosition, _ string) error {
		gotGuild, got = guild, positions
		return nil
	}
	if err := syncCurrentRoutingDiscordOrder(project, routes, db); err != nil {
		t.Fatal(err)
	}
	if gotGuild != "guild-1" || len(got) != 2 || got[0].ID != "channel-2" || got[0].Position != 4 || got[1].ID != "channel-1" || got[1].Position != 5 {
		t.Fatalf("unexpected position payload: %#v", got)
	}
}

func TestSyncCurrentRoutingDiscordOrderCompactsPositionsWhenAllCategoryChannelsAreOwned(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-sync-compact", Name: "Routing Sync Compact", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	db.Create(&project)
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "test-token")
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "first", "", "https://example.invalid/1", "channel-1"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "second", "", "https://example.invalid/2", "channel-2"); err != nil {
		t.Fatal(err)
	}
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	routes := []model.ProductionNotificationRoute{{TaskTypeID: "one", DestinationWebhookID: webhooks[0].ID}, {TaskTypeID: "two", DestinationWebhookID: webhooks[1].ID}}
	oldCheck, oldList, oldSet := currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingSetPositions
	defer func() {
		currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingSetPositions = oldCheck, oldList, oldSet
	}()
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-1", Type: 0, ParentID: "category-1", Position: 4}, {ID: "channel-2", Type: 0, ParentID: "category-1", Position: 6}}, nil
	}
	var got []DiscordChannelPosition
	currentRoutingSetPositions = func(_ string, positions []DiscordChannelPosition, _ string) error { got = positions; return nil }
	if err := syncCurrentRoutingDiscordOrder(project, routes, db); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Position != 4 || got[1].Position != 5 {
		t.Fatalf("owned category positions were not compacted: %#v", got)
	}
}

func TestSyncCurrentRoutingDiscordOrderPreservesSlotsForUnrelatedChannels(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-sync-unrelated", Name: "Routing Sync Unrelated", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	db.Create(&project)
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "test-token")
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "first", "", "https://example.invalid/1", "channel-1"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "second", "", "https://example.invalid/2", "channel-2"); err != nil {
		t.Fatal(err)
	}
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	routes := []model.ProductionNotificationRoute{{TaskTypeID: "one", DestinationWebhookID: webhooks[0].ID}, {TaskTypeID: "two", DestinationWebhookID: webhooks[1].ID}}
	oldCheck, oldList, oldSet := currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingSetPositions
	defer func() {
		currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingSetPositions = oldCheck, oldList, oldSet
	}()
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-1", Type: 0, ParentID: "category-1", Position: 4}, {ID: "unrelated", Type: 0, ParentID: "category-1", Position: 5}, {ID: "channel-2", Type: 0, ParentID: "category-1", Position: 6}}, nil
	}
	var got []DiscordChannelPosition
	currentRoutingSetPositions = func(_ string, positions []DiscordChannelPosition, _ string) error { got = positions; return nil }
	if err := syncCurrentRoutingDiscordOrder(project, routes, db); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Position != 4 || got[1].Position != 6 {
		t.Fatalf("unrelated channel slot was not preserved: %#v", got)
	}
}

func TestRoutingDiscordStatusReadyRequiresBotGuildAndManageChannels(t *testing.T) {
	if routingDiscordStatusReady(DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}) == false {
		t.Fatal("complete routing preflight should be ready")
	}
	for _, status := range []DiscordStatusInfo{
		{GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}},
		{BotValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}},
		{BotValid: true, GuildValid: true},
	} {
		if routingDiscordStatusReady(status) {
			t.Fatalf("incomplete routing preflight should be blocked: %#v", status)
		}
	}
}

func TestCurrentRoutingEditorDeleteDialogDoesNotBlockSaveForm(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-editor-form", Name: "Routing Editor Form"}
	db.Create(&project)
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "owned", "", "https://example.invalid/owned", "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-1", TaskTypeName: "Task 1", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	body := renderCurrentIARoutingEditorSetupStyle(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang=en", nil), project, "en")
	if got := strings.Count(body, "<form"); got != 1 {
		t.Fatalf("routing editor rendered %d forms; want only the save form", got)
	}
	if strings.Contains(body, `name="confirm_name"`) || strings.Contains(body, " required") {
		t.Fatal("delete confirmation controls can participate in save-form validation")
	}
	if !strings.Contains(body, `data-routing-delete-form`) {
		t.Fatal("delete dialog staging container is missing")
	}
}

func TestCurrentRoutingChannelDeleteRequiresExactNameAndOwnership(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-delete", Name: "Routing Delete", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	db.Create(&project)
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "test-token")
	model.CreateProjectWebhook(db, project.KitsuProjectID, "owned", "", "https://example.invalid/owned", "channel-1")
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	oldCheck, oldList, oldDelete := currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingDeleteChannel
	defer func() {
		currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingDeleteChannel = oldCheck, oldList, oldDelete
	}()
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-1", Type: 0, ParentID: "category-1"}}, nil
	}
	deleted := false
	currentRoutingDeleteChannel = func(string, string) error { deleted = true; return nil }
	form := url.Values{"project_id": {project.KitsuProjectID}, "webhook_id": {"1"}, "action": {"delete_current_routing_channel"}, "confirm_name": {"wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleCurrentIARoutingMutation(rec, req, "en", db)
	if deleted {
		t.Fatal("wrong confirmation reached Discord delete")
	}
	if got := model.FindProjectWebhookByID(db, webhook.ID); got == nil {
		t.Fatal("wrong confirmation removed local ownership")
	}
}

func TestCurrentRoutingChannelDeleteRemovesOnlyVerifiedRouteAndWebhook(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-delete-success", Name: "Routing Delete Success", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	db.Create(&project)
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "test-token")
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "owned", "", "https://example.invalid/owned", "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-1", TaskTypeName: "Task 1", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	oldCheck, oldList, oldDelete := currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingDeleteChannel
	defer func() {
		currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingDeleteChannel = oldCheck, oldList, oldDelete
	}()
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-1", Type: 0, ParentID: "category-1"}}, nil
	}
	deleted := false
	currentRoutingDeleteChannel = func(string, string) error { deleted = true; return nil }
	form := url.Values{"project_id": {project.KitsuProjectID}, "webhook_id": {"1"}, "action": {"delete_current_routing_channel"}, "confirm_name": {"owned"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleCurrentIARoutingMutation(rec, req, "en", db)
	if !deleted || rec.Code != http.StatusSeeOther {
		t.Fatalf("verified delete did not complete: deleted=%v status=%d", deleted, rec.Code)
	}
	if model.FindProjectWebhookByID(db, webhook.ID) != nil {
		t.Fatal("verified channel delete left local webhook state")
	}
	if routes := model.ListProductionNotificationRoutes(db, project.KitsuProjectID); len(routes) != 0 {
		t.Fatalf("verified channel delete left local routes: %#v", routes)
	}
}

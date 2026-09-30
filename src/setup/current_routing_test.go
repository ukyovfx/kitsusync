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
	body := renderCurrentIARoutingEditorSetupStyle(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang=en", nil), project, "en", "")
	if got := strings.Count(body, `data-current-routing-form data-async-notification-apply`); got != 1 {
		t.Fatalf("routing editor rendered %d apply forms; want one global Apply form", got)
	}
	if strings.Contains(body, `name="confirm_name"`) {
		t.Fatal("delete confirmation controls can participate in save-form validation")
	}
	deleteStart := strings.Index(body, `class="routing-delete-dialog"`)
	if deleteStart < 0 {
		t.Fatal("delete confirmation dialog is missing")
	}
	deleteEnd := strings.Index(body[deleteStart:], `</dialog>`)
	if deleteEnd < 0 || strings.Contains(body[deleteStart:deleteStart+deleteEnd], " required") {
		t.Fatal("delete confirmation controls can participate in save-form validation")
	}
	if !strings.Contains(body, `data-new-channel-name maxlength="100" autocomplete="off" disabled required`) {
		t.Fatal("new-channel name must start disabled until New channel mode is selected")
	}
	if !strings.Contains(body, `data-routing-delete-form`) {
		t.Fatal("delete dialog staging container is missing")
	}
	formEnd := strings.Index(body, "</form>")
	deleteDialog := strings.Index(body, `class="routing-delete-dialog"`)
	if formEnd < 0 || deleteDialog < formEnd {
		t.Fatal("destructive delete dialog must stay outside the Apply form")
	}
}

func TestCurrentRoutingEditorKeepsAddDialogOutsideApplyForm(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-editor-dialog", Name: "Routing Editor Dialog"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	body := renderCurrentIARoutingEditorSetupStyle(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang=en", nil), project, "en", "")
	formEnd := strings.Index(body, "</form>")
	dialogStart := strings.Index(body, "<dialog data-wfa-add-modal")
	if formEnd < 0 || dialogStart < 0 || dialogStart < formEnd {
		t.Fatal("recipient dialog must be outside the Apply form to avoid invalid nested forms")
	}
	if strings.Contains(body[dialogStart:], `<form method="dialog">`) {
		t.Fatal("recipient dialog must not introduce a nested form")
	}
}

func TestCurrentRoutingEditorNewRouteTemplateCanBeRemoved(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-editor-new-route", Name: "Routing Editor New Route"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	body := renderCurrentIARoutingEditorSetupStyle(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang=en", nil), project, "en", "")
	templateStart := strings.Index(body, `<tr data-routing-new-row hidden>`)
	if templateStart < 0 {
		t.Fatal("new route template is missing")
	}
	templateEnd := strings.Index(body[templateStart:], `</tr>`)
	if templateEnd < 0 {
		t.Fatal("new route template row is incomplete")
	}
	template := body[templateStart : templateStart+templateEnd]
	for _, control := range []string{`class="routing-row-menu"`, `data-routing-remove`, `data-routing-undo`} {
		if !strings.Contains(template, control) {
			t.Fatalf("new route template is missing removable-route control %s", control)
		}
	}
}

func TestCurrentRoutingEditorScriptFindsSiblingAddDialog(t *testing.T) {
	script := currentRoutingEditorScript()
	if !strings.Contains(script, `form.parentElement.querySelector('[data-wfa-add-modal]')`) {
		t.Fatal("Apply editor script must find the add-recipient dialog outside the Apply form")
	}
	if !strings.Contains(script, `var select=kind==='role'?modal.querySelector('[data-wfa-role-id]'):modal.querySelector('[data-wfa-user-id]')`) {
		t.Fatal("pending recipient labels must be resolved from options in the sibling add-recipient dialog")
	}
}

func TestCurrentRecipientListboxEscapesDialogClippingWithOpaqueSurface(t *testing.T) {
	if !strings.Contains(adminThemeCSS, `.recipient-listbox{position:fixed;`) || !strings.Contains(adminThemeCSS, `background:var(--bg2);`) {
		t.Fatal("recipient listbox must use a viewport overlay with an opaque dark surface")
	}
	if !strings.Contains(currentRecipientComboboxScript(), `innerHeight-menu.height-8`) || !strings.Contains(currentRecipientComboboxScript(), `list.style.width=width+'px'`) {
		t.Fatal("recipient listbox must stay aligned to its combobox and inside the viewport")
	}
}

func TestCurrentRoutingMenuRemainsRowAnchoredWithoutScrollContainer(t *testing.T) {
	for _, rule := range []string{
		`.routing-row-menu{position:relative;display:inline-block}`,
		`.routing-row-menu-panel{position:absolute;z-index:3;right:0;top:calc(100% + 6px);`,
		`.production-context .production-routing-editor .wizard-plan-table{overflow:visible}`,
	} {
		if !strings.Contains(adminThemeCSS, rule) {
			t.Errorf("route action menu is missing anchored, unclipped layout rule %q", rule)
		}
	}
}

func TestLegacyRoutingSaveActionCannotBypassUnifiedApply(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "legacy-routing-save", Name: "Legacy Routing Save"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?lang=en", strings.NewReader("action=save_current_production_routing&project_id=legacy-routing-save&task_type_id=task-a&destination_webhook_id=1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !handleCurrentIARoutingMutation(recorder, request, "en", db) {
		t.Fatal("legacy save action was not handled")
	}
	if recorder.Code != http.StatusConflict {
		t.Fatalf("legacy route save returned %d; want conflict", recorder.Code)
	}
	if len(model.ListProductionNotificationRoutes(db, project.KitsuProjectID)) != 0 {
		t.Fatal("legacy route save bypassed the combined Apply path")
	}
}

func TestCurrentRoutingChannelDeleteCannotRemoveLastNotificationRoute(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-delete-last", Name: "Routing Delete Last"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "only", "", "https://example.invalid/only", "channel-only"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-only", TaskTypeName: "Only", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	if canDeleteWebhookWithoutRemovingLastRoute(db, project, webhook.ID) {
		t.Fatal("last notification route can be removed by deleting its Discord channel")
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
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "remaining", "", "https://example.invalid/remaining", "channel-2"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-1", TaskTypeName: "Task 1", DestinationWebhookID: webhook.ID}, {ProductionID: project.KitsuProjectID, TaskTypeID: "task-2", TaskTypeName: "Task 2", DestinationWebhookID: webhooks[1].ID}}); err != nil {
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
	if routes := model.ListProductionNotificationRoutes(db, project.KitsuProjectID); len(routes) != 1 || routes[0].TaskTypeID != "task-2" {
		t.Fatalf("verified channel delete did not preserve the remaining route: %#v", routes)
	}
}

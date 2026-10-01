package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
)

func TestCurrentRoutingDraftTemplateSupportsInlineDestinationModesAndRepeatedAdds(t *testing.T) {
	template := renderCurrentRoutingRowTemplate("en", []kitsu.TaskType{{ID: "task-comp", Name: "Compositing"}}, []model.ProjectWebhook{{ID: 7, ChannelName: "comp-review"}})
	for _, marker := range []string{`data-task-type-select`, `value="task-comp"`, `data-default-channel-name="compositing"`, `data-destination-control`, `value="7">#comp-review`, `value="__auto__"`, `value="__custom__"`, `data-custom-channel-field hidden`, `data-new-channel-name`} {
		if !strings.Contains(template, marker) {
			t.Errorf("draft routing row template is missing %q", marker)
		}
	}
	script := currentRoutingEditorScript()
	for _, marker := range []string{`template.content.firstElementChild.cloneNode(true)`, `body.appendChild(row)`, `new_channel_name`, `route.create_channel_name=`, `control.value==='__custom__'`} {
		if !strings.Contains(script, marker) {
			t.Errorf("routing editor script is missing repeated-add/inline destination behavior %q", marker)
		}
	}
	if strings.Contains(script, `if(visibleRows().length)selectRow`) || strings.Contains(script, `add.disabled=`) {
		t.Fatal("edit entry must not auto-select a route or disable repeated Add Task Type actions")
	}
}

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
	if got := strings.Count(body, `data-current-routing-form`); got != 1 {
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
	if !strings.Contains(body, `data-new-channel-name maxlength="100" autocomplete="off" disabled`) {
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

func TestCurrentRoutingEditorStartsWithRowsAndWFAHiddenBelowList(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-inline-editor", Name: "Routing Inline Editor"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "storyboard", "Storyboard", "synthetic-webhook", "channel-storyboard"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-storyboard", TaskTypeName: "Storyboard", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=routing-inline-editor&tab=notifications&edit_routing=1&lang=en", nil)
	body := renderCurrentIARoutingEditorSetupStyleWithData(db, request, project, "en", "", productionNotificationReviewerView{})
	routeStart := strings.Index(body, `data-routing-row data-task-type="task-storyboard"`)
	if routeStart < 0 {
		t.Fatal("configured route row is missing")
	}
	routeEnd := strings.Index(body[routeStart:], `</tr>`)
	if routeEnd < 0 {
		t.Fatal("configured route row is incomplete")
	}
	afterRoute := strings.TrimSpace(body[routeStart+routeEnd+len(`</tr>`):])
	if strings.HasPrefix(afterRoute, `<tr class="routing-inline-editor-row"`) {
		t.Fatal("WFA detail must not be inserted between route rows")
	}
	if !strings.Contains(body, `<div data-editor-parking><section class="production-wfa-edit-panel" data-wfa-detail-panel aria-live="polite" hidden>`) {
		t.Fatal("the WFA-only detail panel must start hidden below the route list")
	}
	if !strings.Contains(body, `</section></div><div data-apply-message role="status" aria-live="polite"></div>`) {
		t.Fatal("the bottom WFA panel must close only its parking container before the Apply controls; an extra section close ejects them from the form")
	}
	if apply, formEnd := strings.Index(body, `data-apply-submit`), strings.Index(body, `</form>`); apply < 0 || formEnd < apply {
		t.Fatal("Apply must remain inside the routing form after the bottom WFA panel")
	}
	panelStart := strings.Index(body, `<section class="production-wfa-edit-panel" data-wfa-detail-panel`)
	if panelStart < 0 {
		t.Fatal("the hidden WFA detail panel must be complete")
	}
	panelEnd := strings.Index(body[panelStart:], `</section>`)
	if panelEnd < 0 {
		t.Fatal("the hidden WFA detail panel must be complete")
	}
	panel := body[panelStart : panelStart+panelEnd]
	for _, control := range []string{`data-wfa-title`, `data-wfa-automatic-value`, `data-wfa-target-list`, `data-wfa-add-target`} {
		if !strings.Contains(panel, control) {
			t.Errorf("the hidden WFA detail panel is missing %s", control)
		}
	}
	if !strings.Contains(body, `<template data-routing-row-template>`) || !strings.Contains(currentRoutingEditorScript(), `body.appendChild(row)`) {
		t.Fatal("Add Task Type must support repeated browser-pending route rows")
	}
}

func TestCurrentRoutingEditorKeepsWFAInBottomDetailAndRoutingInRows(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-bottom-detail", Name: "Routing Bottom Detail"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "storyboard", "Storyboard", "synthetic-webhook", "channel-storyboard"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-storyboard", TaskTypeName: "Storyboard", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	body := renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang=en", nil), project, "en", "", productionNotificationReviewerView{})
	routeStart := strings.Index(body, `data-routing-row data-task-type="task-storyboard"`)
	if routeStart < 0 {
		t.Fatal("configured route row is missing")
	}
	routeEnd := strings.Index(body[routeStart:], `</tr>`)
	if routeEnd < 0 {
		t.Fatal("configured route row is incomplete")
	}
	route := body[routeStart : routeStart+routeEnd]
	if !strings.Contains(route, `select class="routing-destination-select" name="destination_mode" data-destination-control`) || !strings.Contains(route, `value="__auto__"`) {
		t.Fatal("routing destination must be editable directly in each route row")
	}
	if strings.Contains(route, `data-route-editor`) || strings.Contains(body, `data-route-editor-task=`) {
		t.Fatal("WFA detail must not be inserted between Task Type rows")
	}
	listEnd := strings.Index(body, `</tbody></table></div>`)
	panelStart := strings.Index(body, `<section class="production-wfa-edit-panel" data-wfa-detail-panel`)
	if listEnd < 0 || panelStart < listEnd {
		t.Fatal("the single WFA detail panel must be placed after the complete Task Type list")
	}
	panelEnd := strings.Index(body[panelStart:], `</section>`)
	if panelEnd < 0 {
		t.Fatal("WFA detail panel is incomplete")
	}
	panel := body[panelStart : panelStart+panelEnd]
	if strings.Contains(panel, `data-wfa-channel-control`) || strings.Contains(panel, `data-new-channel-name`) {
		t.Fatal("routing controls must remain in route rows; the detail panel is WFA-only")
	}
	panelTagEnd := strings.Index(body[panelStart:], `>`)
	if panelTagEnd < 0 || !strings.Contains(body[panelStart:panelStart+panelTagEnd], ` hidden`) {
		t.Fatal("edit mode must begin with no WFA detail selected")
	}
	if strings.Contains(body, `routing-destructive-note`) || strings.Contains(body, `解除予定`) || strings.Contains(body, `Pending removal`) || strings.Contains(body, `data-routing-undo`) {
		t.Fatal("verbose routing note and row-level pending removal/Undo UI must be absent")
	}
}

func TestProductionNotificationReadSummaryShowsOnlyEffectiveRecipients(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-read-summary", Name: "Routing Read Summary"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "storyboard", "Storyboard", "synthetic-webhook", "channel-storyboard"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-storyboard", TaskTypeName: "Storyboard", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"ja", "en"} {
		body := renderProductionNotificationsReadTableWithData(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang="+lang, nil), project, lang, "success", "Healthy", productionNotificationReviewerView{TaskTypes: []kitsu.TaskType{{ID: "task-storyboard", Name: "Storyboard"}}, BotToken: "synthetic-token", KitsuReady: true})
		tableStart := strings.Index(body, `<tbody>`)
		tableEnd := strings.Index(body[tableStart:], `</tbody>`)
		if tableStart < 0 || tableEnd < 0 {
			t.Fatal("read-mode Notifications table is missing")
		}
		visibleRows := body[tableStart : tableStart+tableEnd]
		for _, internalLabel := range []string{"Automatic", "Additional", "自動", "追加"} {
			if strings.Contains(visibleRows, internalLabel) {
				t.Errorf("%s read table exposes internal recipient provenance label %q", lang, internalLabel)
			}
		}
		if !strings.Contains(visibleRows, `No recipients`) && lang == "en" {
			t.Error("empty effective recipient state should be concise")
		}
		if !strings.Contains(visibleRows, `通知先なし`) && lang == "ja" {
			t.Error("Japanese empty effective recipient state should be concise")
		}
		if !strings.Contains(body, `class="status-pill success" role="status"`) {
			t.Errorf("%s read-mode status must use the shared semantic status pill", lang)
		}
	}
}

func TestCurrentRoutingDisclosureAndCanonicalStatusPresentation(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-disclosure-state", Name: "Routing Disclosure State"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "storyboard", "Storyboard", "synthetic-webhook", "channel-storyboard"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-storyboard", TaskTypeName: "Storyboard", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	body := renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=routing-disclosure-state&tab=notifications&edit_routing=1&lang=en", nil), project, "en", "", productionNotificationReviewerView{})
	if !strings.Contains(body, `class="routing-select-task" data-select-task aria-pressed="false" aria-expanded="false"`) {
		t.Fatal("Task Type row control must expose its collapsed state")
	}
	if !strings.Contains(currentRoutingEditorScript(), `setAttribute('aria-expanded',String(selected))`) {
		t.Fatal("Task Type inline expansion state must be announced to assistive technology")
	}
	if !strings.Contains(adminThemeCSS, `.production-context .production-notification-actions .status-pill{align-self:center;min-width:0;min-height:28px;height:auto;padding:5px 8px;border-radius:var(--status-radius);`) {
		t.Fatal("Healthy must use the shared compact status-pill shape, not an action-button shape")
	}
	if !strings.Contains(adminThemeCSS, `.production-settings-disclosure-row.danger-zone>summary{color:var(--color-status-danger)}`) {
		t.Fatal("Danger Zone must use the canonical semantic danger color")
	}
}

func TestInlineRoutingEditorUsesIntegratedDisclosureSurface(t *testing.T) {
	expected := `.editorial-workbench .production-context .production-wfa-edit-panel{display:grid;gap:var(--space-4,16px);padding:var(--space-4,16px) 18px;border:0;border-top:1px solid var(--divider-color);border-radius:0;background:var(--surface-subtle);box-shadow:none}`
	if !strings.Contains(adminThemeCSS, expected) {
		t.Fatal("inline route editor must continue from its selected row without a bright nested card outline")
	}
}

func TestCurrentRoutingRecipientDialogUsesExplicitUserRoleModesAndEmptyState(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-recipient-modes", Name: "Routing Recipient Modes"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	roleID := "123456789012345680"
	for _, tc := range []struct{ lang, user, role, empty string }{
		{"ja", "ユーザー", "ロール", "追加可能なDiscordロールはありません"},
		{"en", "Users", "Roles", "No eligible Discord roles available"},
	} {
		body := renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang="+tc.lang, nil), project, tc.lang, "", productionNotificationReviewerView{
			Team:       []kitsu.Person{{ID: "person-linked", FullName: "Linked User", Active: true}},
			Users:      []model.UserMap{{KitsuID: "person-linked", DiscordID: "123456789012345681", DiscordDisplayName: "Linked User"}},
			GuildUsers: []DiscordGuildMember{reviewerTestGuildMember("123456789012345681", "linked-user", "Linked User", "")},
			Roles:      []DiscordGuildRole{{ID: roleID, Name: "comp-leads", Mentionable: true}},
			RolesReady: true,
		})
		for _, want := range []string{`role="tablist"`, `data-recipient-mode="user"`, `data-recipient-mode="role"`, tc.user, tc.role, `data-wfa-option-value="` + roleID + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s recipient picker missing %q", tc.lang, want)
			}
		}
		if strings.Count(body, `data-wfa-user-combobox`) != 1 || strings.Count(body, `data-wfa-role-combobox`) != 1 {
			t.Errorf("%s recipient picker should expose one combobox for each explicit mode", tc.lang)
		}
		emptyBody := renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang="+tc.lang, nil), project, tc.lang, "", productionNotificationReviewerView{RolesReady: true})
		if !strings.Contains(emptyBody, tc.empty) {
			t.Errorf("%s role mode should explain the empty candidate state", tc.lang)
		}
	}
}

func TestCurrentRoutingEditorNewRouteTemplateCanBeRemoved(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-editor-new-route", Name: "Routing Editor New Route"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	body := renderCurrentIARoutingEditorSetupStyle(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang=en", nil), project, "en", "")
	templateStart := strings.Index(body, `<template data-routing-row-template>`)
	if templateStart < 0 {
		t.Fatal("new route template is missing")
	}
	templateEnd := strings.Index(body[templateStart:], `</template>`)
	if templateEnd < 0 {
		t.Fatal("new route template row is incomplete")
	}
	template := body[templateStart : templateStart+templateEnd]
	for _, control := range []string{`data-task-type-select`, `data-destination-control`, `value="__auto__"`, `data-routing-remove`} {
		if !strings.Contains(template, control) {
			t.Fatalf("new route template is missing route control %s", control)
		}
	}
	if strings.Contains(template, `data-routing-undo`) || strings.Contains(template, `Pending removal`) || strings.Contains(template, `解除予定`) {
		t.Fatal("new route template must not expose pending-removal/Undo UI")
	}
}

func TestCurrentRoutingEditorScriptFindsSiblingAddDialog(t *testing.T) {
	script := currentRoutingEditorScript()
	if !strings.Contains(script, `form.parentElement.querySelector('[data-wfa-add-modal]')`) {
		t.Fatal("Apply editor script must find the add-recipient dialog outside the Apply form")
	}
	if !strings.Contains(script, `var kind=modal.dataset.recipientMode,select=modal.querySelector(kind==='role'?'[data-wfa-role-id]':'[data-wfa-user-id]')`) {
		t.Fatal("recipient mode tabs must select the exact User or Role value control")
	}
	if !strings.Contains(script, `panel.hidden=false`) || !strings.Contains(script, `body.appendChild(row)`) {
		t.Fatal("Task Type selection must reveal the shared bottom panel and permit multiple draft rows")
	}
}

func TestCurrentRecipientListboxEscapesDialogClippingWithOpaqueSurface(t *testing.T) {
	if strings.Contains(currentRecipientComboboxScript(), `document.body.appendChild(list`) {
		t.Fatal("recipient listbox must not be portaled outside the native dialog top layer")
	}
	if !strings.Contains(adminThemeCSS, `.recipient-listbox{position:fixed;`) || !strings.Contains(adminThemeCSS, `background:var(--bg2);`) {
		t.Fatal("recipient listbox must use a viewport overlay with an opaque dark surface")
	}
	if !strings.Contains(adminThemeCSS, `.production-context dialog[data-wfa-add-modal]{overflow:visible}`) {
		t.Fatal("recipient listbox must remain visible outside the modal content box")
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

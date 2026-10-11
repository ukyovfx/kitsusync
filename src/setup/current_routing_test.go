package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"app/src/api/kitsu"
	"app/src/model"
)

func TestRoutingRowSelectionSupportsWholeRowAndKeyboardWithoutCapturingControls(t *testing.T) {
	template := renderCurrentRoutingRowTemplate("ja", []kitsu.TaskType{{ID: "task-storyboard", Name: "Storyboard"}}, nil)
	for _, marker := range []string{`data-routing-row`, `tabindex="0"`, `aria-controls="production-wfa-detail-panel"`, `aria-selected="false"`, `aria-expanded="false"`} {
		if !strings.Contains(template, marker) {
			t.Errorf("routing row template is missing accessible selection marker %q", marker)
		}
	}
	for _, marker := range []string{
		`target.closest('button,a,label,input,select,textarea,details,summary,[role="button"],[role="combobox"],[role="option"],[role="menuitem"],[role="tab"],[contenteditable="true"]')`,
		`selectRow(row)})`,
		`event.target!==row||!['Enter',' '].includes(event.key)`,
		`item.setAttribute('aria-expanded',String(selected));item.setAttribute('aria-selected',String(selected))`,
	} {
		if !strings.Contains(currentRoutingEditorScript(), marker) {
			t.Errorf("routing row selection script is missing interaction marker %q", marker)
		}
	}
	if !strings.Contains(adminThemeCSS, `.production-context .production-routing-editor [data-routing-row][tabindex="0"]:focus-visible`) {
		t.Fatal("keyboard-focused routing rows need a visible scoped focus indicator")
	}
}

func TestCurrentRoutingDraftTemplateSupportsInlineDestinationModesAndRepeatedAdds(t *testing.T) {
	template := renderCurrentRoutingRowTemplate("en", []kitsu.TaskType{{ID: "task-comp", Name: "Compositing"}}, []model.ProjectWebhook{{ID: 7, ChannelName: "comp-review"}})
	for _, marker := range []string{`data-task-type-select`, `value="task-comp"`, `data-default-channel-name="compositing"`, `data-destination-control`, `value="7">#comp-review`, `value="__auto__"`, `value="__custom__"`, `data-custom-channel-field hidden`, `data-new-channel-name`} {
		if !strings.Contains(template, marker) {
			t.Errorf("draft routing row template is missing %q", marker)
		}
	}
	for _, marker := range []string{`data-task-type-combobox`, `role="listbox"`, `data-task-type-option="task-comp"`, `data-select-task aria-pressed="false" aria-expanded="false" hidden`} {
		if !strings.Contains(template, marker) {
			t.Errorf("draft Task Type must use one accessible dark picker before selection: missing %q", marker)
		}
	}
	if strings.Contains(template, `value="">Select Discord Channel`) {
		t.Fatal("draft destination must default to automatic creation without a selectable empty channel option")
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
	for _, marker := range []string{`taskSelect.dispatchEvent(new Event('change'`, `select.hidden=!taskSelect.value`, `aria-disabled`, `event.key==='Escape'`, `[data-task-type-combobox]')?.focus()`} {
		if !strings.Contains(script, marker) {
			t.Errorf("draft Task Type selection must remain single-row and keyboard accessible: missing %q", marker)
		}
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
	if got := strings.Count(body, `<form method="post" action="`); got != 1 {
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
	if !strings.Contains(body, `<div data-editor-parking><section id="production-wfa-detail-panel" class="production-wfa-edit-panel" data-wfa-detail-panel aria-live="polite" hidden>`) {
		t.Fatal("the WFA-only detail panel must start hidden below the route list")
	}
	if !strings.Contains(body, `</section></div><div data-apply-message role="status" aria-live="polite"></div>`) {
		t.Fatal("the bottom WFA panel must close only its parking container before the Apply controls; an extra section close ejects them from the form")
	}
	if apply, formEnd := strings.Index(body, `data-apply-submit`), strings.Index(body, `</form>`); apply < 0 || formEnd < apply {
		t.Fatal("Apply must remain inside the routing form after the bottom WFA panel")
	}
	panelStart := strings.Index(body, `<section id="production-wfa-detail-panel" class="production-wfa-edit-panel" data-wfa-detail-panel`)
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
	panelStart := strings.Index(body, `<section id="production-wfa-detail-panel" class="production-wfa-edit-panel" data-wfa-detail-panel`)
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

func TestRoutingEditorEncodesMissingWFARecipientsAsEmptyArray(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-empty-recipients", Name: "Empty WFA Recipients"}
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
	body := renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?tab=notifications&edit_routing=1", nil), project, "en", "", productionNotificationReviewerView{TaskTypes: []kitsu.TaskType{{ID: "task-storyboard", Name: "Storyboard"}}})
	if !strings.Contains(body, `data-task-type="task-storyboard"`) || !strings.Contains(body, `data-existing-targets="W10="`) {
		t.Fatal("a routed Task Type with no persisted WFA targets must render an empty JSON array for browser .filter/.some operations, not JSON null")
	}
	if strings.Contains(body, `data-existing-targets="bnVsbA=="`) {
		t.Fatal("nil WFA target data must not be serialized as JSON null")
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
	if !strings.Contains(body, `tabindex="0" aria-controls="production-wfa-detail-panel" aria-selected="false" aria-expanded="false"`) {
		t.Fatal("routing rows must be keyboard-focusable and expose their WFA detail selection state")
	}
	if !strings.Contains(body, `id="production-wfa-detail-panel"`) || !strings.Contains(body, `class="routing-select-task" data-select-task aria-pressed="false" aria-expanded="false"`) {
		t.Fatal("routing rows must control the WFA detail panel and retain the Task Type selection button")
	}
	if !strings.Contains(currentRoutingEditorScript(), `row.addEventListener('click',function(event){var target=event.target;if(target&&target.closest&&target.closest('button,a,label,input,select,textarea,details,summary,[role="button"],[role="combobox"],[role="option"],[role="menuitem"],[role="tab"],[contenteditable="true"]'))return;selectRow(row)})`) {
		t.Fatal("only non-interactive routing-row surfaces should select the WFA detail")
	}
	if !strings.Contains(currentRoutingEditorScript(), `row.addEventListener('keydown',function(event){if(event.target!==row||!['Enter',' '].includes(event.key))return;event.preventDefault();selectRow(row)})`) {
		t.Fatal("routing rows must support Enter/Space selection without intercepting descendant controls")
	}
	if !strings.Contains(currentRoutingEditorScript(), `item.setAttribute('aria-expanded',String(selected));item.setAttribute('aria-selected',String(selected))`) {
		t.Fatal("routing row selection state must be announced to assistive technology")
	}
	if !strings.Contains(adminThemeCSS, `.production-context .production-notification-actions .status-pill{align-self:center;min-width:0;min-height:32px;height:32px;padding:0 8px;border-radius:var(--status-radius);font-size:11px;`) {
		t.Fatal("Healthy and Edit must share the compact control-height rhythm while retaining the semantic status-pill shape")
	}
	if !strings.Contains(adminThemeCSS, `.production-context .production-notification-actions .btn-ghost{min-width:0;padding-inline:10px;font-size:12px}`) {
		t.Fatal("Notifications Edit must use compact local button sizing without changing global buttons")
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

func TestWFADetailUsesOneSubtleDividerBetweenAutomaticAndAdditional(t *testing.T) {
	automatic := `.editorial-workbench .production-context .production-wfa-edit-panel [data-wfa-automatic]{border:0;padding:var(--space-2,8px) 0}`
	additional := `.editorial-workbench .production-context .production-wfa-edit-panel [data-wfa-additional]{border:0;border-top:1px solid var(--divider-color);margin-top:var(--space-1,4px);padding:var(--space-3,12px) 0 var(--space-1,4px)}`
	if !strings.Contains(adminThemeCSS, automatic) {
		t.Fatal("Automatic recipients should stay unboxed and have no redundant divider")
	}
	if !strings.Contains(adminThemeCSS, additional) {
		t.Fatal("Additional recipients need one quiet divider and spacing to remain distinguishable")
	}
}

func TestCurrentRoutingRecipientDialogUsesExplicitUserRoleModesAndEmptyState(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-recipient-modes", Name: "Routing Recipient Modes", DiscordGuildID: "123456789012345678"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	roleID := "123456789012345680"
	for _, tc := range []struct{ lang, user, role, empty, routeRequired, targetRequired string }{
		{"ja", "ユーザー", "ロール", "このDiscordサーバーにカスタムロールはありません。", "先にTask Typeを選択してください。", "追加するDiscordユーザーまたはロールを選択してください。"},
		{"en", "Users", "Roles", "No custom Discord roles are configured.", "Select a Task Type before adding a recipient.", "Select a Discord user or role to add."},
	} {
		body := renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang="+tc.lang, nil), project, tc.lang, "", productionNotificationReviewerView{
			Team:        []kitsu.Person{{ID: "person-linked", FullName: "Linked User", Active: true}},
			Users:       []model.UserMap{{KitsuID: "person-linked", DiscordID: "123456789012345681", DiscordDisplayName: "Linked User"}},
			GuildUsers:  []DiscordGuildMember{reviewerTestGuildMember("123456789012345681", "linked-user", "Linked User", "")},
			BotToken:    "configured",
			Roles:       []DiscordGuildRole{{ID: roleID, Name: "comp-leads", Mentionable: true}},
			RoleCatalog: []DiscordGuildRole{{ID: roleID, Name: "comp-leads", Mentionable: true}},
			RolesReady:  true,
		})
		for _, want := range []string{`role="tablist"`, `data-recipient-mode="user"`, `data-recipient-mode="role"`, tc.user, tc.role, `data-wfa-option-value="` + roleID + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s recipient picker missing %q", tc.lang, want)
			}
		}
		if strings.Count(body, `data-wfa-user-combobox`) != 1 || strings.Count(body, `data-wfa-role-combobox`) != 1 {
			t.Errorf("%s recipient picker should expose one combobox for each explicit mode", tc.lang)
		}
		for _, localized := range []string{`data-route-required-label="` + tc.routeRequired + `"`, `data-target-required-label="` + tc.targetRequired + `"`} {
			if !strings.Contains(body, localized) {
				t.Errorf("%s recipient Add validation is missing localized feedback %q", tc.lang, localized)
			}
		}
		emptyBody := renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang="+tc.lang, nil), project, tc.lang, "", productionNotificationReviewerView{BotToken: "configured", RolesReady: true})
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

func TestCurrentRoutingChannelDeleteSerializesWithProductionApply(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-delete-apply-race", Name: "Routing Delete Apply Race", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "test-token")
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "first", "", "https://example.invalid/first", "channel-first"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "second", "", "https://example.invalid/second", "channel-second"); err != nil {
		t.Fatal(err)
	}
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	if len(webhooks) != 2 {
		t.Fatalf("expected two managed webhooks, got %d", len(webhooks))
	}
	var first, second model.ProjectWebhook
	for _, webhook := range webhooks {
		switch webhook.ChannelName {
		case "first":
			first = webhook
		case "second":
			second = webhook
		}
	}
	if first.ID == 0 || second.ID == 0 {
		t.Fatalf("test webhooks were not found: %#v", webhooks)
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{
		{ProductionID: project.KitsuProjectID, TaskTypeID: "task-first", TaskTypeName: "First", DestinationWebhookID: first.ID},
		{ProductionID: project.KitsuProjectID, TaskTypeID: "task-second", TaskTypeName: "Second", DestinationWebhookID: second.ID},
	}); err != nil {
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
		return []DiscordGuildChannel{{ID: "channel-first", Type: 0, ParentID: "category-1"}}, nil
	}
	deleteReached := make(chan struct{})
	allowDelete := make(chan struct{})
	currentRoutingDeleteChannel = func(string, string) error {
		close(deleteReached)
		<-allowDelete
		return nil
	}

	// Hold the same Production lock as Apply while simulating an Apply that
	// removes the second route but keeps the channel this request wants to delete.
	unlockApply := lockProductionNotificationApply(project.KitsuProjectID)
	applyUnlocked := false
	deleteReleased := false
	defer func() {
		if !deleteReleased {
			close(allowDelete)
		}
		if !applyUnlocked {
			unlockApply()
		}
	}()
	revision := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	form := url.Values{"project_id": {project.KitsuProjectID}, "webhook_id": {strconv.FormatUint(uint64(first.ID), 10)}, "action": {"delete_current_routing_channel"}, "confirm_name": {"first"}}
	requestStarted := make(chan struct{})
	requestDone := make(chan struct{})
	response := httptest.NewRecorder()
	go func() {
		defer close(requestDone)
		close(requestStarted)
		request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handleCurrentIARoutingMutation(response, request, "en", db)
	}()
	<-requestStarted

	deleteRanBeforeApplyFinished := false
	select {
	case <-deleteReached:
		deleteRanBeforeApplyFinished = true
	case <-time.After(100 * time.Millisecond):
	}

	if _, err := model.ApplyProductionNotificationState(db, project.ID, project.KitsuProjectID, project.Name, revision, []model.ProductionNotificationRoute{
		{ProductionID: project.KitsuProjectID, TaskTypeID: "task-first", TaskTypeName: "First", DestinationWebhookID: first.ID},
	}, nil); err != nil {
		t.Fatalf("simulated Apply could not keep the selected route: %v", err)
	}
	unlockApply()
	applyUnlocked = true
	close(allowDelete)
	deleteReleased = true
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("routing deletion did not finish after Apply released the Production lock")
	}
	routes := model.ListProductionNotificationRoutes(db, project.KitsuProjectID)
	if deleteRanBeforeApplyFinished || response.Code != http.StatusSeeOther || len(routes) != 1 || routes[0].TaskTypeID != "task-first" {
		t.Fatalf("channel delete raced Apply and removed the last route: early_delete=%v status=%d routes=%#v", deleteRanBeforeApplyFinished, response.Code, routes)
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
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "owned", "", "https://discord.com/api/webhooks/webhook-owned/token", "channel-1"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "remaining", "", "https://discord.com/api/webhooks/webhook-remaining/token", "channel-2"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-1", TaskTypeName: "Task 1", DestinationWebhookID: webhook.ID}, {ProductionID: project.KitsuProjectID, TaskTypeID: "task-2", TaskTypeName: "Task 2", DestinationWebhookID: webhooks[1].ID}}); err != nil {
		t.Fatal(err)
	}
	oldCheck, oldList, oldDelete, oldInspect := currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingDeleteChannel, currentRoutingInspectWebhook
	defer func() {
		currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingDeleteChannel, currentRoutingInspectWebhook = oldCheck, oldList, oldDelete, oldInspect
	}()
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-1", Type: 0, ParentID: "category-1"}}, nil
	}
	currentRoutingInspectWebhook = func(raw string) (routingWebhookIdentity, bool, error) {
		id, err := discordWebhookID(raw)
		if err != nil {
			return routingWebhookIdentity{}, false, err
		}
		return routingWebhookIdentity{ID: id, Type: 1, GuildID: "guild-1", ChannelID: "channel-1"}, false, nil
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
	var tombstone model.ManagedChannelDeletion
	if err := db.Where("webhook_row_id = ?", webhook.ID).First(&tombstone).Error; err != nil || tombstone.Disposition != model.ChannelDeletionComplete {
		t.Fatalf("successful external deletion did not finalize its tombstone: row=%+v err=%v", tombstone, err)
	}
}

func TestCurrentRoutingChannelDeleteFailureLeavesRecoverableTombstone(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-delete-recover", Name: "Routing Delete Recover", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "test-token")
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "owned", "", "https://discord.com/api/webhooks/webhook-owned/token", "channel-1"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "remaining", "", "https://discord.com/api/webhooks/webhook-remaining/token", "channel-2"); err != nil {
		t.Fatal(err)
	}
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-1", DestinationWebhookID: webhooks[0].ID}, {ProductionID: project.KitsuProjectID, TaskTypeID: "task-2", DestinationWebhookID: webhooks[1].ID}}); err != nil {
		t.Fatal(err)
	}
	oldCheck, oldList, oldDelete, oldInspect := currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingDeleteChannel, currentRoutingInspectWebhook
	defer func() {
		currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingDeleteChannel, currentRoutingInspectWebhook = oldCheck, oldList, oldDelete, oldInspect
	}()
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-1", Type: 0, ParentID: "category-1"}}, nil
	}
	currentRoutingInspectWebhook = func(raw string) (routingWebhookIdentity, bool, error) {
		id, err := discordWebhookID(raw)
		return routingWebhookIdentity{ID: id, Type: 1, GuildID: "guild-1", ChannelID: "channel-1"}, false, err
	}
	deleteCount := 0
	currentRoutingDeleteChannel = func(string, string) error {
		deleteCount++
		if deleteCount == 1 {
			return errors.New("synthetic Discord deletion failure")
		}
		return nil
	}
	form := url.Values{"project_id": {project.KitsuProjectID}, "webhook_id": {strconv.FormatUint(uint64(webhooks[0].ID), 10)}, "action": {"delete_current_routing_channel"}, "confirm_name": {"owned"}}
	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handleCurrentIARoutingMutation(rec, req, "en", db)
		return rec
	}
	if post().Code != http.StatusSeeOther {
		t.Fatal("failed external deletion response was not returned")
	}
	var tombstone model.ManagedChannelDeletion
	if err := db.Where("webhook_row_id = ?", webhooks[0].ID).First(&tombstone).Error; err != nil || tombstone.Disposition != model.ChannelDeletionNeedsHelp {
		t.Fatalf("failed Discord delete lost recoverable tombstone: row=%+v err=%v", tombstone, err)
	}
	if model.FindProjectWebhookByID(db, webhooks[0].ID) == nil || len(model.ListProductionNotificationRoutes(db, project.KitsuProjectID)) != 2 {
		t.Fatal("failed Discord deletion removed local route ownership")
	}
	if post().Code != http.StatusSeeOther || model.FindProjectWebhookByID(db, webhooks[0].ID) != nil || len(model.ListProductionNotificationRoutes(db, project.KitsuProjectID)) != 1 {
		t.Fatal("retry did not converge after external deletion succeeded")
	}
}

func TestCurrentRoutingChannelDeleteRejectsWebhookOwnedByAnotherProduction(t *testing.T) {
	db := newIAViewDB(t)
	projectA := model.Project{KitsuProjectID: "routing-owner-a", Name: "Production A", DiscordGuildID: "guild-shared", DiscordCategoryID: "category-shared"}
	projectB := model.Project{KitsuProjectID: "routing-owner-b", Name: "Production B", DiscordGuildID: "guild-shared", DiscordCategoryID: "category-shared"}
	if err := db.Create(&projectA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&projectB).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, projectB.KitsuProjectID, "owned-b", "", "https://example.invalid/owned-b", "channel-b"); err != nil {
		t.Fatal(err)
	}
	webhookB := model.ListProjectWebhooks(db, projectB.KitsuProjectID)[0]
	oldCheck, oldList, oldDelete := currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingDeleteChannel
	defer func() {
		currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingDeleteChannel = oldCheck, oldList, oldDelete
	}()
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-b", Type: 0, ParentID: "category-shared"}}, nil
	}
	deleted := false
	currentRoutingDeleteChannel = func(string, string) error { deleted = true; return nil }
	form := url.Values{"project_id": {projectA.KitsuProjectID}, "webhook_id": {strconv.FormatUint(uint64(webhookB.ID), 10)}, "action": {"delete_current_routing_channel"}, "confirm_name": {"owned-b"}}
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handleCurrentIARoutingMutation(response, request, "en", db)
	if deleted {
		t.Fatal("cross-Production webhook reached Discord deletion")
	}
	if got := model.FindProjectWebhookByID(db, webhookB.ID); got == nil || got.KitsuProjectID != projectB.KitsuProjectID {
		t.Fatal("cross-Production request removed the other Production's webhook")
	}
}

func TestCurrentRoutingOrderRecoveryReconcilesOnlyVerifiedPersistedDestinations(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-order-recovery", Name: "Order Recovery", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
	webhook := model.ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "comp", TaskType: "task-comp", DiscordChannelID: "channel-comp", WebhookURL: "https://discord.com/api/webhooks/111111111111111111/synthetic-token"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationRoute{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationWriteLock{ProductionID: project.KitsuProjectID, MutationState: model.NotificationMutationReorderHelp, MutationToken: "old-token"}).Error; err != nil {
		t.Fatal(err)
	}
	oldCheck, oldList, oldSet, oldInspect := currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingSetPositions, currentRoutingInspectWebhook
	defer func() {
		currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingSetPositions, currentRoutingInspectWebhook = oldCheck, oldList, oldSet, oldInspect
	}()
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}
	}
	currentRoutingListChannels = func(guild, _ string) ([]DiscordGuildChannel, error) {
		if guild != "guild-1" {
			t.Fatalf("recovery used an unexpected Guild: %q", guild)
		}
		return []DiscordGuildChannel{{ID: "channel-comp", Type: 0, ParentID: "category-1", Position: 9}}, nil
	}
	currentRoutingInspectWebhook = func(raw string) (routingWebhookIdentity, bool, error) {
		if raw != webhook.WebhookURL {
			t.Fatalf("recovery inspected an unexpected webhook identity")
		}
		return routingWebhookIdentity{ID: "111111111111111111", Type: 1, GuildID: "guild-1", ChannelID: "channel-comp"}, false, nil
	}
	var got []DiscordChannelPosition
	reorderCalls := 0
	currentRoutingSetPositions = func(guild string, positions []DiscordChannelPosition, _ string) error {
		if guild != "guild-1" {
			t.Fatalf("recovery reordered an unexpected Guild: %q", guild)
		}
		reorderCalls++
		got = append([]DiscordChannelPosition(nil), positions...)
		if reorderCalls == 1 {
			return errors.New("synthetic Discord reconciliation failure")
		}
		return nil
	}
	form := url.Values{"project_id": {project.KitsuProjectID}, "action": {"recover_current_routing_order"}}
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	if !handleCurrentIARoutingMutation(response, request, "en", db) {
		t.Fatal("order recovery action was not handled")
	}
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "routing_order_recovery=failed") || reorderCalls != 1 {
		t.Fatalf("failed Discord reconciliation was not reported through the safe recovery path: status=%d calls=%d location=%q", response.Code, reorderCalls, response.Header().Get("Location"))
	}
	var failedLock model.ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", project.KitsuProjectID).First(&failedLock).Error; err != nil || failedLock.MutationState != model.NotificationMutationReorderHelp {
		t.Fatalf("failed reconciliation must remain durably fenced: lock=%+v err=%v", failedLock, err)
	}
	request = httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	if !handleCurrentIARoutingMutation(response, request, "en", db) {
		t.Fatal("recovery retry action was not handled")
	}
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "routing_order_recovery=done") || reorderCalls != 2 || len(got) != 1 || got[0] != (DiscordChannelPosition{ID: "channel-comp", Position: 9}) {
		t.Fatalf("explicit recovery did not reconcile persisted route order: status=%d positions=%#v location=%q", response.Code, got, response.Header().Get("Location"))
	}
	var lock model.ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", project.KitsuProjectID).First(&lock).Error; err != nil || lock.MutationState != "" || lock.MutationToken != "" {
		t.Fatalf("successful reconciliation did not release its own mutation fence: lock=%+v err=%v", lock, err)
	}
}

func TestLegacyTaskTypeDeleteWaitsForProductionApply(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "legacy-task-delete", Name: "Legacy Task Delete"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "comp", "Compositing", "https://example.invalid/comp", "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]

	unlockApply := lockProductionNotificationApply(project.KitsuProjectID)
	requestStarted := make(chan struct{})
	requestDone := make(chan struct{})
	response := httptest.NewRecorder()
	go func() {
		defer close(requestDone)
		close(requestStarted)
		form := url.Values{"action": {"task_type_delete"}, "webhook_id": {strconv.FormatUint(uint64(webhook.ID), 10)}, "task_type": {"Compositing"}, "confirm_text": {"delete"}}
		request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handleProjectRoutingMutation(response, request, "en", "", "", db)
	}()
	<-requestStarted

	completedBeforeApply := false
	select {
	case <-requestDone:
		completedBeforeApply = true
	case <-time.After(100 * time.Millisecond):
	}
	if got := model.FindProjectWebhookByID(db, webhook.ID); got == nil {
		unlockApply()
		t.Fatal("legacy task-type deletion removed a webhook while Production Apply held its lock")
	}
	unlockApply()
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("legacy task-type deletion did not finish after Apply released its lock")
	}
	if completedBeforeApply || model.FindProjectWebhookByID(db, webhook.ID) != nil {
		t.Fatalf("legacy task-type delete was not serialized: early=%v", completedBeforeApply)
	}
}

func TestDeleteProjectChannelWaitsForProductionApply(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "legacy-channel-delete", Name: "Legacy Channel Delete", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "comp", "Compositing", "https://discord.com/api/webhooks/123456789012345678/test-token", "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "animation", "Animation", "https://discord.com/api/webhooks/123456789012345679/test-token", "channel-2"); err != nil {
		t.Fatal(err)
	}
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "comp", DestinationWebhookID: webhooks[0].ID}, {ProductionID: project.KitsuProjectID, TaskTypeID: "animation", DestinationWebhookID: webhooks[1].ID}}); err != nil {
		t.Fatal(err)
	}
	oldCheck, oldList, oldInspect := currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true}}
	}
	currentRoutingListChannels = func(string, string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: "channel-1", Type: 0, ParentID: "category-1"}}, nil
	}
	currentRoutingInspectWebhook = func(raw string) (routingWebhookIdentity, bool, error) {
		id, err := discordWebhookID(raw)
		return routingWebhookIdentity{ID: id, Type: 1, GuildID: "guild-1", ChannelID: "channel-1"}, false, err
	}
	defer func() {
		currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook = oldCheck, oldList, oldInspect
	}()
	oldDelete := deleteProjectChannelOnDiscord
	deleteReached := make(chan struct{})
	deleteProjectChannelOnDiscord = func(string, string) error {
		close(deleteReached)
		return nil
	}
	defer func() { deleteProjectChannelOnDiscord = oldDelete }()

	unlockApply := lockProductionNotificationApply(project.KitsuProjectID)
	deleteStarted := make(chan struct{})
	deleteDone := make(chan error, 1)
	go func() {
		close(deleteStarted)
		deleteDone <- DeleteProjectChannel(db, "test-token", webhook.ID)
	}()
	<-deleteStarted
	deleteRanBeforeApply := false
	select {
	case <-deleteReached:
		deleteRanBeforeApply = true
	case <-time.After(100 * time.Millisecond):
	}
	if model.FindProjectWebhookByID(db, webhook.ID) == nil {
		unlockApply()
		t.Fatal("legacy channel deletion removed webhook state while Production Apply held its lock")
	}
	unlockApply()
	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatalf("legacy channel delete failed after Apply released its lock: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("legacy channel delete did not finish after Apply released its lock")
	}
	if deleteRanBeforeApply || model.FindProjectWebhookByID(db, webhook.ID) != nil {
		t.Fatalf("legacy channel delete was not serialized: early=%v", deleteRanBeforeApply)
	}
}

func TestDeleteProjectConnectionOnlyWaitsForProductionApply(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "connection-delete-lock", Name: "Connection Delete Lock"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	unlockApply := lockProductionNotificationApply(project.KitsuProjectID)
	deleteStarted := make(chan struct{})
	deleteDone := make(chan error, 1)
	go func() {
		close(deleteStarted)
		deleteDone <- DeleteProjectConnectionOnly(project.KitsuProjectID, db)
	}()
	<-deleteStarted

	completedBeforeApply := false
	var earlyErr error
	select {
	case err := <-deleteDone:
		completedBeforeApply = err == nil
		earlyErr = err
	case <-time.After(100 * time.Millisecond):
	}
	if model.FindProjectByKitsuID(db, project.KitsuProjectID) == nil {
		unlockApply()
		t.Fatal("connection cleanup removed the Production while Apply held its lock")
	}
	unlockApply()
	if !completedBeforeApply {
		select {
		case err := <-deleteDone:
			if err != nil {
				t.Fatalf("connection cleanup failed after Apply released its lock: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("connection cleanup did not finish after Apply released its lock")
		}
	} else if earlyErr != nil {
		t.Fatalf("connection cleanup failed before Apply released its lock: %v", earlyErr)
	}
	if completedBeforeApply || model.FindProjectByKitsuID(db, project.KitsuProjectID) != nil {
		t.Fatalf("connection cleanup was not serialized: early=%v", completedBeforeApply)
	}
}

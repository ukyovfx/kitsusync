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

// laneSelectState extracts the rendered lane select of the first routing row
// (or the row template when template is true) and reports which option is
// selected.
func laneSelectState(t *testing.T, rendered string, template bool) (kind string, selected string) {
	t.Helper()
	marker := `data-routing-row`
	if template {
		marker = `data-routing-row-template`
	}
	rowStart := strings.Index(rendered, marker)
	if rowStart < 0 {
		t.Fatalf("no %s in rendered editor", marker)
	}
	selectStart := strings.Index(rendered[rowStart:], `<select class="routing-lane-select"`)
	if selectStart < 0 {
		t.Fatalf("no lane select rendered in %s", marker)
	}
	selectEnd := strings.Index(rendered[rowStart+selectStart:], `</select>`)
	if selectEnd < 0 {
		t.Fatal("lane select is not closed")
	}
	block := rendered[rowStart+selectStart : rowStart+selectStart+selectEnd]
	if valueStart := strings.Index(rendered[rowStart:rowStart+selectStart], `data-lane-kind="`); valueStart >= 0 {
		attrStart := rowStart + valueStart + len(`data-lane-kind="`)
		attrRest := rendered[attrStart:]
		kind = attrRest[:strings.Index(attrRest, `"`)]
	}
	for _, option := range []string{model.LaneChannel, model.LaneDM, model.LaneRecordOnly} {
		optionStart := strings.Index(block, `<option value="`+option+`"`)
		if optionStart < 0 {
			continue
		}
		optionEnd := strings.Index(block[optionStart:], `</option>`)
		optionBlock := block[optionStart : optionStart+optionEnd]
		if strings.Contains(optionBlock, " selected") {
			selected = option
		}
	}
	return kind, selected
}

// TestRoutingLaneEditorRoundTrip reproduces the production finding: the lane
// select must render the saved value, an apply carrying destination_kind must
// persist it, and a re-render must show the persisted lane.
func TestRoutingLaneEditorRoundTrip(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "lane-roundtrip", Name: "Round Trip", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "first", "", "https://discord.com/api/webhooks/111111111111111111/token-a", "channel-first"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "task-a", TaskTypeName: "First", DestinationKind: model.LaneChannel, DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-first"}}); err != nil {
		t.Fatal(err)
	}
	revision := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)

	stubRoutingEditorBackends(t)
	render := func() string {
		return renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest("GET", "/bot/admin/projects?lang=ja", nil), project, "ja", "", productionNotificationReviewerView{})
	}

	if _, selected := laneSelectState(t, render(), false); selected != model.LaneChannel {
		t.Fatalf("initial lane display = %q, want the saved channel lane", selected)
	}

	requested := `{"project_id":"lane-roundtrip","expected_revision":"` + revision + `","routes":[{"task_type_id":"task-a","destination_kind":"dm","destination_webhook_id":` + strconv.FormatUint(uint64(webhook.ID), 10) + `}],"reviewer_changes":[]}`
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", strings.NewReader(requested))
	if !handleProductionNotificationApply(response, request, "ja", db) || response.Code != http.StatusOK {
		t.Fatalf("lane-only apply failed: status=%d body=%s", response.Code, response.Body.String())
	}
	var route model.ProductionNotificationRoute
	if err := db.Where("production_id = ? AND task_type_id = ?", project.KitsuProjectID, "task-a").First(&route).Error; err != nil {
		t.Fatal(err)
	}
	if route.DestinationKind != model.LaneDM {
		t.Fatalf("persisted lane = %q, want dm", route.DestinationKind)
	}
	if _, selected := laneSelectState(t, render(), false); selected != model.LaneDM {
		t.Fatalf("re-rendered lane display = %q, want the persisted dm lane", selected)
	}
}

// TestRoutingEditorTemplateDefaultsToDMLane covers the design-v2 standard:
// a newly added route defaults to the DM lane.
func TestRoutingEditorTemplateDefaultsToDMLane(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "lane-default-dm", Name: "Default DM", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
	rendered := renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest("GET", "/bot/admin/projects?lang=ja", nil), project, "ja", "", productionNotificationReviewerView{})
	kind, selected := laneSelectState(t, rendered, true)
	if kind != model.LaneDM || selected != model.LaneDM {
		t.Fatalf("new-route lane default = kind %q selected %q, want dm/dm", kind, selected)
	}
	if !strings.Contains(rendered, `data-standard-lane`) {
		t.Fatal("standard-lane bulk action button is missing")
	}
}

// stubRoutingEditorDiscordBackends stubs the Discord-backed helpers the
// routing editor consults while rendering and applying.
func stubRoutingEditorBackends(t *testing.T) {
	t.Helper()
	originals := []struct {
		restore func()
	}{
		{func() {}},
	}
	_ = originals
	oldTasks, oldCheck, oldList, oldInspect, oldPositions := reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook, currentRoutingSetPositions
	t.Cleanup(func() {
		reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook, currentRoutingSetPositions = oldTasks, oldCheck, oldList, oldInspect, oldPositions
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
	currentRoutingSetPositions = func(string, []DiscordChannelPosition, string) error { return nil }
}

// TestRoutingEditorScriptsAreBalanced guards against the production finding:
// an unbalanced brace in an inline editor script silently killed the whole
// script, so no change detection or apply enablement ever ran. Every script
// block in the rendered editor must keep its braces balanced outside string
// literals.
func TestRoutingEditorScriptsAreBalanced(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "lane-script-balance", Name: "Balance", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "first", "", "https://discord.com/api/webhooks/111111111111111111/token-a", "channel-first"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "task-a", TaskTypeName: "First", DestinationKind: model.LaneChannel, DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-first"}}); err != nil {
		t.Fatal(err)
	}
	rendered := renderCurrentIARoutingEditorSetupStyleWithData(db, httptest.NewRequest("GET", "/bot/admin/projects?lang=ja", nil), project, "ja", "", productionNotificationReviewerView{})

	rest := rendered
	scriptIndex := 0
	for {
		start := strings.Index(rest, "<script>")
		if start < 0 {
			break
		}
		end := strings.Index(rest[start:], "</script>")
		if end < 0 {
			t.Fatalf("script block %d is not closed", scriptIndex)
		}
		script := rest[start+8 : start+end]
		depth, inString, escape := 0, false, false
		for _, char := range script {
			if inString {
				if escape {
					escape = false
				} else if char == '\\' {
					escape = true
				} else if char == '\'' {
					inString = false
				}
				continue
			}
			switch char {
			case '\'':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth < 0 {
					t.Fatalf("script block %d closes a brace that was never opened", scriptIndex)
				}
			}
		}
		if depth != 0 {
			t.Fatalf("script block %d ends with unbalanced braces (depth=%d)", scriptIndex, depth)
		}
		rest = rest[start+end:]
		scriptIndex++
	}
	if scriptIndex == 0 {
		t.Fatal("no script blocks found in the rendered editor")
	}
}

package setup

import (
	"app/src/api/kitsu"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskTypePlanUsesEntityContextForDuplicateNames(t *testing.T) {
	plan := BuildTaskTypeChannelPlan("production-1", "guild-1", []kitsu.TaskType{
		{ID: "tt-shot", Name: "Concept", ForEntity: "Shot"},
		{ID: "tt-asset", Name: "Concept", ForEntity: "Asset"},
	}, nil)
	if !plan.Valid() {
		t.Fatalf("semantic duplicate plan should be valid: %#v", plan)
	}
	for _, want := range []string{"Asset / Concept", "Shot / Concept"} {
		found := false
		for _, entry := range plan.Entries {
			if entry.DisplayName() == want {
				found = true
				if entry.ChannelName != "concept-"+strings.ToLower(strings.Split(want, " /")[0]) {
					t.Fatalf("%s got channel %q", want, entry.ChannelName)
				}
			}
		}
		if !found {
			t.Fatalf("missing semantic label %q: %#v", want, plan.Entries)
		}
	}
}

func TestTaskTypePlanWithoutContextFailsClosedOnDuplicateNames(t *testing.T) {
	plan := BuildTaskTypeChannelPlan("production-1", "guild-1", []kitsu.TaskType{
		{ID: "tt-1", Name: "Concept"},
		{ID: "tt-2", Name: "Concept"},
	}, nil)
	if plan.Valid() || len(plan.Conflicts) != 1 {
		t.Fatalf("context-free duplicate must remain blocked: %#v", plan)
	}
}

func TestProductionTaskTypeDiscoveryExcludesArchivedRecords(t *testing.T) {
	got := filterActiveTaskTypes([]kitsu.TaskType{
		{ID: "active", Name: "Concept"},
		{ID: "archived", Name: "Concept", Archived: true},
		{ID: "is-archived", Name: "Shading", IsArchived: true},
	})
	if len(got) != 1 || got[0].ID != "active" {
		t.Fatalf("archived Task Types were not excluded: %#v", got)
	}
}

func TestDepartmentDoesNotChangeTaskTypeRoutingIdentity(t *testing.T) {
	base := BuildTaskTypeChannelPlan("production-1", "guild-1", []kitsu.TaskType{{ID: "tt-1", Name: "Concept", ForEntity: "Asset", DepartmentID: "dept-a"}}, nil)
	changed := BuildTaskTypeChannelPlan("production-1", "guild-1", []kitsu.TaskType{{ID: "tt-1", Name: "Concept", ForEntity: "Asset", DepartmentID: "dept-b"}}, nil)
	if base.Entries[0].TaskTypeID != changed.Entries[0].TaskTypeID || base.Entries[0].ChannelName != changed.Entries[0].ChannelName {
		t.Fatalf("department changed routing identity: base=%#v changed=%#v", base.Entries[0], changed.Entries[0])
	}
}

func TestBlockedWizardPlanKeepsBackAndDisablesForwardAction(t *testing.T) {
	r := httptest.NewRequest("GET", "/bot/setup?lang=ja", nil)
	body := renderBlockedWizardPlanNavigation("ja", r, "production-1", "guild-1", false)
	if !strings.Contains(body, tr("ja", "wizard.back")) || !strings.Contains(body, "disabled") || !strings.Contains(body, "aria-disabled=\"true\"") {
		t.Fatalf("blocked Step 4 navigation is incomplete: %s", body)
	}
	if strings.Contains(body, "href=\"/bot/setup?lang=ja&amp;project=production-1&amp;wizard_step=5") {
		t.Fatal("blocked plan exposed a forward navigation URL")
	}
}

func TestTaskTypePlanOverridesPreserveIdentityAndOrder(t *testing.T) {
	types := []kitsu.TaskType{{ID: "tt-1", Name: "Concept"}, {ID: "tt-2", Name: "Modeling"}}
	base := BuildTaskTypeChannelPlan("production-1", "guild-1", types, nil)
	overridden := BuildTaskTypeChannelPlanWithOverrides("production-1", "guild-1", types, nil, map[string]TaskTypeChannelPlanOverride{
		"tt-1": {ChannelName: "review", Order: 2},
		"tt-2": {ChannelName: "modeling", Order: 1},
	})
	if !overridden.Valid() || overridden.Entries[0].TaskTypeID != "tt-2" || overridden.Entries[1].TaskTypeID != "tt-1" {
		t.Fatalf("overridden plan did not preserve requested order: %#v", overridden)
	}
	if overridden.Entries[1].TaskTypeID != base.Entries[0].TaskTypeID && overridden.Entries[1].TaskTypeID != "tt-1" {
		t.Fatalf("task type identity changed during override: %#v", overridden)
	}
	if base.Fingerprint() == overridden.Fingerprint() {
		t.Fatal("channel name/order edits did not change the plan fingerprint")
	}
}

func TestTaskTypePlanOverrideDuplicateNamesRemainBlocked(t *testing.T) {
	types := []kitsu.TaskType{{ID: "tt-1", Name: "Concept"}, {ID: "tt-2", Name: "Modeling"}}
	plan := BuildTaskTypeChannelPlanWithOverrides("production-1", "guild-1", types, nil, map[string]TaskTypeChannelPlanOverride{
		"tt-1": {ChannelName: "same", Order: 1},
		"tt-2": {ChannelName: "same", Order: 2},
	})
	if plan.Valid() || len(plan.Conflicts) == 0 {
		t.Fatalf("duplicate final channel names must fail closed: %#v", plan)
	}
}

func TestTaskTypePlanRequestExcludesAndReaddsProductionTaskTypes(t *testing.T) {
	types := []kitsu.TaskType{{ID: "tt-1", Name: "Concept"}, {ID: "tt-2", Name: "Modeling"}, {ID: "tt-3", Name: "Shading"}}
	r := httptest.NewRequest("GET", "/bot/setup?included_task_type_id=tt-1&included_task_type_id=tt-3&channel_name_tt-1=concept&channel_order_tt-1=2&channel_name_tt-3=shading&channel_order_tt-3=1&add_task_type=tt-2", nil)
	active, overrides := taskTypePlanRequest(r, types)
	if len(active) != 3 || active[1].ID != "tt-2" {
		t.Fatalf("re-add did not restore the stable Task Type ID: %#v", active)
	}
	plan := BuildTaskTypeChannelPlanWithOverrides("production-1", "guild-1", active, nil, overrides)
	if len(plan.Entries) != 3 || plan.Entries[0].TaskTypeID != "tt-3" || plan.Entries[1].TaskTypeID != "tt-1" || plan.Entries[2].TaskTypeID != "tt-2" {
		t.Fatalf("included order was not preserved: %#v", plan.Entries)
	}
}

func TestTaskTypePlanRequestExcludesWithoutDiscordDeletion(t *testing.T) {
	types := []kitsu.TaskType{{ID: "tt-1", Name: "Concept"}, {ID: "tt-2", Name: "Modeling"}}
	r := httptest.NewRequest("GET", "/bot/setup?included_task_type_id=tt-1&channel_name_tt-1=concept&channel_order_tt-1=1", nil)
	active, _ := taskTypePlanRequest(r, types)
	if len(active) != 1 || active[0].ID != "tt-1" {
		t.Fatalf("excluded Task Type remained in the plan: %#v", active)
	}
}

func TestStepFourExcludeRequestRerendersExcludedCandidate(t *testing.T) {
	types := []kitsu.TaskType{{ID: "tt-animation", Name: "Animation"}, {ID: "tt-compositing", Name: "Compositing"}, {ID: "tt-concept", Name: "Concept"}}
	r := httptest.NewRequest("GET", "/bot/setup?lang=en&project=production-1&plan_guild=guild-1&wizard_step=4&included_task_type_id=tt-animation&included_task_type_id=tt-concept&channel_name_tt-animation=animation&channel_order_tt-animation=1&channel_name_tt-concept=concept&channel_order_tt-concept=2", nil)
	active, overrides := taskTypePlanRequest(r, types)
	if len(active) != 2 || active[0].ID != "tt-animation" || active[1].ID != "tt-concept" {
		t.Fatalf("exclude request did not preserve the submitted stable ID set: %#v", active)
	}
	body := renderWizardPlanPolished("en", r, KitsuProject{ID: "production-1", Name: "Example Production"}, "production-1", "guild-1", types, BuildTaskTypeChannelPlanWithOverrides("production-1", "guild-1", active, nil, overrides))
	if strings.Contains(body, `data-task-type="tt-compositing"`) && strings.Contains(body, `<strong>Compositing</strong>`) {
		t.Fatal("excluded Task Type was still rendered as an active row")
	}
	if !strings.Contains(body, `<option value="tt-compositing">Compositing</option>`) {
		t.Fatalf("excluded Task Type was not rendered as an Add candidate: %s", body)
	}
	if strings.Contains(body, "All Task Types are included in notifications.") {
		t.Fatal("empty-candidate explanation appeared while a candidate exists")
	}
}

func TestKitsuSyncCategoryNameIsDeterministicAndBounded(t *testing.T) {
	name := KitsuSyncCategoryName("KitsuSync Local Test")
	if name != "KitsuSync – KitsuSync Local Test" {
		t.Fatalf("unexpected category name %q", name)
	}
	if got := len([]rune(KitsuSyncCategoryName(strings.Repeat("Production ", 30)))); got > 100 {
		t.Fatalf("category name exceeded Discord limit: %d", got)
	}
}

func TestWizardReviewIsConciseAndOmitsZeroStateDetails(t *testing.T) {
	plan := BuildTaskTypeChannelPlan("production-1", "guild-1", []kitsu.TaskType{{ID: "tt-1", Name: "Concept"}, {ID: "tt-2", Name: "Modeling"}}, nil)
	body := renderWizardPlanReview("en", httptest.NewRequest("GET", "/bot/setup?lang=en", nil), KitsuProject{ID: "production-1", Name: "Example Production"}, "guild-1", "", plan)
	for _, want := range []string{"Example Production", "Selected Discord server", "Discord Category", "2 channels will be created.", "#concept", "#modeling", "wizard-confirm"} {
		if !strings.Contains(body, want) {
			t.Fatalf("review missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"conflicts: 0", "deleted: 0", "reused: 0", "Task Type channel plan"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("review exposed unnecessary detail %q", forbidden)
		}
	}
	if !strings.Contains(body, `type="checkbox"`) || !strings.Contains(body, `required`) {
		t.Fatal("review confirmation control is not a required compact checkbox")
	}
}

func TestWizardReviewJapaneseLabelsAndExecuteState(t *testing.T) {
	plan := BuildTaskTypeChannelPlan("production-1", "guild-1", []kitsu.TaskType{{ID: "tt-1", Name: "Concept"}}, nil)
	body := renderWizardPlanReview("ja", httptest.NewRequest("GET", "/bot/setup?lang=ja", nil), KitsuProject{ID: "production-1", Name: "Example Production"}, "guild-1", "", plan)
	for _, want := range []string{"Kitsuプロダクション", "Discordカテゴリ", `id="wizard-confirm"`, `id="wizard-execute"`, `disabled`} {
		if !strings.Contains(body, want) {
			t.Fatalf("Japanese review missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Kitsuプロダクション。") || strings.Contains(body, "Discordカテゴリ。") || strings.Contains(tr("ja", "wizard.confirm"), "。") {
		t.Fatal("short Japanese review labels or checkbox text have trailing punctuation")
	}
}

func TestWizardPlanPolishedRowsExposeExclusionWithoutPermanentReorderControls(t *testing.T) {
	r := httptest.NewRequest("GET", "/bot/setup?lang=en", nil)
	plan := BuildTaskTypeChannelPlan("production-1", "guild-1", []kitsu.TaskType{{ID: "tt-1", Name: "Concept"}}, nil)
	body := renderWizardPlanPolished("en", r, KitsuProject{ID: "production-1", Name: "Example Production"}, "production-1", "guild-1", []kitsu.TaskType{{ID: "tt-1", Name: "Concept"}, {ID: "tt-2", Name: "Modeling"}}, plan)
	for _, want := range []string{`draggable="true"`, `data-exclude="tt-1"`, "Exclude from notifications", "Add Task Type", "Alt+Arrow Up/Down"} {
		if !strings.Contains(body, want) {
			t.Fatalf("polished plan missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"wizard-move", "wizard-drag-handle", `data-move="up"`, `data-move="down"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("permanent reorder control remains: %q", forbidden)
		}
	}
	if strings.Contains(body, `<span class="status-pill ok">`) || strings.Contains(body, "Ready") {
		t.Fatalf("valid Step 4 should not render a redundant ready badge: %s", body)
	}
}

func TestWizardPlanPolishedAlwaysShowsAddTaskTypeControl(t *testing.T) {
	r := httptest.NewRequest("GET", "/bot/setup?lang=en", nil)
	all := []kitsu.TaskType{{ID: "tt-1", Name: "Concept"}, {ID: "tt-2", Name: "Modeling"}}
	active := BuildTaskTypeChannelPlan("production-1", "guild-1", all, nil)
	body := renderWizardPlanPolished("en", r, KitsuProject{ID: "production-1", Name: "Example Production"}, "production-1", "guild-1", all, active)
	if !strings.Contains(body, `id="wizard-add-task-type" name="task_type_id" disabled aria-disabled="true"`) || !strings.Contains(body, `name="action" value="include" disabled aria-disabled="true"`) {
		t.Fatalf("add group controls should remain visible and disabled when all Task Types are included: %s", body)
	}
	if !strings.Contains(body, `grid-template-columns:auto minmax(220px,1fr) auto`) {
		t.Fatalf("add group should keep label, select, and button in one desktop row: %s", body)
	}
	if !strings.Contains(body, `class="wizard-add-task-type-select`) || !strings.Contains(body, `appearance:none;-webkit-appearance:none`) || !strings.Contains(body, `pointer-events:none`) {
		t.Fatalf("add select should use a scoped, keyboard-safe visual indicator: %s", body)
	}
	if strings.Contains(body, "+ Add Task Type") || strings.Contains(body, "+ Task Type") {
		t.Fatalf("add control should not have a leading plus: %s", body)
	}
	if !strings.Contains(body, "All included") {
		t.Fatalf("missing disabled-state explanation: %s", body)
	}
	if strings.Contains(body, "wizard-add-toggle") {
		t.Fatalf("standalone add toggle remains in the rendered UI: %s", body)
	}
	jpBody := renderWizardPlanPolished("ja", r, KitsuProject{ID: "production-1", Name: "Example Production"}, "production-1", "guild-1", all, active)
	if !strings.Contains(jpBody, "Task Type") || !strings.Contains(jpBody, "すべて追加済み") {
		t.Fatalf("Japanese add control or disabled-state explanation is missing: %s", jpBody)
	}

	partial := BuildTaskTypeChannelPlan("production-1", "guild-1", all[:1], nil)
	body = renderWizardPlanPolished("en", r, KitsuProject{ID: "production-1", Name: "Example Production"}, "production-1", "guild-1", all, partial)
	if strings.Contains(body, `id="wizard-add-task-type" name="task_type_id" disabled`) || strings.Contains(body, `name="action" value="include" disabled`) {
		t.Fatalf("add controls should be enabled when a valid Task Type is excluded: %s", body)
	}
	if !strings.Contains(body, `<option value="tt-2">Modeling</option>`) || strings.Contains(body, `<option value="tt-1">Concept</option>`) {
		t.Fatalf("add control should list only excluded Task Types: %s", body)
	}
	if !strings.Contains(body, `type="submit" name="action" value="exclude" class="wizard-exclude"`) {
		t.Fatalf("exclude action should resubmit the current included ID set for candidate recalculation: %s", body)
	}
}

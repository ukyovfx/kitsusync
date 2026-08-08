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

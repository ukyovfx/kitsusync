package setup

import (
	"net/http/httptest"
	"strings"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
)

// Removing a row in the channel plan must update the page in place instead of
// submitting the whole wizard form.
func TestWizardPlanExcludeUpdatesInPlace(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "inplace-production", Name: "In Place", DiscordGuildID: "guild-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	types := []kitsu.TaskType{{ID: "tt1", Name: "Concept"}, {ID: "tt2", Name: "Modeling"}}
	plan := BuildTaskTypeChannelPlanWithOverrides(project.KitsuProjectID, project.DiscordGuildID, types, nil, nil)
	body := renderWizardPlanPolished("en", httptest.NewRequest("GET", "/bot/setup?wizard_step=4", nil), KitsuProject{ID: project.KitsuProjectID, Name: project.Name}, project.KitsuProjectID, project.DiscordGuildID, types, plan)

	if !strings.Contains(body, "event.preventDefault()") || !strings.Contains(body, "row.remove()") {
		t.Fatal("exclude still submits the whole wizard form instead of removing the row in place")
	}
	if !strings.Contains(body, "wizard-add-task-type") || !strings.Contains(body, "appendChild(option)") {
		t.Fatal("excluded task type is not offered again in the add-task-type control")
	}
	if !strings.Contains(body, `name="action" value="exclude"`) {
		t.Fatal("exclude lost its no-JavaScript fallback submit")
	}
}

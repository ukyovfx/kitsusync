package discord

import (
	"strings"
	"testing"

	"app/src/model"
)

// An en Production must get English signal cards; an empty language
// keeps the Japanese fallback the pre-existing tests rely on.

func TestForumSignalCardFollowsEnglishProduction(t *testing.T) {
	signal := RenderForumSignalCard(ForumCardInput{
		Intent:     model.ForumIntentStatusWFA,
		TaskType:   "Layout",
		EntityType: "Shot",
		EntityName: "sh010",
		Submitter:  "Jane Doe",
		TaskURL:    "https://kitsu.example/t/1",
		StatusName: "wfa",
		Assignees:  []string{"John Smith"},
		Language:   "en",
	})
	description := signal.Embeds[0].Description
	for _, want := range []string{"### 👀 WFA", "Please review", "Submitted by: Jane Doe", "Status: wfa", "Assignees: John Smith"} {
		if !strings.Contains(description, want) {
			t.Errorf("description %q missing %q", description, want)
		}
	}
	for _, gone := range []string{"提出：", "ステータス：", "担当："} {
		if strings.Contains(description, gone) {
			t.Errorf("description %q still has %q", description, gone)
		}
	}
}

func TestForumSignalAssignLineFollowsEnglishProduction(t *testing.T) {
	signal := RenderForumSignalCard(ForumCardInput{
		Intent:   model.ForumIntentAssign,
		TaskType: "Layout",
		Language: "en",
	})
	description := signal.Embeds[0].Description
	if !strings.Contains(description, "You have been assigned to this task.") {
		t.Errorf("description = %q, want the English assign line", description)
	}
	if strings.Contains(description, "このタスクの担当になりました。") {
		t.Errorf("description = %q, want no Japanese", description)
	}
}

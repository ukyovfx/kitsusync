package discord

import (
	"strings"
	"testing"

	"app/src/model"
)

func handoffCardInput(lang string) ForumCardInput {
	return ForumCardInput{
		Intent:                model.ForumIntentHandoff,
		TaskType:              "Compositing",
		EntityType:            "Shot",
		ParentName:            "sq010",
		EntityName:            "sh002",
		TaskURL:               "https://kitsu.example/tasks/task-1",
		StatusName:            "wip",
		Assignees:             []string{"Comp Artist"},
		Language:              lang,
		HandoffProductName:    "camera",
		HandoffProductVersion: "v003",
		HandoffSourceTaskType: "Layout",
		HandoffSourceAuthor:   "Layout Artist",
		Components: []ActionRow{{Type: 1, Components: []Button{
			{Type: 2, Style: ButtonStyleSuccess, Label: "OK", CustomID: "handoff:ok:comment-1", Disabled: true},
		}}},
	}
}

func TestHandoffSignalCardJapanese(t *testing.T) {
	signal := RenderForumSignalCard(handoffCardInput("ja"))
	if len(signal.Embeds) != 1 {
		t.Fatalf("embeds = %+v", signal.Embeds)
	}
	desc := signal.Embeds[0].Description
	for _, want := range []string{"🔁 素材の受け渡し — camera", "版: v003", "Layout Artist の Layout から届きました。", "ステータス：wip", "担当：Comp Artist"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description %q missing %q", desc, want)
		}
	}
	if len(signal.Components) != 1 {
		t.Fatalf("components = %+v", signal.Components)
	}
	body := ForumSignalMessage(signal, "nonce-1")
	if len(body.Components) != 1 || body.Components[0].Components[0].CustomID != "handoff:ok:comment-1" {
		t.Fatalf("message components = %+v", body.Components)
	}
}

func TestHandoffSignalCardEnglish(t *testing.T) {
	signal := RenderForumSignalCard(handoffCardInput("en"))
	desc := signal.Embeds[0].Description
	for _, want := range []string{"🔁 Handoff received — camera", "Version: v003", "Delivered from Layout Artist's Layout task.", "Status: wip"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description %q missing %q", desc, want)
		}
	}
}

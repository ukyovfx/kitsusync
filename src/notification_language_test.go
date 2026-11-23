package main

import (
	"strings"
	"testing"

	"app/src/api/discord"
	"app/src/model"
)

// All notification surfaces must follow the Production language setting.
// These prove the morning digest, the handoff signal and its reply buttons in
func TestImmediateHandoffSignalFollowsEnglishProduction(t *testing.T) {
	signal := discord.RenderForumSignalCard(discord.ForumCardInput{
		Intent:                model.ForumIntentHandoff,
		TaskType:              "Compositing",
		EntityType:            "Shot",
		EntityName:            "sq010_sh001",
		TaskURL:               "https://kitsu.example/t/1/productions/p1/shots/tasks/task-comp-1",
		Language:              discord.NotificationLanguage("en"),
		HandoffProductName:    "camera",
		HandoffProductVersion: "v003",
		HandoffSourceTaskType: "Layout",
		HandoffSourceAuthor:   "Layout Artist",
	})
	text := signal.Embeds[0].Description
	for _, want := range []string{
		"🔁 Handoff received — camera",
		"Version: v003",
		"Delivered from Layout Artist's Layout task.",
		"[🦊 Kitsu](https://kitsu.example/t/1/productions/p1/shots/tasks/task-comp-1)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("signal %q missing %q", text, want)
		}
	}
	event := model.HandoffEvent{CommentID: "comment-1"}
	rows := handoffReplyRows([]model.HandoffEvent{event}, discord.NotificationLanguage("en"))
	if labels := []string{rows[0].Components[0].Label, rows[0].Components[1].Label}; labels[0] != "OK" || labels[1] != "Retake" {
		t.Errorf("reply buttons = %v, want OK/Retake", labels)
	}
}

package main

import (
	"strings"
	"testing"

	"app/src/api/discord"
	"app/src/model"
)

// All notification surfaces must follow the Production language setting.
// These prove the morning digest, the handoff DM and its reply buttons in
// English; empty language keeps the Japanese fallback.

func TestForumDigestFollowsEnglishProduction(t *testing.T) {
	recipient := forumDigestRecipient{
		Waiting: []forumDigestItem{{Title: "Shot / sq010 - sh002"}},
		Handoffs: []model.HandoffEvent{{
			EntityName: "sq010", ProductName: "camera", ProductVersion: "v003",
			SourceAuthorName: "Layout Artist", SourceTaskTypeName: "Layout",
		}},
		HandoffsMore: 2,
	}
	card := renderForumDigestCard("Forest", recipient, "https://kitsu.example/t/1", discord.NotificationLanguage("en"))
	for _, want := range []string{
		"📋 Morning digest — Forest",
		"- Awaiting review (WFA): 1",
		"- Retakes (RETAKE): 0",
		"- Asset handoffs: 1",
		"sq010: camera v003 ← from Layout Artist's Layout",
		"2 more awaiting a reply",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("digest %q missing %q", card, want)
		}
	}
	for _, gone := range []string{"朝のダイジェスト", "チェック待ち", "素材の受け渡し"} {
		if strings.Contains(card, gone) {
			t.Errorf("digest %q still has %q", card, gone)
		}
	}
}

func TestImmediateHandoffDMFollowsEnglishProduction(t *testing.T) {
	event := model.HandoffEvent{
		EntityName: "sq010_sh001", ProductName: "camera", ProductVersion: "v003",
		SourceAuthorName: "Layout Artist", SourceTaskTypeName: "Layout",
		ProductionID: "p1", EntityKind: model.ForumEntityKindShot, NotifyTaskID: "task-comp-1",
	}
	card := renderHandoffCard("https://kitsu.example/t/1", event, discord.NotificationLanguage("en"))
	for _, want := range []string{
		"🔁 Asset handoff — sq010_sh001 / camera",
		"Version: v003",
		"Received from Layout Artist's Layout.",
		"Kitsu: https://kitsu.example/t/1/productions/p1/shots/tasks/task-comp-1",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card %q missing %q", card, want)
		}
	}
	rows := handoffReplyRows([]model.HandoffEvent{event}, discord.NotificationLanguage("en"))
	if labels := []string{rows[0].Components[0].Label, rows[0].Components[1].Label}; labels[0] != "OK" || labels[1] != "Retake" {
		t.Errorf("reply buttons = %v, want OK/Retake", labels)
	}
}

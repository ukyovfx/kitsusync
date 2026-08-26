package discord

import (
	"strings"
	"testing"

	"app/src/utils/config"
)

func TestParseKitsuStatusColorUsesNeutralFallback(t *testing.T) {
	if got := parseKitsuStatusColor("#12abEF"); got != 0x12abef {
		t.Fatalf("valid status color = %#x, want %#x", got, 0x12abef)
	}
	if got := parseKitsuStatusColor("not-a-color"); got != neutralEmbedColor {
		t.Fatalf("invalid status color = %#x, want %#x", got, neutralEmbedColor)
	}
	if got := parseKitsuStatusColor(""); got != neutralEmbedColor {
		t.Fatalf("empty status color = %#x, want %#x", got, neutralEmbedColor)
	}
}

func TestNotificationRecipientPolicy(t *testing.T) {
	conf := testMentionConfig()
	if got := notificationRecipientCandidates("WFA", false, []string{"101"}, []string{"202"}, conf); strings.Join(got, ",") != "202" {
		t.Fatalf("WFA recipients = %v, want checker", got)
	}
	if got := notificationRecipientCandidates("RETAKE", false, []string{"101", "101"}, []string{"202"}, conf); strings.Join(got, ",") != "101" {
		t.Fatalf("RETAKE recipients = %v, want deduplicated assignee", got)
	}
	if got := notificationRecipientCandidates("DONE", false, []string{"101"}, []string{"202"}, conf); len(got) != 0 {
		t.Fatalf("DONE default recipients = %v, want none", got)
	}
	if got := notificationRecipientCandidates("WFA", false, []string{"101"}, []string{"bad", "202", "202", "<@303>"}, conf); strings.Join(got, ",") != "202" {
		t.Fatalf("recipient validation = %v", got)
	}
}

func TestNotificationRecipientBoundAndMentionContentMatch(t *testing.T) {
	ids := make([]string, maxNotificationRecipients+2)
	for i := range ids {
		ids[i] = "10" + strings.Repeat("0", i+1)
	}
	got := uniqueDiscordIDs(ids)
	if len(got) != maxNotificationRecipients {
		t.Fatalf("recipient count = %d, want %d", len(got), maxNotificationRecipients)
	}
	content := mentionContent(got)
	for _, id := range got {
		if !strings.Contains(content, "<@"+id+">") {
			t.Fatalf("mention content does not match allowed ID %s: %q", id, content)
		}
	}
	if strings.Contains(content, "everyone") || strings.Contains(content, "here") || strings.Contains(content, "<@&") {
		t.Fatalf("unsafe mention content: %q", content)
	}
}

func TestFinalNotificationCardHasNoDuplicateStatusOrTaskTypeEmoji(t *testing.T) {
	useRepositoryRootForTemplates(t)
	data := Template{
		TaskType:                "Compositing",
		GroupName:               "Shot",
		ParentName:              "SC02",
		TaskName:                "cut009",
		StatusUpper:             "RETAKE",
		StatusEmoji:             "🔄",
		StatusMessage:           "A revision is needed",
		StatusTransitionMessage: "A revision is required after review",
		PreviousStatus:          "WFA",
		AssigneeLabel:           "Assignee",
		AssigneesStr:            "UKYO M",
		NotificationLanguage:    "en",
	}
	payload := RenderNotificationPayload(data, "rich")
	if len(payload.Embeds) != 1 {
		t.Fatalf("embed count = %d, want 1", len(payload.Embeds))
	}
	embed := payload.Embeds[0]
	if embed.Author.Name != "Compositing" {
		t.Fatalf("task type author = %q", embed.Author.Name)
	}
	if strings.Contains(embed.Description, "Status") || strings.Contains(embed.Description, "<@") {
		t.Fatalf("duplicate status or mention leaked into embed: %q", embed.Description)
	}
	if len(embed.Fields) != 1 || embed.Fields[0].Name != "Assignee" {
		t.Fatalf("unexpected compact fields: %+v", embed.Fields)
	}
	if strings.HasPrefix(embed.Author.Name, "🧩") {
		t.Fatalf("decorative task type emoji leaked into card: %q", embed.Author.Name)
	}
}

func TestNotificationCardFixturesCoverStatusCommentsLinksAndPreview(t *testing.T) {
	useRepositoryRootForTemplates(t)
	cases := []struct {
		name, status, message string
		commentOnly           bool
		drive, preview        bool
	}{
		{"WFA", "WFA", "Please review", false, true, true},
		{"RETAKE", "RETAKE", "A revision is needed", false, false, false},
		{"DONE", "DONE", "Completed. Please check if needed.", false, true, false},
		{"comment-only-WFA", "WFA", "Please review", true, false, false},
		{"comment-only-RETAKE", "RETAKE", "A revision is needed", true, false, false},
		{"comment-only-DONE", "DONE", "Completed. Please check if needed.", true, false, false},
	}
	for _, tc := range cases {
		data := Template{
			TaskType:                "Compositing",
			StatusUpper:             tc.status,
			StatusMessage:           tc.message,
			StatusEmoji:             "•",
			CommentOnlyMessage:      "Revision details were updated",
			IsCommentOnly:           tc.commentOnly,
			StatusTransitionMessage: "A transition",
			PreviousStatus:          "WFA",
			CommentLabel:            "Comment",
			CommentContent:          "Please adjust this area",
			AssigneeLabel:           "Assignee",
			AssigneesStr:            "Artist A, Artist B",
			NotificationLanguage:    "en",
		}
		if tc.drive {
			data.GoogleDriveURL = "https://drive.example/folder"
		}
		if tc.preview {
			data.PreviewImageURL = "https://kitsu.example/preview.png"
		}
		payload := RenderNotificationPayload(data, "rich")
		embed := payload.Embeds[0]
		if tc.commentOnly && strings.Contains(embed.Description, "A transition") {
			t.Fatalf("%s fixture contains a fake transition: %q", tc.name, embed.Description)
		}
		if (embed.Image != nil) != tc.preview {
			t.Fatalf("%s preview presence = %v, want %v", tc.name, embed.Image != nil, tc.preview)
		}
		if tc.drive != strings.Contains(embed.Description, "Drive") {
			t.Fatalf("%s drive presence mismatch: %q", tc.name, embed.Description)
		}
	}
}

func TestRenderNotificationPayloadFitsDiscordEmbedTextLimit(t *testing.T) {
	useRepositoryRootForTemplates(t)
	payload := RenderNotificationPayload(Template{
		TaskType:             "Animation",
		StatusUpper:          "WFA",
		StatusEmoji:          "👀",
		StatusMessage:        strings.Repeat("x", 4096),
		CommentLabel:         "Comment",
		CommentContent:       strings.Repeat("y", 4096),
		AssigneeLabel:        "Assignee",
		AssigneesStr:         strings.Repeat("z", 1024),
		NotificationLanguage: "en",
	}, "rich")
	if got := embedTextLength(payload.Embeds[0]); got > maxDiscordEmbedText {
		t.Fatalf("embed text length = %d, exceeds %d", got, maxDiscordEmbedText)
	}
}

func testMentionConfig() config.Config {
	return config.Config{}
}

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
		TaskURL:                 "https://kitsu.example/tasks/cut009",
		GoogleDriveURL:          "https://drive.example/cut009",
	}
	payload := RenderNotificationPayload(data, "rich")
	if len(payload.Embeds) != 1 {
		t.Fatalf("embed count = %d, want 1", len(payload.Embeds))
	}
	embed := payload.Embeds[0]
	if embed.Title != "Shot / SC02 - cut009" {
		t.Fatalf("task context title = %q", embed.Title)
	}
	if embed.Url != "" {
		t.Fatalf("task title must not be a hyperlink: %q", embed.Url)
	}
	if embed.Author.Name != "Compositing" {
		t.Fatalf("task type author = %q", embed.Author.Name)
	}
	if strings.Contains(embed.Description, "Status") || strings.Contains(embed.Description, "<@") {
		t.Fatalf("duplicate status or mention leaked into embed: %q", embed.Description)
	}
	if strings.Contains(embed.Description, " → ") {
		t.Fatalf("transition leaked into description: %q", embed.Description)
	}
	if len(embed.Fields) != 3 || embed.Fields[0].Name != "\u200b" || embed.Fields[1].Name != "📊 Status" || embed.Fields[2].Name != "👤 Assignee" {
		t.Fatalf("unexpected compact fields: %+v", embed.Fields)
	}
	if strings.Contains(embed.Fields[0].Name, "Links") || strings.Contains(embed.Fields[0].Name, "リンク") {
		t.Fatalf("generic links heading must be omitted: %+v", embed.Fields[0])
	}
	if strings.Contains(embed.Fields[0].Name, "🔗") || !strings.Contains(embed.Fields[0].Value, "📁 [Drive]") || !strings.Contains(embed.Fields[0].Value, "🦊 [Kitsu]") {
		t.Fatalf("service link hierarchy is incorrect: %+v", embed.Fields[0])
	}
	if embed.Fields[0].Inline || !strings.Contains(embed.Fields[0].Value, "Drive") || !strings.Contains(embed.Fields[0].Value, "Kitsu") {
		t.Fatalf("links are not compact or ordered: %+v", embed.Fields[1])
	}
	if embed.Fields[1].Value != "wfa → retake" || !embed.Fields[1].Inline || !embed.Fields[2].Inline {
		t.Fatalf("bottom metadata is not inline or transition-aware: %+v", embed.Fields[1:])
	}
	if strings.HasPrefix(embed.Author.Name, "🧩") {
		t.Fatalf("decorative task type emoji leaked into card: %q", embed.Author.Name)
	}
}

func TestNotificationStatusColorUsesStableSemanticAccents(t *testing.T) {
	if got := notificationStatusColor("WFA"); got != 0xD4A72C {
		t.Fatalf("WFA color = %#x", got)
	}
	if got := notificationStatusColor("RETAKE"); got != 0xC45656 {
		t.Fatalf("RETAKE color = %#x", got)
	}
	if got := notificationStatusColor("DONE"); got != 0x4FAF78 {
		t.Fatalf("DONE color = %#x", got)
	}
	if got := notificationStatusColor("UNKNOWN"); got != neutralEmbedColor {
		t.Fatalf("unknown color = %#x", got)
	}
}

func TestNotificationCardUsesDiscordMarkdownHierarchy(t *testing.T) {
	useRepositoryRootForTemplates(t)
	for _, status := range []string{"WFA", "RETAKE", "DONE"} {
		payload := RenderNotificationPayload(Template{
			EntityType: "Shot", TaskName: "cut001", TaskType: "Animation",
			StatusUpper: status, StatusEmoji: "•", StatusMessage: "Review body",
			NotificationLanguage: "en", Color: notificationStatusColor(status),
		}, "rich")
		description := payload.Embeds[0].Description
		if !strings.HasPrefix(description, "**• "+status+"**") {
			t.Fatalf("%s status is not a compact emphasized line: %q", status, description)
		}
		if strings.Contains(description, "##") || strings.Contains(description, "###") || strings.Contains(description, "-#") {
			t.Fatalf("%s body hierarchy changed unexpectedly: %q", status, description)
		}
		if !strings.Contains(description, "**• "+status+"**\nReview body") {
			t.Fatalf("%s status/body spacing is not compact: %q", status, description)
		}
	}
}

func TestAssigneeDisplayUsesOnlyValidatedLinkedUserMentions(t *testing.T) {
	if got := assigneeDisplayName("Artist A", "123456789012345678"); got != "<@123456789012345678>" {
		t.Fatalf("linked assignee = %q", got)
	}
	if got := assigneeDisplayName("Artist <A>", "not-an-id"); got != "Artist ＜A＞" {
		t.Fatalf("unlinked assignee = %q", got)
	}
	data := Template{
		EntityType: "Shot", TaskName: "cut001", StatusUpper: "WFA", StatusEmoji: "•",
		StatusMessage: "Please review", AssigneesStr: "<@123456789012345678>",
		AllowedUserIDs: []string{"123456789012345678"}, NotificationLanguage: "en",
	}
	payload := RenderNotificationPayload(data, "rich")
	if !strings.Contains(payload.Embeds[0].Fields[1].Value, "<@123456789012345678>") {
		t.Fatalf("linked mention missing from assignee field: %+v", payload.Embeds)
	}
	if len(payload.AllowedMentions.Users) != 1 || payload.AllowedMentions.Users[0] != "123456789012345678" {
		t.Fatalf("allowed mentions = %v", payload.AllowedMentions.Users)
	}
	if len(payload.AllowedMentions.Parse) != 0 || len(payload.AllowedMentions.Roles) != 0 {
		t.Fatalf("broad mention parsing enabled: %+v", payload.AllowedMentions)
	}
}

func TestNotificationCardUsesReferenceHierarchyInJapanese(t *testing.T) {
	useRepositoryRootForTemplates(t)
	payload := RenderNotificationPayload(Template{
		ProjectName:          "Escape",
		EntityType:           "Shot",
		ParentName:           "SC02",
		TaskName:             "cut012",
		TaskType:             "Color Grading",
		StatusUpper:          "DONE",
		StatusEmoji:          "✅",
		StatusMessage:        "完了しました。必要に応じてご確認ください。",
		CommentLabel:         "コメント",
		CommentContent:       "確認をお願いします。",
		CommentAuthorLabel:   "コメント投稿者",
		CommentAuthor:        "USER A",
		AssigneeLabel:        "担当",
		AssigneesStr:         "KOTARO MITA",
		LinksLabel:           "リンク",
		TaskURL:              "https://kitsu.example/tasks/cut012",
		GoogleDriveURL:       "https://drive.example/cut012",
		NotificationLanguage: "ja",
		CardContext:          "テスト通知",
		Color:                0x4FAF78,
	}, "rich")
	embed := payload.Embeds[0]
	if embed.Title != "Shot / SC02 - cut012" || embed.Author.Name != "Color Grading" {
		t.Fatalf("unexpected title/task type hierarchy: %+v", embed)
	}
	if embed.Footer.Text != "テスト通知" {
		t.Fatalf("test marker is not footer-only: %q", embed.Footer.Text)
	}
	if len(embed.Fields) != 4 {
		t.Fatalf("field count = %d, want comment/links/status/assignee", len(embed.Fields))
	}
	if embed.Fields[0].Name != "コメント" || embed.Fields[1].Name != "\u200b" {
		t.Fatalf("supporting fields are not ordered: %+v", embed.Fields[:2])
	}
	if embed.Url != "" {
		t.Fatalf("task title must not be a hyperlink: %q", embed.Url)
	}
	if !strings.Contains(embed.Fields[1].Value, "Drive") || !strings.Contains(embed.Fields[1].Value, "Kitsu") {
		t.Fatalf("links are incomplete: %q", embed.Fields[1].Value)
	}
	if embed.Fields[2].Name != "📊 ステータス" || embed.Fields[3].Name != "👤 担当" || embed.Fields[2].Value != "done" {
		t.Fatalf("metadata labels are not localized/aligned: %+v", embed.Fields[2:])
	}
	for _, field := range embed.Fields[2:] {
		if !field.Inline {
			t.Fatalf("metadata field is not inline: %+v", field)
		}
	}
}

func TestNotificationCardNativeHierarchyIsExplicit(t *testing.T) {
	useRepositoryRootForTemplates(t)
	payload := RenderNotificationPayload(Template{
		EntityType: "Shot", ParentName: "SC02", TaskName: "cut012", TaskType: "Color Grading",
		StatusUpper: "WFA", StatusEmoji: "👀", StatusMessage: "チェックをお願いします",
		PreviousStatus: "TODO", NotificationLanguage: "ja", CardContext: "テスト通知",
		AssigneeLabel: "担当", AssigneesStr: "未割り当て", Color: 0xD4A72C,
	}, "rich")
	if len(payload.Embeds) != 1 {
		t.Fatalf("embed count = %d, want 1", len(payload.Embeds))
	}
	embed := payload.Embeds[0]
	if embed.Author.Name != "Color Grading" {
		t.Fatalf("author must contain only Task Type, got %q", embed.Author.Name)
	}
	if embed.Title != "Shot / SC02 - cut012" {
		t.Fatalf("title must contain only shot context, got %q", embed.Title)
	}
	if embed.Description != "**👀 WFA**\nチェックをお願いします" {
		t.Fatalf("description must be the separated status/body block, got %q", embed.Description)
	}
	if embed.Footer.Text != "テスト通知" {
		t.Fatalf("test marker must be footer-only, got %q", embed.Footer.Text)
	}
	if len(embed.Fields) != 2 || !embed.Fields[0].Inline || !embed.Fields[1].Inline {
		t.Fatalf("expected only two inline metadata fields without comment/links, got %+v", embed.Fields)
	}
	if embed.Fields[0].Name != "📊 ステータス" || embed.Fields[0].Value != "todo → wfa" {
		t.Fatalf("status transition field mismatch: %+v", embed.Fields[0])
	}
	if embed.Fields[1].Name != "👤 担当" || embed.Fields[1].Value != "未割り当て" {
		t.Fatalf("assignee field mismatch: %+v", embed.Fields[1])
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
		if strings.Contains(embed.Description, "A transition") || strings.Contains(embed.Description, " → ") {
			t.Fatalf("%s fixture contains transition text in description: %q", tc.name, embed.Description)
		}
		if (embed.Image != nil) != tc.preview {
			t.Fatalf("%s preview presence = %v, want %v", tc.name, embed.Image != nil, tc.preview)
		}
		fieldsText := ""
		for _, field := range embed.Fields {
			fieldsText += field.Name + " " + field.Value
		}
		if tc.drive != strings.Contains(fieldsText, "Drive") {
			t.Fatalf("%s drive presence mismatch: %q", tc.name, fieldsText)
		}
	}
}

func TestNotificationCardLinksRequireValidURLsAndOmitUnavailableLinks(t *testing.T) {
	useRepositoryRootForTemplates(t)
	cases := []struct {
		name, drive, kitsu, want string
	}{
		{"both", "https://drive.example/folder", "https://kitsu.example/tasks/1", "Drive"},
		{"drive-only", "https://drive.example/folder", "", "Drive"},
		{"kitsu-only", "", "https://kitsu.example/tasks/1", "Kitsu"},
		{"neither", "", "", ""},
		{"malformed-drive", "not a url", "https://kitsu.example/tasks/1", "Kitsu"},
		{"unreliable-kitsu", "https://drive.example/folder", "", "Drive"},
	}
	for _, tc := range cases {
		payload := RenderNotificationPayload(Template{
			TaskType: "Animation", StatusUpper: "WFA", StatusEmoji: "👀", StatusMessage: "Please review",
			AssigneeLabel: "Assignee", AssigneesStr: "Unassigned", NotificationLanguage: "en",
			GoogleDriveURL: tc.drive, TaskURL: tc.kitsu,
		}, "rich")
		var links string
		for _, field := range payload.Embeds[0].Fields {
			if strings.Contains(field.Value, "Drive") || strings.Contains(field.Value, "Kitsu") {
				links = field.Value
			}
			if field.Name == "Links" || field.Name == "リンク" {
				t.Fatalf("generic links heading must be absent: %q", field.Name)
			}
		}
		if tc.want == "" {
			if links != "" {
				t.Fatalf("%s links = %q, want omitted", tc.name, links)
			}
			continue
		}
		if !strings.Contains(links, tc.want) {
			t.Fatalf("%s links = %q, want %q", tc.name, links, tc.want)
		}
	}
}

func TestKitsuTaskURLUsesVerifiedFrontendRoute(t *testing.T) {
	if got := KitsuTaskURL("https://kitsu.example.com/", "production-1", "Shot", "task-1"); got != "https://kitsu.example.com/productions/production-1/shots/tasks/task-1" {
		t.Fatalf("shot task URL = %q", got)
	}
	if got := KitsuTaskURL("https://kitsu.example.com", "production-1", "Asset", "task-1"); got != "https://kitsu.example.com/productions/production-1/assets/tasks/task-1" {
		t.Fatalf("asset task URL = %q", got)
	}
	if got := KitsuTaskURL("https://kitsu.example.com", "production-1", "Sequence", "task-1"); got != "" {
		t.Fatalf("unsupported entity type URL = %q, want omitted", got)
	}
	if got := KitsuTaskURL("not-a-url", "production-1", "Shot", "task-1"); got != "" {
		t.Fatalf("malformed base URL = %q, want omitted", got)
	}
	if got := KitsuTaskURL("https://kitsu.example.com", "production-1", "Shot", ""); got != "" {
		t.Fatalf("missing task ID URL = %q, want omitted", got)
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

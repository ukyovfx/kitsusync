package discord

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func useRepositoryRootForTemplates(t *testing.T) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate notification test")
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func TestSupportedNotificationEventsAreBounded(t *testing.T) {
	want := []NotificationEventKind{
		NotificationEventStatusChange,
		NotificationEventCommentUpdate,
		NotificationEventAssignment,
	}
	got := SupportedNotificationEvents()
	if len(got) != len(want) {
		t.Fatalf("expected %d supported events, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestNotificationLanguageIsProductionScopedAndNormalized(t *testing.T) {
	if NotificationLanguage("en") != "en" || NotificationLanguage(" EN ") != "en" {
		t.Fatal("English Production language was not normalized")
	}
	if NotificationLanguage("ja") != "ja" || NotificationLanguage("fr") != "ja" || NotificationLanguage("") != "ja" {
		t.Fatal("unsupported or missing Production language did not fail closed to Japanese")
	}
}

func TestRenderNotificationPayloadIsDeterministicAndMentionScoped(t *testing.T) {
	useRepositoryRootForTemplates(t)
	data := Template{
		ProjectName:          "Production",
		TaskName:             "Asset",
		TaskType:             "Animation",
		CurrentStatus:        "WFA",
		StatusUpper:          "WFA",
		StatusEmoji:          "🔎",
		StatusMessage:        "Please review",
		NotificationLanguage: "en",
		MentionContent:       "<@123>",
		AllowedUserIDs:       []string{"123", "123"},
		TaskURL:              "https://kitsu.example/tasks/1",
		Color:                123,
	}
	one := RenderNotificationPayload(data, "rich")
	two := RenderNotificationPayload(data, "rich")
	if one.Content != two.Content || len(one.Embeds) != 1 || one.Embeds[0].Description != two.Embeds[0].Description {
		t.Fatal("notification rendering was not deterministic")
	}
	if len(one.AllowedMentions.Users) != 1 || one.AllowedMentions.Users[0] != "123" {
		t.Fatalf("expected one deduplicated user mention, got %+v", one.AllowedMentions)
	}
	raw := one.Content + one.Embeds[0].Title + one.Embeds[0].Description
	if strings.Contains(raw, "@here") || strings.Contains(raw, "@everyone") {
		t.Fatalf("broadcast mention leaked into rendered notification: %q", raw)
	}
}

func TestCurrentStatusCardsDoNotUseLegacyTemplateFiles(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	for _, preset := range []string{"rich", "eng", "legacy"} {
		for _, status := range []string{"WFA", "RETAKE", "DONE"} {
			payload := RenderNotificationPayload(Template{
				EntityType: "Shot", ParentName: "sc001", TaskName: "sh001", TaskType: "Compositing",
				StatusUpper: status, StatusEmoji: "•", StatusMessage: "Please review", NotificationLanguage: "en",
			}, preset)
			want := "## Shot / sc001 - sh001\n\n### • " + status + "\nPlease review"
			if len(payload.Embeds) != 1 || payload.Embeds[0].Author.Name != "Compositing" || payload.Embeds[0].Title != "" || !strings.HasPrefix(payload.Embeds[0].Description, want) || !strings.Contains(payload.Embeds[0].Description, "**📊 Status**　　　**👤 Assignee**\n"+strings.ToLower(status)) {
				t.Fatalf("current status card used legacy preset %q for %s: %+v", preset, status, payload)
			}
		}
	}
}

func TestSupportedEventsRenderInJapaneseAndEnglish(t *testing.T) {
	useRepositoryRootForTemplates(t)
	cases := []struct {
		name       string
		status     string
		assignment bool
		comment    bool
	}{
		{name: "status", status: "WFA"},
		{name: "comment", status: "RETAKE", comment: true},
		{name: "assignment", status: "TODO", assignment: true},
	}
	for _, tc := range cases {
		for _, lang := range []string{"ja", "en"} {
			data := Template{
				ProjectName:          "Production",
				TaskName:             "Asset",
				TaskType:             "Animation",
				CurrentStatus:        tc.status,
				StatusUpper:          tc.status,
				StatusMessage:        "status message",
				StatusEmoji:          "•",
				NotificationLanguage: lang,
				IsAssignNotification: tc.assignment,
				IsCommentOnly:        tc.comment,
				CommentAuthor:        "Artist",
				CommentContent:       "review note",
				TaskURL:              "https://kitsu.example/tasks/1",
			}
			payload := RenderNotificationPayload(data, "rich")
			if len(payload.Embeds) != 1 || strings.TrimSpace(payload.Embeds[0].Description) == "" {
				t.Fatalf("%s/%s did not render a message", tc.name, lang)
			}
		}
	}
}

package discord

import (
	"strings"
	"testing"
	"time"
)

func TestRenderForumSignalCommentCard(t *testing.T) {
	signal := RenderForumSignalCard(ForumCardInput{
		Intent:     "comment",
		TaskType:   "Layout",
		EntityType: "Shot",
		ParentName: "sq01",
		EntityName: "sh002",
		Quote:      "ここを直してください",
		TaskURL:    "https://kitsu.example/productions/1/shots/tasks/2",
		StatusName: "wip",
		Assignees:  []string{"山田 太郎"},
		UserIDs:    []string{"111111111111111111"},
	})
	if signal.Content != "<@111111111111111111>" {
		t.Errorf("content must be the mention line only: %q", signal.Content)
	}
	if len(signal.Embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(signal.Embeds))
	}
	embed := signal.Embeds[0]
	if embed.Title != "Shot / sq01 - sh002" {
		t.Errorf("title = %q", embed.Title)
	}
	if embed.Author.Name != "Layout" {
		t.Errorf("author = %q", embed.Author.Name)
	}
	for _, want := range []string{"💬 ここを直してください", "[🦊 Kitsu](https://kitsu.example/productions/1/shots/tasks/2)", "ステータス：wip", "担当：山田 太郎"} {
		if !strings.Contains(embed.Description, want) {
			t.Errorf("description %q missing %q", embed.Description, want)
		}
	}
	if embed.Color != neutralEmbedColor {
		t.Errorf("comment card must be neutral, got %#x", embed.Color)
	}
	if len(signal.AllowedUserIDs) != 1 || signal.AllowedUserIDs[0] != "111111111111111111" {
		t.Errorf("allowed users = %v", signal.AllowedUserIDs)
	}
}

func TestRenderForumSignalRetakeCard(t *testing.T) {
	signal := RenderForumSignalCard(ForumCardInput{
		Intent:     "retake",
		TaskType:   "Animation",
		EntityType: "Shot",
		EntityName: "sh010",
		Quote:      ".Environment_Hall_Door",
		TaskURL:    "https://kitsu.example/t/1",
		StatusName: "retake",
		Assignees:  []string{"山田", "佐藤"},
		UserIDs:    []string{"222222222222222222"},
	})
	embed := signal.Embeds[0]
	if !strings.Contains(embed.Description, "🔄 RETAKE 修正をお願いします") {
		t.Errorf("description %q missing the retake status line", embed.Description)
	}
	if !strings.Contains(embed.Description, "> .Environment_Hall_Door") {
		t.Errorf("description %q missing the retake quote", embed.Description)
	}
	if embed.Color != 0xC45656 {
		t.Errorf("retake color = %#x", embed.Color)
	}
}

func TestRenderForumSignalWFACardKeepsSubmitter(t *testing.T) {
	signal := RenderForumSignalCard(ForumCardInput{
		Intent:    "wfa",
		TaskType:  "Modeling",
		Submitter: "山田 太郎",
		TaskURL:   "https://kitsu.example/t/1",
		UserIDs:   []string{"222222222222222222"},
	})
	embed := signal.Embeds[0]
	if !strings.Contains(embed.Description, "提出：山田 太郎") {
		t.Errorf("description %q missing the submitter line", embed.Description)
	}
	if strings.Contains(signal.Content, "山田") {
		t.Errorf("submitter must not become a mention: %q", signal.Content)
	}
	if embed.Color != 0xD4A72C {
		t.Errorf("wfa color = %#x", embed.Color)
	}
}

func TestRenderForumSignalPublishCardHasNoMention(t *testing.T) {
	signal := RenderForumSignalCard(ForumCardInput{
		Intent:      "publish",
		TaskType:    "Lighting",
		PublishLine: "📦 Env_A（mayaScene）v012 が publish されました",
		TaskURL:     "https://kitsu.example/t/1",
		StatusName:  "wip",
	})
	if strings.Contains(signal.Content, "<@") {
		t.Errorf("publish signals must not mention: %q", signal.Content)
	}
	embed := signal.Embeds[0]
	if !strings.Contains(embed.Description, "📦 Env_A（mayaScene）v012 が publish されました") {
		t.Errorf("description %q missing the publish line", embed.Description)
	}
	if embed.Color != neutralEmbedColor {
		t.Errorf("publish card must be neutral, got %#x", embed.Color)
	}
}

func TestRenderForumSignalCardFallsBackToLegacyTitle(t *testing.T) {
	signal := RenderForumSignalCard(ForumCardInput{
		Intent:      "comment",
		LegacyTitle: "Layout Shot / sq01 - sh002",
	})
	if signal.Embeds[0].Title != "Layout Shot / sq01 - sh002" {
		t.Errorf("title = %q, want the legacy title", signal.Embeds[0].Title)
	}
}

func TestForumFlapWarningHasNoMentions(t *testing.T) {
	signal := ForumFlapWarning("https://kitsu.example/t/1", 4, 12*time.Minute, "ja")
	for _, want := range []string{"⚠️", "4 回", "12分", "タグは最新の状態を表しています。", "https://kitsu.example/t/1"} {
		if !strings.Contains(signal.Content, want) {
			t.Errorf("content %q missing %q", signal.Content, want)
		}
	}
	if len(signal.AllowedUserIDs) != 0 || len(signal.AllowedRoleIDs) != 0 {
		t.Errorf("warning must not mention: %+v", signal)
	}
	if strings.Contains(signal.Content, "<@") {
		t.Errorf("warning content must not contain mentions: %q", signal.Content)
	}
}

func TestForumSignalMessageDoesNotSuppressEmbeds(t *testing.T) {
	// Regression: ForumSignalMessage used to send Flags: 4, Discord's
	// SUPPRESS_EMBEDS, which hid the whole signal (it lives in the embed)
	// and left publish traces completely blank.
	signal := RenderForumSignalCard(ForumCardInput{
		Intent:      "publish",
		TaskType:    "Compositing",
		EntityType:  "Shot",
		ParentName:  "sq01",
		EntityName:  "sh002",
		PublishLine: "📦 imageReference（image）3 が publish されました",
		StatusName:  "waiting for approval",
	})
	body := ForumSignalMessage(signal, "kitsusync-1")
	if body.Flags&4 != 0 {
		t.Errorf("flags = %d, SUPPRESS_EMBEDS (4) must not be set", body.Flags)
	}
	if len(body.Embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(body.Embeds))
	}
	if !strings.Contains(body.Embeds[0].Description, "📦 imageReference（image）3 が publish されました") {
		t.Errorf("publish line missing from embed description: %q", body.Embeds[0].Description)
	}
}

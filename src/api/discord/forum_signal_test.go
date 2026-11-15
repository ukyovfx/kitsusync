package discord

import (
	"strings"
	"testing"
	"time"
)

func TestRenderForumSignalCommentCarriesQuote(t *testing.T) {
	const url = "https://kitsu.example/productions/1/shots/tasks/2"
	signal := RenderForumSignal("comment", url, "ここを直してください", []string{"111111111111111111"}, nil)
	for _, want := range []string{"<@111111111111111111>", "コメントがあります。", "> ここを直してください", url} {
		if !strings.Contains(signal.Content, want) {
			t.Errorf("content %q missing %q", signal.Content, want)
		}
	}
	if strings.Contains(signal.Content, "@everyone") || strings.Contains(signal.Content, "@here") {
		t.Errorf("content must never mention everyone or here: %q", signal.Content)
	}
}

func TestRenderForumSignalCommentWithoutQuote(t *testing.T) {
	signal := RenderForumSignal("comment", "https://kitsu.example/t/1", "", nil, nil)
	if strings.Contains(signal.Content, ">") {
		t.Errorf("content must not quote an empty comment: %q", signal.Content)
	}
	if !strings.HasPrefix(signal.Content, "コメントがあります。") {
		t.Errorf("content = %q", signal.Content)
	}
}

func TestRenderForumSignalIgnoresQuoteForOtherIntents(t *testing.T) {
	signal := RenderForumSignal("done", "https://kitsu.example/t/1", "余計な引用", nil, nil)
	if strings.Contains(signal.Content, "余計な引用") {
		t.Errorf("only retake and comment quote: %q", signal.Content)
	}
}

func TestForumFlapWarningHasNoMentions(t *testing.T) {
	signal := ForumFlapWarning("https://kitsu.example/t/1", 4, 12*time.Minute)
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

func TestForumSpanText(t *testing.T) {
	cases := []struct {
		span time.Duration
		want string
	}{
		{30 * time.Second, "1分以内"},
		{12 * time.Minute, "12分"},
		{2 * time.Hour, "2時間"},
	}
	for _, tc := range cases {
		if got := forumSpanText(tc.span); got != tc.want {
			t.Errorf("forumSpanText(%v) = %q, want %q", tc.span, got, tc.want)
		}
	}
}

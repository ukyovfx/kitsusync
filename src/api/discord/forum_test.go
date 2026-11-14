package discord

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func withForumAPI(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	previous := SetDiscordBotAPIBase(server.URL)
	t.Cleanup(func() {
		SetDiscordBotAPIBase(previous)
		server.Close()
	})
	return server
}

func TestCreateForumThreadSendsCardWithEmptyContent(t *testing.T) {
	var got map[string]any
	withForumAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/channels/forum-1/threads" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bot token-1" {
			t.Errorf("authorization = %q", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"thread-1","name":"Compositing sq010 sh002","parent_id":"forum-1","applied_tags":["tag-wfa"]}`))
	})

	card := Payload{Content: "should be dropped", Embeds: []Embed{{Description: "card"}}}
	thread, err := CreateForumThread("token-1", "forum-1", ForumThreadCreate{
		Name:                "Compositing sq010 sh002",
		AutoArchiveDuration: ForumArchiveMinutes("wfa"),
		AppliedTags:         []string{"tag-wfa"},
		Message:             ForumFirstMessage(card),
	})
	if err != nil {
		t.Fatalf("CreateForumThread: %v", err)
	}
	if thread.ID != "thread-1" {
		t.Fatalf("thread id = %q", thread.ID)
	}
	if got["name"] != "Compositing sq010 sh002" {
		t.Errorf("name = %v", got["name"])
	}
	if got["auto_archive_duration"] != float64(10080) {
		t.Errorf("auto_archive_duration = %v", got["auto_archive_duration"])
	}
	message, ok := got["message"].(map[string]any)
	if !ok {
		t.Fatalf("message missing: %v", got)
	}
	if message["content"] != "" {
		t.Errorf("first message content = %v, want empty", message["content"])
	}
	if _, has := message["embeds"]; !has {
		t.Errorf("first message has no embeds: %v", message)
	}
}

func TestPatchForumThreadSendsOneChangePerCall(t *testing.T) {
	var bodies []map[string]any
	withForumAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusNoContent)
	})

	if err := PatchForumThread("token-1", "thread-1", ForumUnarchivePatch()); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if err := PatchForumThread("token-1", "thread-1", ForumSyncPatch(ForumSyncPlan{
		ArchiveMinutes: 4320,
		AppliedTags:    []string{"tag-done"},
	})); err != nil {
		t.Fatalf("retag: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("patch calls = %d, want 2", len(bodies))
	}
	if bodies[0]["archived"] != false {
		t.Errorf("first patch = %v, want archived:false", bodies[0])
	}
	if _, has := bodies[0]["applied_tags"]; has {
		t.Errorf("unarchive patch must not carry tags: %v", bodies[0])
	}
	if bodies[1]["auto_archive_duration"] != float64(4320) {
		t.Errorf("retag patch duration = %v", bodies[1]["auto_archive_duration"])
	}
	tags, _ := bodies[1]["applied_tags"].([]any)
	if len(tags) != 1 || tags[0] != "tag-done" {
		t.Errorf("retag patch tags = %v", bodies[1]["applied_tags"])
	}
}

func TestForumRequestClassifiesRateLimit(t *testing.T) {
	withForumAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2.5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":0,"message":"You are being rate limited.","retry_after":2.5}`))
	})

	_, err := CreateForumThread("token-1", "forum-1", ForumThreadCreate{Name: "x"})
	var apiErr *ForumAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want ForumAPIError", err)
	}
	if apiErr.ErrorClass() != "rate_limited" {
		t.Errorf("class = %q", apiErr.ErrorClass())
	}
	if apiErr.RetryAfterDuration() != 2500*time.Millisecond {
		t.Errorf("retry after = %v", apiErr.RetryAfterDuration())
	}
}

func TestForumRequestClassifiesPermissionAndMissing(t *testing.T) {
	cases := []struct {
		status int
		body   string
		class  string
	}{
		{http.StatusForbidden, `{"code":50001,"message":"Missing Access"}`, "missing_permission"},
		{http.StatusNotFound, `{"code":10003,"message":"Unknown Channel"}`, "missing_destination"},
		{http.StatusBadRequest, `{"code":50035,"message":"Invalid Form Body"}`, "invalid_request"},
		{http.StatusInternalServerError, `{}`, "discord_server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.class, func(t *testing.T) {
			withForumAPI(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := GetForumThread("token-1", "thread-1")
			var apiErr *ForumAPIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want ForumAPIError", err)
			}
			if apiErr.ErrorClass() != tc.class {
				t.Errorf("class = %q, want %q", apiErr.ErrorClass(), tc.class)
			}
		})
	}
}

func TestForumRequestRejectsMissingToken(t *testing.T) {
	_, err := GetForumThread("  ", "thread-1")
	if err == nil {
		t.Fatal("expected an error for a missing bot token")
	}
}

func TestPostForumMessageCarriesNonceAndSuppressedMentions(t *testing.T) {
	var got map[string]any
	withForumAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels/thread-1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"msg-1","channel_id":"thread-1"}`))
	})

	signal := RenderForumSignal("wfa", "https://kitsu/productions/1/shots/tasks/2", "", []string{"111111111111111111"}, nil)
	msg, err := PostForumMessage("token-1", "thread-1", ForumSignalMessage(signal, ForumNonce("task-1", 7)))
	if err != nil {
		t.Fatalf("PostForumMessage: %v", err)
	}
	if msg.ID != "msg-1" {
		t.Errorf("message id = %q", msg.ID)
	}
	if got["nonce"] != "kitsusync-task-1-7" {
		t.Errorf("nonce = %v", got["nonce"])
	}
	if got["enforce_nonce"] != true {
		t.Errorf("enforce_nonce = %v", got["enforce_nonce"])
	}
	if got["flags"] != float64(4) {
		t.Errorf("flags = %v", got["flags"])
	}
	mentions, _ := got["allowed_mentions"].(map[string]any)
	users, _ := mentions["users"].([]any)
	if len(users) != 1 || users[0] != "111111111111111111" {
		t.Errorf("allowed users = %v", mentions["users"])
	}
	parse, _ := mentions["parse"].([]any)
	if len(parse) != 0 {
		t.Errorf("parse = %v, want empty", mentions["parse"])
	}
}

func TestRenderForumSignalTemplates(t *testing.T) {
	const url = "https://kitsu.example/productions/1/shots/tasks/2"
	cases := []struct {
		intent string
		quote  string
		want   []string
	}{
		{"wfa", "", []string{"<@111111111111111111>", "チェックをお願いします。", url}},
		{"retake", "直してください", []string{"リテイクです。", "> 直してください", url}},
		{"done", "", []string{"完了しました。", url}},
		{"assign", "", []string{"このタスクの担当になりました。", url}},
	}
	for _, tc := range cases {
		t.Run(tc.intent, func(t *testing.T) {
			signal := RenderForumSignal(tc.intent, url, tc.quote, []string{"111111111111111111"}, nil)
			for _, want := range tc.want {
				if !strings.Contains(signal.Content, want) {
					t.Errorf("content %q missing %q", signal.Content, want)
				}
			}
			if strings.Contains(signal.Content, "@everyone") {
				t.Errorf("content must never mention everyone: %q", signal.Content)
			}
		})
	}
}

func TestRenderForumSignalWithoutRecipientsKeepsURL(t *testing.T) {
	signal := RenderForumSignal("done", "https://kitsu.example/t/1", "", nil, nil)
	if signal.Content != "完了しました。\nhttps://kitsu.example/t/1" {
		t.Errorf("content = %q", signal.Content)
	}
	if len(signal.AllowedUserIDs) != 0 || len(signal.AllowedRoleIDs) != 0 {
		t.Errorf("mentions must be empty: %v %v", signal.AllowedUserIDs, signal.AllowedRoleIDs)
	}
}

func TestPlanForumSyncUnarchivesAndRetagsSeparately(t *testing.T) {
	archived := &ForumThread{
		AppliedTags: []string{"tag-wfa", "human-tag"},
		ThreadMetadata: &struct {
			Archived            bool `json:"archived"`
			AutoArchiveDuration int  `json:"auto_archive_duration"`
		}{Archived: true, AutoArchiveDuration: 10080},
	}
	plan := PlanForumSync(*archived, []string{"tag-wfa", "tag-retake", "tag-done"}, "tag-done", "done", nil)
	if !plan.NeedUnarchive {
		t.Error("archived thread must be unarchived first")
	}
	if !plan.NeedRetag {
		t.Fatal("role change must retag")
	}
	if plan.ArchiveMinutes != 4320 {
		t.Errorf("done archive minutes = %d", plan.ArchiveMinutes)
	}
	if !sameStringSet(plan.AppliedTags, []string{"human-tag", "tag-done"}) {
		t.Errorf("applied tags = %v, want human tag kept and role tag set", plan.AppliedTags)
	}
}

func TestPlanForumSyncNoOpWhenAlreadyCorrect(t *testing.T) {
	current := ForumThread{
		AppliedTags: []string{"tag-wfa"},
		ThreadMetadata: &struct {
			Archived            bool `json:"archived"`
			AutoArchiveDuration int  `json:"auto_archive_duration"`
		}{Archived: false, AutoArchiveDuration: 10080},
	}
	plan := PlanForumSync(current, []string{"tag-wfa", "tag-done"}, "tag-wfa", "wfa", nil)
	if plan.NeedUnarchive || plan.NeedRetag {
		t.Fatalf("expected a no-op plan, got %+v", plan)
	}
}

func TestPlanForumSyncKeepsRoleTagAtFiveTagLimit(t *testing.T) {
	current := ForumThread{
		AppliedTags: []string{"a", "b", "c", "d", "e"},
		ThreadMetadata: &struct {
			Archived            bool `json:"archived"`
			AutoArchiveDuration int  `json:"auto_archive_duration"`
		}{Archived: false, AutoArchiveDuration: 10080},
	}
	plan := PlanForumSync(current, nil, "role-tag", "wfa", nil)
	if len(plan.AppliedTags) > 5 {
		t.Fatalf("applied tags = %d, Discord allows 5", len(plan.AppliedTags))
	}
	if !containsString(plan.AppliedTags, "role-tag") {
		t.Errorf("role tag dropped: %v", plan.AppliedTags)
	}
}

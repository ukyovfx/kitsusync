package forum

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"app/src/api/discord"
	"app/src/model"
)

type recordedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

type fakeDiscord struct {
	mu       sync.Mutex
	requests []recordedRequest
	// respond lets a test override the reply for a path.
	respond func(r *http.Request) (int, string)
}

func (f *fakeDiscord) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Body: body})
		respond := f.respond
		f.mu.Unlock()
		if respond != nil {
			status, payload := respond(r)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(payload))
			return
		}
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/threads"):
			_, _ = w.Write([]byte(`{"id":"thread-1","parent_id":"forum-1"}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"thread-1","applied_tags":[],"thread_metadata":{"archived":false,"auto_archive_duration":10080}}`))
		case r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"id":"msg-1","channel_id":"thread-1"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":10003,"message":"Unknown Channel"}`))
		}
	}
}

func (f *fakeDiscord) snapshot() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

func newExecutor(t *testing.T, db *gorm.DB) (Executor, *fakeDiscord) {
	t.Helper()
	fake := &fakeDiscord{}
	server := httptest.NewServer(fake.handler(t))
	previous := discord.SetDiscordBotAPIBase(server.URL)
	t.Cleanup(func() {
		discord.SetDiscordBotAPIBase(previous)
		server.Close()
	})
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	return Executor{
		DB:       db,
		BotToken: func() string { return "token-1" },
		GuildID:  func() string { return "guild-1" },
		TaskURL:  func(taskID string) string { return "https://kitsu.example/tasks/" + taskID },
		Recipients: func(taskID string, payload model.ForumPostMessagePayload) ([]string, []string) {
			return []string{"111111111111111111"}, nil
		},
		Now:   func() time.Time { return now },
		Sleep: func(time.Duration) {},
	}, fake
}

func seedForumSettings(t *testing.T, db *gorm.DB) {
	t.Helper()
	settings := testSettings()
	if err := db.Create(&settings.Config).Error; err != nil {
		t.Fatalf("create config: %v", err)
	}
	for i := range settings.Routes {
		if err := db.Create(&settings.Routes[i]).Error; err != nil {
			t.Fatalf("create route: %v", err)
		}
	}
	for i := range settings.StatusRoles {
		if err := db.Create(&settings.StatusRoles[i]).Error; err != nil {
			t.Fatalf("create status role: %v", err)
		}
	}
	for i := range settings.Tags {
		if err := db.Create(&settings.Tags[i]).Error; err != nil {
			t.Fatalf("create tag: %v", err)
		}
	}
}

func seedForumTask(t *testing.T, db *gorm.DB, threadID, statusID string) {
	t.Helper()
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	row := model.ForumTask{
		TaskID:      "task-1",
		ProjectID:   "prod-1",
		TaskTypeID:  "tt-comp",
		EntityID:    "entity-1",
		EntityKind:  model.ForumEntityKindShot,
		Title:       "Compositing sq010 sh002",
		StatusID:    statusID,
		StatusSince: now,
		ForumID:     "forum-1",
		ThreadID:    threadID,
		FirstSeenAt: now,
		LastSeenAt:  now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create forum task: %v", err)
	}
}

func enqueue(t *testing.T, db *gorm.DB, row model.ForumOutbox) model.ForumOutbox {
	t.Helper()
	id, err := model.EnqueueForumOutbox(db, row)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	loaded, err := loadOutbox(db, id)
	if err != nil {
		t.Fatalf("load outbox: %v", err)
	}
	return loaded
}

func loadOutbox(db *gorm.DB, id uint) (model.ForumOutbox, error) {
	var row model.ForumOutbox
	err := db.Where("id = ?", id).First(&row).Error
	return row, err
}

func TestExecutorCreatesThreadAndStoresThreadID(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "", "s-wfa")
	executor, fake := newExecutor(t, db)

	payload, _ := json.Marshal(model.ForumCreateThreadPayload{
		ForumID:        "forum-1",
		Name:           "Compositing sq010 sh002",
		Role:           model.ForumRoleWFA,
		ArchiveMinutes: model.ForumArchiveOtherMinutes,
		Card:           json.RawMessage(`{"embeds":[{"description":"card"}]}`),
	})
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:    "task-1",
		Kind:      model.ForumOutboxCreateThread,
		DedupeKey: model.ForumCreateThreadDedupeKey("task-1", 0),
		Payload:   string(payload),
		State:     model.ForumOutboxPending,
		NotBefore: executor.Now(),
		CreatedAt: executor.Now(),
	})

	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	task, err := model.FindForumTaskWithError(db, "task-1")
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if task.ThreadID != "thread-1" {
		t.Errorf("thread id = %q, want it stored immediately", task.ThreadID)
	}
	done, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if done.State != model.ForumOutboxDone {
		t.Errorf("row state = %q", done.State)
	}
	requests := fake.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if requests[0].Path != "/channels/forum-1/threads" {
		t.Errorf("path = %q", requests[0].Path)
	}
	tags, _ := requests[0].Body["applied_tags"].([]any)
	if len(tags) != 1 || tags[0] != "tag-wfa" {
		t.Errorf("applied tags = %v", requests[0].Body["applied_tags"])
	}
}

func TestExecutorSyncsUnarchiveThenRetag(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-done")
	executor, fake := newExecutor(t, db)
	fake.respond = func(r *http.Request) (int, string) {
		switch r.Method {
		case http.MethodGet:
			return http.StatusOK, `{"id":"thread-1","applied_tags":["tag-wfa"],"thread_metadata":{"archived":true,"auto_archive_duration":10080}}`
		case http.MethodPatch:
			return http.StatusNoContent, ""
		}
		return http.StatusNotFound, `{}`
	}

	enqueue(t, db, model.ForumOutbox{
		TaskID:      "task-1",
		Kind:        model.ForumOutboxSyncThread,
		DedupeKey:   model.ForumSyncThreadCoalesceKey("task-1") + ":1",
		CoalesceKey: model.ForumSyncThreadCoalesceKey("task-1"),
		Payload:     "{}",
		State:       model.ForumOutboxPending,
		NotBefore:   executor.Now(),
		CreatedAt:   executor.Now(),
	})

	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	var patches []recordedRequest
	for _, request := range fake.snapshot() {
		if request.Method == http.MethodPatch {
			patches = append(patches, request)
		}
	}
	if len(patches) != 2 {
		t.Fatalf("patch calls = %d, want unarchive then retag", len(patches))
	}
	if patches[0].Body["archived"] != false {
		t.Errorf("first patch = %v, want archived:false", patches[0].Body)
	}
	if _, has := patches[0].Body["applied_tags"]; has {
		t.Errorf("unarchive patch must not carry tags: %v", patches[0].Body)
	}
	tags, _ := patches[1].Body["applied_tags"].([]any)
	if len(tags) != 1 || tags[0] != "tag-done" {
		t.Errorf("retag patch tags = %v", patches[1].Body["applied_tags"])
	}
	if patches[1].Body["auto_archive_duration"] != float64(model.ForumArchiveDoneMinutes) {
		t.Errorf("done archive duration = %v", patches[1].Body["auto_archive_duration"])
	}
}

func TestExecutorPostsSignalWithNonce(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, fake := newExecutor(t, db)

	payload, _ := json.Marshal(model.ForumPostMessagePayload{
		Intent:         model.ForumIntentStatusWFA,
		WindowOpenedAt: executor.Now(),
	})
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:      "task-1",
		Kind:        model.ForumOutboxPostMessage,
		DedupeKey:   model.ForumPostMessageCoalesceKey(model.ForumIntentStatusWFA, "task-1") + ":1",
		CoalesceKey: model.ForumPostMessageCoalesceKey(model.ForumIntentStatusWFA, "task-1"),
		Payload:     string(payload),
		State:       model.ForumOutboxPending,
		NotBefore:   executor.Now(),
		CreatedAt:   executor.Now(),
	})

	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	requests := fake.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if requests[0].Path != "/channels/thread-1/messages" {
		t.Errorf("path = %q", requests[0].Path)
	}
	if requests[0].Body["nonce"] != discord.ForumNonce("task-1", row.ID) {
		t.Errorf("nonce = %v", requests[0].Body["nonce"])
	}
	if requests[0].Body["enforce_nonce"] != true {
		t.Errorf("enforce_nonce = %v", requests[0].Body["enforce_nonce"])
	}
	content, _ := requests[0].Body["content"].(string)
	if !strings.Contains(content, "チェックをお願いします。") {
		t.Errorf("content = %q", content)
	}
	if !strings.Contains(content, "https://kitsu.example/tasks/task-1") {
		t.Errorf("content missing the Kitsu link: %q", content)
	}
	done, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if done.State != model.ForumOutboxDone {
		t.Errorf("state = %q", done.State)
	}
}

func TestExecutorPostWaitsForPendingSync(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-retake")
	executor, _ := newExecutor(t, db)

	enqueue(t, db, model.ForumOutbox{
		TaskID:      "task-1",
		Kind:        model.ForumOutboxSyncThread,
		DedupeKey:   model.ForumSyncThreadCoalesceKey("task-1") + ":1",
		CoalesceKey: model.ForumSyncThreadCoalesceKey("task-1"),
		Payload:     "{}",
		State:       model.ForumOutboxPending,
		NotBefore:   executor.Now().Add(time.Hour),
		CreatedAt:   executor.Now(),
	})
	signal := enqueue(t, db, model.ForumOutbox{
		TaskID:      "task-1",
		Kind:        model.ForumOutboxPostMessage,
		DedupeKey:   model.ForumPostMessageCoalesceKey(model.ForumIntentStatusRetake, "task-1") + ":1",
		CoalesceKey: model.ForumPostMessageCoalesceKey(model.ForumIntentStatusRetake, "task-1"),
		Payload:     `{"intent":"retake"}`,
		State:       model.ForumOutboxPending,
		NotBefore:   executor.Now(),
		CreatedAt:   executor.Now(),
	})

	processed, err := executor.Drain(10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if processed != 0 {
		t.Fatalf("processed = %d, want the signal blocked by the pending sync", processed)
	}
	row, err := loadOutbox(db, signal.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.State != model.ForumOutboxPending {
		t.Errorf("state = %q, want pending", row.State)
	}
}

func TestExecutorRetriesRateLimitWithoutConsumingAttempts(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, fake := newExecutor(t, db)
	fake.respond = func(r *http.Request) (int, string) {
		return http.StatusTooManyRequests, `{"retry_after":12,"message":"rate limited"}`
	}

	payload, _ := json.Marshal(model.ForumPostMessagePayload{Intent: model.ForumIntentStatusWFA})
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:    "task-1",
		Kind:      model.ForumOutboxPostMessage,
		DedupeKey: "post:1",
		Payload:   string(payload),
		State:     model.ForumOutboxPending,
		NotBefore: executor.Now(),
		CreatedAt: executor.Now(),
	})

	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	got, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if got.State != model.ForumOutboxPending {
		t.Fatalf("state = %q, want pending after a 429", got.State)
	}
	if got.Attempts != 0 {
		t.Errorf("attempts = %d, a rate limit must not count as an attempt", got.Attempts)
	}
	if !got.NextAttemptAt.Equal(executor.Now().Add(12 * time.Second)) {
		t.Errorf("next attempt = %v", got.NextAttemptAt)
	}
}

func TestExecutorFailsOnPermanentError(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, fake := newExecutor(t, db)
	fake.respond = func(r *http.Request) (int, string) {
		return http.StatusBadRequest, `{"code":50035,"message":"Invalid Form Body"}`
	}

	payload, _ := json.Marshal(model.ForumPostMessagePayload{Intent: model.ForumIntentStatusWFA})
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:    "task-1",
		Kind:      model.ForumOutboxPostMessage,
		DedupeKey: "post:2",
		Payload:   string(payload),
		State:     model.ForumOutboxPending,
		NotBefore: executor.Now(),
		CreatedAt: executor.Now(),
	})

	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	got, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if got.State != model.ForumOutboxFailed {
		t.Errorf("state = %q, want failed", got.State)
	}
	if got.LastErrorClass != "invalid_request" {
		t.Errorf("error class = %q", got.LastErrorClass)
	}
}

func TestExecutorRecreatesThreadOnMissingDestination(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, fake := newExecutor(t, db)
	fake.respond = func(r *http.Request) (int, string) {
		return http.StatusNotFound, `{"code":10003,"message":"Unknown Channel"}`
	}

	payload, _ := json.Marshal(model.ForumPostMessagePayload{Intent: model.ForumIntentStatusWFA})
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:    "task-1",
		Kind:      model.ForumOutboxPostMessage,
		DedupeKey: "post:3",
		Payload:   string(payload),
		State:     model.ForumOutboxPending,
		NotBefore: executor.Now(),
		CreatedAt: executor.Now(),
	})

	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	task, err := model.FindForumTaskWithError(db, "task-1")
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if task.ThreadID != "" {
		t.Errorf("thread id = %q, want cleared so a new post is created", task.ThreadID)
	}
	if task.ThreadGeneration != 1 {
		t.Errorf("thread generation = %d, want 1", task.ThreadGeneration)
	}
	got, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if got.State != model.ForumOutboxCancelled {
		t.Errorf("state = %q, want cancelled", got.State)
	}
}

func TestExecutorRequeuesWithoutToken(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, fake := newExecutor(t, db)
	executor.BotToken = func() string { return "" }

	payload, _ := json.Marshal(model.ForumPostMessagePayload{Intent: model.ForumIntentStatusWFA})
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:    "task-1",
		Kind:      model.ForumOutboxPostMessage,
		DedupeKey: "post:4",
		Payload:   string(payload),
		State:     model.ForumOutboxPending,
		NotBefore: executor.Now(),
		CreatedAt: executor.Now(),
	})

	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if requests := fake.snapshot(); len(requests) != 0 {
		t.Fatalf("requests = %d, want none without a token", len(requests))
	}
	got, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if got.State != model.ForumOutboxPending {
		t.Errorf("state = %q", got.State)
	}
	if got.LastErrorClass != "missing_bot_token" {
		t.Errorf("error class = %q", got.LastErrorClass)
	}
}

func TestReconcileSendingRowsRequeuesClaimedRows(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:    "task-1",
		Kind:      model.ForumOutboxPostMessage,
		DedupeKey: "post:5",
		Payload:   `{"intent":"wfa"}`,
		State:     model.ForumOutboxPending,
		NotBefore: now,
		CreatedAt: now,
	})
	if _, err := model.ClaimNextForumOutboxRow(db, now); err != nil {
		t.Fatalf("claim: %v", err)
	}

	if err := ReconcileSendingRows(db); err != nil {
		t.Fatalf("ReconcileSendingRows: %v", err)
	}
	got, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if got.State != model.ForumOutboxPending {
		t.Errorf("state = %q, want pending", got.State)
	}
	if got.Note != "reconciled_after_restart" {
		t.Errorf("note = %q", got.Note)
	}
}

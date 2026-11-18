package forum

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"

	"app/src/model"
)

func auditLogs(t *testing.T, db *gorm.DB) []model.AuditLog {
	t.Helper()
	var logs []model.AuditLog
	if err := db.Order("id").Find(&logs).Error; err != nil {
		t.Fatalf("load audit logs: %v", err)
	}
	return logs
}

func TestDrainCreateThreadWritesAuditSuccess(t *testing.T) {
	db := newForumDB(t)
	if err := db.AutoMigrate(&model.AuditLog{}, &model.Project{}); err != nil {
		t.Fatalf("migrate audit tables: %v", err)
	}
	seedForumSettings(t, db)
	seedForumTask(t, db, "", "s-wfa")
	if err := db.Create(&model.Project{Name: "Prod One", KitsuProjectID: "prod-1", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	executor, _ := newExecutor(t, db)

	payload, _ := json.Marshal(model.ForumCreateThreadPayload{
		ForumID: "forum-1",
		Name:    "Compositing sq010 sh002",
	})
	enqueue(t, db, model.ForumOutbox{
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
	logs := auditLogs(t, db)
	if len(logs) != 1 {
		t.Fatalf("audit logs = %d, want 1", len(logs))
	}
	got := logs[0]
	if !got.Success {
		t.Errorf("Success = false, want true (error %q)", got.ErrorMessage)
	}
	if got.TaskID != "task-1" {
		t.Errorf("TaskID = %q", got.TaskID)
	}
	if got.ProjectID != "prod-1" {
		t.Errorf("ProjectID = %q", got.ProjectID)
	}
	if got.ProjectName != "Prod One" {
		t.Errorf("ProjectName = %q", got.ProjectName)
	}
	if got.GuildID != "guild-1" {
		t.Errorf("GuildID = %q", got.GuildID)
	}
	if got.EntityName != "Compositing sq010 sh002" {
		t.Errorf("EntityName = %q", got.EntityName)
	}
	if got.TaskType != "Compositing" {
		t.Errorf("TaskType = %q", got.TaskType)
	}
	if got.DiscordMsgID != "thread-1" {
		t.Errorf("DiscordMsgID = %q", got.DiscordMsgID)
	}
	if got.NewStatus != "thread=thread-1" {
		t.Errorf("NewStatus = %q", got.NewStatus)
	}
	if got.ActorKind != model.AuditActorSystem || got.ActorName != "forum-outbox" {
		t.Errorf("actor = %q/%q", got.ActorKind, got.ActorName)
	}
	if got.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0", got.RetryCount)
	}
}

func TestDrainPostMessageWritesAuditSuccess(t *testing.T) {
	db := newForumDB(t)
	if err := db.AutoMigrate(&model.AuditLog{}); err != nil {
		t.Fatalf("migrate audit table: %v", err)
	}
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, _ := newExecutor(t, db)

	payload, _ := json.Marshal(model.ForumPostMessagePayload{
		Intent:         model.ForumIntentStatusWFA,
		WindowOpenedAt: executor.Now(),
	})
	enqueue(t, db, model.ForumOutbox{
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
	logs := auditLogs(t, db)
	if len(logs) != 1 {
		t.Fatalf("audit logs = %d, want 1", len(logs))
	}
	got := logs[0]
	if !got.Success {
		t.Errorf("Success = false, want true (error %q)", got.ErrorMessage)
	}
	if got.DiscordMsgID != "msg-1" {
		t.Errorf("DiscordMsgID = %q", got.DiscordMsgID)
	}
	if got.TaskType != "Compositing" {
		t.Errorf("TaskType = %q", got.TaskType)
	}
}

func TestDrainFailureLeavesUnsuccessfulAuditRow(t *testing.T) {
	db := newForumDB(t)
	if err := db.AutoMigrate(&model.AuditLog{}); err != nil {
		t.Fatalf("migrate audit table: %v", err)
	}
	seedForumSettings(t, db)
	seedForumTask(t, db, "", "s-wfa")
	executor, fake := newExecutor(t, db)
	fake.respond = func(r *http.Request) (int, string) {
		return http.StatusNotFound, `{"code":10003,"message":"Unknown Channel"}`
	}

	payload, _ := json.Marshal(model.ForumCreateThreadPayload{
		ForumID: "forum-1",
		Name:    "Compositing sq010 sh002",
	})
	enqueue(t, db, model.ForumOutbox{
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
	logs := auditLogs(t, db)
	if len(logs) != 1 {
		t.Fatalf("audit logs = %d, want 1", len(logs))
	}
	got := logs[0]
	if got.Success {
		t.Errorf("Success = true, want false")
	}
	if got.ErrorMessage == "" {
		t.Errorf("ErrorMessage = %q, want a failure summary", got.ErrorMessage)
	}
	if got.DiscordMsgID != "" {
		t.Errorf("DiscordMsgID = %q, want empty", got.DiscordMsgID)
	}
}

func TestDrainRetriesConvergeOnSingleAuditRow(t *testing.T) {
	db := newForumDB(t)
	if err := db.AutoMigrate(&model.AuditLog{}); err != nil {
		t.Fatalf("migrate audit table: %v", err)
	}
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, fake := newExecutor(t, db)

	// First POST /messages is rate limited; the retry must converge to one row.
	var messagePosts int
	fake.respond = func(r *http.Request) (int, string) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages") {
			messagePosts++
			if messagePosts == 1 {
				return http.StatusTooManyRequests, `{"retry_after":12,"message":"rate limited"}`
			}
			return http.StatusOK, `{"id":"msg-1","channel_id":"thread-1"}`
		}
		switch {
		case r.Method == http.MethodGet:
			return http.StatusOK, `{"id":"thread-1","applied_tags":[],"thread_metadata":{"archived":false,"auto_archive_duration":10080}}`
		case r.Method == http.MethodPatch:
			return http.StatusNoContent, ""
		default:
			return http.StatusNotFound, `{"code":10003,"message":"Unknown Channel"}`
		}
	}

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

	if _, err := executor.Drain(10); !errors.Is(err, ErrForumRateLimited) {
		t.Fatalf("first Drain err = %v, want ErrForumRateLimited", err)
	}
	if logs := auditLogs(t, db); len(logs) != 0 {
		t.Fatalf("audit logs after retryable failure = %d, want 0", len(logs))
	}
	got, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if got.State != model.ForumOutboxPending {
		t.Fatalf("state = %q, want pending after a 429", got.State)
	}

	if err := db.Model(&model.ForumOutbox{}).Where("id = ?", row.ID).
		Update("next_attempt_at", executor.Now()).Error; err != nil {
		t.Fatalf("reset next_attempt_at: %v", err)
	}
	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("second Drain: %v", err)
	}

	logs := auditLogs(t, db)
	if len(logs) != 1 {
		t.Fatalf("audit logs after retry = %d, want exactly 1", len(logs))
	}
	got2 := logs[0]
	if !got2.Success {
		t.Errorf("Success = false, want true (error %q)", got2.ErrorMessage)
	}
	if got2.DiscordMsgID != "msg-1" {
		t.Errorf("DiscordMsgID = %q", got2.DiscordMsgID)
	}
	if got2.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0 (429 releases do not consume attempts)", got2.RetryCount)
	}
}

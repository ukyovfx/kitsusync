package forum

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"app/src/model"
)

// sendCounter records what the executor reported to the runtime stats hook.
type sendCounter struct {
	sent   int64
	failed int64
}

func (c *sendCounter) record(sentCount, failedCount int, _, _ string) {
	c.sent += int64(sentCount)
	c.failed += int64(failedCount)
}

// TestExecutorRecordsOneSendPerFinalDelivery proves a post that retries a
// transient failure still counts exactly once, at the moment the send reaches
// its final result — never once per attempt.
func TestExecutorRecordsOneSendPerFinalDelivery(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, fake := newExecutor(t, db)
	attempts := 0
	fake.respond = func(r *http.Request) (int, string) {
		if r.Method == http.MethodPost {
			attempts++
			if attempts == 1 {
				return http.StatusInternalServerError, `{"message":"boom"}`
			}
			return http.StatusOK, `{"id":"msg-1","channel_id":"thread-1"}`
		}
		return http.StatusNotFound, `{"code":10003,"message":"Unknown Channel"}`
	}
	var counter sendCounter
	executor.RecordSend = counter.record
	now := executor.Now()
	executor.Now = func() time.Time { return now }

	payload, _ := json.Marshal(model.ForumPostMessagePayload{Intent: model.ForumIntentStatusWFA})
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:    "task-1",
		Kind:      model.ForumOutboxPostMessage,
		DedupeKey: "post:stats-delivery",
		Payload:   string(payload),
		State:     model.ForumOutboxPending,
		NotBefore: now,
		CreatedAt: now,
	})

	for i := 0; i < 5; i++ {
		if _, err := executor.Drain(10); err != nil {
			t.Fatalf("Drain: %v", err)
		}
		got, err := loadOutbox(db, row.ID)
		if err != nil {
			t.Fatalf("load row: %v", err)
		}
		if got.State == model.ForumOutboxDone {
			break
		}
		now = now.Add(6 * time.Minute)
	}
	got, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if got.State != model.ForumOutboxDone {
		t.Fatalf("state = %q, want done", got.State)
	}
	if attempts < 2 {
		t.Fatalf("discord POST attempts = %d, want a transient retry before delivery", attempts)
	}
	if counter.sent != 1 || counter.failed != 0 {
		t.Fatalf("RecordSend = (%d sent, %d failed), want (1, 0)", counter.sent, counter.failed)
	}
}

// TestExecutorRecordsSendFailureOnPermanentError proves a send that can never
// reach Discord counts exactly one failure and never a success.
func TestExecutorRecordsSendFailureOnPermanentError(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, fake := newExecutor(t, db)
	fake.respond = func(r *http.Request) (int, string) {
		return http.StatusBadRequest, `{"code":50035,"message":"Invalid Form Body"}`
	}
	var counter sendCounter
	executor.RecordSend = counter.record

	payload, _ := json.Marshal(model.ForumPostMessagePayload{Intent: model.ForumIntentStatusWFA})
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:    "task-1",
		Kind:      model.ForumOutboxPostMessage,
		DedupeKey: "post:stats-fail",
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
		t.Fatalf("state = %q, want failed", got.State)
	}
	if counter.sent != 0 || counter.failed != 1 {
		t.Fatalf("RecordSend = (%d sent, %d failed), want (0, 1)", counter.sent, counter.failed)
	}
}

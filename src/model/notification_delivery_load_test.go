package model

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestNotificationDeliveryFencedClaimRejectsStaleEvent covers the race the
// "ForEvent" claim exists for: a caller that already read an event and verified
// its destination must not dispatch once the durable intent changed underneath
// it (for example a known-rejected refresh replacing the payload or revision).
func TestNotificationDeliveryFencedClaimRejectsStaleEvent(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)

	event, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()

	payloadChanged := *event
	payloadChanged.PayloadSHA256 += "00"
	if _, ok, err := BeginNotificationDeliveryForEvent(db, payloadChanged, now); !errors.Is(err, ErrDeliveryUnavailable) || ok {
		t.Fatalf("a payload change must fence the claim, ok=%v err=%v", ok, err)
	}
	revisionChanged := *event
	revisionChanged.NotificationRevision = "revision-2"
	if _, ok, err := BeginNotificationDeliveryForEvent(db, revisionChanged, now); !errors.Is(err, ErrDeliveryUnavailable) || ok {
		t.Fatalf("a revision change must fence the claim, ok=%v err=%v", ok, err)
	}
	disposition, err := NotificationDeliveryDisposition(db, event.EventID)
	if err != nil || disposition != DeliveryPending {
		t.Fatalf("a fenced claim must not claim or mutate the event, got %q err=%v", disposition, err)
	}

	attempt, ok, err := BeginNotificationDeliveryForEvent(db, *event, now)
	if err != nil || !ok || attempt == nil {
		t.Fatalf("an unchanged event must still be claimable, ok=%v attempt=%v err=%v", ok, attempt, err)
	}
	disposition, err = NotificationDeliveryDisposition(db, event.EventID)
	if err != nil || disposition != DeliveryInFlight {
		t.Fatalf("a successful claim must be durable, got %q err=%v", disposition, err)
	}
}

// TestNotificationDeliveryCompletionIsAttemptBoundAndCheckpointsTask pins that
// completion is bound to the claimed attempt and that the task delivery
// checkpoint is created on first delivery and updated on the next one.
func TestNotificationDeliveryCompletionIsAttemptBoundAndCheckpointsTask(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	now := time.Now().UTC()

	event, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := BeginNotificationDelivery(db, event.EventID, now)
	if err != nil || !ok || attempt == nil {
		t.Fatalf("claim must succeed, ok=%v attempt=%v err=%v", ok, attempt, err)
	}

	if err := CompleteNotificationDelivery(db, DeliveryAttempt{ID: "wrong-attempt", Event: *event}, "msg-x", "thread-x", now); !errors.Is(err, ErrDeliveryAttemptConflict) {
		t.Fatalf("completion must be bound to the claimed attempt, got %v", err)
	}
	if disposition, err := NotificationDeliveryDisposition(db, event.EventID); err != nil || disposition != DeliveryInFlight {
		t.Fatalf("a rejected completion must leave the claim in flight, got %q err=%v", disposition, err)
	}

	if err := CompleteNotificationDelivery(db, *attempt, "msg-1", "thread-1", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var task Task
	if err := db.Where("task_id = ?", "task-1").First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.DiscordMessageID != "msg-1" || task.DiscordThreadID != "thread-1" || task.DeliveredTaskStatus != "WFA" {
		t.Fatalf("completion must checkpoint the task row, got %+v", task)
	}
	if err := CompleteNotificationDelivery(db, *attempt, "msg-2", "", now.Add(2*time.Second)); !errors.Is(err, ErrDeliveryAttemptConflict) {
		t.Fatalf("a repeated completion must not duplicate delivery, got %v", err)
	}

	nextIntent := notificationIntent()
	nextIntent.CommentID = "comment-2"
	next, err := PrepareNotificationDelivery(db, nextIntent)
	if err != nil {
		t.Fatal(err)
	}
	nextAttempt, ok, err := BeginNotificationDelivery(db, next.EventID, now)
	if err != nil || !ok {
		t.Fatalf("a delivered earlier event must not block the next one, ok=%v err=%v", ok, err)
	}
	if err := CompleteNotificationDelivery(db, *nextAttempt, "msg-3", "", now); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("task_id = ?", "task-1").First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.DiscordMessageID != "msg-3" {
		t.Fatalf("the second delivery must update the existing task row, got %q", task.DiscordMessageID)
	}
}

// TestNotificationDeliverySyntheticBurstDrainsHeadOfLine is a synthetic load
// regression: a burst of distinct observations for one task must not create
// duplicates, must preserve arrival order on the queue, must not let a newer
// event overtake an unresolved earlier one, and must drain fully in order.
func TestNotificationDeliverySyntheticBurstDrainsHeadOfLine(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	now := time.Now().UTC()

	const burst = 30
	ids := make([]string, 0, burst)
	for i := 0; i < burst; i++ {
		intent := notificationIntent()
		intent.CommentID = fmt.Sprintf("comment-%03d", i)
		event, err := PrepareNotificationDelivery(db, intent)
		if err != nil {
			t.Fatalf("burst prepare %d failed: %v", i, err)
		}
		ids = append(ids, event.EventID)
	}

	replayIntent := notificationIntent()
	replayIntent.CommentID = "comment-000"
	replay, err := PrepareNotificationDelivery(db, replayIntent)
	if err != nil || replay.EventID != ids[0] {
		t.Fatalf("a replayed observation must map to the original intent, got %q want %q err=%v", replay.EventID, ids[0], err)
	}
	var count int64
	if err := db.Model(&NotificationDeliveryEvent{}).Count(&count).Error; err != nil || count != burst {
		t.Fatalf("the burst must not create duplicate intents, count=%d err=%v", count, err)
	}

	pending, err := ListPendingNotificationDeliveries(db, 0)
	if err != nil || len(pending) != burst {
		t.Fatalf("burst queue size mismatch, rows=%d err=%v", len(pending), err)
	}
	for i := range pending {
		if pending[i].EventID != ids[i] {
			t.Fatalf("queue must preserve arrival order at %d, got %q want %q", i, pending[i].EventID, ids[i])
		}
	}

	if _, ok, err := BeginNotificationDelivery(db, ids[burst-1], now); !errors.Is(err, ErrDeliveryUnavailable) || ok {
		t.Fatalf("a newer event must not overtake an unresolved earlier one, ok=%v err=%v", ok, err)
	}

	for i := 0; i < burst; i++ {
		attempt, ok, err := BeginNotificationDelivery(db, ids[i], now)
		if err != nil || !ok {
			t.Fatalf("drain %d must be claimable in order, ok=%v err=%v", i, ok, err)
		}
		if err := CompleteNotificationDelivery(db, *attempt, fmt.Sprintf("msg-%03d", i), "", now); err != nil {
			t.Fatalf("drain %d completion failed: %v", i, err)
		}
	}
	var delivered int64
	if err := db.Model(&NotificationDeliveryEvent{}).Where("disposition = ?", DeliveryDelivered).Count(&delivered).Error; err != nil || delivered != burst {
		t.Fatalf("a drained burst must be fully delivered, delivered=%d err=%v", delivered, err)
	}
}

// TestNotificationDeliveryConcurrentPrepareIsExactlyOnce exercises the durable
// SQLite runtime under concurrent intake: many writers preparing distinct
// observations must produce exactly one row per observation and no errors.
func TestNotificationDeliveryConcurrentPrepareIsExactlyOnce(t *testing.T) {
	db := notificationDeliveryFileDB(t, filepath.Join(t.TempDir(), "load.db"))
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// Serialize writers at the pool level, mirroring the durable SQLite runtime.
	sqlDB.SetMaxOpenConns(1)
	notificationDeliveryFixture(t, db)

	const workers, perWorker = 6, 5
	var wg sync.WaitGroup
	failures := make(chan error, workers*perWorker)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				intent := notificationIntent()
				intent.CommentID = fmt.Sprintf("worker-%d-comment-%d", worker, i)
				if _, err := PrepareNotificationDelivery(db, intent); err != nil {
					failures <- fmt.Errorf("worker %d prepare %d: %w", worker, i, err)
				}
			}
		}(w)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}

	var count int64
	if err := db.Model(&NotificationDeliveryEvent{}).Count(&count).Error; err != nil || count != workers*perWorker {
		t.Fatalf("concurrent intake must be exactly-once, count=%d err=%v", count, err)
	}
	pending, err := ListPendingNotificationDeliveries(db, 0)
	if err != nil || len(pending) != workers*perWorker {
		t.Fatalf("every accepted intent must be queued, rows=%d err=%v", len(pending), err)
	}
}

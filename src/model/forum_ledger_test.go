package model

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newForumLedgerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_busy_timeout=5000", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&ForumBaseline{}, &ForumTask{}, &ForumTransition{}, &ForumOutbox{}); err != nil {
		t.Fatalf("failed to migrate schema: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func baselineRow(taskID string) ForumTask {
	return ForumTask{TaskID: taskID, Title: "Compositing sq010 " + taskID}
}

// Multiple tasks observed while forum mode is switched on have no thread yet.
// They must all be recorded; an empty thread id is not a unique value.
func TestRecordForumBaselineTasksStoresManyTasksWithoutThreads(t *testing.T) {
	db := newForumLedgerTestDB(t)
	if err := RecordForumBaselineTasks(db, "prod-1", "tt-comp", []ForumTask{
		baselineRow("task-1"), baselineRow("task-2"), baselineRow("task-3"),
	}); err != nil {
		t.Fatalf("RecordForumBaselineTasks: %v", err)
	}
	var count int64
	if err := db.Model(&ForumTask{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("baseline rows = %d, want 3", count)
	}
}

// A second baseline pass for the same task type must not fail either.
func TestRecordForumBaselineTasksIsRepeatable(t *testing.T) {
	db := newForumLedgerTestDB(t)
	rows := []ForumTask{baselineRow("task-1"), baselineRow("task-2")}
	if err := RecordForumBaselineTasks(db, "prod-1", "tt-comp", rows); err != nil {
		t.Fatalf("first baseline: %v", err)
	}
	if err := RecordForumBaselineTasks(db, "prod-1", "tt-comp", rows); err != nil {
		t.Fatalf("repeat baseline: %v", err)
	}
}

// A real thread id is still unique across tasks.
func TestForumTaskThreadIDStaysUnique(t *testing.T) {
	db := newForumLedgerTestDB(t)
	first := baselineRow("task-1")
	firstThread := "thread-1"
	first.ThreadID = &firstThread
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := baselineRow("task-2")
	second.ThreadID = &firstThread
	if err := db.Create(&second).Error; err == nil {
		t.Fatal("duplicate thread id was accepted")
	}
}

// A post queued before a sync must not wait for it: the sync waits for the post.
func TestClaimForumOutboxRunsEarlierPostBeforeLaterSync(t *testing.T) {
	db := newForumLedgerTestDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	postID, err := EnqueueForumOutbox(db, ForumOutbox{
		TaskID: "task-1", Kind: ForumOutboxPostMessage, DedupeKey: "msg:wfa:task-1",
		Payload: "{}", NotBefore: now,
	})
	if err != nil {
		t.Fatalf("enqueue post: %v", err)
	}
	if _, err := EnqueueForumOutbox(db, ForumOutbox{
		TaskID: "task-1", Kind: ForumOutboxSyncThread, DedupeKey: "sync:task-1:1",
		Payload: "{}", NotBefore: now,
	}); err != nil {
		t.Fatalf("enqueue sync: %v", err)
	}

	claimed, err := ClaimNextForumOutboxRow(db, now)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed == nil {
		t.Fatal("nothing was runnable: the post and the sync deadlock")
	}
	if claimed.ID != postID {
		t.Fatalf("claimed id = %d, want the earlier post %d", claimed.ID, postID)
	}
}

// A runnable row behind 50 blocked rows must still be claimed: the head of the
// queue must not hide later rows.
func TestClaimForumOutboxSkipsBlockedHeadOfLine(t *testing.T) {
	db := newForumLedgerTestDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	// Each blocked post waits for its task's create_thread, which is pending
	// but not due yet (retrying later), so nothing in front can be claimed.
	for i := 0; i < 50; i++ {
		task := fmt.Sprintf("task-%d", i)
		if _, err := EnqueueForumOutbox(db, ForumOutbox{
			TaskID: task, Kind: ForumOutboxCreateThread, DedupeKey: "create:" + task,
			Payload: "{}", NotBefore: now.Add(10 * time.Minute),
		}); err != nil {
			t.Fatalf("enqueue create %d: %v", i, err)
		}
		if _, err := EnqueueForumOutbox(db, ForumOutbox{
			TaskID: task, Kind: ForumOutboxPostMessage, DedupeKey: "msg:wfa:" + task,
			Payload: "{}", NotBefore: now,
		}); err != nil {
			t.Fatalf("enqueue post %d: %v", i, err)
		}
	}
	normalID, err := EnqueueForumOutbox(db, ForumOutbox{
		TaskID: "task-normal", Kind: ForumOutboxCreateThread, DedupeKey: "create:task-normal",
		Payload: "{}", NotBefore: now,
	})
	if err != nil {
		t.Fatalf("enqueue normal create: %v", err)
	}

	claimed, err := ClaimNextForumOutboxRow(db, now)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed == nil {
		t.Fatal("nothing was claimed: the blocked head of the line hid the normal row")
	}
	if claimed.ID != normalID {
		t.Fatalf("claimed id = %d, want the normal row %d", claimed.ID, normalID)
	}
}

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

// A thread that is lost a second time must clear again: the first clear
// bumps the generation, so the second clear must not collide on the unique
// index or fail with UNIQUE constraint failed: forum_tasks.thread_id.
func TestClearForumTaskThreadTwice(t *testing.T) {
	db := newForumLedgerTestDB(t)
	thread := "thread-1"
	row := baselineRow("task-1")
	row.ThreadID = &thread
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	first, err := ClearForumTaskThread(db, "task-1")
	if err != nil {
		t.Fatalf("first clear: %v", err)
	}
	if first != 1 {
		t.Fatalf("first generation = %d, want 1", first)
	}
	// The replacement thread arrives and is lost too.
	if err := db.Model(&ForumTask{}).Where("task_id = ?", "task-1").Update("thread_id", &thread).Error; err != nil {
		t.Fatalf("store replacement thread: %v", err)
	}
	second, err := ClearForumTaskThread(db, "task-1")
	if err != nil {
		t.Fatalf("second clear: %v", err)
	}
	if second != 2 {
		t.Fatalf("second generation = %d, want 2", second)
	}
	var stored ForumTask
	if err := db.Where("task_id = ?", "task-1").First(&stored).Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if stored.ThreadID != nil && *stored.ThreadID != "" {
		t.Fatalf("thread id = %q, want cleared again", *stored.ThreadID)
	}
	if stored.ThreadGeneration != 2 {
		t.Fatalf("generation = %d, want 2", stored.ThreadGeneration)
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

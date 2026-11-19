package model

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newForumOutboxRetentionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&ForumTask{}, &ForumOutbox{}); err != nil {
		t.Fatalf("failed to migrate schema: %v", err)
	}
	return db
}

func TestPurgeOldForumOutboxRemovesExpiredTerminalRows(t *testing.T) {
	db := newForumOutboxRetentionTestDB(t)
	now := time.Now()
	expired := now.AddDate(0, 0, -31).UTC()
	recent := now.Add(-time.Hour).UTC()

	rows := []ForumOutbox{
		{TaskID: "t1", Kind: ForumOutboxPostMessage, DedupeKey: "expired-done", State: ForumOutboxDone, FinishedAt: &expired, CreatedAt: expired},
		{TaskID: "t2", Kind: ForumOutboxPostMessage, DedupeKey: "expired-failed", State: ForumOutboxFailed, FinishedAt: &expired, CreatedAt: expired},
		{TaskID: "t3", Kind: ForumOutboxSyncThread, DedupeKey: "expired-cancelled", State: ForumOutboxCancelled, FinishedAt: &expired, CreatedAt: expired},
		{TaskID: "t4", Kind: ForumOutboxPostMessage, DedupeKey: "recent-done", State: ForumOutboxDone, FinishedAt: &recent, CreatedAt: recent},
		{TaskID: "t5", Kind: ForumOutboxCreateThread, DedupeKey: "stale-pending", State: ForumOutboxPending, CreatedAt: expired},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}

	deleted := PurgeOldForumOutbox(db, 30)
	if deleted != 3 {
		t.Fatalf("purged rows = %d, want 3", deleted)
	}

	var states map[string]int64
	var total int64
	if err := db.Model(&ForumOutbox{}).Count(&total).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 2 {
		t.Fatalf("remaining rows = %d, want 2", total)
	}
	var pending int64
	if err := db.Model(&ForumOutbox{}).Where("state = ?", ForumOutboxPending).Count(&pending).Error; err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending rows = %d, want 1", pending)
	}
	var recentDone int64
	if err := db.Model(&ForumOutbox{}).Where("dedupe_key = ?", "recent-done").Count(&recentDone).Error; err != nil {
		t.Fatalf("count recent done: %v", err)
	}
	if recentDone != 1 {
		t.Fatalf("recent done rows = %d, want 1", recentDone)
	}
	_ = states
}

func TestPurgeOldForumOutboxKeepsRecentRowsWithNinetyDays(t *testing.T) {
	db := newForumOutboxRetentionTestDB(t)
	old := time.Now().AddDate(0, 0, -45).UTC()
	row := ForumOutbox{TaskID: "t1", Kind: ForumOutboxPostMessage, DedupeKey: "d1", State: ForumOutboxDone, FinishedAt: &old}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if deleted := PurgeOldForumOutbox(db, 90); deleted != 0 {
		t.Fatalf("purged rows = %d, want 0", deleted)
	}
	var total int64
	if err := db.Model(&ForumOutbox{}).Count(&total).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 1 {
		t.Fatalf("remaining rows = %d, want 1", total)
	}
}

func TestPurgeOldForumOutboxRejectsInvalidInput(t *testing.T) {
	db := newForumOutboxRetentionTestDB(t)
	if deleted := PurgeOldForumOutbox(db, 0); deleted != 0 {
		t.Fatalf("purged rows = %d, want 0 for non-positive retention", deleted)
	}
}

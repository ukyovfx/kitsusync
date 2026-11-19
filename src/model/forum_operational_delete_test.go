package model

import (
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newForumDeleteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(
		&ProductionNotificationRoute{}, &ProductionNotificationConfig{}, &ProductionChannelMapping{},
		&ProductionForumConfig{}, &ProductionForumRoute{}, &ProductionForumStatusRole{}, &ProductionForumTag{},
		&ForumBaseline{}, &ForumTask{}, &ForumTransition{}, &ForumOutbox{},
	); err != nil {
		t.Fatalf("failed to migrate schema: %v", err)
	}
	return db
}

func TestDeleteProductionOperationalStateRemovesForumLedger(t *testing.T) {
	db := newForumDeleteTestDB(t)
	task := ForumTask{TaskID: "task-1", ProjectID: "p1", TaskTypeID: "tt-1"}
	baseline := ForumBaseline{ProjectID: "p1", TaskTypeID: "tt-1"}
	transition := ForumTransition{TaskID: "task-1"}
	outbox := ForumOutbox{TaskID: "task-1", Kind: ForumOutboxPostMessage, DedupeKey: "d-1", State: ForumOutboxDone}
	otherOutbox := ForumOutbox{TaskID: "task-x", Kind: ForumOutboxPostMessage, DedupeKey: "d-x", State: ForumOutboxDone}
	otherTask := ForumTask{TaskID: "task-x", ProjectID: "p2", TaskTypeID: "tt-1"}
	config := ProductionForumConfig{ProductionID: "p1", GuildID: "g1"}
	route := ProductionForumRoute{ProductionID: "p1", TaskTypeID: "tt-1", TaskTypeName: "Animation", ForumChannelID: "f1"}
	for _, row := range []interface{}{&task, &baseline, &transition, &outbox, &otherTask, &otherOutbox, &config, &route} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	if err := DeleteProductionOperationalState(db, "p1"); err != nil {
		t.Fatalf("DeleteProductionOperationalState: %v", err)
	}

	checks := []struct {
		name  string
		model interface{}
		where string
	}{
		{"ForumBaseline", &ForumBaseline{}, "project_id = ?"},
		{"ForumTask", &ForumTask{}, "task_id = ?"},
		{"ForumTransition", &ForumTransition{}, "task_id = ?"},
		{"ForumOutbox", &ForumOutbox{}, "task_id = ?"},
		{"ForumConfig", &ProductionForumConfig{}, "production_id = ?"},
		{"ForumRoute", &ProductionForumRoute{}, "production_id = ?"},
	}
	for _, c := range checks {
		var n int64
		if err := db.Model(c.model).Where(c.where, "p1").Count(&n).Error; err != nil {
			t.Fatalf("count %s: %v", c.name, err)
		}
		if n != 0 {
			t.Fatalf("%s rows for p1 = %d, want 0 (row survived production delete)", c.name, n)
		}
	}

	// Other productions' rows must survive.
	var kept int64
	if err := db.Model(&ForumTask{}).Where("task_id = ?", "task-x").Count(&kept).Error; err != nil {
		t.Fatalf("count other task: %v", err)
	}
	if kept != 1 {
		t.Fatalf("other production's ForumTask rows = %d, want 1", kept)
	}
	if err := db.Model(&ForumOutbox{}).Where("dedupe_key = ?", "d-x").Count(&kept).Error; err != nil {
		t.Fatalf("count other outbox: %v", err)
	}
	if kept != 1 {
		t.Fatalf("other production's ForumOutbox rows = %d, want 1", kept)
	}
}

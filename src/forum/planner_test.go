package forum

import (
	"encoding/json"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"app/src/api/kitsu"
	"app/src/model"
)

func newForumDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.ForumBaseline{}, &model.ForumTask{}, &model.ForumTransition{}, &model.ForumOutbox{},
		&model.ProductionForumConfig{}, &model.ProductionForumRoute{},
		&model.ProductionForumStatusRole{}, &model.ProductionForumTag{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func testSettings() model.ProductionForumSettings {
	return model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "prod-1", Mode: model.NotificationModeForum, GuildID: "guild-1"},
		Routes: []model.ProductionForumRoute{{
			ProductionID:    "prod-1",
			TaskTypeID:      "tt-comp",
			TaskTypeName:    "Compositing",
			ForumChannelID:  "forum-1",
			ForumChannelName: "Compositing",
		}},
		StatusRoles: []model.ProductionForumStatusRole{
			{ProductionID: "prod-1", StatusID: "s-wip", StatusName: "WIP", Role: model.ForumRoleWIP},
			{ProductionID: "prod-1", StatusID: "s-wfa", StatusName: "WFA", Role: model.ForumRoleWFA},
			{ProductionID: "prod-1", StatusID: "s-retake", StatusName: "RETAKE", Role: model.ForumRoleRetake},
			{ProductionID: "prod-1", StatusID: "s-done", StatusName: "DONE", Role: model.ForumRoleDone},
		},
		Tags: []model.ProductionForumTag{
			{ProductionID: "prod-1", ForumChannelID: "forum-1", Role: model.ForumRoleWFA, TagID: "tag-wfa"},
			{ProductionID: "prod-1", ForumChannelID: "forum-1", Role: model.ForumRoleRetake, TagID: "tag-retake"},
			{ProductionID: "prod-1", ForumChannelID: "forum-1", Role: model.ForumRoleDone, TagID: "tag-done"},
		},
	}
}

func testPlanner(db *gorm.DB, now time.Time) Planner {
	settings := testSettings()
	return Planner{DB: db, Production: "prod-1", Settings: settings, Now: now}
}

func taskInput(statusID, role string, assignees ...string) TaskInput {
	return TaskInput{
		TaskID:     "task-1",
		ProjectID:  "prod-1",
		TaskTypeID: "tt-comp",
		EntityID:   "entity-1",
		EntityKind: model.ForumEntityKindShot,
		EntityName: "sh002",
		ParentName: "sq010",
		EntityType: "Shot",
		Title:      TaskTitle("Compositing", "sq010", "sh002"),
		StatusID:   statusID,
		StatusRole: role,
		Assignees:  assignees,
	}
}

func stubCard() func() (json.RawMessage, error) {
	return func() (json.RawMessage, error) {
		return json.RawMessage(`{"embeds":[{"description":"card"}]}`), nil
	}
}

func outboxRows(t *testing.T, db *gorm.DB) []model.ForumOutbox {
	t.Helper()
	var rows []model.ForumOutbox
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("load outbox: %v", err)
	}
	return rows
}

func TestPlannerBaselinesWithoutPosting(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)

	result, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA), stubCard())
	if err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	if !result.Baselined {
		t.Fatalf("first observation must baseline, got %+v", result)
	}
	if rows := outboxRows(t, db); len(rows) != 0 {
		t.Fatalf("baseline must not queue anything, got %d rows", len(rows))
	}
	row, err := model.FindForumTaskWithError(db, "task-1")
	if err != nil {
		t.Fatalf("ledger row: %v", err)
	}
	if row.ThreadID != "" {
		t.Errorf("baseline row has a thread: %q", row.ThreadID)
	}
	if row.Title != "Compositing sq010 sh002" {
		t.Errorf("title = %q", row.Title)
	}
}

func TestPlannerCreatesThreadOnFirstTriggerAfterBaseline(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)

	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	result, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA), stubCard())
	if err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	if !result.Created {
		t.Fatalf("expected a thread to be created, got %+v", result)
	}
	rows := outboxRows(t, db)
	if len(rows) != 2 {
		t.Fatalf("outbox rows = %d, want create_thread + post_message", len(rows))
	}
	if rows[0].Kind != model.ForumOutboxCreateThread {
		t.Errorf("first row kind = %q", rows[0].Kind)
	}
	if rows[1].Kind != model.ForumOutboxPostMessage {
		t.Errorf("second row kind = %q", rows[1].Kind)
	}
	var create model.ForumCreateThreadPayload
	if err := json.Unmarshal([]byte(rows[0].Payload), &create); err != nil {
		t.Fatalf("decode create payload: %v", err)
	}
	if create.Name != "Compositing sq010 sh002" {
		t.Errorf("thread name = %q", create.Name)
	}
	if create.ArchiveMinutes != model.ForumArchiveOtherMinutes {
		t.Errorf("archive minutes = %d", create.ArchiveMinutes)
	}
	if create.ForumID != "forum-1" {
		t.Errorf("forum id = %q", create.ForumID)
	}
	if len(create.Card) == 0 {
		t.Error("create payload has no card")
	}
	if !rows[1].NotBefore.Equal(now.Add(model.ForumMentionWindow)) {
		t.Errorf("signal not_before = %v", rows[1].NotBefore)
	}
	row, err := model.FindForumTaskWithError(db, "task-1")
	if err != nil {
		t.Fatalf("ledger row: %v", err)
	}
	if row.StatusID != "s-wfa" {
		t.Errorf("status not advanced: %q", row.StatusID)
	}
}

func TestPlannerDoesNotPostForNonTriggerStatus(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)

	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	result, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP), stubCard())
	if err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	if result.Created || result.Synced {
		t.Fatalf("a non-trigger status must not create or sync: %+v", result)
	}
	if rows := outboxRows(t, db); len(rows) != 0 {
		t.Fatalf("outbox rows = %d, want 0", len(rows))
	}
}

func TestPlannerSyncsBeforeSignallingExistingPost(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)

	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if _, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA), stubCard()); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Pretend the thread now exists.
	if err := db.Model(&model.ForumTask{}).Where("task_id = ?", "task-1").Updates(map[string]interface{}{
		"thread_id": "thread-1",
		"forum_id":  "forum-1",
	}).Error; err != nil {
		t.Fatalf("store thread: %v", err)
	}
	if err := db.Model(&model.ForumOutbox{}).Where("task_id = ?", "task-1").Update("state", model.ForumOutboxDone).Error; err != nil {
		t.Fatalf("finish rows: %v", err)
	}

	later := now.Add(10 * time.Minute)
	planner.Now = later
	result, err := planner.PlanTask(taskInput("s-retake", model.ForumRoleRetake, "person-1"), stubCard())
	if err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	if !result.Synced || result.Signalled != model.ForumIntentStatusRetake {
		t.Fatalf("unexpected result %+v", result)
	}
	rows := outboxRows(t, db)
	// create_thread, the creation signal, the assignee signal queued by this
	// poll (assignees went from none to person-1), sync_thread, and the retake
	// signal. The sync row must sit before the retake signal.
	syncIndex := -1
	for i, row := range rows {
		if row.Kind == model.ForumOutboxSyncThread {
			syncIndex = i
			break
		}
	}
	if syncIndex == -1 {
		t.Fatalf("no sync_thread row: %+v", rows)
	}
	if syncIndex+1 >= len(rows) || rows[syncIndex+1].Kind != model.ForumOutboxPostMessage {
		t.Fatalf("sync_thread must be followed by the signal row: %+v", rows)
	}
}

func TestPlannerCoalescesRepeatedSignalsWithoutExtendingWindow(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)

	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if _, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA), stubCard()); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Model(&model.ForumTask{}).Where("task_id = ?", "task-1").Update("thread_id", "thread-1").Error; err != nil {
		t.Fatalf("store thread: %v", err)
	}

	planner.Now = now.Add(time.Minute)
	if _, err := planner.PlanTask(taskInput("s-retake", model.ForumRoleRetake), stubCard()); err != nil {
		t.Fatalf("retake: %v", err)
	}
	planner.Now = now.Add(2 * time.Minute)
	if _, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA), stubCard()); err != nil {
		t.Fatalf("wfa again: %v", err)
	}

	var signals []model.ForumOutbox
	if err := db.Where("kind = ?", model.ForumOutboxPostMessage).Order("id asc").Find(&signals).Error; err != nil {
		t.Fatalf("load signals: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("signals = %d, want the original wfa plus one coalesced retake", len(signals))
	}
	// The first signal is the creation-time WFA; the second must still be the
	// retake window opened at +1m, not extended to +2m.
	if !signals[1].NotBefore.Equal(now.Add(time.Minute).Add(model.ForumMentionWindow)) {
		t.Errorf("coalesced not_before = %v, want the original window", signals[1].NotBefore)
	}
}

func TestPlannerSignalsAssigneeChangeOnlyWhenPostExists(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)

	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	// No post yet: an assignee change must stay silent.
	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP, "person-1"), stubCard()); err != nil {
		t.Fatalf("assignee change: %v", err)
	}
	if rows := outboxRows(t, db); len(rows) != 0 {
		t.Fatalf("outbox rows = %d, want 0 before a post exists", len(rows))
	}
}

func TestPlannerIgnoresUnroutedTaskType(t *testing.T) {
	db := newForumDB(t)
	planner := testPlanner(db, time.Now().UTC())
	input := taskInput("s-wfa", model.ForumRoleWFA)
	input.TaskTypeID = "tt-other"

	result, err := planner.PlanTask(input, stubCard())
	if err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	if !result.Skipped {
		t.Fatalf("unrouted task type must be skipped: %+v", result)
	}
	if rows := outboxRows(t, db); len(rows) != 0 {
		t.Fatalf("outbox rows = %d, want 0", len(rows))
	}
}

func TestPlannerRecordsSingleTransitionPerChange(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)

	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP), stubCard()); err != nil {
		t.Fatalf("no-op poll: %v", err)
	}
	planner.Now = now.Add(time.Minute)
	if _, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA), stubCard()); err != nil {
		t.Fatalf("status change: %v", err)
	}

	var transitions []model.ForumTransition
	if err := db.Order("id asc").Find(&transitions).Error; err != nil {
		t.Fatalf("load transitions: %v", err)
	}
	if len(transitions) != 1 {
		t.Fatalf("transitions = %d, want 1", len(transitions))
	}
	if transitions[0].FromStatusID != "s-wip" || transitions[0].ToStatusID != "s-wfa" {
		t.Errorf("transition = %+v", transitions[0])
	}
}

func TestPlannerAssetTitleUsesEntityTypeAsMiddleSegment(t *testing.T) {
	payload := kitsu.MessagePayload{}
	payload.Project.Project.ID = "prod-1"
	payload.Task.Task.ID = "task-9"
	payload.Task.Task.TaskTypeID = "tt-comp"
	payload.Task.Task.EntityID = "entity-9"
	payload.Task.Task.TaskStatusID = "s-wfa"
	payload.Entity.Entity.ID = "entity-9"
	payload.Entity.Entity.Name = "tree_A"
	payload.Entity.Entity.ParentID = "seq-1"
	payload.Entity.Entity.EntityTypeID = "et-1"
	payload.EntityType.EntityType.Name = "Model"
	payload.Parent.Entity.Name = "sq010"
	payload.TaskType.TaskType.Name = "Compositing"

	input := taskInputFromPayload(payload)
	if input.Title != "Compositing Model tree_A" {
		t.Errorf("asset title = %q, want the Q-02 form", input.Title)
	}
	if input.EntityKind != model.ForumEntityKindAsset {
		t.Errorf("entity kind = %q", input.EntityKind)
	}
}

func TestPlannerShotTitleUsesSequenceName(t *testing.T) {
	payload := kitsu.MessagePayload{}
	payload.Project.Project.ID = "prod-1"
	payload.Task.Task.ID = "task-8"
	payload.Task.Task.TaskTypeID = "tt-comp"
	payload.Task.Task.EntityID = "entity-8"
	payload.Task.Task.TaskStatusID = "s-wfa"
	payload.Entity.Entity.ID = "entity-8"
	payload.Entity.Entity.Name = "sh002"
	payload.Entity.Entity.ParentID = "seq-1"
	payload.Entity.Entity.EntityTypeID = "et-1"
	payload.EntityType.EntityType.Name = "Shot"
	payload.Parent.Entity.Name = "sq010"
	payload.TaskType.TaskType.Name = "Compositing"

	input := taskInputFromPayload(payload)
	if input.Title != "Compositing sq010 sh002" {
		t.Errorf("shot title = %q", input.Title)
	}
	if input.EntityKind != model.ForumEntityKindShot {
		t.Errorf("entity kind = %q", input.EntityKind)
	}
}

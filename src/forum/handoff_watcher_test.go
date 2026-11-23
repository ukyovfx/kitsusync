package forum

import (
	"strconv"
	"testing"
	"time"

	"gorm.io/gorm"

	"app/src/api/kitsu"
	"app/src/model"
)

// The watcher tests seed the determination state directly: forum settings
// for prod-1 (Layout + Compositing routed to forum-1), studio policies for
// the statuses, and one studio route Layout -> Compositing. Transitions are
// inserted as the planner would have written them.

func migrateWatcherTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(
		&model.HandoffEvent{}, &model.StudioTaskRoute{}, &model.ProductionTaskRoute{},
		&model.StudioStatusPolicy{}, &model.AdminException{}, &model.NotificationDecision{},
		&model.ProductionHandoffConfig{}, &model.ProductionHandoffProductType{}, &model.ProductionHandoffRoute{},
	); err != nil {
		t.Fatalf("migrate watcher tables: %v", err)
	}
}

func seedWatcherSettings(t *testing.T, db *gorm.DB) {
	t.Helper()
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "prod-1", GuildID: "guild-1"},
		Routes: []model.ProductionForumRoute{
			{ProductionID: "prod-1", TaskTypeID: "tt-layout", TaskTypeName: "Layout", ForumChannelID: "forum-1"},
			{ProductionID: "prod-1", TaskTypeID: "tt-comp", TaskTypeName: "Compositing", ForumChannelID: "forum-1"},
		},
		StatusRoles: []model.ProductionForumStatusRole{
			{StatusID: "s-wfa", StatusName: "Waiting For Approval", Role: model.ForumRoleWFA},
			{StatusID: "s-retake", StatusName: "Retake", Role: model.ForumRoleRetake},
			{StatusID: "s-done", StatusName: "Done", Role: model.ForumRoleDone},
			{StatusID: "s-approved", StatusName: "Approved", Role: model.ForumRoleDone},
		},
		Tags: []model.ProductionForumTag{
			{ProductionID: "prod-1", ForumChannelID: "forum-1", Role: model.ForumRoleWFA, TagID: "tag-wfa"},
			{ProductionID: "prod-1", ForumChannelID: "forum-1", Role: model.ForumRoleRetake, TagID: "tag-retake"},
			{ProductionID: "prod-1", ForumChannelID: "forum-1", Role: model.ForumRoleDone, TagID: "tag-done"},
		},
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatalf("seed forum settings: %v", err)
	}
	for _, policy := range []model.StudioStatusPolicy{
		{StatusID: "s-wip", StatusName: "Work In Progress", Nature: model.StatusNatureWIP},
		{StatusID: "s-wfa", StatusName: "Waiting For Approval", Nature: model.StatusNatureReview},
		{StatusID: "s-retake", StatusName: "Retake", Nature: model.StatusNatureRetake},
		{StatusID: "s-done", StatusName: "Done", Nature: model.StatusNatureDone},
		{StatusID: "s-approved", StatusName: "Approved", Nature: model.StatusNatureDone},
	} {
		if err := db.Create(&policy).Error; err != nil {
			t.Fatalf("seed policy: %v", err)
		}
	}
	if err := model.SaveStudioTaskRoute(db, model.StudioTaskRoute{FromTaskTypeID: "tt-layout", FromTaskTypeName: "Layout", NotifyTaskTypeIDs: "tt-comp"}); err != nil {
		t.Fatalf("seed route: %v", err)
	}
}

func watcherPayload(taskID, taskTypeID, taskTypeName, statusID, statusName string, assignees ...string) kitsu.MessagePayload {
	payload := kitsu.MessagePayload{}
	payload.Project.Project.ID = "prod-1"
	payload.Task.Task.ID = taskID
	payload.Task.Task.TaskTypeID = taskTypeID
	payload.Task.Task.EntityID = "shot-1"
	payload.Task.Task.TaskStatusID = statusID
	payload.Entity.Entity.ID = "shot-1"
	payload.Entity.Entity.Name = "sq010_sh001"
	payload.Parent.Entity.Name = "sq010"
	payload.EntityType.EntityType.Name = "Shot"
	payload.TaskType.TaskType.ID = taskTypeID
	payload.TaskType.TaskType.Name = taskTypeName
	payload.TaskStatus.TaskStatus.ID = statusID
	payload.TaskStatus.TaskStatus.Name = statusName
	for _, id := range assignees {
		payload.Assignees = append(payload.Assignees, kitsu.Person{ID: id})
	}
	return payload
}

func watcherPayloads(receiverStatus string) []kitsu.MessagePayload {
	return []kitsu.MessagePayload{
		watcherPayload("task-lay-1", "tt-layout", "Layout", "s-done", "Done", "person-layout"),
		watcherPayload("task-comp-1", "tt-comp", "Compositing", receiverStatus, "Work In Progress", "person-comp"),
	}
}

func insertTransition(t *testing.T, db *gorm.DB, taskID, from, to string) model.ForumTransition {
	t.Helper()
	row := model.ForumTransition{TaskID: taskID, At: time.Now().UTC(), FromStatusID: from, ToStatusID: to}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("insert transition: %v", err)
	}
	return row
}

func episodeEvents(t *testing.T, db *gorm.DB) []model.HandoffEvent {
	t.Helper()
	var events []model.HandoffEvent
	if err := db.Where("episode_key <> ''").Order("id asc").Find(&events).Error; err != nil {
		t.Fatalf("load events: %v", err)
	}
	return events
}

func TestHandoffWatcherFiresOnDoneEntry(t *testing.T) {
	db := newForumDB(t)
	migrateWatcherTables(t, db)
	seedWatcherSettings(t, db)
	transition := insertTransition(t, db, "task-lay-1", "s-wip", "s-done")

	watcher := HandoffWatcher{DB: db}
	result, err := watcher.WatchHandoffs(watcherPayloads("s-wip"), 0)
	if err != nil || result.Fired != 1 {
		t.Fatalf("watch: fired=%d err=%v", result.Fired, err)
	}
	events := episodeEvents(t, db)
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	event := events[0]
	wantKey := "t" + strconv.Itoa(int(transition.ID))
	if event.EpisodeKey != wantKey || event.NotifyTaskID != "task-comp-1" || event.SourceTaskID != "task-lay-1" {
		t.Fatalf("event = %+v, want episode %s lay->comp", event, wantKey)
	}
	if event.FallbackUsed != model.HandoffFallbackNone || event.NotifyPersonIDs != "person-comp" {
		t.Fatalf("recipients = fallback %q persons %q", event.FallbackUsed, event.NotifyPersonIDs)
	}
	if event.ProductName != "" || event.ProductType != "" {
		t.Fatalf("episode events carry no product detail: %+v", event)
	}
	var decisions []model.NotificationDecision
	if err := db.Where("signal_kind = ?", "handoff").Find(&decisions).Error; err != nil || len(decisions) != 1 {
		t.Fatalf("decisions = %+v err=%v", decisions, err)
	}
	if decisions[0].RuleID != "handoff.status_route" || decisions[0].Outcome != model.DecisionOutcomeQueued {
		t.Fatalf("decision = %+v", decisions[0])
	}

	// Idempotency: the same watermark pass records nothing twice.
	result, err = watcher.WatchHandoffs(watcherPayloads("s-wip"), 0)
	if err != nil || result.Fired != 0 {
		t.Fatalf("rewatch: fired=%d err=%v", result.Fired, err)
	}
	if events := episodeEvents(t, db); len(events) != 1 {
		t.Fatalf("events after rewatch = %d", len(events))
	}
}

func TestHandoffWatcherRefiresAfterRetake(t *testing.T) {
	db := newForumDB(t)
	migrateWatcherTables(t, db)
	seedWatcherSettings(t, db)
	watcher := HandoffWatcher{DB: db}

	insertTransition(t, db, "task-lay-1", "s-wip", "s-done")
	if _, err := watcher.WatchHandoffs(watcherPayloads("s-wip"), 0); err != nil {
		t.Fatal(err)
	}
	// Retake, then done again: a new episode fires.
	insertTransition(t, db, "task-lay-1", "s-done", "s-retake")
	second := insertTransition(t, db, "task-lay-1", "s-retake", "s-done")
	result, err := watcher.WatchHandoffs(watcherPayloads("s-wip"), 0)
	if err != nil {
		t.Fatal(err)
	}
	events := episodeEvents(t, db)
	if len(events) != 2 {
		t.Fatalf("events = %+v (refire result %+v)", events, result)
	}
	if events[1].EpisodeKey != "t"+strconv.Itoa(int(second.ID)) {
		t.Fatalf("second episode key = %q", events[1].EpisodeKey)
	}
	// The first episode's unanswered event was superseded by the refire.
	if events[0].State != model.HandoffEventCancelled {
		t.Fatalf("first event state = %q, want cancelled (superseded)", events[0].State)
	}
	count, err := model.CountHandoffEpisodes(db, "task-lay-1")
	if err != nil || count != 2 {
		t.Fatalf("episode count = %d err=%v", count, err)
	}
}

func TestHandoffWatcherStaysSilentInsideDoneNature(t *testing.T) {
	db := newForumDB(t)
	migrateWatcherTables(t, db)
	seedWatcherSettings(t, db)
	insertTransition(t, db, "task-lay-1", "s-approved", "s-done")
	watcher := HandoffWatcher{DB: db}
	result, err := watcher.WatchHandoffs(watcherPayloads("s-wip"), 0)
	if err != nil || result.Fired != 0 {
		t.Fatalf("fired=%d err=%v, want silence within the done nature", result.Fired, err)
	}
	if events := episodeEvents(t, db); len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}
}

func TestHandoffWatcherSuppressesClosedReceiver(t *testing.T) {
	db := newForumDB(t)
	migrateWatcherTables(t, db)
	seedWatcherSettings(t, db)
	insertTransition(t, db, "task-lay-1", "s-wip", "s-done")
	watcher := HandoffWatcher{DB: db}
	payloads := watcherPayloads("s-done") // the receiver already finished
	result, err := watcher.WatchHandoffs(payloads, 0)
	if err != nil || result.Fired != 0 || result.Suppressed != 1 {
		t.Fatalf("result = %+v err=%v", result, err)
	}
	if events := episodeEvents(t, db); len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}
	var decision model.NotificationDecision
	if err := db.Where("signal_kind = ?", "handoff").First(&decision).Error; err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != model.DecisionOutcomeSuppressed || decision.SuppressReason != "receiver_closed" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestHandoffWatcherNoRouteRaisesExceptionOnce(t *testing.T) {
	db := newForumDB(t)
	migrateWatcherTables(t, db)
	seedWatcherSettings(t, db)
	if err := db.Where("from_task_type_id = ?", "tt-layout").Delete(&model.StudioTaskRoute{}).Error; err != nil {
		t.Fatal(err)
	}
	insertTransition(t, db, "task-lay-1", "s-wip", "s-done")
	watcher := HandoffWatcher{DB: db}
	result, err := watcher.WatchHandoffs(watcherPayloads("s-wip"), 0)
	if err != nil || result.Suppressed != 1 {
		t.Fatalf("result = %+v err=%v", result, err)
	}
	open, err := model.OpenAdminExceptions(db)
	if err != nil || len(open) != 1 || open[0].Kind != model.ExceptionNoRoute {
		t.Fatalf("exceptions = %+v err=%v", open, err)
	}
	var decision model.NotificationDecision
	if err := db.Where("signal_kind = ?", "handoff").First(&decision).Error; err != nil {
		t.Fatal(err)
	}
	if decision.RuleID != "handoff.no_route" || decision.SuppressReason != "no_route" {
		t.Fatalf("decision = %+v", decision)
	}
}

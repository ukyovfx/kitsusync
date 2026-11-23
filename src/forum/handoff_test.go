package forum

import (
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"app/src/api/kitsu"
	"app/src/model"
)

// AYON's default custom_comment_template, as a publish comment renders it.
const deliverTable = "|  |  |\n|--|--|\n| version | 3 |\n| family | matchmove |\n| name | sq010_sh001_matchmove |\n"

// newHandoffDB opens its own in-memory database: the shared :memory: cache of
// newForumDB would leak handoff rows between tests.
func newHandoffDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:handoff-%s?mode=memory&cache=shared&_busy_timeout=5000", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.ProductionHandoffConfig{}, &model.ProductionHandoffProductType{},
		&model.ProductionHandoffRoute{}, &model.HandoffEvent{}, &model.Setting{}, &model.AuditLog{}, &model.ForumTask{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// seedHandoffRoutes saves prod-1 settings with the immediate timing.
func seedHandoffRoutes(t *testing.T, db *gorm.DB, flow, noAssignee string) {
	t.Helper()
	seedHandoffTiming(t, db, flow, noAssignee, model.HandoffTimingImmediate)
}

// seedHandoffTiming saves prod-1 settings: Matchmove delivers "matchmove" and
// the Compositing task on the same shot is told.
func seedHandoffTiming(t *testing.T, db *gorm.DB, flow, noAssignee, timing string) {
	t.Helper()
	err := model.SaveProductionHandoffSettings(db, model.ProductionHandoffSettings{
		Config: model.ProductionHandoffConfig{
			ProductionID:   "prod-1",
			NotifyTiming:   timing,
			NoAssignee:     noAssignee,
			FallbackUserID: "person-lead",
		},
		ProductTypes: []model.ProductionHandoffProductType{{Name: "matchmove"}},
		Routes: []model.ProductionHandoffRoute{{
			TaskTypeID:        "tt-mm",
			TaskTypeName:      "Matchmove",
			ProductType:       "matchmove",
			Flow:              flow,
			NotifyTaskTypeIDs: "tt-comp",
		}},
	})
	if err != nil {
		t.Fatalf("seed handoff settings: %v", err)
	}
}

func deliveryPayload(taskID, taskTypeID, commentID, text string) kitsu.MessagePayload {
	item := payload(taskID, taskTypeID)
	item.LatestComment.Comment.ID = commentID
	item.LatestComment.Comment.Text = text
	item.LatestComment.Comment.CreatedAt = "2026-11-15T09:00:00"
	item.LatestComment.Comment.PersonID = "person-artist"
	item.LatestComment.Author.Person.ID = "person-artist"
	item.LatestComment.Author.Person.FullName = "Artist"
	return item
}

func receiverPayload(taskTypeID string, assigneeIDs ...string) kitsu.MessagePayload {
	item := payload("task-recv", taskTypeID)
	for _, id := range assigneeIDs {
		item.Assignees = append(item.Assignees, kitsu.Person{ID: id})
	}
	return item
}

// A publish comment with no status change on a 受け渡し row becomes an event.
func TestHandoffDetectRecordsDelivery(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)
	payloads := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-1", "matchmove v3 上げました\n\n"+deliverTable),
		receiverPayload("tt-comp", "person-comp"),
	}
	result, err := (HandoffDetector{DB: db}).Detect(payloads)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if result.Events != 1 {
		t.Fatalf("events = %d, want 1", result.Events)
	}
	event, err := model.FindHandoffEvent(db, "comment-1")
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if event.SourceTaskID != "task-src" || event.SourceAuthorID != "person-artist" {
		t.Fatalf("source = %q/%q", event.SourceTaskID, event.SourceAuthorID)
	}
	if event.ProductType != "matchmove" || event.ProductVersion != "3" || event.ProductName != "sq010_sh001_matchmove" {
		t.Fatalf("product = %q/%q/%q", event.ProductType, event.ProductVersion, event.ProductName)
	}
	if event.NotifyTaskID != "task-recv" || event.NotifyTaskTypeID != "tt-comp" {
		t.Fatalf("receiving = %q/%q", event.NotifyTaskID, event.NotifyTaskTypeID)
	}
	if got := model.SplitHandoffPersonIDs(event.NotifyPersonIDs); len(got) != 1 || got[0] != "person-comp" {
		t.Fatalf("recipients = %v", got)
	}
	if event.State != model.HandoffEventPending || event.FallbackUsed != model.HandoffFallbackNone {
		t.Fatalf("state = %q fallback = %q", event.State, event.FallbackUsed)
	}
}

// A comment that accompanies a status change belongs to the submit flow.
// The move is read from the forum ledger: the status it last observed for
// the task differs from the status the task carries now.
func TestHandoffDetectIgnoresStatusChange(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)
	if err := db.Create(&model.ForumTask{TaskID: "task-src", StatusID: "status-wip"}).Error; err != nil {
		t.Fatalf("seed forum task: %v", err)
	}
	delivery := deliveryPayload("task-src", "tt-mm", "comment-1", deliverTable)
	delivery.Task.Task.TaskStatusID = "status-wfa"
	delivery.StatusChangeAuthor.Person.ID = "person-artist"
	payloads := []kitsu.MessagePayload{delivery, receiverPayload("tt-comp", "person-comp")}
	result, err := (HandoffDetector{DB: db}).Detect(payloads)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if result.Events != 0 {
		t.Fatalf("a status change must not create an event, got %d", result.Events)
	}
	if _, err := model.FindHandoffEvent(db, "comment-1"); err == nil {
		t.Fatal("no event row expected")
	}
}

// The Kitsu comment form attaches the task's current status to every
// comment posted through it, so the payload builder marks any latest UI
// comment with a StatusChangeAuthor. That alone is not a status change:
// with the ledger still showing the same status, a publish comment must
// be detected.
func TestHandoffDetectRecordsCommentCarryingCurrentStatus(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)
	if err := db.Create(&model.ForumTask{TaskID: "task-src", StatusID: "status-wip"}).Error; err != nil {
		t.Fatalf("seed forum task: %v", err)
	}
	delivery := deliveryPayload("task-src", "tt-mm", "comment-1", deliverTable)
	delivery.Task.Task.TaskStatusID = "status-wip"
	delivery.StatusChangeAuthor.Person.ID = "person-artist"
	payloads := []kitsu.MessagePayload{delivery, receiverPayload("tt-comp", "person-comp")}
	result, err := (HandoffDetector{DB: db}).Detect(payloads)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if result.Events != 1 {
		t.Fatalf("events = %d, want 1", result.Events)
	}
}

// A submit or shelve row is not a handoff.
func TestHandoffDetectIgnoresSubmitAndShelve(t *testing.T) {
	for _, flow := range []string{model.HandoffFlowSubmit, model.HandoffFlowShelve} {
		t.Run(flow, func(t *testing.T) {
			db := newHandoffDB(t)
			seedHandoffRoutes(t, db, flow, model.HandoffFallbackAdmin)
			payloads := []kitsu.MessagePayload{
				deliveryPayload("task-src", "tt-mm", "comment-1", deliverTable),
				receiverPayload("tt-comp", "person-comp"),
			}
			result, err := (HandoffDetector{DB: db}).Detect(payloads)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if result.Events != 0 {
				t.Fatalf("%s must not create a handoff event", flow)
			}
			if _, err := model.FindHandoffEvent(db, "comment-1"); err == nil {
				t.Fatalf("%s must not record an event", flow)
			}
		})
	}
}

// An unmapped source Task Type × family pair is not a handoff.
func TestHandoffDetectIgnoresUnmappedCombination(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)
	payloads := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-layout", "comment-1", deliverTable),
		receiverPayload("tt-comp", "person-comp"),
	}
	result, err := (HandoffDetector{DB: db}).Detect(payloads)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if result.Events != 0 {
		t.Fatalf("an unmapped Task Type × product must not create an event, got %d", result.Events)
	}
}

// A receiver with nobody assigned follows the Production's no-assignee setting.
func TestHandoffDetectNoAssigneeFallback(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)
	payloads := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-1", deliverTable),
		receiverPayload("tt-comp"),
	}
	if _, err := (HandoffDetector{DB: db}).Detect(payloads); err != nil {
		t.Fatalf("detect: %v", err)
	}
	event, err := model.FindHandoffEvent(db, "comment-1")
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if got := model.SplitHandoffPersonIDs(event.NotifyPersonIDs); len(got) != 0 {
		t.Fatalf("admin fallback leaves the recipients to K3, got %v", got)
	}
	if event.FallbackUsed != model.HandoffFallbackAdmin {
		t.Fatalf("fallback = %q, want admin", event.FallbackUsed)
	}

	// The chosen-user handling names the person who is told instead.
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackUsers)
	payloads[0] = deliveryPayload("task-src", "tt-mm", "comment-2", deliverTable)
	if _, err := (HandoffDetector{DB: db}).Detect(payloads); err != nil {
		t.Fatalf("detect: %v", err)
	}
	event, err = model.FindHandoffEvent(db, "comment-2")
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if got := model.SplitHandoffPersonIDs(event.NotifyPersonIDs); len(got) != 1 || got[0] != "person-lead" {
		t.Fatalf("chosen-user fallback = %v", got)
	}
}

// The event Detect records for a named assignee is one the mixed rule sends
// immediately. The sending side's own test builds the event by hand, which is
// exactly how the empty FallbackUsed slipped past it.
func TestHandoffDetectAssignedEventIsImmediateUnderMixed(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffTiming(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin, model.HandoffTimingMixed)
	payloads := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-1", deliverTable),
		receiverPayload("tt-comp", "person-comp"),
	}
	if _, err := (HandoffDetector{DB: db}).Detect(payloads); err != nil {
		t.Fatalf("detect: %v", err)
	}
	event, err := model.FindHandoffEvent(db, "comment-1")
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	// handoffImmediate is the sender's rule; this test only pins the values it
	// reads (state, fallback flag, recipient list) as K2 wrote them.
	ids := model.SplitHandoffPersonIDs(event.NotifyPersonIDs)
	if len(ids) != 1 || event.State != model.HandoffEventPending || event.FallbackUsed != model.HandoffFallbackNone {
		t.Fatalf("detected event = state %q fallback %q recipients %v, want a pending non-fallback handoff", event.State, event.FallbackUsed, ids)
	}
}

// Nothing is told when a receiver has no assignee and the Production chose the
// disabled handling, so no pending event waits for a reply that cannot come.
func TestHandoffDetectNoAssigneeDisabledRecordsNoTarget(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffTiming(t, db, model.HandoffFlowHand, model.HandoffFallbackDisabled, model.HandoffTimingMixed)
	payloads := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-1", deliverTable),
		receiverPayload("tt-comp"),
	}
	if _, err := (HandoffDetector{DB: db}).Detect(payloads); err != nil {
		t.Fatalf("detect: %v", err)
	}
	event, err := model.FindHandoffEvent(db, "comment-1")
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if event.State != model.HandoffEventNoTarget {
		t.Fatalf("state = %q, want no_target", event.State)
	}
	pending, err := model.PendingHandoffEvents(db)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending = %d err = %v, want none", len(pending), err)
	}
}

// Only a newer delivery of the same product replaces a pending event: another
// product on the same receiving task still waits for its own reply.
func TestHandoffDetectSupersedesOnlyTheSameProduct(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffTiming(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin, model.HandoffTimingMixed)
	err := model.SaveProductionHandoffSettings(db, model.ProductionHandoffSettings{
		Config: model.ProductionHandoffConfig{
			ProductionID: "prod-1", NotifyTiming: model.HandoffTimingMixed, NoAssignee: model.HandoffFallbackAdmin,
		},
		ProductTypes: []model.ProductionHandoffProductType{{Name: "matchmove"}, {Name: "layout"}},
		Routes: []model.ProductionHandoffRoute{
			{TaskTypeID: "tt-mm", TaskTypeName: "Matchmove", ProductType: "matchmove", Flow: model.HandoffFlowHand, NotifyTaskTypeIDs: "tt-comp"},
			{TaskTypeID: "tt-layout", TaskTypeName: "Layout", ProductType: "layout", Flow: model.HandoffFlowHand, NotifyTaskTypeIDs: "tt-comp"},
		},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	detector := HandoffDetector{DB: db}
	layoutTable := "| version | 2 |\n| family | layout |\n| name | sq010_sh002_layout |\n"
	if _, err := detector.Detect([]kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-layout", "comment-layout", layoutTable),
		receiverPayload("tt-comp", "person-comp"),
	}); err != nil {
		t.Fatalf("detect layout: %v", err)
	}
	if _, err := detector.Detect([]kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-mm1", deliverTable),
		receiverPayload("tt-comp", "person-comp"),
	}); err != nil {
		t.Fatalf("detect matchmove 1: %v", err)
	}
	if _, err := detector.Detect([]kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-mm2", "v4\n\n"+deliverTable),
		receiverPayload("tt-comp", "person-comp"),
	}); err != nil {
		t.Fatalf("detect matchmove 2: %v", err)
	}
	pending, err := model.PendingHandoffEvents(db)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	got := make([]string, 0, len(pending))
	for _, event := range pending {
		got = append(got, event.CommentID)
	}
	if len(got) != 2 || got[0] != "comment-layout" || got[1] != "comment-mm2" {
		t.Fatalf("pending = %v, want the layout row and the newer matchmove", got)
	}
}

// A newer delivery to the same receiving task supersedes the older pending
// event, so the receiving artist reads only the latest delivery.
func TestHandoffDetectNewerDeliverySupersedes(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackDisabled)
	detector := HandoffDetector{DB: db}
	first := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-1", deliverTable),
		receiverPayload("tt-comp", "person-comp"),
	}
	if _, err := detector.Detect(first); err != nil {
		t.Fatalf("detect first: %v", err)
	}
	second := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-2", "v4\n\n"+deliverTable),
		receiverPayload("tt-comp", "person-comp"),
	}
	if _, err := detector.Detect(second); err != nil {
		t.Fatalf("detect second: %v", err)
	}
	pending, err := model.PendingHandoffEvents(db)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].CommentID != "comment-2" {
		t.Fatalf("pending = %+v, want only the newer delivery", pending)
	}
	older, err := model.FindHandoffEvent(db, "comment-1")
	if err != nil {
		t.Fatalf("older event: %v", err)
	}
	if older.State != model.HandoffEventCancelled || older.SupersededAt == nil {
		t.Fatalf("older event = %q, want cancelled with superseded_at", older.State)
	}
}

// Re-polling the same comment must not create a second event.
func TestHandoffDetectIsIdempotent(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)
	payloads := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-1", deliverTable),
		receiverPayload("tt-comp", "person-comp"),
	}
	detector := HandoffDetector{DB: db}
	if _, err := detector.Detect(payloads); err != nil {
		t.Fatalf("detect: %v", err)
	}
	result, err := detector.Detect(payloads)
	if err != nil {
		t.Fatalf("detect again: %v", err)
	}
	if result.Events != 0 || result.Known != 1 {
		t.Fatalf("second poll = %+v, want 0 events and 1 known", result)
	}
	var count int64
	if err := db.Model(&model.HandoffEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows = %d, want 1", count)
	}
}

// A plain comment is not a publish.
func TestHandoffDetectIgnoresNonTemplateComment(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)
	payloads := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-1", "確認お願いします"),
		receiverPayload("tt-comp", "person-comp"),
	}
	result, err := (HandoffDetector{DB: db}).Detect(payloads)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if result.Events != 0 {
		t.Fatalf("a comment without the publish table must not create an event, got %d", result.Events)
	}
}

func TestParseDeliverTable(t *testing.T) {
	table, ok := ParseDeliverTable("コメント\n\n" + deliverTable)
	if !ok {
		t.Fatal("table not parsed")
	}
	if table.Version != "3" || table.Family != "matchmove" || table.Name != "sq010_sh001_matchmove" {
		t.Fatalf("table = %+v", table)
	}
	if _, ok := ParseDeliverTable("| version | 3 |\n| family | matchmove |"); ok {
		t.Fatal("a table without name is not a delivery")
	}
	if _, ok := ParseDeliverTable("自由文だけ"); ok {
		t.Fatal("a plain comment is not a delivery")
	}
}

package forum

import (
	"encoding/json"
	"testing"
	"time"

	"gorm.io/gorm"

	"app/src/model"
)

// seedPostedTask baselines a task, creates its thread, and marks the queued
// rows done so the next poll starts from a task that already has a post.
func seedPostedTask(t *testing.T, db *gorm.DB, planner *Planner) {
	t.Helper()
	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if _, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA), stubCard()); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Model(&model.ForumTask{}).Where("task_id = ?", "task-1").Updates(map[string]interface{}{
		"thread_id": "thread-1",
		"forum_id":  "forum-1",
	}).Error; err != nil {
		t.Fatalf("store thread: %v", err)
	}
	if err := db.Model(&model.ForumOutbox{}).Where("task_id = ?", "task-1").Update("state", model.ForumOutboxDone).Error; err != nil {
		t.Fatalf("finish rows: %v", err)
	}
}

func commentInput(id, text string, mentions ...string) CommentInput {
	return CommentInput{ID: id, Text: text, CreatedAt: "2026-11-15T09:00:00Z", Mentions: mentions}
}

func TestPlannerQueuesCommentSignalForPostedTask(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	input.CommentDate = "2026-11-15T09:04:00.000Z"
	input.Comment = commentInput("comment-1", "ここを直してください\n二行目", "person-2")
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}

	var row model.ForumOutbox
	if err := db.Where("kind = ? AND dedupe_key LIKE ?", model.ForumOutboxPostMessage, "msg:comment:%").
		First(&row).Error; err != nil {
		t.Fatalf("no comment signal queued: %v", err)
	}
	var payload model.ForumPostMessagePayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Intent != model.ForumIntentComment {
		t.Errorf("intent = %q", payload.Intent)
	}
	if payload.Quote != "ここを直してください" {
		t.Errorf("quote = %q, want the first line only", payload.Quote)
	}
	if len(payload.Mentions) != 1 || payload.Mentions[0] != "person-2" {
		t.Errorf("mentions = %v", payload.Mentions)
	}
}

func TestPlannerIgnoresCommentsWithoutPost(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)

	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	input := taskInput("s-wip", model.ForumRoleWIP)
	input.Comment = commentInput("comment-1", "メモ", "person-2")
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	if rows := outboxRows(t, db); len(rows) != 0 {
		t.Fatalf("outbox rows = %d, want 0 for a task without a post", len(rows))
	}
}

func TestPlannerSkipsCommentSignalWhenCommentsUnavailable(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	input.Comment = CommentInput{ID: "comment-1", Text: "x", Unavailable: true}
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:comment:%").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("comment signals = %d, want 0 when comments are unavailable", count)
	}
}

func TestPlannerRetriesCommentSignalOnNextPollAfterUnavailable(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	// Poll 1: Kitsu has a new comment date, but comments fetch fails (Unavailable = true).
	planner.Now = now.Add(5 * time.Minute)
	poll1 := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	poll1.CommentDate = "2026-11-15T09:04:00.000Z"
	poll1.Comment = CommentInput{ID: "comment-1", Text: "直してください", Unavailable: true}
	if _, err := planner.PlanTask(poll1, stubCard()); err != nil {
		t.Fatalf("PlanTask poll 1: %v", err)
	}

	var poll1Count int64
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:comment:%").Count(&poll1Count).Error; err != nil {
		t.Fatalf("count poll 1: %v", err)
	}
	if poll1Count != 0 {
		t.Fatalf("poll 1 comment signals = %d, want 0 when comments are unavailable", poll1Count)
	}
	task1, err := model.FindForumTaskWithError(db, "task-1")
	if err != nil {
		t.Fatalf("find task poll 1: %v", err)
	}
	if task1.LastCommentAt != "" {
		t.Fatalf("poll 1 last_comment_at = %q, want empty (checkpoint preserved)", task1.LastCommentAt)
	}

	// Poll 2: The comment fetch succeeds on the next poll with the same CommentDate.
	planner.Now = now.Add(10 * time.Minute)
	poll2 := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	poll2.CommentDate = "2026-11-15T09:04:00.000Z"
	poll2.Comment = commentInput("comment-1", "直してください", "person-2")
	if _, err := planner.PlanTask(poll2, stubCard()); err != nil {
		t.Fatalf("PlanTask poll 2: %v", err)
	}

	var poll2Count int64
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:comment:%").Count(&poll2Count).Error; err != nil {
		t.Fatalf("count poll 2: %v", err)
	}
	if poll2Count != 1 {
		t.Fatalf("poll 2 comment signals = %d, want 1 when retry succeeds", poll2Count)
	}
	task2, err := model.FindForumTaskWithError(db, "task-1")
	if err != nil {
		t.Fatalf("find task poll 2: %v", err)
	}
	if task2.LastCommentAt != "2026-11-15T09:04:00.000Z" {
		t.Fatalf("poll 2 last_comment_at = %q, want updated", task2.LastCommentAt)
	}

	// Poll 3: Subsequent poll with no new comment keeps signal count at 1.
	planner.Now = now.Add(15 * time.Minute)
	if _, err := planner.PlanTask(poll2, stubCard()); err != nil {
		t.Fatalf("PlanTask poll 3: %v", err)
	}
	var poll3Count int64
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:comment:%").Count(&poll3Count).Error; err != nil {
		t.Fatalf("count poll 3: %v", err)
	}
	if poll3Count != 1 {
		t.Fatalf("poll 3 comment signals = %d, want 1 (no duplicate)", poll3Count)
	}
}

func TestPlannerPreservesCheckpointOnlyForFailingTask(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)

	// Seed task-1
	seedPostedTask(t, db, &planner)

	// Seed task-2
	task2Input := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	task2Input.TaskID = "task-2"
	if _, err := planner.PlanTask(task2Input, stubCard()); err != nil {
		t.Fatalf("create task-2: %v", err)
	}
	if err := db.Model(&model.ForumTask{}).Where("task_id = ?", "task-2").Updates(map[string]interface{}{
		"thread_id": "thread-2",
		"forum_id":  "forum-1",
	}).Error; err != nil {
		t.Fatalf("store thread task-2: %v", err)
	}
	if err := db.Model(&model.ForumOutbox{}).Where("task_id = ?", "task-2").Update("state", model.ForumOutboxDone).Error; err != nil {
		t.Fatalf("finish rows task-2: %v", err)
	}

	planner.Now = now.Add(5 * time.Minute)

	// task-1 fails comments
	t1 := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	t1.TaskID = "task-1"
	t1.CommentDate = "2026-11-15T09:05:00.000Z"
	t1.Comment = CommentInput{ID: "c1", Unavailable: true}
	if _, err := planner.PlanTask(t1, stubCard()); err != nil {
		t.Fatalf("plan t1: %v", err)
	}

	// task-2 succeeds comments
	t2 := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	t2.TaskID = "task-2"
	t2.CommentDate = "2026-11-15T09:05:00.000Z"
	t2.Comment = commentInput("c2", "task2 comment", "person-2")
	if _, err := planner.PlanTask(t2, stubCard()); err != nil {
		t.Fatalf("plan t2: %v", err)
	}

	row1, err := model.FindForumTaskWithError(db, "task-1")
	if err != nil {
		t.Fatalf("find task-1: %v", err)
	}
	if row1.LastCommentAt != "" {
		t.Fatalf("task-1 last_comment_at = %q, want empty (preserved)", row1.LastCommentAt)
	}

	row2, err := model.FindForumTaskWithError(db, "task-2")
	if err != nil {
		t.Fatalf("find task-2: %v", err)
	}
	if row2.LastCommentAt != "2026-11-15T09:05:00.000Z" {
		t.Fatalf("task-2 last_comment_at = %q, want updated", row2.LastCommentAt)
	}

	// Only task-2 comment signal should exist in pending/queued
	var c1Count, c2Count int64
	if err := db.Model(&model.ForumOutbox{}).Where("task_id = ? AND dedupe_key LIKE ?", "task-1", "msg:comment:%").Count(&c1Count).Error; err != nil {
		t.Fatalf("count c1: %v", err)
	}
	if err := db.Model(&model.ForumOutbox{}).Where("task_id = ? AND dedupe_key LIKE ?", "task-2", "msg:comment:%").Count(&c2Count).Error; err != nil {
		t.Fatalf("count c2: %v", err)
	}
	if c1Count != 0 {
		t.Fatalf("task-1 comment signals = %d, want 0", c1Count)
	}
	if c2Count != 1 {
		t.Fatalf("task-2 comment signals = %d, want 1", c2Count)
	}
}

func TestPlannerQuotesLatestCommentOnRetake(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-retake", model.ForumRoleRetake, "person-1")
	input.Comment = commentInput("comment-1", "## 修正してください\n詳細")
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}

	var row model.ForumOutbox
	if err := db.Where("kind = ? AND dedupe_key LIKE ?", model.ForumOutboxPostMessage, "msg:status:%").
		Order("id desc").First(&row).Error; err != nil {
		t.Fatalf("no retake signal: %v", err)
	}
	var payload model.ForumPostMessagePayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Quote != "修正してください" {
		t.Errorf("retake quote = %q", payload.Quote)
	}
}

// The RETAKE quote shares the timestamp parsing, so a zone-less created_at must
// quote too instead of silently going empty.
func TestPlannerQuotesRetakeCommentWithoutTimezone(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-retake", model.ForumRoleRetake, "person-1")
	input.Comment = commentInput("comment-1", "## 修正してください\n詳細")
	input.Comment.CreatedAt = "2026-11-15T09:04:00"
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}

	var row model.ForumOutbox
	if err := db.Where("kind = ? AND dedupe_key LIKE ?", model.ForumOutboxPostMessage, "msg:status:%").
		Order("id desc").First(&row).Error; err != nil {
		t.Fatalf("no retake signal: %v", err)
	}
	var payload model.ForumPostMessagePayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Quote != "修正してください" {
		t.Errorf("retake quote = %q, want a quote for a zone-less created_at", payload.Quote)
	}
}

func wfaSignalPayload(t *testing.T, db *gorm.DB) model.ForumPostMessagePayload {
	t.Helper()
	var row model.ForumOutbox
	// The newest row: a task whose opening transition was a WFA also queues a
	// creation-time wfa signal, so an older row can match the same filter.
	if err := db.Where("kind = ? AND dedupe_key LIKE ?", model.ForumOutboxPostMessage, "msg:status:%").
		Order("id desc").First(&row).Error; err != nil {
		t.Fatalf("no wfa signal: %v", err)
	}
	var payload model.ForumPostMessagePayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload
}

// planWFAFromWIP seeds a posted task at WIP and moves it to WFA with comment,
// so the WFA signal is a real transition.
func planWFAFromWIP(t *testing.T, db *gorm.DB, planner *Planner, comment CommentInput) {
	t.Helper()
	seedPostedTask(t, db, planner)
	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP, "person-1"), stubCard()); err != nil {
		t.Fatalf("back to wip: %v", err)
	}
	planner.Now = planner.Now.Add(5 * time.Minute)
	input := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	input.Comment = comment
	input.CommentDate = "2026-11-15T09:04:00.000Z"
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
}

func TestPlannerReadsSubmitterMarkerOnWFA(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	comment := commentInput("comment-1", "提出しました\nkitsusync-submitter: 山田 太郎")
	comment.AuthorName = "Web Publisher"
	planWFAFromWIP(t, db, &planner, comment)

	if got := wfaSignalPayload(t, db).Submitter; got != "山田 太郎" {
		t.Errorf("submitter = %q, want the marker name", got)
	}
}

// A marker written by anyone but the web publisher must not be trusted: it
// would let a member fake who submitted the work.
func TestPlannerIgnoresSubmitterMarkerFromAnotherAuthor(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	comment := commentInput("comment-1", "提出しました\nkitsusync-submitter: 山田 太郎")
	comment.AuthorID = "person-9"
	comment.AuthorName = "鈴木 一郎"
	planWFAFromWIP(t, db, &planner, comment)

	if got := wfaSignalPayload(t, db).Submitter; got != "" {
		t.Errorf("submitter = %q, want empty for a non-service author", got)
	}
}

// Kitsu sometimes omits the zone; that form must still resolve, not silently
// disable the marker.
func TestPlannerReadsSubmitterMarkerWithoutTimezone(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	comment := commentInput("comment-1", "提出しました\nkitsusync-submitter: 山田 太郎")
	comment.CreatedAt = "2026-11-15T09:04:00"
	comment.AuthorName = "Web Publisher"
	planWFAFromWIP(t, db, &planner, comment)

	if got := wfaSignalPayload(t, db).Submitter; got != "山田 太郎" {
		t.Errorf("submitter = %q, want the marker name for a zone-less created_at", got)
	}
}

func TestPlannerOmitsSubmitterWithoutMarker(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	planWFAFromWIP(t, db, &planner, commentInput("comment-1", "提出しました"))

	if got := wfaSignalPayload(t, db).Submitter; got != "" {
		t.Errorf("submitter = %q, want empty without a marker", got)
	}
}

func TestPlannerIgnoresMalformedSubmitterMarker(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	planWFAFromWIP(t, db, &planner, commentInput("comment-1", "提出しました\n kitsusync-submitter: 山田\nkitsusync-submitter:   "))

	if got := wfaSignalPayload(t, db).Submitter; got != "" {
		t.Errorf("submitter = %q, want empty for a malformed marker", got)
	}
}

func TestPlannerWarnsOncePerFlapEpisode(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	// WFA -> RETAKE -> WFA inside the round-trip window is a flap.
	planner.Now = now.Add(time.Minute)
	if _, err := planner.PlanTask(taskInput("s-retake", model.ForumRoleRetake, "person-1"), stubCard()); err != nil {
		t.Fatalf("retake: %v", err)
	}
	planner.Now = now.Add(2 * time.Minute)
	if _, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA, "person-1"), stubCard()); err != nil {
		t.Fatalf("wfa: %v", err)
	}

	var warnings []model.ForumOutbox
	if err := db.Where("kind = ?", model.ForumOutboxWarnMessage).Order("id asc").Find(&warnings).Error; err != nil {
		t.Fatalf("load warnings: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want exactly one per episode", len(warnings))
	}
	var payload struct {
		Transitions int `json:"transitions"`
	}
	if err := json.Unmarshal([]byte(warnings[0].Payload), &payload); err != nil {
		t.Fatalf("decode warning payload: %v", err)
	}
	if payload.Transitions < 2 {
		t.Errorf("warning transitions = %d", payload.Transitions)
	}

	// Another round trip in the same episode must not warn again.
	planner.Now = now.Add(3 * time.Minute)
	if _, err := planner.PlanTask(taskInput("s-retake", model.ForumRoleRetake, "person-1"), stubCard()); err != nil {
		t.Fatalf("retake again: %v", err)
	}
	planner.Now = now.Add(4 * time.Minute)
	if _, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA, "person-1"), stubCard()); err != nil {
		t.Fatalf("wfa again: %v", err)
	}
	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("kind = ?", model.ForumOutboxWarnMessage).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("warnings = %d, want 1 for the whole episode", count)
	}
}

func TestPlannerWarnsOnFourTransitionsInAnHour(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	// Four distinct statuses, each 20 minutes apart: no round trip, but enough
	// transitions to count as a flap.
	steps := []struct {
		at   time.Duration
		role string
		id   string
	}{
		{20 * time.Minute, model.ForumRoleRetake, "s-retake"},
		{40 * time.Minute, model.ForumRoleDone, "s-done"},
		{60 * time.Minute, model.ForumRoleWIP, "s-wip"},
	}
	for _, step := range steps {
		planner.Now = now.Add(step.at)
		if _, err := planner.PlanTask(taskInput(step.id, step.role, "person-1"), stubCard()); err != nil {
			t.Fatalf("step %v: %v", step.at, err)
		}
	}
	planner.Now = now.Add(80 * time.Minute)
	if _, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA, "person-1"), stubCard()); err != nil {
		t.Fatalf("final step: %v", err)
	}

	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("kind = ?", model.ForumOutboxWarnMessage).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("warnings = %d, want 1 after four transitions in an hour", count)
	}
}

func TestPlannerDoesNotWarnForASingleChange(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(time.Minute)
	if _, err := planner.PlanTask(taskInput("s-retake", model.ForumRoleRetake, "person-1"), stubCard()); err != nil {
		t.Fatalf("retake: %v", err)
	}
	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("kind = ?", model.ForumOutboxWarnMessage).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("warnings = %d, want 0 for a single transition", count)
	}
}

func TestCommentQuoteKeepsOneLineAndTruncates(t *testing.T) {
	if got := commentQuote("> 引用\n次の行"); got != "引用" {
		t.Errorf("quote = %q", got)
	}
	if got := commentQuote("\n\n  \n"); got != "" {
		t.Errorf("empty quote = %q", got)
	}
	long := ""
	for i := 0; i < model.ForumQuoteMaxRunes+20; i++ {
		long += "あ"
	}
	got := []rune(commentQuote(long))
	if len(got) != model.ForumQuoteMaxRunes+1 || got[len(got)-1] != '…' {
		t.Errorf("truncated quote length = %d", len(got))
	}
}

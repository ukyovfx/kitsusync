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
	return CommentInput{ID: id, Text: text, CreatedAt: "2026-11-15T09:00:00", Mentions: mentions}
}

func TestPlannerQueuesCommentSignalForPostedTask(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
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
	if err := db.Where("kind = ? AND dedupe_key LIKE ?", model.ForumOutboxPostMessage, "msg:retake:%").
		First(&row).Error; err != nil {
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

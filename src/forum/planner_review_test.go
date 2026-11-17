package forum

import (
	"encoding/json"
	"testing"
	"time"

	"gorm.io/gorm"

	"app/src/model"
)

// commentAt builds a comment input created at a given instant, formatted the
// way Kitsu reports comment timestamps.
func commentAt(id, text string, createdAt time.Time) CommentInput {
	return CommentInput{ID: id, Text: text, CreatedAt: createdAt.Format("2006-01-02T15:04:05.000Z")}
}

// PR #283 の指摘: the comment marker must be the stored comment date, not the
// comment ID. A first poll after the baseline must not quote a comment that was
// already there when the task row was written.
func TestPlannerSkipsCommentAlreadyRecordedAtBaseline(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	// The stored row already knows about this comment date.
	if err := db.Model(&model.ForumTask{}).Where("task_id = ?", "task-1").
		Update("last_comment_at", "2026-11-15T08:00:00.000Z").Error; err != nil {
		t.Fatalf("set last_comment_at: %v", err)
	}

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	input.CommentDate = "2026-11-15T08:00:00.000Z"
	input.Comment = commentAt("comment-1", "古いコメント", now.Add(-time.Hour))
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}

	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:comment:%").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("comment signals = %d, want 0 for a comment already recorded", count)
	}
}

// A genuinely newer comment date must still be announced.
func TestPlannerAnnouncesCommentWithNewerDate(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	if err := db.Model(&model.ForumTask{}).Where("task_id = ?", "task-1").
		Update("last_comment_at", "2026-11-15T08:00:00.000Z").Error; err != nil {
		t.Fatalf("set last_comment_at: %v", err)
	}

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	input.CommentDate = "2026-11-15T09:04:00.000Z"
	input.Comment = commentAt("comment-2", "新しいコメント", now.Add(4*time.Minute))
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}

	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:comment:%").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("comment signals = %d, want 1 for a newer comment", count)
	}
}

// PR #283 の指摘: flapping must be checked on every status transition, not only
// on trigger roles. A round trip that ends in WIP still flaps.
func TestPlannerWarnsWhenFlapEndsInWIP(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	// RETAKE -> WIP -> RETAKE: the round trip happens but the final transition
	// is not a trigger role, so only a flap check on every transition sees it.
	// The round trip repeats so the count rule also holds once the first
	// transition ages out of the one-hour window.
	steps := []struct {
		at   time.Duration
		role string
		id   string
	}{
		{time.Minute, model.ForumRoleRetake, "s-retake"},
		{2 * time.Minute, model.ForumRoleWIP, "s-wip"},
		{3 * time.Minute, model.ForumRoleRetake, "s-retake"},
		{4 * time.Minute, model.ForumRoleWIP, "s-wip"},
		{5 * time.Minute, model.ForumRoleRetake, "s-retake"},
		{6 * time.Minute, model.ForumRoleWIP, "s-wip"},
		{7 * time.Minute, model.ForumRoleRetake, "s-retake"},
	}
	for _, step := range steps {
		planner.Now = now.Add(step.at)
		if _, err := planner.PlanTask(taskInput(step.id, step.role, "person-1"), stubCard()); err != nil {
			t.Fatalf("step %v: %v", step.at, err)
		}
	}

	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("kind = ?", model.ForumOutboxWarnMessage).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("warnings = %d, want 1 for a round trip through WIP", count)
	}
}

// PR #283 の指摘: the RETAKE quote must respect the comment lookback window.
func TestPlannerDropsStaleRetakeQuote(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-retake", model.ForumRoleRetake, "person-1")
	input.Comment = commentAt("comment-1", "古い指摘", now.Add(-time.Hour))
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
	if payload.Quote != "" {
		t.Fatalf("retake quote = %q, want empty for a comment outside the lookback", payload.Quote)
	}
}

// PR #283 の指摘: a round trip needs a return to the original status. A→B then
// B→C is a normal progression, not a flap.
func TestPlannerDoesNotWarnForProgressionThroughStatuses(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(time.Minute)
	if _, err := planner.PlanTask(taskInput("s-retake", model.ForumRoleRetake, "person-1"), stubCard()); err != nil {
		t.Fatalf("retake: %v", err)
	}
	planner.Now = now.Add(2 * time.Minute)
	if _, err := planner.PlanTask(taskInput("s-done", model.ForumRoleDone, "person-1"), stubCard()); err != nil {
		t.Fatalf("done: %v", err)
	}

	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("kind = ?", model.ForumOutboxWarnMessage).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("warnings = %d, want 0 for a straight progression", count)
	}
}

// PR #283 の指摘: one warning per continuous flap episode. Transitions that keep
// coming for longer than the flap period are still one episode as long as no
// quiet period separates them.
func TestPlannerWarnsOnceAcrossALongFlapEpisode(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	// Alternate RETAKE/WFA every 30 minutes for three hours: never a quiet
	// hour, so the whole run is one episode. The count rule fires at the
	// fourth transition, by which time the first one has aged out of the
	// one-hour window.
	statuses := []struct {
		role string
		id   string
	}{
		{model.ForumRoleRetake, "s-retake"},
		{model.ForumRoleWFA, "s-wfa"},
	}
	for i := 1; i <= 8; i++ {
		step := statuses[i%2]
		planner.Now = now.Add(time.Duration(i) * 30 * time.Minute)
		if _, err := planner.PlanTask(taskInput(step.id, step.role, "person-1"), stubCard()); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}

	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("kind = ?", model.ForumOutboxWarnMessage).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("warnings = %d, want 1 for one continuous flap episode", count)
	}
}

// The round-trip rule only sees transitions inside the round-trip window: a
// normal WFA -> RETAKE (+10 min) -> WFA (+50 min) round trip is not a flap.
func TestPlannerDoesNotWarnForSlowRoundTrip(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(10 * time.Minute)
	if _, err := planner.PlanTask(taskInput("s-retake", model.ForumRoleRetake, "person-1"), stubCard()); err != nil {
		t.Fatalf("retake: %v", err)
	}
	planner.Now = now.Add(60 * time.Minute)
	if _, err := planner.PlanTask(taskInput("s-wfa", model.ForumRoleWFA, "person-1"), stubCard()); err != nil {
		t.Fatalf("wfa: %v", err)
	}

	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("kind = ?", model.ForumOutboxWarnMessage).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("warnings = %d, want 0 for a round trip slower than the window", count)
	}
}

// A genuine quiet period starts a new episode, which may warn again.
func TestPlannerWarnsAgainAfterAQuietPeriod(t *testing.T) {	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	flap := func(base time.Time) {
		steps := []struct {
			at   time.Duration
			role string
			id   string
		}{
			{time.Minute, model.ForumRoleRetake, "s-retake"},
			{2 * time.Minute, model.ForumRoleWFA, "s-wfa"},
		}
		for _, step := range steps {
			planner.Now = base.Add(step.at)
			if _, err := planner.PlanTask(taskInput(step.id, step.role, "person-1"), stubCard()); err != nil {
				t.Fatalf("step %v: %v", step.at, err)
			}
		}
	}
	flap(now)
	flap(now.Add(3 * time.Hour))

	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("kind = ?", model.ForumOutboxWarnMessage).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("warnings = %d, want one per episode", count)
	}
}

// guard against unused helper warnings when only some tests run
var _ = gorm.ErrRecordNotFound

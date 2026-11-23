package forum

import (
	"testing"
	"time"

	"app/src/model"
)

// The review_stale rule (fix C-2) is evaluated on every poll against the
// CURRENT status's dwell: a task that entered WIP and simply stayed there
// crosses the 3-day threshold on a later poll, with no transition at all.
// The first implementation measured the previous status's dwell at
// transition time, so exactly those tasks never raised.

func openStaleCount(t *testing.T, planner Planner) int64 {
	t.Helper()
	var count int64
	err := planner.DB.Model(&model.AdminException{}).
		Where("kind = ? AND subject_id = ? AND state = ?", model.ExceptionReviewStale, "task-1", model.ExceptionStateOpen).
		Count(&count).Error
	if err != nil {
		t.Fatalf("count stale exceptions: %v", err)
	}
	return count
}

func wipInput(commentDate string) TaskInput {
	input := taskInput("s-wip", model.ForumRoleWIP, "person-1")
	input.CommentDate = commentDate
	if commentDate != "" {
		input.Comment = commentInput("comment-1", "publishしました")
	}
	return input
}

func TestReviewStaleRaisesWhileTaskSitsInWIP(t *testing.T) {
	db := newForumDB(t)
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, t0)
	if _, err := planner.PlanTask(wipInput(""), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}

	// Two days in: below the threshold.
	planner.Now = t0.Add(2 * 24 * time.Hour)
	if _, err := planner.PlanTask(wipInput(t0.Add(24*time.Hour).Format(time.RFC3339)), stubCard()); err != nil {
		t.Fatalf("poll at +2d: %v", err)
	}
	if got := openStaleCount(t, planner); got != 0 {
		t.Fatalf("open stale exceptions at +2d = %d, want 0", got)
	}

	// Four days in, with a comment newer than the WIP entry: raises, even
	// though the status never changed.
	planner.Now = t0.Add(4 * 24 * time.Hour)
	if _, err := planner.PlanTask(wipInput(t0.Add(2*24*time.Hour).Format(time.RFC3339)), stubCard()); err != nil {
		t.Fatalf("poll at +4d: %v", err)
	}
	if got := openStaleCount(t, planner); got != 1 {
		t.Fatalf("open stale exceptions at +4d = %d, want 1", got)
	}

	// Still WIP on the next poll: raised once, not re-counted.
	planner.Now = t0.Add(5 * 24 * time.Hour)
	if _, err := planner.PlanTask(wipInput(t0.Add(3*24*time.Hour).Format(time.RFC3339)), stubCard()); err != nil {
		t.Fatalf("poll at +5d: %v", err)
	}
	if got := openStaleCount(t, planner); got != 1 {
		t.Fatalf("open stale exceptions at +5d = %d, want 1", got)
	}
	var row model.AdminException
	if err := db.Where("kind = ?", model.ExceptionReviewStale).First(&row).Error; err != nil {
		t.Fatalf("load exception: %v", err)
	}
	if row.SeenCount != 1 {
		t.Errorf("seen_count = %d, want 1 (raised once per episode)", row.SeenCount)
	}
}

func TestReviewStaleNeedsActivityAfterEnteringWIP(t *testing.T) {
	db := newForumDB(t)
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, t0)
	// The only comment predates the WIP entry: an idle task is not a
	// forgotten review.
	old := t0.Add(-24 * time.Hour).Format(time.RFC3339)
	if _, err := planner.PlanTask(wipInput(old), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	planner.Now = t0.Add(4 * 24 * time.Hour)
	if _, err := planner.PlanTask(wipInput(old), stubCard()); err != nil {
		t.Fatalf("poll at +4d: %v", err)
	}
	if got := openStaleCount(t, planner); got != 0 {
		t.Fatalf("open stale exceptions = %d, want 0 without activity inside WIP", got)
	}
}

func TestReviewStaleDoesNotInheritPreviousStatusDwell(t *testing.T) {
	db := newForumDB(t)
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, t0)
	// Five days in an unclassified status, then the task moves to WIP: the
	// old semantics fired here, on the previous status's dwell. The WIP
	// dwell has only just started, so nothing raises yet.
	todo := taskInput("s-todo", "")
	if _, err := planner.PlanTask(todo, stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	planner.Now = t0.Add(5 * 24 * time.Hour)
	if _, err := planner.PlanTask(wipInput(t0.Add(4*24*time.Hour).Format(time.RFC3339)), stubCard()); err != nil {
		t.Fatalf("transition to WIP: %v", err)
	}
	if got := openStaleCount(t, planner); got != 0 {
		t.Fatalf("open stale exceptions on entering WIP = %d, want 0", got)
	}
	// Four more days sitting in WIP with fresh activity: now it raises.
	planner.Now = t0.Add(9 * 24 * time.Hour)
	if _, err := planner.PlanTask(wipInput(t0.Add(6*24*time.Hour).Format(time.RFC3339)), stubCard()); err != nil {
		t.Fatalf("poll at +9d: %v", err)
	}
	if got := openStaleCount(t, planner); got != 1 {
		t.Fatalf("open stale exceptions at +9d = %d, want 1", got)
	}
}

func TestReviewStaleAutoResolvesWhenTaskLeavesWIP(t *testing.T) {
	db := newForumDB(t)
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, t0)
	if _, err := planner.PlanTask(wipInput(""), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	planner.Now = t0.Add(4 * 24 * time.Hour)
	if _, err := planner.PlanTask(wipInput(t0.Add(2*24*time.Hour).Format(time.RFC3339)), stubCard()); err != nil {
		t.Fatalf("poll at +4d: %v", err)
	}
	if got := openStaleCount(t, planner); got != 1 {
		t.Fatalf("open stale exceptions = %d, want 1 before the exit", got)
	}

	planner.Now = t0.Add(5 * 24 * time.Hour)
	wfa := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	if _, err := planner.PlanTask(wfa, stubCard()); err != nil {
		t.Fatalf("transition to WFA: %v", err)
	}
	if got := openStaleCount(t, planner); got != 0 {
		t.Fatalf("open stale exceptions after leaving WIP = %d, want 0", got)
	}
	var row model.AdminException
	if err := db.Where("kind = ?", model.ExceptionReviewStale).First(&row).Error; err != nil {
		t.Fatalf("load exception: %v", err)
	}
	if row.State != model.ExceptionStateResolved {
		t.Errorf("exception state = %q, want resolved", row.State)
	}
}

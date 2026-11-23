package forum

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"app/src/model"
)

// A comment attached to a retake belongs to the status signal (its quote
// carries the text): mentions that repeat the signal's own recipients are
// suppressed with it, but a mention of a third person still relays
// (interference fix C-7).
func TestPlannerTriggerChangeRelaysOnlyThirdPartyMentions(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-retake", model.ForumRoleRetake, "person-1")
	input.CommentDate = "2026-11-15T09:04:00.000Z"
	input.Comment = commentInput("comment-1", "直してください\n二行目", "person-2")
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}

	var commentCount int64
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:comment:%").Count(&commentCount).Error; err != nil {
		t.Fatalf("count comment signals: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("comment signals = %d, want 1 relaying the third-party mention", commentCount)
	}
	var commentRow model.ForumOutbox
	if err := db.Where("dedupe_key LIKE ?", "msg:comment:%").First(&commentRow).Error; err != nil {
		t.Fatalf("find comment signal: %v", err)
	}
	var commentPayload model.ForumPostMessagePayload
	if err := json.Unmarshal([]byte(commentRow.Payload), &commentPayload); err != nil {
		t.Fatalf("decode comment payload: %v", err)
	}
	if len(commentPayload.Mentions) != 1 || commentPayload.Mentions[0] != "person-2" {
		t.Fatalf("relay mentions = %v, want only the third person", commentPayload.Mentions)
	}
	var statusCount int64
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:status:%").Count(&statusCount).Error; err != nil {
		t.Fatalf("count status signals: %v", err)
	}
	if statusCount == 0 {
		t.Fatal("status signal missing for the retake")
	}
	task, err := model.FindForumTaskWithError(db, "task-1")
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if task.LastCommentAt != "2026-11-15T09:04:00.000Z" {
		t.Fatalf("last_comment_at = %q, want updated even without a comment signal", task.LastCommentAt)
	}
}

// An empty comment riding a trigger change must not become an empty comment
// notification either.
func TestPlannerSkipsEmptyCommentOnTriggerChange(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-done", model.ForumRoleDone, "person-1")
	input.CommentDate = "2026-11-15T09:04:00.000Z"
	input.Comment = commentInput("comment-1", "")
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	var commentCount int64
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:comment:%").Count(&commentCount).Error; err != nil {
		t.Fatalf("count comment signals: %v", err)
	}
	if commentCount != 0 {
		t.Fatalf("comment signals = %d, want 0 for an empty comment on a done change", commentCount)
	}
}

// A comment on a non-trigger change is unrelated to the status signal and is
// still announced as before.
func TestPlannerStillAnnouncesCommentOnNonTriggerChange(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-wip", model.ForumRoleWIP, "person-1")
	input.CommentDate = "2026-11-15T09:04:00.000Z"
	input.Comment = commentInput("comment-1", "進捗メモ", "person-2")
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	var row model.ForumOutbox
	if err := db.Where("dedupe_key LIKE ?", "msg:comment:%").First(&row).Error; err != nil {
		t.Fatalf("comment signal missing for a WIP change: %v", err)
	}
}

// A retake inside the same window must not quote a comment that was already
// announced; a later retake outside the window quotes it normally.
func TestPlannerRetakeQuotesAnnouncedCommentOnlyOutsideWindow(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	// 09:05: the comment announces itself.
	announceAt := now.Add(5 * time.Minute)
	planner.Now = announceAt
	commentPoll := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	commentPoll.CommentDate = "2026-11-15T09:04:00.000Z"
	commentPoll.Comment = commentInput("comment-1", "直してください", "person-2")
	commentPoll.Comment.CreatedAt = "2026-11-15T09:04:00Z"
	if _, err := planner.PlanTask(commentPoll, stubCard()); err != nil {
		t.Fatalf("PlanTask comment: %v", err)
	}

	// Half a window after the announce, the retake is inside the same
	// aggregation window and must quote nothing.
	planner.Now = announceAt.Add(model.ForumMentionWindow / 2)
	retakePoll := taskInput("s-retake", model.ForumRoleRetake, "person-1")
	retakePoll.CommentDate = "2026-11-15T09:04:00.000Z"
	retakePoll.Comment = commentInput("comment-1", "直してください")
	retakePoll.Comment.CreatedAt = "2026-11-15T09:04:00Z"
	if _, err := planner.PlanTask(retakePoll, stubCard()); err != nil {
		t.Fatalf("PlanTask retake: %v", err)
	}
	if got := latestStatusQuote(t, db); got != "" {
		t.Fatalf("retake quote = %q, want empty inside the aggregation window", got)
	}

	// One window after the announce, step away and retake again; the window
	// has closed but the comment is still in the quote lookback, so the quote
	// is allowed again.
	planner.Now = announceAt.Add(model.ForumMentionWindow + 10*time.Second)
	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP, "person-1"), stubCard()); err != nil {
		t.Fatalf("PlanTask wip: %v", err)
	}
	planner.Now = announceAt.Add(model.ForumMentionWindow + 20*time.Second)
	if _, err := planner.PlanTask(retakePoll, stubCard()); err != nil {
		t.Fatalf("PlanTask retake again: %v", err)
	}
	if got := latestStatusQuote(t, db); got != "直してください" {
		t.Fatalf("retake quote = %q, want the comment line outside the window", got)
	}
}

// A comment without mentions notifies nobody (ruling B): no comment signal
// is queued at all, whatever the comment carries.
func TestPlannerCommentSignalWithoutMentionsMentionsNobody(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-wfa", model.ForumRoleWFA, "person-1")
	input.CommentDate = "2026-11-15T09:04:00.000Z"
	input.Comment = commentInput("comment-1", "| 提出物 | リンク |\n|---|---|\n| card | http://example.invalid |")
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	var count int64
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:comment:%").Count(&count).Error; err != nil {
		t.Fatalf("count comment signals: %v", err)
	}
	if count != 0 {
		t.Fatalf("comment signals = %d, want 0 for a mention-less comment", count)
	}
}

// The outbox claim must hand out the status signal (queued first) before the
// later comment signal, even though the comment row becomes due earlier.
func TestOutboxClaimKeepsStatusBeforeLaterComment(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	// 09:05: a retake opens a status signal, due one window later.
	retakeAt := now.Add(5 * time.Minute)
	planner.Now = retakeAt
	if _, err := planner.PlanTask(taskInput("s-retake", model.ForumRoleRetake), stubCard()); err != nil {
		t.Fatalf("PlanTask retake: %v", err)
	}
	// The retake also queues a sync_thread row ahead of the signal (existing
	// ordering rule); mark it done so this test only compares the two signal
	// rows.
	if err := db.Model(&model.ForumOutbox{}).Where("kind = ?", model.ForumOutboxSyncThread).Update("state", model.ForumOutboxDone).Error; err != nil {
		t.Fatalf("finish sync rows: %v", err)
	}

	// 10s later: a fresh comment signal, due at once.
	planner.Now = retakeAt.Add(10 * time.Second)
	commentPoll := taskInput("s-retake", model.ForumRoleRetake)
	commentPoll.CommentDate = "2026-11-15T09:05:00.000Z"
	commentPoll.Comment = commentInput("comment-2", "追加の指摘", "person-2")
	if _, err := planner.PlanTask(commentPoll, stubCard()); err != nil {
		t.Fatalf("PlanTask comment: %v", err)
	}

	// The due comment row is blocked by the still-pending status row.
	claimed, err := model.ClaimNextForumOutboxRow(db, retakeAt.Add(10*time.Second))
	if err != nil {
		t.Fatalf("claim before the status row: %v", err)
	}
	if claimed != nil {
		t.Fatalf("claimed %q before the status signal, want nothing", claimed.DedupeKey)
	}

	// One window after the retake: the status row comes first.
	claimed, err = model.ClaimNextForumOutboxRow(db, retakeAt.Add(model.ForumMentionWindow))
	if err != nil {
		t.Fatalf("claim the status row: %v", err)
	}
	if claimed == nil || !strings.HasPrefix(claimed.DedupeKey, "msg:status:") {
		t.Fatalf("claimed = %v, want the status signal first", claimed)
	}
	if err := model.FinishForumOutboxRow(db, claimed.ID, "test"); err != nil {
		t.Fatalf("finish status row: %v", err)
	}

	// Only then the comment row follows.
	claimed, err = model.ClaimNextForumOutboxRow(db, retakeAt.Add(model.ForumMentionWindow))
	if err != nil {
		t.Fatalf("claim comment row: %v", err)
	}
	if claimed == nil || !strings.HasPrefix(claimed.DedupeKey, "msg:comment:") {
		t.Fatalf("claimed = %v, want the comment signal after the status signal", claimed)
	}
}

func latestStatusQuote(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var row model.ForumOutbox
	if err := db.Where("kind = ? AND dedupe_key LIKE ?", model.ForumOutboxPostMessage, "msg:status:%").
		Order("id desc").First(&row).Error; err != nil {
		t.Fatalf("no status signal: %v", err)
	}
	var payload model.ForumPostMessagePayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload.Quote
}

// Fix C-1: a mention on a task that has no post yet still reaches its person:
// the post opens (like a handoff delivery does) and the mention relays into
// it.
func TestPlannerMentionOnPostlessTaskOpensPost(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	// Baseline only: the task row exists, no thread was ever opened.
	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP, "person-1"), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-wip", model.ForumRoleWIP, "person-1")
	input.CommentDate = "2026-11-15T09:04:00.000Z"
	input.Comment = commentInput("comment-1", "確認お願いします", "person-9")
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	var creates, comments int64
	if err := db.Model(&model.ForumOutbox{}).Where("kind = ?", model.ForumOutboxCreateThread).Count(&creates).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ForumOutbox{}).Where("dedupe_key LIKE ?", "msg:comment:%").Count(&comments).Error; err != nil {
		t.Fatal(err)
	}
	if creates != 1 || comments != 1 {
		t.Fatalf("creates=%d comments=%d, want the post opened and the mention relayed", creates, comments)
	}
}

// Fix C-9: when the only assignee is the person who made the change (the
// newest comment's author), the DONE signal is queued with mentions
// suppressed: it posts, but pings nobody.
func TestPlannerSuppressesMentionWhenChangerIsSoleRecipient(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedPostedTask(t, db, &planner)

	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-done", model.ForumRoleDone, "person-1")
	input.CommentDate = "2026-11-15T09:04:00.000Z"
	input.Comment = commentInput("comment-1", "完了しました")
	input.Comment.AuthorID = "person-1"
	if _, err := planner.PlanTask(input, stubCard()); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	var row model.ForumOutbox
	if err := db.Where("dedupe_key LIKE ?", "msg:status:%").Order("id desc").First(&row).Error; err != nil {
		t.Fatalf("status signal missing: %v", err)
	}
	var payload model.ForumPostMessagePayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.SuppressMentions {
		t.Fatalf("payload = %+v, want SuppressMentions", payload)
	}
}

package forum

import (
	"testing"

	"gorm.io/gorm"

	"app/src/api/discord"
	"app/src/model"
)

// commentRecipients builds a Recipients with fixed resolvers so the comment
// intent can be checked without the Discord wiring.
func commentRecipients(db *gorm.DB) Recipients {
	return Recipients{
		DB: db,
		ResolveAssignees: func(projectID string, kitsuPersonIDs []string) []string {
			ids := make([]string, 0, len(kitsuPersonIDs))
			for _, id := range kitsuPersonIDs {
				ids = append(ids, "d-"+id)
			}
			return ids
		},
		ResolveReviewers: func(projectID, taskTypeID, taskTypeName string) ([]discord.ReviewerTarget, error) {
			return []discord.ReviewerTarget{{Kind: model.ReviewerTargetUser, ID: "d-reviewer"}}, nil
		},
	}
}

func seedRecipientTask(t *testing.T, db *gorm.DB, assignees ...string) {
	t.Helper()
	threadID := "thread-1"
	row := model.ForumTask{
		TaskID:      "task-1",
		ProjectID:   "prod-1",
		TaskTypeID:  "tt-comp",
		StatusID:    "s-wfa",
		AssigneeIDs: encodeIDs(model.NormalizeForumAssigneeIDs(assignees)),
		ThreadID:    &threadID,
		ForumID:     "forum-1",
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

func TestRecipientsCommentIntentMentionsOnly(t *testing.T) {
	db := newForumDB(t)
	seedRecipientTask(t, db, "person-1", "person-2")

	users, roles, err := commentRecipients(db).ForTask("task-1", model.ForumPostMessagePayload{
		Intent:   model.ForumIntentComment,
		Mentions: []string{"person-9"},
	})
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("comment intent must not mention roles: %v", roles)
	}
	want := []string{"d-person-9"}
	if len(users) != len(want) {
		t.Fatalf("users = %v, want %v", users, want)
	}
	for i := range want {
		if users[i] != want[i] {
			t.Fatalf("users = %v, want %v", users, want)
		}
	}
}

func TestRecipientsCommentIntentSkipsAssigneesWhenDisabled(t *testing.T) {
	db := newForumDB(t)
	seedRecipientTask(t, db, "person-1")

	users, _, err := commentRecipients(db).ForTask("task-1", model.ForumPostMessagePayload{
		Intent:   model.ForumIntentComment,
		Mentions: []string{"person-9"},
	})
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if len(users) != 1 || users[0] != "d-person-9" {
		t.Fatalf("users = %v, want only the mentioned person", users)
	}
}

func TestRecipientsCommentIntentDeduplicatesOverlap(t *testing.T) {
	db := newForumDB(t)
	seedRecipientTask(t, db, "person-9")

	users, _, err := commentRecipients(db).ForTask("task-1", model.ForumPostMessagePayload{
		Intent:   model.ForumIntentComment,
		Mentions: []string{"person-9"},
	})
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("users = %v, want one entry for the overlapping person", users)
	}
}

// wfaRecipients builds a Recipients whose reviewer chain is empty, so the
// WFA admin fallback decides the outcome.
func wfaRecipients(db *gorm.DB, admins []string, adminCalls *int) Recipients {
	return Recipients{
		DB: db,
		ResolveReviewers: func(projectID, taskTypeID, taskTypeName string) ([]discord.ReviewerTarget, error) {
			return nil, nil
		},
		ResolveAdmins: func(projectID string) ([]string, error) {
			if adminCalls != nil {
				*adminCalls++
			}
			return admins, nil
		},
		TaskTypeName: func(taskTypeID string) string { return "Compositing" },
	}
}

// Core v2 §8.3 condition 1: a WFA signal whose reviewer chain (reviewer,
// then department supervisor) resolves to nobody is delivered to the
// guild administrators instead of being dropped, and the miss lands in
// the exception queue as recipient_unresolved.
func TestRecipientsWFAFallsBackToAdminsWhenReviewerChainEmpty(t *testing.T) {
	db := newForumDB(t)
	seedRecipientTask(t, db, "person-1")

	users, roles, err := wfaRecipients(db, []string{"d-admin-1", "d-admin-2"}, nil).
		ForTask("task-1", model.ForumPostMessagePayload{Intent: model.ForumIntentStatusWFA})
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if len(users) != 2 || users[0] != "d-admin-1" || users[1] != "d-admin-2" {
		t.Fatalf("users = %v, want the guild administrators", users)
	}
	if len(roles) != 0 {
		t.Fatalf("roles = %v, want none", roles)
	}
	open, err := model.OpenAdminExceptions(db)
	if err != nil {
		t.Fatalf("open exceptions: %v", err)
	}
	if len(open) != 1 || open[0].Kind != model.ExceptionRecipientUnresolved {
		t.Fatalf("exceptions = %+v, want one recipient_unresolved", open)
	}
	if open[0].SubjectID != "tt-comp" || open[0].ProductionID != "prod-1" || open[0].SubjectName != "Compositing" {
		t.Errorf("exception = %+v, want the task type of the production", open[0])
	}
	var decisions []model.NotificationDecision
	if err := db.Find(&decisions).Error; err != nil {
		t.Fatalf("decisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].RuleID != "status.review.admin_fallback" {
		t.Fatalf("decisions = %+v, want the admin fallback recorded", decisions)
	}
}

// A resolved reviewer chain never touches the admin fallback.
func TestRecipientsWFAUsesReviewersWithoutFallback(t *testing.T) {
	db := newForumDB(t)
	seedRecipientTask(t, db, "person-1")
	adminCalls := 0
	recipients := wfaRecipients(db, []string{"d-admin-1"}, &adminCalls)
	recipients.ResolveReviewers = func(projectID, taskTypeID, taskTypeName string) ([]discord.ReviewerTarget, error) {
		return []discord.ReviewerTarget{{Kind: model.ReviewerTargetUser, ID: "d-reviewer"}}, nil
	}

	users, _, err := recipients.ForTask("task-1", model.ForumPostMessagePayload{Intent: model.ForumIntentStatusWFA})
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if len(users) != 1 || users[0] != "d-reviewer" {
		t.Fatalf("users = %v, want the reviewer", users)
	}
	if adminCalls != 0 {
		t.Errorf("admin resolver called %d times, want 0", adminCalls)
	}
	open, err := model.OpenAdminExceptions(db)
	if err != nil {
		t.Fatalf("open exceptions: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("exceptions = %+v, want none", open)
	}
}

// When no administrator can be resolved either, the signal stays
// recipient-less (the executor cancels it as before), but the exception
// is still raised: that is exactly what an admin must see.
func TestRecipientsWFAFallbackWithoutAdminsStillRaises(t *testing.T) {
	db := newForumDB(t)
	seedRecipientTask(t, db, "person-1")

	users, roles, err := wfaRecipients(db, nil, nil).
		ForTask("task-1", model.ForumPostMessagePayload{Intent: model.ForumIntentStatusWFA})
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if len(users) != 0 || len(roles) != 0 {
		t.Fatalf("users=%v roles=%v, want both empty", users, roles)
	}
	open, err := model.OpenAdminExceptions(db)
	if err != nil {
		t.Fatalf("open exceptions: %v", err)
	}
	if len(open) != 1 || open[0].Kind != model.ExceptionRecipientUnresolved {
		t.Fatalf("exceptions = %+v, want one recipient_unresolved", open)
	}
}

// The fallback aggregates per task type (the no_route semantics): two
// tasks of one type raise one row, not two.
func TestRecipientsWFAFallbackAggregatesPerTaskType(t *testing.T) {
	db := newForumDB(t)
	seedRecipientTask(t, db, "person-1")
	threadID := "thread-2"
	if err := db.Create(&model.ForumTask{
		TaskID: "task-2", ProjectID: "prod-1", TaskTypeID: "tt-comp",
		StatusID: "s-wfa", ThreadID: &threadID, ForumID: "forum-1",
	}).Error; err != nil {
		t.Fatalf("seed task-2: %v", err)
	}
	recipients := wfaRecipients(db, []string{"d-admin-1"}, nil)
	for _, taskID := range []string{"task-1", "task-2"} {
		if _, _, err := recipients.ForTask(taskID, model.ForumPostMessagePayload{Intent: model.ForumIntentStatusWFA}); err != nil {
			t.Fatalf("ForTask %s: %v", taskID, err)
		}
	}
	open, err := model.OpenAdminExceptions(db)
	if err != nil {
		t.Fatalf("open exceptions: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("exceptions = %+v, want one aggregated row", open)
	}
	if open[0].SeenCount != 2 {
		t.Errorf("seen_count = %d, want 2", open[0].SeenCount)
	}
}

package forum

import (
	"testing"

	"gorm.io/gorm"

	"app/src/api/discord"
	"app/src/model"
)

// commentRecipients builds a Recipients with fixed resolvers so the comment
// intent can be checked without the Discord wiring.
func commentRecipients(db *gorm.DB, notifyAssignees bool) Recipients {
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
		NotifyAssigneesOnComment: notifyAssignees,
	}
}

func seedRecipientTask(t *testing.T, db *gorm.DB, assignees ...string) {
	t.Helper()
	row := model.ForumTask{
		TaskID:      "task-1",
		ProjectID:   "prod-1",
		TaskTypeID:  "tt-comp",
		StatusID:    "s-wfa",
		AssigneeIDs: encodeIDs(model.NormalizeForumAssigneeIDs(assignees)),
		ThreadID:    "thread-1",
		ForumID:     "forum-1",
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

func TestRecipientsCommentIntentMentionsAndAssignees(t *testing.T) {
	db := newForumDB(t)
	seedRecipientTask(t, db, "person-1", "person-2")

	users, roles := commentRecipients(db, true).ForTask("task-1", model.ForumPostMessagePayload{
		Intent:   model.ForumIntentComment,
		Mentions: []string{"person-9"},
	})
	if len(roles) != 0 {
		t.Errorf("comment intent must not mention roles: %v", roles)
	}
	want := []string{"d-person-9", "d-person-1", "d-person-2"}
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

	users, _ := commentRecipients(db, false).ForTask("task-1", model.ForumPostMessagePayload{
		Intent:   model.ForumIntentComment,
		Mentions: []string{"person-9"},
	})
	if len(users) != 1 || users[0] != "d-person-9" {
		t.Fatalf("users = %v, want only the mentioned person", users)
	}
}

func TestRecipientsCommentIntentDeduplicatesOverlap(t *testing.T) {
	db := newForumDB(t)
	seedRecipientTask(t, db, "person-9")

	users, _ := commentRecipients(db, true).ForTask("task-1", model.ForumPostMessagePayload{
		Intent:   model.ForumIntentComment,
		Mentions: []string{"person-9"},
	})
	if len(users) != 1 {
		t.Fatalf("users = %v, want one entry for the overlapping person", users)
	}
}

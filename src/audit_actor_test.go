package main

import (
	"app/src/api/kitsu"
	"app/src/model"
	"testing"
)

func TestStatusChangeCommentRequiresZouStatusAndLastCommentMatch(t *testing.T) {
	task := kitsu.Task{ID: "task-1", TaskStatusID: "status-done", LastCommentDate: "2026-09-01T10:00:00Z"}
	comments := []kitsu.Comment{
		{ObjectID: "task-1", TaskStatusID: "status-wfa", PersonID: "wrong-person", CreatedAt: task.LastCommentDate},
		{ObjectID: "task-1", TaskStatusID: "status-done", PersonID: "person-1", CreatedAt: task.LastCommentDate},
	}
	got, ok := statusChangeComment(task, comments)
	if !ok || got.PersonID != "person-1" {
		t.Fatalf("status change comment = %+v/%v, want person-1/true", got, ok)
	}
	comments[1].CreatedAt = "2026-09-01T10:01:00Z"
	if _, ok := statusChangeComment(task, comments); ok {
		t.Fatal("mismatched comment was treated as deterministic status actor")
	}
}

func TestStatusChangeActorUsesZouCommentAuthorNotAssigneeOrLatestComment(t *testing.T) {
	var payload kitsu.MessagePayload
	payload.StatusChangeAuthor.ID = "person-transition"
	payload.StatusChangeAuthor.FullName = "Status Changer"
	payload.LatestComment.Author.ID = "person-comment"
	payload.LatestComment.Author.FullName = "Comment Author"
	payload.Assignees = []kitsu.Person{{ID: "person-assignee", FullName: "Assignee"}}
	if kind, id, name := auditActorFromPayload(payload); kind != model.AuditActorHuman || id != "person-transition" || name != "Status Changer" {
		t.Fatalf("status actor = %q/%q/%q, want transition author", kind, id, name)
	}
}

func TestStatusChangeBotActorIsSystemAndMissingActorIsUnknown(t *testing.T) {
	var botPayload kitsu.MessagePayload
	botPayload.StatusChangeAuthor.ID = "bot-1"
	botPayload.StatusChangeAuthor.IsBot = true
	if kind, id, name := auditActorFromPayload(botPayload); kind != model.AuditActorSystem || id != "bot-1" || name != "" {
		t.Fatalf("status bot actor = %q/%q/%q, want system/bot-1/empty", kind, id, name)
	}
	var unknownPayload kitsu.MessagePayload
	unknownPayload.StatusChangeAuthor.ID = "person-without-name"
	if kind, id, name := auditActorFromPayload(unknownPayload); kind != model.AuditActorUnknown || id != "" || name != "" {
		t.Fatalf("incomplete status actor = %q/%q/%q, want unknown", kind, id, name)
	}
}

func TestAuditActorFromPayloadUsesProvenCommentAuthor(t *testing.T) {
	var payload kitsu.MessagePayload
	payload.IsCommentOnly = true
	payload.LatestComment.Author.ID = "person-1"
	payload.LatestComment.Author.FullName = "Ukyo Matsuo"
	if kind, id, name := auditActorFromPayload(payload); kind != model.AuditActorHuman || id != "person-1" || name != "Ukyo Matsuo" {
		t.Fatalf("actor = %q/%q/%q, want human/person-1/Ukyo Matsuo", kind, id, name)
	}
}

func TestAuditActorFromPayloadClassifiesSystemAndUnknownConservatively(t *testing.T) {
	var botPayload kitsu.MessagePayload
	botPayload.IsCommentOnly = true
	botPayload.LatestComment.Author.ID = "bot-1"
	botPayload.LatestComment.Author.IsBot = true
	if kind, id, name := auditActorFromPayload(botPayload); kind != model.AuditActorSystem || id != "bot-1" || name != "" {
		t.Fatalf("bot actor = %q/%q/%q, want system/bot-1/empty", kind, id, name)
	}

	var statusPayload kitsu.MessagePayload
	statusPayload.LatestComment.Author.ID = "person-1"
	statusPayload.LatestComment.Author.FullName = "Wrong attribution"
	if kind, id, name := auditActorFromPayload(statusPayload); kind != model.AuditActorUnknown || id != "" || name != "" {
		t.Fatalf("status actor = %q/%q/%q, want unknown/empty/empty", kind, id, name)
	}
}

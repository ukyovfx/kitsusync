package main

import (
	"testing"

	"app/src/api/kitsu"
	"app/src/forum"
	"app/src/model"
)

// The mixed rule's whole promise is that a handoff with an assignee is sent at
// once. This drives the two real paths in the order main.runForumPoll runs
// them: forum.HandoffDetector records the event, sendImmediateHandoffs sends
// it. The sending side's own tests build the event by hand, which is how an
// empty FallbackUsed hid the bug.
func TestDetectedAssignedHandoffIsSentImmediatelyUnderMixed(t *testing.T) {
	db := handoffNotifyDB(t, model.HandoffTimingMixed)
	fixture := newDMFixture(t)
	stubHandoffIdentity(t, map[string]string{"person-comp": "222222222222222222"})

	payloads := []kitsu.MessagePayload{
		handoffSourcePayload(),
		handoffReceiverPayload("person-comp"),
	}
	if _, err := (forum.HandoffDetector{DB: db}).Detect(payloads); err != nil {
		t.Fatalf("detect: %v", err)
	}
	if event, err := model.FindHandoffEvent(db, "comment-real-1"); err != nil {
		t.Fatalf("event: %v", err)
	} else if event.FallbackUsed != model.HandoffFallbackNone {
		t.Fatalf("detected fallback = %q, want none", event.FallbackUsed)
	}

	seedHandoffForumTask(t, db, "")
	sent, err := sendImmediateHandoffs(db, handoffTestPoller(db), payloads)
	if err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v, want the assigned handoff sent now", sent, err)
	}
	if fixture.sendsFor("222222222222222222") != 0 {
		t.Fatalf("immediate handoff must not DM anymore: sends = %d", fixture.sendsFor("222222222222222222"))
	}
	rows := handoffOutboxRows(t, db)
	if len(rows) != 2 || rows[0].Kind != model.ForumOutboxCreateThread || rows[1].Kind != model.ForumOutboxPostMessage {
		t.Fatalf("outbox = %+v, want the receiving post opened and the signal queued", rows)
	}
	if state := handoffState(t, db, "comment-real-1"); state != model.HandoffEventNotified {
		t.Fatalf("state = %q, want notified", state)
	}
}

// handoffSourcePayload is a Layout task's publish comment with the delivery
// table, the shape the Kitsu poll returns.
func handoffSourcePayload() kitsu.MessagePayload {
	item := kitsu.MessagePayload{}
	item.Project.ID = "p1"
	item.Task.ID = "task-lay-1"
	item.TaskType.ID = "tt-lay"
	item.TaskType.Name = "Layout"
	item.Entity.ID = "shot-1"
	item.Entity.Name = "sq010_sh001"
	item.EntityType.EntityType.Name = "Shot"
	item.LatestComment.Comment.ID = "comment-real-1"
	item.LatestComment.Comment.Text = "| version | v003 |\n| family | matchmove |\n| name | camera |\n"
	item.LatestComment.Author.Person.ID = "person-author"
	item.LatestComment.Author.Person.FullName = "Layout Artist"
	return item
}

func handoffReceiverPayload(assigneeIDs ...string) kitsu.MessagePayload {
	item := kitsu.MessagePayload{}
	item.Project.ID = "p1"
	item.Task.ID = "task-comp-1"
	item.TaskType.ID = "tt-comp"
	item.TaskType.Name = "Compositing"
	item.Entity.ID = "shot-1"
	item.Entity.Name = "sq010_sh001"
	for _, id := range assigneeIDs {
		item.Assignees = append(item.Assignees, kitsu.Person{ID: id})
	}
	return item
}

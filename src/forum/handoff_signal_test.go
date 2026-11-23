package forum

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"app/src/model"
)

func handoffEventFixture() model.HandoffEvent {
	return model.HandoffEvent{
		CommentID:          "comment-1",
		ProductionID:       "prod-1",
		SourceTaskID:       "task-lay-1",
		SourceTaskTypeName: "Layout",
		SourceAuthorName:   "Layout Artist",
		EntityName:         "sh002",
		ProductName:        "camera",
		ProductVersion:     "v003",
		NotifyTaskID:       "task-1",
		NotifyTaskTypeName: "Compositing",
	}
}

// A receiving task without a post gets one opened for the delivery, and the
// signal is queued behind the creation.
func TestPlanHandoffOpensThreadForTaskWithoutPost(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedForumTask(t, db, "", "s-wip")

	queued, err := planner.PlanHandoff(taskInput("s-wip", model.ForumRoleWIP, "person-1"), stubCard(), handoffEventFixture(), []string{"person-1"}, json.RawMessage(`[]`))
	if err != nil || !queued {
		t.Fatalf("queued=%v err=%v", queued, err)
	}
	rows := outboxRows(t, db)
	if len(rows) != 2 || rows[0].Kind != model.ForumOutboxCreateThread || rows[1].Kind != model.ForumOutboxPostMessage {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[1].DedupeKey != "handoff:comment-1" {
		t.Fatalf("dedupe = %q", rows[1].DedupeKey)
	}
	var payload model.ForumPostMessagePayload
	if err := json.Unmarshal([]byte(rows[1].Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Intent != model.ForumIntentHandoff || payload.HandoffProductName != "camera" || payload.HandoffProductVersion != "v003" ||
		payload.HandoffSourceTaskType != "Layout" || payload.HandoffSourceAuthor != "Layout Artist" ||
		len(payload.Mentions) != 1 || payload.Mentions[0] != "person-1" {
		t.Fatalf("payload = %+v", payload)
	}
}

// A task that already has a post only gains the signal message.
func TestPlanHandoffUsesExistingPost(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	seedForumTask(t, db, "thread-1", "s-wip")

	queued, err := planner.PlanHandoff(taskInput("s-wip", model.ForumRoleWIP), stubCard(), handoffEventFixture(), nil, nil)
	if err != nil || !queued {
		t.Fatalf("queued=%v err=%v", queued, err)
	}
	rows := outboxRows(t, db)
	if len(rows) != 1 || rows[0].Kind != model.ForumOutboxPostMessage {
		t.Fatalf("rows = %+v", rows)
	}
}

// A task the ledger has not recorded yet is skipped, so the caller leaves
// the event pending instead of losing it.
func TestPlanHandoffSkipsUnrecordedTask(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)

	queued, err := planner.PlanHandoff(taskInput("s-wip", model.ForumRoleWIP), stubCard(), handoffEventFixture(), nil, nil)
	if err != nil || queued {
		t.Fatalf("queued=%v err=%v", queued, err)
	}
	if rows := outboxRows(t, db); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

// The executor posts the handoff signal into the thread with its mentions
// and the pre-built reply buttons attached.
func TestExecutorPostsHandoffSignalWithButtons(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wip")
	components := `[{"type":1,"components":[{"type":2,"style":3,"label":"OK","custom_id":"handoff:ok:comment-1","disabled":true}]}]`
	payload, err := json.Marshal(model.ForumPostMessagePayload{
		Intent:                model.ForumIntentHandoff,
		Mentions:              []string{"person-1"},
		EntityType:            "Shot",
		ParentName:            "sq010",
		EntityName:            "sh002",
		TaskType:              "Compositing",
		HandoffProductName:    "camera",
		HandoffProductVersion: "v003",
		HandoffSourceTaskType: "Layout",
		HandoffSourceAuthor:   "Layout Artist",
		Components:            json.RawMessage(components),
	})
	if err != nil {
		t.Fatal(err)
	}
	enqueue(t, db, model.ForumOutbox{
		TaskID:    "task-1",
		Kind:      model.ForumOutboxPostMessage,
		DedupeKey: "handoff:comment-1",
		Payload:   string(payload),
		State:     model.ForumOutboxPending,
	})
	executor, fake := newExecutor(t, db)
	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("drain: %v", err)
	}
	var posted map[string]any
	for _, req := range fake.snapshot() {
		if req.Method == "POST" && strings.HasSuffix(req.Path, "/messages") {
			posted = req.Body
		}
	}
	if posted == nil {
		t.Fatal("no message posted")
	}
	if content, _ := posted["content"].(string); !strings.Contains(content, "<@111111111111111111>") {
		t.Fatalf("content = %v", posted["content"])
	}
	embeds, _ := posted["embeds"].([]any)
	if len(embeds) != 1 {
		t.Fatalf("embeds = %v", posted["embeds"])
	}
	embed, _ := embeds[0].(map[string]any)
	if desc, _ := embed["description"].(string); !strings.Contains(desc, "素材の受け渡し") || !strings.Contains(desc, "camera") {
		t.Fatalf("description = %v", embed["description"])
	}
	rows, _ := posted["components"].([]any)
	if len(rows) != 1 {
		t.Fatalf("components = %v", posted["components"])
	}
}

// Handoff recipients: the dispatcher's pinned people win; without a pinned
// set the receiving task's current assignees are mentioned.
func TestRecipientsHandoffIntent(t *testing.T) {
	db := newForumDB(t)
	seedRecipientTask(t, db, "person-1", "person-2")
	recipients := commentRecipients(db, false)

	users, _, err := recipients.ForTask("task-1", model.ForumPostMessagePayload{
		Intent:   model.ForumIntentHandoff,
		Mentions: []string{"person-9"},
	})
	if err != nil || len(users) != 1 || users[0] != "d-person-9" {
		t.Fatalf("pinned: users=%v err=%v", users, err)
	}
	users, _, err = recipients.ForTask("task-1", model.ForumPostMessagePayload{Intent: model.ForumIntentHandoff})
	if err != nil || len(users) != 2 {
		t.Fatalf("assignees: users=%v err=%v", users, err)
	}
}

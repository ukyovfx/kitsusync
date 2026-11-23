package forum

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
)

// emptyTemplateCard is the card the preset template renderer produces for
// a non-trigger status when the template files cannot be read: an embed
// with a color and nothing else. Discord rejects a thread opened with it
// (400 Invalid Form Body), so creation must never carry it.
func emptyTemplateCard(kitsu.MessagePayload) (discord.Payload, error) {
	return discord.Payload{Embeds: []discord.Embed{{
		Color:  6583435,
		Author: discord.EmbedAuthor{Name: ""},
		Footer: discord.EmbedFooter{Text: ""},
	}}}, nil
}

func wipPayload() kitsu.MessagePayload {
	var payload kitsu.MessagePayload
	payload.TaskType.TaskType.Name = "Compositing"
	payload.TaskStatus.TaskStatus.Name = "Work In Progress"
	payload.TaskStatus.TaskStatus.ShortName = "WIP"
	payload.Entity.Entity.Name = "sh002"
	payload.Parent.Entity.Name = "sq010"
	payload.EntityType.EntityType.Name = "Shot"
	payload.Assignees = []kitsu.Person{{FullName: "山田 太郎"}}
	return payload
}

// The fallback card carries the task's current data — task type, shot
// name, status and assignee — in the standard card shape.
func TestRenderCardFallsBackWhenRenderedCardIsEmpty(t *testing.T) {
	raw, err := RenderCard(wipPayload(), emptyTemplateCard)
	if err != nil {
		t.Fatalf("RenderCard: %v", err)
	}
	var card discord.Payload
	if err := json.Unmarshal(raw, &card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	if CardEmbedEmpty(card) {
		t.Fatalf("fallback card is still empty: %s", raw)
	}
	if len(card.Embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(card.Embeds))
	}
	embed := card.Embeds[0]
	if embed.Author.Name != "Compositing" {
		t.Errorf("author = %q, want the task type", embed.Author.Name)
	}
	if embed.Title != "Shot / sq010 - sh002" {
		t.Errorf("title = %q, want the shot heading", embed.Title)
	}
	for _, want := range []string{"ステータス：work in progress", "担当：山田 太郎"} {
		if !strings.Contains(embed.Description, want) {
			t.Errorf("description %q missing %q", embed.Description, want)
		}
	}
	if card.Content != "" {
		t.Errorf("content = %q, want the silent first message", card.Content)
	}
}

// A rendered card with real content is passed through untouched.
func TestRenderCardKeepsRenderedCard(t *testing.T) {
	build := func(kitsu.MessagePayload) (discord.Payload, error) {
		return discord.Payload{Embeds: []discord.Embed{{Description: "rendered"}}}, nil
	}
	raw, err := RenderCard(wipPayload(), build)
	if err != nil {
		t.Fatalf("RenderCard: %v", err)
	}
	var card discord.Payload
	if err := json.Unmarshal(raw, &card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	if len(card.Embeds) != 1 || card.Embeds[0].Description != "rendered" {
		t.Fatalf("card = %s, want the rendered card unchanged", raw)
	}
}

// Regression (F4): a @mention relay to a WIP task with no post opens the
// post (fix C-1). The queued creation payload must carry a valid card
// even when the renderer produced the empty template embed, or Discord
// rejects the creation and the mention is lost.
func TestPlannerMentionRelayQueuesValidCardForWIPTask(t *testing.T) {
	db := newForumDB(t)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	planner := testPlanner(db, now)
	if _, err := planner.PlanTask(taskInput("s-wip", model.ForumRoleWIP, "person-1"), stubCard()); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	card := func() (json.RawMessage, error) {
		return RenderCard(wipPayload(), emptyTemplateCard)
	}
	planner.Now = now.Add(5 * time.Minute)
	input := taskInput("s-wip", model.ForumRoleWIP, "person-1")
	input.CommentDate = "2026-11-15T09:04:00.000Z"
	input.Comment = commentInput("comment-1", "確認お願いします", "person-9")
	if _, err := planner.PlanTask(input, card); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	var createRow model.ForumOutbox
	if err := db.Where("kind = ?", model.ForumOutboxCreateThread).First(&createRow).Error; err != nil {
		t.Fatalf("find create row: %v", err)
	}
	var createPayload model.ForumCreateThreadPayload
	if err := json.Unmarshal([]byte(createRow.Payload), &createPayload); err != nil {
		t.Fatalf("decode create payload: %v", err)
	}
	var queued discord.Payload
	if err := json.Unmarshal(createPayload.Card, &queued); err != nil {
		t.Fatalf("decode queued card: %v", err)
	}
	if CardEmbedEmpty(queued) {
		t.Fatalf("queued card is empty and Discord would reject the creation: %s", createPayload.Card)
	}
	embed := queued.Embeds[0]
	if embed.Author.Name != "Compositing" || embed.Title != "Shot / sq010 - sh002" {
		t.Errorf("queued card = %+v, want the fallback card for the task", embed)
	}
}

// A create row queued with an empty card (before the fallback existed)
// still opens its thread: the executor substitutes a minimal valid card
// instead of letting Discord reject the creation.
func TestExecutorCreatesThreadWithFallbackForEmptyQueuedCard(t *testing.T) {
	db := newForumDB(t)
	seedForumSettings(t, db)
	seedForumTask(t, db, "", "s-wip")
	executor, fake := newExecutor(t, db)

	payload, _ := json.Marshal(model.ForumCreateThreadPayload{
		ForumID:        "forum-1",
		Name:           "Compositing sq010 sh002",
		Role:           model.ForumRoleWIP,
		ArchiveMinutes: model.ForumArchiveOtherMinutes,
		Card:           json.RawMessage(`{"embeds":[{"color":6583435,"author":{"name":""},"footer":{"text":""}}]}`),
	})
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:    "task-1",
		Kind:      model.ForumOutboxCreateThread,
		DedupeKey: model.ForumCreateThreadDedupeKey("task-1", 0),
		Payload:   string(payload),
		State:     model.ForumOutboxPending,
		NotBefore: executor.Now(),
		CreatedAt: executor.Now(),
	})
	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	done, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if done.State != model.ForumOutboxDone {
		t.Fatalf("row state = %q, want done", done.State)
	}
	requests := fake.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	message, _ := requests[0].Body["message"].(map[string]any)
	embeds, _ := message["embeds"].([]any)
	if len(embeds) != 1 {
		t.Fatalf("embeds = %v, want one fallback embed", message["embeds"])
	}
	first, _ := embeds[0].(map[string]any)
	if first["title"] != "Compositing sq010 sh002" {
		t.Errorf("fallback embed = %v, want the task title", first)
	}
}

package main

import (
	"strings"
	"testing"
	"time"

	"app/src/api/discord"
	"app/src/model"

	"gorm.io/gorm"
)

// handoffReplyRowsFor is what the digest or the DM sends for these events.
func handoffReplyRowsFor(events []model.HandoffEvent) []discord.ActionRow {
	return handoffReplyRows(events, "ja")
}

// A digest keeps one reply row per handoff, so the configured cap is what
// keeps the card inside Discord's five-rows-per-message limit.
func TestHandoffDigestReplyRowsFitTheMessageLimit(t *testing.T) {
	if handoffDigestMaxItems > 5 {
		t.Fatalf("cap %d exceeds five reply rows", handoffDigestMaxItems)
	}
	events := make([]model.HandoffEvent, 0, handoffDigestMaxItems)
	for i := 0; i < handoffDigestMaxItems; i++ {
		events = append(events, model.HandoffEvent{CommentID: "comment-" + string(rune('a'+i))})
	}
	rows := handoffReplyRowsFor(events)
	if len(rows) != handoffDigestMaxItems || len(rows) > 5 {
		t.Fatalf("rows = %d", len(rows))
	}
	for _, row := range rows {
		if len(row.Components) != 2 || row.Components[0].Label != "OK" || row.Components[1].Label != "リテイク" {
			t.Fatalf("row = %+v", row)
		}
	}
}

// Nothing receives a press yet, so the buttons are sent inert rather than
// pretending to work: no CustomID, but the receiver still sees the options.
func TestHandoffButtonsAreInertUntilTheInteractionEndpointExists(t *testing.T) {
	if handoffRepliesWired() {
		t.Fatal("expected replies to be unwired without an interaction endpoint")
	}
	rows := handoffReplyRowsFor([]model.HandoffEvent{{CommentID: "comment-1"}})
	if len(rows) != 1 || rows[0].Components[0].CustomID != "" || rows[0].Components[1].CustomID != "" {
		t.Fatalf("rows = %+v", rows)
	}
	if got := handoffReplyCustomID("ok", "comment-1"); got != "" {
		t.Fatalf("unwired custom id = %q", got)
	}
}

// handoffEventNamed records one pending event of its own product.
func handoffEventNamed(t *testing.T, db *gorm.DB, commentID, productName string) {
	t.Helper()
	event := model.HandoffEvent{
		CommentID: commentID, ProductionID: "p1", SourceTaskID: "task-lay-1", SourceTaskTypeID: "tt-lay",
		SourceTaskTypeName: "Layout", SourceAuthorName: "Layout Artist", EntityID: "shot-1",
		EntityKind: model.ForumEntityKindShot, EntityName: "sq010", ProductType: "matchmove",
		ProductVersion: "v003", ProductName: productName, NotifyTaskID: "task-comp-1",
		NotifyTaskTypeID: "tt-comp", NotifyTaskTypeName: "Compositing",
		NotifyPersonIDs: model.JoinHandoffNotifyIDs([]string{"person-comp"}),
		FallbackUsed:    model.HandoffFallbackNone,
	}
	if _, err := model.RecordHandoffEvent(db, event); err != nil {
		t.Fatal(err)
	}
}

// handoffReplyDB seeds one Production whose receiving person maps to a Discord
// user, with the forum settings the digest needs.
func handoffReplyDB(t *testing.T, timing string) *gorm.DB {
	t.Helper()
	db := handoffNotifyDB(t, timing)
	stubHandoffIdentity(t, map[string]string{
		"person-comp":   "222222222222222222",
		"person-other":  "333333333333333333",
		"person-author": "444444444444444444",
	})
	return db
}

// A reported handoff that the receiver answers takes a reply state, and both
// reply states are terminal: a second press finds nothing to advance.
func TestHandoffReplyMovesTheEventOutOfTheDigestOnce(t *testing.T) {
	for _, state := range []string{model.HandoffEventAnswered, model.HandoffEventRetaken} {
		t.Run(state, func(t *testing.T) {
			db := handoffReplyDB(t, model.HandoffTimingMixed)
			handoffEvent(t, db, "comment-1", model.HandoffFallbackNone, "person-comp")
			event, err := model.FindHandoffEvent(db, "comment-1")
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := model.MarkHandoffNotified(db, event.ID); err != nil || !ok {
				t.Fatalf("notify: ok=%v err=%v", ok, err)
			}
			event, _ = model.FindHandoffEvent(db, "comment-1")
			ok, err := model.MarkHandoffReplied(db, event.ID, state)
			if err != nil || !ok {
				t.Fatalf("reply: ok=%v err=%v", ok, err)
			}
			if got := handoffState(t, db, "comment-1"); got != state {
				t.Fatalf("state = %q", got)
			}
			// A second press is invalid: the event already left the open states.
			if ok, err := model.MarkHandoffReplied(db, event.ID, model.HandoffEventRetaken); err != nil || ok {
				t.Fatalf("second press accepted: ok=%v err=%v", ok, err)
			}
			// An answered or retaken handoff is not listed again.
			unanswered, err := model.UnansweredHandoffEvents(db)
			if err != nil {
				t.Fatal(err)
			}
			if len(unanswered) != 0 {
				t.Fatalf("still unanswered: %d", len(unanswered))
			}
		})
	}
}

// The digest repeats an unanswered handoff every morning and drops it after
// the reply, with one reply row per listed handoff.
func TestHandoffDigestListsUntilRepliedAndDropsAfter(t *testing.T) {
	db := handoffReplyDB(t, model.HandoffTimingDigest)
	fixture := newDMFixture(t)
	handoffEvent(t, db, "comment-1", model.HandoffFallbackNone, "person-comp")

	morning := time.Date(2026, 11, 16, 9, 0, 0, 0, jst)
	if sent, err := sendForumMorningDigests(digestTestConfig(), db, morning, "2026-11-16"); err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	sent, seen := fixture.body()
	if !seen {
		t.Fatal("no digest payload")
	}
	if len(sent.Embeds) != 1 || !strings.Contains(sent.Embeds[0].Description, "素材の受け渡し: 1件") {
		t.Fatalf("card = %+v", sent)
	}
	if len(sent.Components) != 1 || sent.Components[0].Components[0].Label != "OK" {
		t.Fatalf("components = %+v", sent.Components)
	}

	event, err := model.FindHandoffEvent(db, "comment-1")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := model.MarkHandoffReplied(db, event.ID, model.HandoffEventAnswered); err != nil || !ok {
		t.Fatalf("reply: ok=%v err=%v", ok, err)
	}

	if sent, err := sendForumMorningDigests(digestTestConfig(), db, morning.AddDate(0, 0, 1), "2026-11-17"); err != nil || sent != 0 {
		t.Fatalf("answered handoff re-listed: sent=%d err=%v", sent, err)
	}
	if fixture.sendsFor("222222222222222222") != 1 {
		t.Fatalf("sends = %d", fixture.sendsFor("222222222222222222"))
	}
}

// A capped card counts what it left out, and keeps one reply row per listed
// handoff only.
func TestHandoffDigestCountsTheHandoffsItLeftOut(t *testing.T) {
	db := handoffReplyDB(t, model.HandoffTimingDigest)
	fixture := newDMFixture(t)
	// Different products: the same product on the same receiving task would
	// supersede the older one instead of queueing.
	for _, product := range []string{"camera", "model", "rig", "clone", "anim", "fx", "light"} {
		handoffEventNamed(t, db, "comment-"+product, product)
	}

	morning := time.Date(2026, 11, 16, 9, 0, 0, 0, jst)
	if sent, err := sendForumMorningDigests(digestTestConfig(), db, morning, "2026-11-16"); err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	card, seen := fixture.body()
	if !seen {
		t.Fatal("no digest payload")
	}
	if len(card.Embeds) != 1 {
		t.Fatalf("embeds = %+v", card.Embeds)
	}
	if !strings.Contains(card.Embeds[0].Description, "素材の受け渡し: 5件") {
		t.Fatalf("card = %q", card.Embeds[0].Description)
	}
	if !strings.Contains(card.Embeds[0].Description, "他に未返事が 2 件あります") {
		t.Fatalf("missing leftover count in %q", card.Embeds[0].Description)
	}
	if len(card.Components) != 5 {
		t.Fatalf("rows = %d, want 5", len(card.Components))
	}
}

// The immediate DM's first line names the shot or asset as well as the
// product, so the receiver knows what arrived.
func TestHandoffImmediateDMCardNamesTheEntity(t *testing.T) {
	card := renderHandoffCard("", model.HandoffEvent{EntityName: "sq010_sh001", ProductName: "camera", ProductVersion: "v003"}, "ja")
	if !strings.Contains(card, "🔁 素材の受け渡し — sq010_sh001 / camera") {
		t.Fatalf("card = %q", card)
	}
}

// The immediate DM carries its own reply row under the card.
func TestHandoffImmediateDMCarriesReplyButtons(t *testing.T) {
	db := handoffReplyDB(t, model.HandoffTimingMixed)
	fixture := newDMFixture(t)
	handoffEvent(t, db, "comment-1", model.HandoffFallbackNone, "person-comp")

	if sent, err := sendImmediateHandoffs(digestTestConfig(), db); err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	card, seen := fixture.body()
	if !seen || len(card.Components) != 1 || len(card.Components[0].Components) != 2 {
		t.Fatalf("components = %+v", card.Components)
	}
}

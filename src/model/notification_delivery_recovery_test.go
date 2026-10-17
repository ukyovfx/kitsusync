package model

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"
)

// TestQuarantineInflightNotificationDeliveriesOnRestart reproduces a process
// restart mid-dispatch. An in-flight row must become a durable unknown so it is
// never silently resent, interrupted external mutations must stay fenced for a
// human, and an idempotent message-deletion claim may be released for retry.
func TestQuarantineInflightNotificationDeliveriesOnRestart(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)

	event, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now().UTC())
	if err != nil || !ok || attempt == nil {
		t.Fatalf("expected an in-flight claim, ok=%v attempt=%v err=%v", ok, attempt, err)
	}
	if err := db.Model(&NotificationDeliveryEvent{}).Where("event_id = ?", event.EventID).
		Updates(map[string]any{"prior_cleanup_pending": true, "prior_cleanup_in_flight": true}).Error; err != nil {
		t.Fatal(err)
	}
	// Prepare already created the write-lock row for production-1.
	if err := db.Model(&ProductionNotificationWriteLock{}).Where("production_id = ?", "production-1").
		Update("mutation_state", NotificationMutationDeleting).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationWriteLock{ProductionID: "production-2", MutationState: NotificationMutationReordering}).Error; err != nil {
		t.Fatal(err)
	}

	if err := QuarantineInflightNotificationDeliveries(db); err != nil {
		t.Fatal(err)
	}

	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Disposition != DeliveryUnknown || stored.FailureClass != "process_restart_during_delivery" {
		t.Fatalf("in-flight delivery must quarantine as unknown, got %q/%q", stored.Disposition, stored.FailureClass)
	}
	if stored.PriorCleanupInFlight {
		t.Fatal("an interrupted message-deletion claim must be released for idempotent retry")
	}
	if !stored.PriorCleanupPending {
		t.Fatal("releasing the in-flight flag must not drop the pending cleanup request")
	}

	var deleting ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", "production-1").First(&deleting).Error; err != nil {
		t.Fatal(err)
	}
	if deleting.MutationState != NotificationMutationDeletionHelp || deleting.MutationAt != nil {
		t.Fatalf("interrupted deletion must require human help, got state=%q at=%v", deleting.MutationState, deleting.MutationAt)
	}
	var reordering ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", "production-2").First(&reordering).Error; err != nil {
		t.Fatal(err)
	}
	if reordering.MutationState != NotificationMutationReorderHelp {
		t.Fatalf("interrupted reorder must require human help, got %q", reordering.MutationState)
	}

	if err := QuarantineInflightNotificationDeliveries(db); err != nil {
		t.Fatalf("restart quarantine must be safely repeatable: %v", err)
	}
}

// TestListPendingNotificationDeliveriesQueueOrderAndLimit pins the worker
// queue contract: only pending/retryable rows, oldest-first by created_at then
// id, and a positive limit bounds the page while zero means unbounded.
func TestListPendingNotificationDeliveriesQueueOrderAndLimit(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	base := time.Now().UTC().Add(-time.Hour)

	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		intent := notificationIntent()
		intent.CommentID = fmt.Sprintf("comment-%d", i)
		event, err := PrepareNotificationDelivery(db, intent)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, event.EventID)
		if err := db.Model(&NotificationDeliveryEvent{}).Where("event_id = ?", event.EventID).
			UpdateColumn("created_at", base.Add(time.Duration(i)*time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, spec := range []struct {
		index       int
		disposition string
	}{{1, DeliveryRetryable}, {3, DeliveryDelivered}, {4, DeliveryRetryable}} {
		if err := db.Model(&NotificationDeliveryEvent{}).Where("event_id = ?", ids[spec.index]).
			UpdateColumns(map[string]any{"disposition": spec.disposition, "failure_class": "rate_limited"}).Error; err != nil {
			t.Fatal(err)
		}
	}

	// Rows 0 and 2 are pending, 1 and 4 are retryable, 3 is delivered.
	want := []string{ids[0], ids[1], ids[2], ids[4]}
	pending, err := ListPendingNotificationDeliveries(db, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != len(want) {
		t.Fatalf("queue must expose only pending/retryable rows, got %d", len(pending))
	}
	for i, id := range want {
		if pending[i].EventID != id {
			t.Fatalf("queue order mismatch at %d: got %q want %q", i, pending[i].EventID, id)
		}
		if pending[i].EventID == ids[3] {
			t.Fatal("a delivered row must never appear on the queue")
		}
	}

	limited, err := ListPendingNotificationDeliveries(db, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 || limited[0].EventID != ids[0] || limited[1].EventID != ids[1] {
		t.Fatalf("a positive limit must return the oldest rows, got %+v", limited)
	}

	t.Run("empty", func(t *testing.T) {
		empty, err := ListPendingNotificationDeliveries(notificationDeliveryTestDB(t), 10)
		if err != nil || len(empty) != 0 {
			t.Fatalf("an empty queue must return no rows, rows=%d err=%v", len(empty), err)
		}
	})
}

// TestSetUnstartedNotificationDeliveryDispositionGuards pins the pre-attempt
// quarantine path used when an intent is rejected before any Discord send.
func TestSetUnstartedNotificationDeliveryDispositionGuards(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)

	if err := SetUnstartedNotificationDeliveryDisposition(db, "any", DeliveryPending, "x"); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("unstarted disposition must reject a non-terminal target, got %v", err)
	}
	if err := SetUnstartedNotificationDeliveryDisposition(db, "", DeliveryTerminalFailure, "x"); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("unstarted disposition must reject an empty event id, got %v", err)
	}
	if err := SetUnstartedNotificationDeliveryDisposition(db, "missing", DeliveryTerminalFailure, "x"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unstarted disposition must fail closed for an unknown event, got %v", err)
	}

	event, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	if err := SetUnstartedNotificationDeliveryDisposition(db, event.EventID, DeliveryTerminalFailure, "consent_revoked"); err != nil {
		t.Fatal(err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Disposition != DeliveryTerminalFailure || stored.FailureClass != "consent_revoked" {
		t.Fatalf("terminal quarantine must persist with its class, got %q/%q", stored.Disposition, stored.FailureClass)
	}
	if err := SetUnstartedNotificationDeliveryDisposition(db, event.EventID, DeliveryUnknown, "again"); !errors.Is(err, ErrDeliveryAttemptConflict) {
		t.Fatalf("the pending-only path must not rewrite a terminal row, got %v", err)
	}

	retryIntent := notificationIntent()
	retryIntent.CommentID = "comment-retry"
	retryEvent, err := PrepareNotificationDelivery(db, retryIntent)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetUnstartedNotificationDeliveryDisposition(db, retryEvent.EventID, DeliveryUnknown, "intake_unknown"); err != nil {
		t.Fatalf("unknown must be an allowed unstarted disposition: %v", err)
	}
}

// TestRecordManagedChannelDeletionFailureTransitions covers the tombstone
// failure recorder that admin teardown depends on and that previously had no
// direct regression test.
func TestRecordManagedChannelDeletionFailureTransitions(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)

	if err := RecordManagedChannelDeletionFailure(db, 0, "class"); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("deletion failure must reject an empty webhook row, got %v", err)
	}
	if err := RecordManagedChannelDeletionFailure(db, 1, "  "); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("deletion failure must reject an empty class, got %v", err)
	}
	if err := RecordManagedChannelDeletionFailure(db, 42, "class"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deletion failure must fail closed without a tombstone, got %v", err)
	}

	tombstone := ManagedChannelDeletion{
		WebhookRowID: 1, ProductionID: "production-1", GuildID: "guild-1", ChannelID: "channel-1",
		DiscordWebhookID: "webhook-1", Disposition: ChannelDeletionPending,
	}
	if err := db.Create(&tombstone).Error; err != nil {
		t.Fatal(err)
	}
	if err := RecordManagedChannelDeletionFailure(db, 1, "delete_rejected"); err != nil {
		t.Fatal(err)
	}
	var stored ManagedChannelDeletion
	if err := db.Where("webhook_row_id = ?", 1).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Disposition != ChannelDeletionNeedsHelp || stored.FailureClass != "delete_rejected" {
		t.Fatalf("a failed deletion must escalate to needs_help, got %q/%q", stored.Disposition, stored.FailureClass)
	}
	if err := RecordManagedChannelDeletionFailure(db, 1, "retry_rejected"); err != nil {
		t.Fatalf("a needs_help tombstone must accept a repeat failure record: %v", err)
	}
	if err := db.Where("webhook_row_id = ?", 1).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.FailureClass != "retry_rejected" {
		t.Fatalf("the latest failure class must be kept, got %q", stored.FailureClass)
	}

	if err := db.Model(&ManagedChannelDeletion{}).Where("webhook_row_id = ?", 1).Update("disposition", ChannelDeletionComplete).Error; err != nil {
		t.Fatal(err)
	}
	if err := RecordManagedChannelDeletionFailure(db, 1, "late"); !errors.Is(err, ErrChannelDeletionConflict) {
		t.Fatalf("a completed tombstone must reject further failure records, got %v", err)
	}
}

// TestCanDeleteProjectWebhookWithoutRemovingLastRoute covers the guard that
// stops an admin action from orphaning a production's only notification route.
func TestCanDeleteProjectWebhookWithoutRemovingLastRoute(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)

	if _, err := CanDeleteProjectWebhookWithoutRemovingLastRoute(db, "", 1); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("guard must reject an empty production, got %v", err)
	}
	if _, err := CanDeleteProjectWebhookWithoutRemovingLastRoute(db, "production-1", 0); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("guard must reject an empty webhook row, got %v", err)
	}

	allowed, err := CanDeleteProjectWebhookWithoutRemovingLastRoute(db, "production-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("the only route's webhook must not be deletable")
	}

	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-2", DestinationWebhookID: 2, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	allowed, err = CanDeleteProjectWebhookWithoutRemovingLastRoute(db, "production-1", 1)
	if err != nil || !allowed {
		t.Fatalf("a webhook with sibling routes must be deletable, allowed=%v err=%v", allowed, err)
	}
	allowed, err = CanDeleteProjectWebhookWithoutRemovingLastRoute(db, "production-1", 99)
	if err != nil || !allowed {
		t.Fatalf("a webhook with no routes must be deletable, allowed=%v err=%v", allowed, err)
	}
}

// TestNotificationWebhookIDStrictParsing pins the strict destination parser:
// only canonical https://discord.com/api/webhooks/<id>/<token> URLs are accepted.
func TestNotificationWebhookIDStrictParsing(t *testing.T) {
	cases := map[string]string{
		"https://discord.com/api/webhooks/123/token":       "123",
		"https://discord.com/api/webhooks/123/token/":      "123",
		"  https://discord.com/api/webhooks/123/token  ":   "123",
		"http://discord.com/api/webhooks/123/token":        "",
		"https://discordapp.com/api/webhooks/123/token":    "",
		"https://discord.com.evil.test/api/webhooks/1/t":   "",
		"https://user@discord.com/api/webhooks/123/token":  "",
		"https://discord.com/api/webhooks/123/token?x=1":   "",
		"https://discord.com/api/webhooks/123/token#frag":  "",
		"https://discord.com/api/webhooks/123":             "",
		"https://discord.com/api/webhooks/123/token/extra": "",
		"https://discord.com/channels/123/token":           "",
		"":                                                 "",
	}
	for raw, want := range cases {
		if got := notificationWebhookID(raw); got != want {
			t.Fatalf("notificationWebhookID(%q)=%q want %q", raw, got, want)
		}
	}
}

// TestNotificationEventTimeParsingIsFailClosed pins the multi-layout timestamp
// parsing that decides whether a newer Kitsu observation supersedes an older
// known-rejected delivery. An unparseable observation must not supersede.
func TestNotificationEventTimeParsingIsFailClosed(t *testing.T) {
	got, ok := notificationEventTime("2026-10-10T12:00:00Z", "")
	if !ok || !got.Equal(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("rfc3339 timestamp must parse, got %v ok=%v", got, ok)
	}
	got, ok = notificationEventTime("2026-10-10T12:00:00Z", "2026-10-10T12:00:01Z")
	if !ok || !got.Equal(time.Date(2026, 10, 10, 12, 0, 1, 0, time.UTC)) {
		t.Fatalf("the latest observation must win, got %v ok=%v", got, ok)
	}
	got, ok = notificationEventTime("", "2026-10-10 12:00:05.123456789")
	if !ok || !got.Equal(time.Date(2026, 10, 10, 12, 0, 5, 123456789, time.UTC)) {
		t.Fatalf("the legacy space-separated layout must parse, got %v ok=%v", got, ok)
	}
	got, ok = notificationEventTime("2026-10-10T12:00:00Z", "not-a-timestamp")
	if !ok || !got.Equal(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("an unparseable side must be ignored while the valid side survives, got %v ok=%v", got, ok)
	}
	if _, ok := notificationEventTime("", "   "); ok {
		t.Fatal("empty timestamps must fail closed")
	}
	if _, ok := notificationEventTime("not-a-timestamp", "also not one"); ok {
		t.Fatal("unparseable timestamps must fail closed")
	}
}

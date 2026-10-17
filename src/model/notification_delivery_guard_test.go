package model

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

// assertDeliveryGates pins the three teardown/mutation gates at once so every
// scenario states exactly which row state blocks and which does not.
func assertDeliveryGates(t *testing.T, db *gorm.DB, productionID string, inFlight, unresolved, deletion error) {
	t.Helper()
	if err := EnsureNoInFlightNotificationDeliveries(db, productionID); !errors.Is(err, inFlight) {
		t.Fatalf("in-flight gate = %v, want %v", err, inFlight)
	}
	if err := EnsureNoUnresolvedNotificationDeliveries(db, productionID); !errors.Is(err, unresolved) {
		t.Fatalf("unresolved gate = %v, want %v", err, unresolved)
	}
	if err := EnsureNoUnresolvedManagedChannelDeletions(db, productionID); !errors.Is(err, deletion) {
		t.Fatalf("tombstone gate = %v, want %v", err, deletion)
	}
}

// TestNotificationDeliveryMutationGatesCountOnlyBlockingRows covers the guards
// that fence production teardown and destination mutation. They are the last
// line of defence against losing a delivery, so each disposition must be
// classified deliberately.
func TestNotificationDeliveryMutationGatesCountOnlyBlockingRows(t *testing.T) {
	const productionID = "production-1"

	t.Run("idle", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		assertDeliveryGates(t, db, productionID, nil, nil, nil)
	})

	t.Run("pending", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if _, err := PrepareNotificationDelivery(db, notificationIntent()); err != nil {
			t.Fatal(err)
		}
		// Pending is unresolved but not in flight: teardown must wait for it,
		// yet the in-flight gate alone must not be used to detect it.
		assertDeliveryGates(t, db, productionID, nil, ErrNotificationDeliveryInFlight, nil)
	})

	t.Run("in flight", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		event, err := PrepareNotificationDelivery(db, notificationIntent())
		if err != nil {
			t.Fatal(err)
		}
		if _, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now().UTC()); err != nil || !ok {
			t.Fatalf("claim must succeed, ok=%v err=%v", ok, err)
		}
		assertDeliveryGates(t, db, productionID, ErrNotificationDeliveryInFlight, ErrNotificationDeliveryInFlight, nil)
	})

	t.Run("unknown", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		event, err := PrepareNotificationDelivery(db, notificationIntent())
		if err != nil {
			t.Fatal(err)
		}
		attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now().UTC())
		if err != nil || !ok {
			t.Fatalf("claim must succeed, ok=%v err=%v", ok, err)
		}
		if err := SetNotificationDeliveryDisposition(db, *attempt, DeliveryUnknown, "transport_error"); err != nil {
			t.Fatal(err)
		}
		// Unknown means "Discord may have accepted it": nothing may claim it
		// again, but teardown must still wait for an explicit resolution.
		assertDeliveryGates(t, db, productionID, nil, ErrNotificationDeliveryInFlight, nil)
	})

	t.Run("terminal failure", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		event, err := PrepareNotificationDelivery(db, notificationIntent())
		if err != nil {
			t.Fatal(err)
		}
		if err := SetUnstartedNotificationDeliveryDisposition(db, event.EventID, DeliveryTerminalFailure, "production_removed"); err != nil {
			t.Fatal(err)
		}
		assertDeliveryGates(t, db, productionID, nil, nil, nil)
	})

	t.Run("another production", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		other := NotificationDeliveryEvent{EventID: "other-production-event", TaskID: "task-2", ProductionID: "production-2", Disposition: DeliveryInFlight}
		if err := db.Create(&other).Error; err != nil {
			t.Fatal(err)
		}
		// Gates are per-production; another production's in-flight row must
		// never block this one.
		assertDeliveryGates(t, db, productionID, nil, nil, nil)
	})
}

func TestEnsureNoUnresolvedManagedChannelDeletionsIgnoresOnlyComplete(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	const productionID = "production-1"

	tombstone := func(webhookRowID uint, disposition string) {
		t.Helper()
		row := ManagedChannelDeletion{WebhookRowID: webhookRowID, ProductionID: productionID, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1", Disposition: disposition}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	tombstone(1, ChannelDeletionComplete)
	if err := EnsureNoUnresolvedManagedChannelDeletions(db, productionID); err != nil {
		t.Fatalf("a completed tombstone must not block teardown: %v", err)
	}
	for i, disposition := range []string{ChannelDeletionPending, ChannelDeletionNeedsHelp} {
		tombstone(uint(10+i), disposition)
		if err := EnsureNoUnresolvedManagedChannelDeletions(db, productionID); !errors.Is(err, ErrChannelDeletionConflict) {
			t.Fatalf("a %s tombstone must block teardown, got %v", disposition, err)
		}
	}
}

// TestEnsureProductionNotificationMutationIdleTokenGate pins that only a
// crashed reorder/delete may resume, and only with its own recovery token. A
// needs-help state must never auto-resume.
func TestEnsureProductionNotificationMutationIdleTokenGate(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	const productionID = "production-1"

	if err := EnsureProductionNotificationMutationIdle(db, productionID, ""); err != nil {
		t.Fatalf("a production with no write lock must be idle: %v", err)
	}
	if err := db.Create(&ProductionNotificationWriteLock{ProductionID: productionID, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureProductionNotificationMutationIdle(db, productionID, ""); err != nil {
		t.Fatalf("an empty mutation state must be idle: %v", err)
	}

	for _, state := range []string{NotificationMutationReordering, NotificationMutationDeleting} {
		if err := db.Model(&ProductionNotificationWriteLock{}).Where("production_id = ?", productionID).Updates(map[string]any{"mutation_state": state, "mutation_token": "token-1"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := EnsureProductionNotificationMutationIdle(db, productionID, "token-1"); err != nil {
			t.Fatalf("%s must be resumable with its own token: %v", state, err)
		}
		for _, allowedToken := range []string{"", "token-2"} {
			if err := EnsureProductionNotificationMutationIdle(db, productionID, allowedToken); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
				t.Fatalf("%s with allowed token %q must require recovery, got %v", state, allowedToken, err)
			}
		}
	}

	for _, state := range []string{NotificationMutationDeletionHelp, NotificationMutationReorderHelp} {
		if err := db.Model(&ProductionNotificationWriteLock{}).Where("production_id = ?", productionID).Updates(map[string]any{"mutation_state": state, "mutation_token": "token-1"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := EnsureProductionNotificationMutationIdle(db, productionID, "token-1"); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
			t.Fatalf("%s must never auto-resume even with its token, got %v", state, err)
		}
	}
}

// TestLatestDeliveredNotificationForDestinationPicksNewestDelivered covers the
// "replace the previous message" lookup: it must be scoped to the exact
// destination and must ignore undelivered rows.
func TestLatestDeliveredNotificationForDestinationPicksNewestDelivered(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	base := time.Now().UTC()
	identity := NotificationDestinationIdentity{WebhookRowID: 1, GuildID: "guild-1", ChannelID: "channel-1", WebhookID: "webhook-1"}
	fingerprint := notificationDestinationFingerprint(identity.GuildID, identity.ChannelID, identity.WebhookID)
	older := base.Add(-time.Hour)
	newer := base.Add(-time.Minute)

	rows := []NotificationDeliveryEvent{
		{EventID: "delivered-old", TaskID: "task-1", ProductionID: "production-1", DestinationFingerprint: fingerprint, Disposition: DeliveryDelivered, DeliveredAt: &older},
		{EventID: "pending-newest-time", TaskID: "task-1", ProductionID: "production-1", DestinationFingerprint: fingerprint, Disposition: DeliveryPending, DeliveredAt: &base},
		{EventID: "delivered-new", TaskID: "task-1", ProductionID: "production-1", DestinationFingerprint: fingerprint, Disposition: DeliveryDelivered, DeliveredAt: &newer},
		{EventID: "other-destination", TaskID: "task-1", ProductionID: "production-1", DestinationFingerprint: notificationDestinationFingerprint("guild-1", "channel-2", "webhook-2"), Disposition: DeliveryDelivered, DeliveredAt: &base},
		{EventID: "other-task", TaskID: "task-2", ProductionID: "production-1", DestinationFingerprint: fingerprint, Disposition: DeliveryDelivered, DeliveredAt: &base},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	got, err := LatestDeliveredNotificationForDestination(db, "task-1", identity)
	if err != nil || got == nil || got.EventID != "delivered-new" {
		t.Fatalf("expected the newest delivered event for the exact destination, got %+v err=%v", got, err)
	}
	if got, err := LatestDeliveredNotificationForDestination(db, "task-3", identity); err != nil || got != nil {
		t.Fatalf("an unknown task must have no prior message, got %+v err=%v", got, err)
	}
}

func TestNotificationDeliveryLookupsFailClosedOnMissingRows(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)

	if row, err := FindNotificationDeliveryByID(db, "missing-event"); err != nil || row != nil {
		t.Fatalf("an unknown event must resolve to nothing, got %+v err=%v", row, err)
	}
	if disposition, err := NotificationDeliveryDisposition(db, "missing-event"); err != nil || disposition != "" {
		t.Fatalf("an unknown event must have no disposition, got %q err=%v", disposition, err)
	}
	if row, err := FindNotificationDeliveryByID(db, "   "); !errors.Is(err, gorm.ErrInvalidData) || row != nil {
		t.Fatalf("a blank event id must be rejected, got %+v err=%v", row, err)
	}
	if row, err := FindNotificationDeliveryByID(nil, "event-1"); !errors.Is(err, gorm.ErrInvalidData) || row != nil {
		t.Fatalf("a nil database must be rejected, got %+v err=%v", row, err)
	}
	if disposition, err := NotificationDeliveryDisposition(nil, "event-1"); !errors.Is(err, gorm.ErrInvalidData) || disposition != "" {
		t.Fatalf("a nil database must be rejected, got %q err=%v", disposition, err)
	}
	if row, err := FindManagedChannelDeletion(db, 0); !errors.Is(err, gorm.ErrInvalidData) || row != nil {
		t.Fatalf("a zero webhook row id must be rejected, got %+v err=%v", row, err)
	}
	if row, err := FindManagedChannelDeletion(db, 4242); err != nil || row != nil {
		t.Fatalf("an unknown tombstone must resolve to nothing, got %+v err=%v", row, err)
	}
}

// TestQuarantineAcceptedButUnpersistedKeepsTheAttemptDurable covers the worst
// case: Discord accepted a message but the checkpoint failed. The attempt must
// be retained, never become retryable, and never be reopened.
func TestQuarantineAcceptedButUnpersistedKeepsTheAttemptDurable(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	event, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now().UTC())
	if err != nil || !ok || attempt == nil {
		t.Fatalf("claim must succeed, ok=%v attempt=%v err=%v", ok, attempt, err)
	}

	if err := QuarantineAcceptedButUnpersisted(db, DeliveryAttempt{ID: "other-attempt", Event: *event}); !errors.Is(err, ErrDeliveryAttemptConflict) {
		t.Fatalf("quarantine must be bound to the claimed attempt, got %v", err)
	}
	if disposition, err := NotificationDeliveryDisposition(db, event.EventID); err != nil || disposition != DeliveryInFlight {
		t.Fatalf("a rejected quarantine must leave the claim in flight, got %q err=%v", disposition, err)
	}

	if err := QuarantineAcceptedButUnpersisted(db, *attempt); err != nil {
		t.Fatal(err)
	}
	row, err := FindNotificationDeliveryByID(db, event.EventID)
	if err != nil || row == nil {
		t.Fatalf("the quarantined event must stay durable, got %+v err=%v", row, err)
	}
	if row.Disposition != DeliveryUnknown || row.FailureClass != "discord_accepted_checkpoint_uncommitted" {
		t.Fatalf("quarantine must record an unknown disposition and its class, got %q/%q", row.Disposition, row.FailureClass)
	}
	if row.AttemptID != attempt.ID {
		t.Fatalf("quarantine must retain the pre-send attempt, got %q want %q", row.AttemptID, attempt.ID)
	}
	pending, err := ListPendingNotificationDeliveries(db, 0)
	if err != nil || len(pending) != 0 {
		t.Fatalf("a quarantined event must never be queued again, rows=%d err=%v", len(pending), err)
	}

	if err := QuarantineAcceptedButUnpersisted(db, *attempt); !errors.Is(err, ErrDeliveryAttemptConflict) {
		t.Fatalf("a repeated quarantine must conflict, got %v", err)
	}
	if disposition, err := NotificationDeliveryDisposition(db, event.EventID); err != nil || disposition != DeliveryUnknown {
		t.Fatalf("a repeated quarantine must not change the disposition, got %q err=%v", disposition, err)
	}
}

// TestListUnresolvedNotificationDeliveriesReportsDiagnostics pins the operator
// facing diagnostic surface: unresolved deliveries, tombstones, mutation locks
// and the identity-conflict summary in one bounded result.
func TestListUnresolvedNotificationDeliveriesReportsDiagnostics(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	event, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	conflictAt := time.Now().UTC()
	conflict := NotificationDeliveryEvent{EventID: "conflict-event-abcdef1234567890", TaskID: "task-9", ProductionID: "production-1", Disposition: DeliveryTerminalFailure, IdentityConflictCount: 3, IdentityConflictAt: &conflictAt}
	if err := db.Create(&conflict).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ManagedChannelDeletion{WebhookRowID: 7, ProductionID: "production-1", GuildID: "guild-1", ChannelID: "channel-9", DiscordWebhookID: "webhook-9", Disposition: ChannelDeletionNeedsHelp, FailureClass: "discord_delete_failed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationWriteLock{ProductionID: "production-9", Revision: 1, MutationState: NotificationMutationReordering, MutationToken: "token-1"}).Error; err != nil {
		t.Fatal(err)
	}

	diagnostics, err := ListUnresolvedNotificationDeliveries(db, 0)
	if err != nil {
		t.Fatal(err)
	}
	byEventID := make(map[string]DeliveryDiagnostic, len(diagnostics))
	for _, diagnostic := range diagnostics {
		byEventID[diagnostic.EventID] = diagnostic
	}

	if got, ok := byEventID["identity-conflicts"]; !ok || got.Disposition != "identity_conflict" || got.FailureClass != "event_identity_conflict" || got.ConflictCount != 3 {
		t.Fatalf("identity conflicts must be summarised once, got %+v", got)
	}
	if got, ok := byEventID[event.EventID[:12]]; !ok || got.Disposition != DeliveryPending {
		t.Fatalf("an unresolved delivery must be reported by truncated event id, got %+v", got)
	}
	if got, ok := byEventID["delete-7"]; !ok || got.Disposition != "channel_"+ChannelDeletionNeedsHelp || got.FailureClass != "discord_delete_failed" {
		t.Fatalf("an unfinished tombstone must be reported, got %+v", got)
	}
	if got, ok := byEventID["mutation-production-9"]; !ok || got.Disposition != "mutation_"+NotificationMutationReordering || got.FailureClass != "external_order_recovery_required" {
		t.Fatalf("an active mutation lock must be reported, got %+v", got)
	}

	limited, err := ListUnresolvedNotificationDeliveries(db, 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("the diagnostic list must be bounded, rows=%d err=%v", len(limited), err)
	}
	if limited[0].EventID != "identity-conflicts" {
		t.Fatalf("the identity-conflict summary must lead the diagnostics, got %q", limited[0].EventID)
	}
}

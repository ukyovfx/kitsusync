package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func notificationDeliveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &Task{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProjectReviewerTarget{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func notificationDeliveryFileDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+filepath.ToSlash(path)+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &Task{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProjectReviewerTarget{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func notificationIntent() NotificationDeliveryIntent {
	return NotificationDeliveryIntent{
		TaskID: "task-1", ProductionID: "production-1", TaskTypeID: "task-type-1", TaskStatus: "WFA",
		TaskUpdatedAt: "2026-10-10T12:00:00Z", CommentID: "comment-1", CommentUpdatedAt: "2026-10-10T12:00:00Z",
		Payload:      []byte(`{"content":"safe synthetic message","allowed_mentions":{"parse":[]}}`),
		WebhookRowID: 1, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1",
	}
}

func notificationDeliveryFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	project := Project{KitsuProjectID: "production-1", DiscordGuildID: "guild-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{ID: 1, KitsuProjectID: "production-1", DiscordChannelID: "channel-1", WebhookURL: "https://discord.com/api/webhooks/webhook-1/token"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-1", DestinationWebhookID: 1, DestinationChannelID: "channel-1"}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestNotificationDeliveryIntentIsUniqueAndDestinationBound(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	intent := notificationIntent()
	first, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareNotificationDelivery(db, intent)
	if err != nil || second.EventID != first.EventID {
		t.Fatalf("same event should return one durable intent: second=%+v err=%v", second, err)
	}
	var count int64
	if err := db.Model(&NotificationDeliveryEvent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected exactly one unique event row, count=%d err=%v", count, err)
	}
	intent.ChannelID = "channel-2"
	if _, err := PrepareNotificationDelivery(db, intent); !errors.Is(err, ErrDeliveryIdentityConflict) {
		t.Fatalf("route change must not rebind an existing event, got %v", err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", first.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryPending {
		t.Fatalf("identity conflict must preserve the original delivery disposition: row=%+v err=%v", stored, err)
	}
	diagnostics, err := ListUnresolvedNotificationDeliveries(db, 50)
	if err != nil {
		t.Fatal(err)
	}
	foundConflict := false
	for _, diagnostic := range diagnostics {
		if diagnostic.EventID == "identity-conflicts" && diagnostic.Disposition == "identity_conflict" && diagnostic.FailureClass == "event_identity_conflict" && diagnostic.ConflictCount == 1 {
			foundConflict = true
			break
		}
	}
	if !foundConflict {
		t.Fatalf("identity conflict must be visible in safe delivery diagnostics: %+v", diagnostics)
	}
}

func TestDeliveredIdentityCollisionPreservesHistoryAcrossSQLiteRestart(t *testing.T) {
	for _, redacted := range []bool{false, true} {
		t.Run(map[bool]string{false: "unredacted", true: "redacted"}[redacted], func(t *testing.T) {
			for _, collision := range []string{"payload", "destination"} {
				t.Run(collision, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "delivery.db")
					db := notificationDeliveryFileDB(t, path)
					notificationDeliveryFixture(t, db)
					intent := notificationIntent()
					intent.ThreadName = "synthetic-sensitive-thread-name"
					original, err := PrepareNotificationDelivery(db, intent)
					if err != nil {
						t.Fatal(err)
					}
					deliveredAt := time.Now().UTC().Add(-40 * 24 * time.Hour)
					attempt, started, err := BeginNotificationDelivery(db, original.EventID, deliveredAt.Add(-time.Minute))
					if err != nil || !started {
						t.Fatalf("begin original delivery: started=%v err=%v", started, err)
					}
					if err := CompleteNotificationDelivery(db, *attempt, "message-original", "thread-original", deliveredAt); err != nil {
						t.Fatal(err)
					}
					if redacted {
						if count, err := RedactDeliveredNotificationPayloads(db, deliveredAt.Add(time.Hour), time.Now().UTC(), 100); err != nil || count != 1 {
							t.Fatalf("redact original payload: rows=%d err=%v", count, err)
						}
					}
					var before NotificationDeliveryEvent
					if err := db.Where("event_id = ?", original.EventID).First(&before).Error; err != nil {
						t.Fatal(err)
					}
					if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
						t.Fatal(err)
					}
					sqlDB, err := db.DB()
					if err != nil {
						t.Fatal(err)
					}
					if err := sqlDB.Close(); err != nil {
						t.Fatal(err)
					}

					db = notificationDeliveryFileDB(t, path)
					conflicting := intent
					if collision == "payload" {
						conflicting.Payload = []byte(`{"content":"different conflicting body","allowed_mentions":{"parse":[]}}`)
					} else {
						conflicting.ChannelID = "different-channel"
					}
					if _, err := PrepareNotificationDelivery(db, conflicting); !errors.Is(err, ErrDeliveryIdentityConflict) {
						t.Fatalf("conflicting identity after restart must fail closed, got %v", err)
					}
					var after NotificationDeliveryEvent
					if err := db.Where("event_id = ?", original.EventID).First(&after).Error; err != nil {
						t.Fatal(err)
					}
					if after.Disposition != DeliveryDelivered || after.AttemptID != before.AttemptID || after.AttemptCount != before.AttemptCount ||
						after.DeliveredAt == nil || !after.DeliveredAt.Equal(*before.DeliveredAt) || after.DiscordMessageID != "message-original" ||
						after.DiscordThreadID != "thread-original" || after.PayloadSHA256 != before.PayloadSHA256 || !after.UpdatedAt.Equal(before.UpdatedAt) ||
						after.PayloadJSON != before.PayloadJSON || (after.PayloadRedactedAt == nil) != (before.PayloadRedactedAt == nil) {
						t.Fatalf("identity collision rewrote durable delivery history: before=%+v after=%+v", before, after)
					}
					var conflicts int
					if err := db.Raw("SELECT identity_conflict_count FROM notification_delivery_events WHERE event_id = ?", original.EventID).Scan(&conflicts).Error; err != nil || conflicts != 1 {
						t.Fatalf("safe conflict evidence was not durably recorded: count=%d err=%v", conflicts, err)
					}
					diagnostics, err := ListUnresolvedNotificationDeliveries(db, 50)
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, item := range diagnostics {
						if item.Disposition == "identity_conflict" && item.FailureClass == "event_identity_conflict" {
							found = true
						}
					}
					if !found {
						t.Fatalf("identity conflict is not operator-visible: %+v", diagnostics)
					}
					replayed, err := PrepareNotificationDelivery(db, intent)
					if err != nil || replayed.Disposition != DeliveryDelivered || replayed.DiscordMessageID != "message-original" {
						t.Fatalf("original replay did not resolve to delivered event: event=%+v err=%v", replayed, err)
					}
					if _, started, err := BeginNotificationDelivery(db, replayed.EventID, time.Now().UTC()); started || !errors.Is(err, ErrDeliveryUnavailable) {
						t.Fatalf("conflict or redaction caused resend: started=%v err=%v", started, err)
					}
				})
			}
		})
	}
}

func TestConcurrentDeliveredIdentityConflictsAreCountedAcrossSQLiteConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent-delivery.db")
	db := notificationDeliveryFileDB(t, path)
	notificationDeliveryFixture(t, db)
	intent := notificationIntent()
	intent.ThreadName = "synthetic-sensitive-thread-name"
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	deliveredAt := time.Now().UTC().Add(-time.Minute)
	attempt, started, err := BeginNotificationDelivery(db, event.EventID, deliveredAt.Add(-time.Minute))
	if err != nil || !started {
		t.Fatalf("begin delivery: started=%v err=%v", started, err)
	}
	if err := CompleteNotificationDelivery(db, *attempt, "message-original", "thread-original", deliveredAt); err != nil {
		t.Fatal(err)
	}
	secondDB := notificationDeliveryFileDB(t, path)
	conflictIntent := intent
	conflictIntent.Payload = []byte(`{"content":"concurrent conflict","allowed_mentions":{"parse":[]}}`)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, conn := range []*gorm.DB{db, secondDB} {
		wg.Add(1)
		go func(conn *gorm.DB) {
			defer wg.Done()
			<-start
			_, err := PrepareNotificationDelivery(conn, conflictIntent)
			results <- err
		}(conn)
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if !errors.Is(err, ErrDeliveryIdentityConflict) {
			t.Fatalf("concurrent conflict should be rejected and recorded, got %v", err)
		}
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryDelivered || stored.DiscordMessageID != "message-original" {
		t.Fatalf("concurrent conflict changed delivered history: event=%+v err=%v", stored, err)
	}
	var conflicts int
	if err := db.Raw("SELECT identity_conflict_count FROM notification_delivery_events WHERE event_id = ?", event.EventID).Scan(&conflicts).Error; err != nil || conflicts != 2 {
		t.Fatalf("lost concurrent conflict evidence: count=%d err=%v", conflicts, err)
	}
}

func TestIdentityConflictEvidenceFailureRollsBackWithoutChangingDeliveredEvent(t *testing.T) {
	db := notificationDeliveryFileDB(t, filepath.Join(t.TempDir(), "rollback-conflict.db"))
	notificationDeliveryFixture(t, db)
	intent := notificationIntent()
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	deliveredAt := time.Now().UTC()
	attempt, started, err := BeginNotificationDelivery(db, event.EventID, deliveredAt.Add(-time.Minute))
	if err != nil || !started {
		t.Fatalf("begin delivery: started=%v err=%v", started, err)
	}
	if err := CompleteNotificationDelivery(db, *attempt, "message-original", "thread-original", deliveredAt); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_identity_conflict BEFORE UPDATE OF identity_conflict_count ON notification_delivery_events WHEN NEW.identity_conflict_count > OLD.identity_conflict_count BEGIN SELECT RAISE(ABORT, 'forced conflict write failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	conflicting := intent
	conflicting.Payload = []byte(`{"content":"rollback conflict","allowed_mentions":{"parse":[]}}`)
	if _, err := PrepareNotificationDelivery(db, conflicting); err == nil || errors.Is(err, ErrDeliveryIdentityConflict) {
		t.Fatalf("conflict write failure must be returned, got %v", err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryDelivered || stored.DiscordMessageID != "message-original" || stored.DiscordThreadID != "thread-original" {
		t.Fatalf("failed conflict recording changed delivered history: event=%+v err=%v", stored, err)
	}
}

func TestRedactDeliveredNotificationPayloadsIsBoundedAndPreservesDeduplication(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-30 * 24 * time.Hour)
	makeDelivered := func(taskID string, deliveredAt time.Time, cleanupPending, cleanupInFlight bool) NotificationDeliveryEvent {
		t.Helper()
		intent := notificationIntent()
		intent.TaskID = taskID
		intent.TaskUpdatedAt = taskID + "-updated"
		intent.ThreadName = "synthetic-thread-" + taskID
		event, err := PrepareNotificationDelivery(db, intent)
		if err != nil {
			t.Fatal(err)
		}
		attempt, started, err := BeginNotificationDelivery(db, event.EventID, deliveredAt.Add(-time.Minute))
		if err != nil || !started {
			t.Fatalf("start delivery %s: started=%v err=%v", taskID, started, err)
		}
		if err := CompleteNotificationDelivery(db, *attempt, "message-"+taskID, "thread-"+taskID, deliveredAt); err != nil {
			t.Fatal(err)
		}
		if cleanupPending || cleanupInFlight {
			if err := db.Model(&NotificationDeliveryEvent{}).Where("event_id = ?", event.EventID).Updates(map[string]any{
				"prior_cleanup_pending": cleanupPending, "prior_cleanup_in_flight": cleanupInFlight,
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
		var stored NotificationDeliveryEvent
		if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		return stored
	}

	eligibleA := makeDelivered("old-a", cutoff.Add(-time.Hour), false, false)
	eligibleB := makeDelivered("old-b", cutoff.Add(-2*time.Hour), false, false)
	eligibleC := makeDelivered("old-c", cutoff.Add(-3*time.Hour), false, false)
	cleanupPending := makeDelivered("cleanup-pending", cutoff.Add(-4*time.Hour), true, false)
	cleanupInFlight := makeDelivered("cleanup-in-flight", cutoff.Add(-5*time.Hour), true, true)
	recent := makeDelivered("recent", cutoff.Add(time.Hour), false, false)
	unresolved := make([]NotificationDeliveryEvent, 0, 6)
	for _, disposition := range []string{DeliveryPending, DeliveryInFlight, DeliveryRetryable, DeliveryUnknown, DeliveryTerminalFailure, DeliveryLegacyUnknown} {
		intent := notificationIntent()
		intent.TaskID, intent.TaskUpdatedAt = "unresolved-"+disposition, "unresolved-"+disposition+"-updated"
		event, err := PrepareNotificationDelivery(db, intent)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&NotificationDeliveryEvent{}).Where("event_id = ?", event.EventID).Update("disposition", disposition).Error; err != nil {
			t.Fatal(err)
		}
		unresolved = append(unresolved, *event)
	}

	redacted, err := RedactDeliveredNotificationPayloads(db, cutoff, now, 2)
	if err != nil || redacted != 2 {
		t.Fatalf("first bounded redaction = %d, %v; want 2", redacted, err)
	}
	rowsToCheck := []NotificationDeliveryEvent{eligibleA, eligibleB, eligibleC, cleanupPending, cleanupInFlight, recent}
	rowsToCheck = append(rowsToCheck, unresolved...)
	expectedDispositions := map[string]string{}
	for _, row := range rowsToCheck {
		expectedDispositions[row.EventID] = row.Disposition
	}
	for i, disposition := range []string{DeliveryPending, DeliveryInFlight, DeliveryRetryable, DeliveryUnknown, DeliveryTerminalFailure, DeliveryLegacyUnknown} {
		expectedDispositions[unresolved[i].EventID] = disposition
	}
	for _, row := range rowsToCheck {
		var got NotificationDeliveryEvent
		if err := db.Where("event_id = ?", row.EventID).First(&got).Error; err != nil {
			t.Fatal(err)
		}
		wantRedacted := row.EventID == eligibleB.EventID || row.EventID == eligibleC.EventID
		if (got.PayloadRedactedAt != nil) != wantRedacted {
			t.Fatalf("event %s redaction marker = %v, want redacted=%v", row.EventID, got.PayloadRedactedAt, wantRedacted)
		}
		if got.Disposition != expectedDispositions[row.EventID] {
			t.Fatalf("redaction changed event %s disposition to %q; want %q", row.EventID, got.Disposition, expectedDispositions[row.EventID])
		}
		if wantRedacted {
			if got.PayloadJSON != "" || got.ThreadName != "" || got.PayloadSHA256 != row.PayloadSHA256 || got.Disposition != DeliveryDelivered || got.DiscordMessageID != row.DiscordMessageID || got.DiscordThreadID != row.DiscordThreadID || got.EventID != row.EventID || got.WebhookRowID != row.WebhookRowID || got.DestinationGuildID != row.DestinationGuildID || got.DestinationChannelID != row.DestinationChannelID || got.DestinationWebhookID != row.DestinationWebhookID || got.DestinationFingerprint != row.DestinationFingerprint {
				t.Fatalf("redaction discarded dedupe/recovery metadata: before=%+v after=%+v", row, got)
			}
		} else if got.PayloadJSON != row.PayloadJSON || got.ThreadName != row.ThreadName {
			t.Fatalf("ineligible recovery data changed for event %s", row.EventID)
		}
	}

	redacted, err = RedactDeliveredNotificationPayloads(db, cutoff, now, 10)
	if err != nil || redacted != 1 {
		t.Fatalf("second redaction = %d, %v; want final eligible record", redacted, err)
	}
	redacted, err = RedactDeliveredNotificationPayloads(db, cutoff, now, 10)
	if err != nil || redacted != 0 {
		t.Fatalf("repeated redaction must be idempotent: %d, %v", redacted, err)
	}

	// A redacted event remains the same delivered event on replay and cannot
	// become dispatchable after process restart or a repeated Kitsu observation.
	intent := notificationIntent()
	intent.TaskID, intent.TaskUpdatedAt = "old-a", "old-a-updated"
	replayed, err := PrepareNotificationDelivery(db, intent)
	if err != nil || replayed.EventID != eligibleA.EventID || replayed.Disposition != DeliveryDelivered || replayed.PayloadJSON != "" || replayed.PayloadSHA256 != eligibleA.PayloadSHA256 {
		t.Fatalf("redacted event was not retained as deduplication evidence: event=%+v err=%v", replayed, err)
	}
	if _, started, err := BeginNotificationDelivery(db, replayed.EventID, now); started || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("redacted delivered event became resendable: started=%v err=%v", started, err)
	}
	var count int64
	if err := db.Model(&NotificationDeliveryEvent{}).Count(&count).Error; err != nil || count != int64(len(rowsToCheck)) {
		t.Fatalf("retention must preserve all event identities: count=%d want=%d err=%v", count, len(rowsToCheck), err)
	}
}

func TestPayloadRetentionCoexistsWithDeletionTombstoneAndMutationRecovery(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	var project Project
	var webhook ProjectWebhook
	if err := db.Where("kitsu_project_id = ?", "production-1").First(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", 1).First(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	intent := notificationIntent()
	intent.ThreadName = "synthetic-sensitive-thread-name"
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	attempt, started, err := BeginNotificationDelivery(db, event.EventID, now.Add(-40*24*time.Hour))
	if err != nil || !started {
		t.Fatalf("start delivery: started=%v err=%v", started, err)
	}
	deliveredAt := now.Add(-39 * 24 * time.Hour)
	if err := CompleteNotificationDelivery(db, *attempt, "retained-message", "retained-thread", deliveredAt); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ManagedChannelDeletion{WebhookRowID: webhook.ID, ProductionID: project.KitsuProjectID, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1", Disposition: ChannelDeletionNeedsHelp, FailureClass: "synthetic_recovery_required", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&ProductionNotificationWriteLock{}).Where("production_id = ?", project.KitsuProjectID).Updates(map[string]any{"mutation_state": NotificationMutationReorderHelp, "mutation_token": "synthetic-operation"}).Error; err != nil {
		t.Fatal(err)
	}
	redacted, err := RedactDeliveredNotificationPayloads(db, now.Add(-30*24*time.Hour), now, 100)
	if err != nil || redacted != 1 {
		t.Fatalf("completed delivery remains redactable beside unrelated recoveries: rows=%d err=%v", redacted, err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PayloadJSON != "" || stored.ThreadName != "" || stored.PayloadRedactedAt == nil || stored.PayloadSHA256 != event.PayloadSHA256 ||
		stored.DiscordMessageID != "retained-message" || stored.DiscordThreadID != "retained-thread" || stored.DestinationFingerprint != event.DestinationFingerprint {
		t.Fatalf("redaction did not preserve the minimum delivery recovery identity: %+v", stored)
	}
	deletion, err := FindManagedChannelDeletion(db, webhook.ID)
	if err != nil || deletion == nil || deletion.Disposition != ChannelDeletionNeedsHelp || deletion.FailureClass != "synthetic_recovery_required" {
		t.Fatalf("redaction affected deletion recovery tombstone: row=%+v err=%v", deletion, err)
	}
	var mutation ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", project.KitsuProjectID).First(&mutation).Error; err != nil || mutation.MutationState != NotificationMutationReorderHelp || mutation.MutationToken != "synthetic-operation" {
		t.Fatalf("redaction affected mutation recovery state: row=%+v err=%v", mutation, err)
	}
}

func TestPayloadRetentionScrubsLegacyRedactedThreadNameWithoutChangingMarker(t *testing.T) {
	db := notificationDeliveryFileDB(t, filepath.Join(t.TempDir(), "legacy-thread-name.db"))
	notificationDeliveryFixture(t, db)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	intent := notificationIntent()
	intent.ThreadName = "legacy-sensitive-thread-name"
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	attempt, started, err := BeginNotificationDelivery(db, event.EventID, now.Add(-40*24*time.Hour))
	if err != nil || !started {
		t.Fatalf("start delivery: started=%v err=%v", started, err)
	}
	deliveredAt := now.Add(-39 * 24 * time.Hour)
	if err := CompleteNotificationDelivery(db, *attempt, "legacy-message", "legacy-thread", deliveredAt); err != nil {
		t.Fatal(err)
	}
	originalMarker := now.Add(-31 * 24 * time.Hour)
	if err := db.Model(&NotificationDeliveryEvent{}).Where("event_id = ?", event.EventID).Updates(map[string]any{"payload_json": "", "payload_redacted_at": originalMarker}).Error; err != nil {
		t.Fatal(err)
	}
	if redacted, err := RedactDeliveredNotificationPayloads(db, now.Add(-30*24*time.Hour), now, 100); err != nil || redacted != 1 {
		t.Fatalf("legacy redacted thread name should be scrubbed in bounded cleanup: rows=%d err=%v", redacted, err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ThreadName != "" || stored.PayloadJSON != "" || stored.PayloadRedactedAt == nil || !stored.PayloadRedactedAt.Equal(originalMarker) ||
		stored.EventID != event.EventID || stored.PayloadSHA256 != event.PayloadSHA256 || stored.Disposition != DeliveryDelivered ||
		stored.DiscordMessageID != "legacy-message" || stored.DiscordThreadID != "legacy-thread" || stored.DestinationFingerprint != event.DestinationFingerprint {
		t.Fatalf("legacy scrub changed redaction marker or durable identity: %+v", stored)
	}
	if redacted, err := RedactDeliveredNotificationPayloads(db, now.Add(-30*24*time.Hour), now.Add(time.Hour), 100); err != nil || redacted != 0 {
		t.Fatalf("legacy thread-name scrub must be idempotent: rows=%d err=%v", redacted, err)
	}
}

func TestIdentityConflictDiagnosticsArePrioritizedWithinLimit(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	intent := notificationIntent()
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	attempt, started, err := BeginNotificationDelivery(db, event.EventID, time.Now().UTC())
	if err != nil || !started {
		t.Fatalf("start delivery: started=%v err=%v", started, err)
	}
	if err := CompleteNotificationDelivery(db, *attempt, "diagnostic-message", "diagnostic-thread", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	conflicting := intent
	conflicting.Payload = []byte(`{"content":"different diagnostic body","allowed_mentions":{"parse":[]}}`)
	if _, err := PrepareNotificationDelivery(db, conflicting); !errors.Is(err, ErrDeliveryIdentityConflict) {
		t.Fatalf("record identity conflict: %v", err)
	}
	for i := 0; i < 55; i++ {
		row := NotificationDeliveryEvent{
			EventID: fmt.Sprintf("pending-%02d", i), TaskID: fmt.Sprintf("pending-task-%02d", i), ProductionID: "production-1",
			TaskTypeID: "task-type-1", TaskStatus: "WFA", PayloadJSON: `{}`, PayloadSHA256: "synthetic", WebhookRowID: 1,
			DestinationGuildID: "guild-1", DestinationChannelID: "channel-1", DestinationWebhookID: "webhook-1",
			DestinationFingerprint: event.DestinationFingerprint, Disposition: DeliveryPending,
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	diagnostics, err := ListUnresolvedNotificationDeliveries(db, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Disposition == "identity_conflict" && diagnostic.EventID == "identity-conflicts" && diagnostic.ConflictCount == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("identity conflict was starved by ordinary pending events: %+v", diagnostics)
	}
}

func TestIdentityConflictDiagnosticSummaryPreservesOtherRecoveryVisibility(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	baseTime := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 55; i++ {
		conflictAt := baseTime.Add(time.Duration(i) * time.Minute)
		event := NotificationDeliveryEvent{
			EventID: fmt.Sprintf("conflict-%03d", i), TaskID: fmt.Sprintf("conflict-task-%03d", i), ProductionID: "production-1",
			TaskTypeID: "task-type-1", TaskStatus: "WFA", PayloadJSON: "", PayloadSHA256: "synthetic", PayloadRedactedAt: &baseTime,
			WebhookRowID: 1, DestinationGuildID: "guild-1", DestinationChannelID: "channel-1", DestinationWebhookID: "webhook-1",
			DestinationFingerprint: "synthetic-fingerprint", Disposition: DeliveryDelivered, IdentityConflictCount: 1,
			IdentityConflictAt: &conflictAt, CreatedAt: baseTime, UpdatedAt: conflictAt,
		}
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}
	unknown := NotificationDeliveryEvent{EventID: "unknown-event", TaskID: "unknown-task", ProductionID: "production-1", TaskTypeID: "task-type-1", TaskStatus: "WFA", PayloadJSON: `{}`, PayloadSHA256: "synthetic", WebhookRowID: 1, DestinationGuildID: "guild-1", DestinationChannelID: "channel-1", DestinationWebhookID: "webhook-1", DestinationFingerprint: "synthetic-fingerprint", Disposition: DeliveryUnknown, CreatedAt: baseTime, UpdatedAt: baseTime}
	if err := db.Create(&unknown).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ManagedChannelDeletion{WebhookRowID: 2, ProductionID: "production-1", GuildID: "guild-1", ChannelID: "channel-2", DiscordWebhookID: "webhook-2", Disposition: ChannelDeletionNeedsHelp, FailureClass: "synthetic_delete_recovery", CreatedAt: baseTime}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationWriteLock{ProductionID: "reorder-recovery", MutationState: NotificationMutationReorderHelp, MutationToken: "synthetic-reorder", UpdatedAt: baseTime}).Error; err != nil {
		t.Fatal(err)
	}
	diagnostics, err := ListUnresolvedNotificationDeliveries(db, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 4 || diagnostics[0].EventID != "identity-conflicts" || diagnostics[0].ConflictCount != 55 || diagnostics[0].UpdatedAt.IsZero() {
		t.Fatalf("conflict summary should remain visible without starving recovery details: %#v", diagnostics)
	}
	want := map[string]bool{"unknown-even": false, "delete-2": false, "mutation-reorder-reco": false}
	for _, diagnostic := range diagnostics[1:] {
		if _, ok := want[diagnostic.EventID]; ok {
			want[diagnostic.EventID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Fatalf("recovery diagnostic %q was hidden by historical conflicts: %#v", id, diagnostics)
		}
	}
}

func TestRedactedDeliveredNotificationPayloadSurvivesSQLiteRestartWithoutResend(t *testing.T) {
	dbPath := t.TempDir() + "/delivery-retention.db"
	openDB := func() *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &Task{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProjectReviewerTarget{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
			t.Fatal(err)
		}
		return db
	}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	db := openDB()
	notificationDeliveryFixture(t, db)
	event, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	attempt, started, err := BeginNotificationDelivery(db, event.EventID, now.Add(-46*24*time.Hour))
	if err != nil || !started {
		t.Fatalf("start old delivery: started=%v err=%v", started, err)
	}
	deliveredAt := now.Add(-45 * 24 * time.Hour)
	if err := CompleteNotificationDelivery(db, *attempt, "durable-message", "durable-thread", deliveredAt); err != nil {
		t.Fatal(err)
	}
	if redacted, err := RedactDeliveredNotificationPayloads(db, now.Add(-30*24*time.Hour), now, 100); err != nil || redacted != 1 {
		t.Fatalf("redact old delivered event: count=%d err=%v", redacted, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	backupPath := t.TempDir() + "/delivery-retention-backup.db"
	contents, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, contents, 0600); err != nil {
		t.Fatal(err)
	}

	reopened, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.AutoMigrate(&Project{}, &ProjectWebhook{}, &Task{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProjectReviewerTarget{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	reopenedSQL, err := reopened.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedSQL.Close() })
	replayed, err := PrepareNotificationDelivery(reopened, notificationIntent())
	if err != nil || replayed.EventID != event.EventID || replayed.Disposition != DeliveryDelivered || replayed.PayloadJSON != "" || replayed.PayloadSHA256 != event.PayloadSHA256 || replayed.DiscordMessageID != "durable-message" || replayed.DiscordThreadID != "durable-thread" {
		t.Fatalf("restart lost deduplication/recovery metadata: event=%+v err=%v", replayed, err)
	}
	if _, started, err := BeginNotificationDelivery(reopened, replayed.EventID, now); started || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("restart made delivered event resendable: started=%v err=%v", started, err)
	}
}

func TestDeliveredPayloadRedactionEnforcesMaximumBatchSize(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	rows := make([]NotificationDeliveryEvent, NotificationPayloadRedactionBatchSize+5)
	for i := range rows {
		n := strconv.Itoa(i)
		deliveredAt := now.Add(-31 * 24 * time.Hour)
		rows[i] = NotificationDeliveryEvent{
			EventID: "bounded-event-" + n, TaskID: "bounded-task-" + n, ProductionID: "production-1",
			TaskTypeID: "task-type-1", TaskStatus: "WFA", PayloadJSON: `{"content":"synthetic"}`,
			PayloadSHA256: "retained-payload-hash", WebhookRowID: 1, DestinationGuildID: "guild-1",
			DestinationChannelID: "channel-1", DestinationWebhookID: "webhook-1", DestinationFingerprint: "destination-fingerprint",
			Disposition: DeliveryDelivered, DeliveredAt: &deliveredAt, DiscordMessageID: "message-" + n,
		}
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	redacted, err := RedactDeliveredNotificationPayloads(db, now.Add(-30*24*time.Hour), now, len(rows))
	if err != nil || redacted != NotificationPayloadRedactionBatchSize {
		t.Fatalf("oversized request redacted %d rows, err=%v; want capped batch %d", redacted, err, NotificationPayloadRedactionBatchSize)
	}
	var remaining int64
	if err := db.Model(&NotificationDeliveryEvent{}).Where("payload_redacted_at IS NULL AND payload_json <> ''").Count(&remaining).Error; err != nil || remaining != 5 {
		t.Fatalf("oversized request did not leave the bounded remainder intact: remaining=%d err=%v", remaining, err)
	}
}

func TestResolveNotificationDestinationRequiresExactCurrentRouteAndRejectsTombstone(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	identity, err := ResolveNotificationDestination(db, "production-1", "task-type-1", "https://discord.com/api/webhooks/webhook-1/token", "webhook-1")
	if err != nil || identity.GuildID != "guild-1" || identity.ChannelID != "channel-1" || identity.WebhookID != "webhook-1" {
		t.Fatalf("expected exact current destination identity, got %+v err=%v", identity, err)
	}
	if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", "https://discord.com/api/webhooks/other/token", "other"); err == nil {
		t.Fatal("destination resolver accepted a webhook URL not bound to the current route")
	}
	secondWebhook := ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-2", WebhookURL: "https://discord.com/api/webhooks/webhook-2/token"}
	if err := db.Create(&secondWebhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-2", DestinationWebhookID: secondWebhook.ID, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := BeginManagedChannelDeletion(db, "production-1", 1, "guild-1", "channel-1", "webhook-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", "https://discord.com/api/webhooks/webhook-1/token", "webhook-1"); !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("tombstoned destination remained dispatchable: %v", err)
	}
}

func TestExpiredInFlightDeliveryIsQuarantinedAndNeverRetried(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	intent, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	started, ok, err := BeginNotificationDelivery(db, intent.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("initial attempt did not start: ok=%v err=%v", ok, err)
	}
	if _, ok, err := BeginNotificationDelivery(db, intent.EventID, time.Now().Add(3*time.Minute)); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("expired attempt must be quarantined rather than resent: ok=%v err=%v", ok, err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", started.Event.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryUnknown {
		t.Fatalf("expired in-flight state is not unknown: row=%+v err=%v", stored, err)
	}
}

func TestInFlightDeliveryBlocksConcurrentReviewerRouteAndChannelMutations(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	revision, err := ProductionNotificationRevisionWithError(db, project.ID, "production-1")
	if err != nil {
		t.Fatal(err)
	}
	intent := notificationIntent()
	intent.NotificationRevision = revision
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now()); err != nil || !ok {
		t.Fatalf("dispatch claim failed: ok=%v err=%v", ok, err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-type-1", "Synthetic", ReviewerTargetUser, "123456789012345678"); !errors.Is(err, ErrNotificationDeliveryInFlight) {
		t.Fatalf("direct Reviewer mutation during dispatch was not rejected: %v", err)
	}
	if _, err := ApplyProductionNotificationState(db, project.ID, "production-1", project.Name, revision, nil, nil); !errors.Is(err, ErrNotificationDeliveryInFlight) {
		t.Fatalf("route Apply during dispatch was not rejected: %v", err)
	}
	if err := BeginManagedChannelDeletion(db, "production-1", 1, "guild-1", "channel-1", "webhook-1", time.Now()); !errors.Is(err, ErrNotificationDeliveryInFlight) {
		t.Fatalf("managed channel deletion during dispatch was not rejected: %v", err)
	}
}

func TestProductionDeletionMutationFencesWritesAndRequiresExplicitRetryAfterInterruption(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	token, err := BeginProductionDeletionMutation(db, "production-1")
	if err != nil || token == "" {
		t.Fatalf("deletion barrier did not start: token=%q err=%v", token, err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-type-1", "Synthetic", ReviewerTargetUser, "123456789012345678"); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("Reviewer write crossed active deletion barrier: %v", err)
	}
	revision, err := ProductionNotificationRevisionWithError(db, project.ID, "production-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyProductionNotificationState(db, project.ID, "production-1", project.Name, revision, nil, nil); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("Apply crossed active deletion barrier: %v", err)
	}
	if _, err := PrepareNotificationDelivery(db, notificationIntent()); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("polling crossed active deletion barrier: %v", err)
	}
	if err := FinishProductionDeletionMutation(db, "production-1", token, true); err != nil {
		t.Fatal(err)
	}
	var lock ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", "production-1").First(&lock).Error; err != nil || lock.MutationState != "deletion_needs_help" {
		t.Fatalf("interrupted deletion was not retained for operator recovery: lock=%+v err=%v", lock, err)
	}
	retryToken, err := BeginProductionDeletionMutation(db, "production-1")
	if err != nil || retryToken != token {
		t.Fatalf("explicit deletion retry did not resume the same operation identity: token=%q original=%q err=%v", retryToken, token, err)
	}
	if err := FinishProductionDeletionMutation(db, "production-1", retryToken, false); err != nil {
		t.Fatal(err)
	}
}

func TestProductionDeletionMutationBarrierIsVisibleAcrossSQLiteConnections(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	secondDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	secondSQL, err := secondDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondSQL.Close() })
	token, err := BeginProductionDeletionMutation(db, "production-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareNotificationDelivery(secondDB, notificationIntent()); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("second SQLite connection did not observe the durable deletion barrier: %v", err)
	}
	if err := FinishProductionDeletionMutation(secondDB, "production-1", token, false); err != nil {
		t.Fatalf("second connection could not finalize the exact barrier token: %v", err)
	}
}

func TestNotificationRevisionChangeBeforeDispatchFailsClosed(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	revision, err := ProductionNotificationRevisionWithError(db, project.ID, "production-1")
	if err != nil {
		t.Fatal(err)
	}
	intent := notificationIntent()
	intent.NotificationRevision = revision
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: ReviewerTargetUser, DiscordID: "123456789012345678"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now()); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("stale recipient revision must not dispatch: ok=%v err=%v", ok, err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryTerminalFailure || stored.FailureClass != "notification_state_changed_before_dispatch" {
		t.Fatalf("stale event was not terminally blocked: row=%+v err=%v", stored, err)
	}
}

func TestRetryableDeliveryCanRestartButAcceptedSendPersistenceFailureCannot(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	intent, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	first, ok, err := BeginNotificationDelivery(db, intent.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("begin first attempt: ok=%v err=%v", ok, err)
	}
	if err := SetNotificationDeliveryDisposition(db, *first, DeliveryRetryable, "rate_limited"); err != nil {
		t.Fatal(err)
	}
	second, ok, err := BeginNotificationDelivery(db, intent.EventID, time.Now().Add(time.Second))
	if err != nil || !ok {
		t.Fatalf("known rejected retryable attempt should retry: ok=%v err=%v", ok, err)
	}
	if err := db.Exec("CREATE TRIGGER fail_task_checkpoint BEFORE INSERT ON tasks BEGIN SELECT RAISE(ABORT, 'injected checkpoint failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := CompleteNotificationDelivery(db, *second, "message-1", "", time.Now()); err == nil {
		t.Fatal("post-accept checkpoint failure must be surfaced")
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", intent.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryInFlight {
		t.Fatalf("failed SQLite commit must preserve the durable pre-send in-flight attempt: row=%+v err=%v", stored, err)
	}
	if err := db.Exec("DROP TRIGGER fail_task_checkpoint").Error; err != nil {
		t.Fatal(err)
	}
	if err := QuarantineAcceptedButUnpersisted(db, *second); err != nil {
		t.Fatalf("accepted-but-unpersisted attempt should be quarantined immediately: %v", err)
	}
	if _, ok, err := BeginNotificationDelivery(db, intent.EventID, time.Now().Add(time.Hour)); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("accepted-but-unpersisted event must not be blindly resent: ok=%v err=%v", ok, err)
	}
	if err := db.Where("event_id = ?", intent.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryUnknown || stored.FailureClass != "discord_accepted_checkpoint_uncommitted" {
		t.Fatalf("accepted but uncommitted result needs durable reconciliation evidence: row=%+v err=%v", stored, err)
	}
}

func TestEarlierUnresolvedEventBlocksOutOfOrderDelivery(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	firstIntent := notificationIntent()
	first, err := PrepareNotificationDelivery(db, firstIntent)
	if err != nil {
		t.Fatal(err)
	}
	secondIntent := firstIntent
	secondIntent.TaskUpdatedAt = "2026-10-10T12:01:00Z"
	second, err := PrepareNotificationDelivery(db, secondIntent)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginNotificationDelivery(db, second.EventID, time.Now()); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("newer event must wait for the earlier pending event: ok=%v err=%v", ok, err)
	}
	firstAttempt, ok, err := BeginNotificationDelivery(db, first.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("earlier event should claim first: ok=%v err=%v", ok, err)
	}
	if err := SetNotificationDeliveryDisposition(db, *firstAttempt, DeliveryRetryable, "rate_limited"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginNotificationDelivery(db, second.EventID, time.Now().Add(time.Second)); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("newer event must remain blocked behind retryable older event: ok=%v err=%v", ok, err)
	}
	retry, ok, err := BeginNotificationDelivery(db, first.EventID, time.Now().Add(2*time.Second))
	if err != nil || !ok {
		t.Fatalf("older retry should be eligible: ok=%v err=%v", ok, err)
	}
	if err := CompleteNotificationDelivery(db, *retry, "message-1", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginNotificationDelivery(db, second.EventID, time.Now().Add(3*time.Second)); err != nil || !ok {
		t.Fatalf("newer event should proceed after prior commit: ok=%v err=%v", ok, err)
	}
}

func TestFailedPriorWebhookVerificationReleasesCleanupClaimForSafeRetry(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	intent := notificationIntent()
	intent.PriorMessageID = "old-message"
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("delivery attempt did not start: ok=%v err=%v", ok, err)
	}
	if err := CompleteNotificationDelivery(db, *attempt, "new-message", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := BeginPriorMessageCleanup(db, event.EventID)
	if err != nil || !ok || !claimed.PriorCleanupInFlight {
		t.Fatalf("prior-message cleanup was not claimed: event=%+v ok=%v err=%v", claimed, ok, err)
	}
	if err := RecordPriorMessageCleanup(db, event.EventID, false, "prior_webhook_verification_failed"); err != nil {
		t.Fatal(err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.PriorCleanupPending || stored.PriorCleanupInFlight || stored.CleanupFailureClass != "prior_webhook_verification_failed" {
		t.Fatalf("failed verification should remain visible and retryable without a stuck claim: %+v", stored)
	}
}

func TestObservedSameStatusCommentEditDoesNotAdvanceDeliveryCheckpoint(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	legacy := Task{TaskID: "task-1", TaskStatus: "WFA", TaskUpdatedAt: "old-task-time", CommentID: "comment-old", CommentUpdatedAt: "old-comment-time"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	current := notificationIntent()
	if err := ObserveTaskStatus(db, current.TaskID, current.TaskUpdatedAt, current.TaskStatus, current.CommentID, current.CommentUpdatedAt); err != nil {
		t.Fatal(err)
	}
	var observed Task
	if err := db.Where("task_id = ?", current.TaskID).First(&observed).Error; err != nil {
		t.Fatal(err)
	}
	if observed.DeliveredTaskUpdatedAt != "old-task-time" || observed.DeliveredCommentUpdatedAt != "old-comment-time" {
		t.Fatalf("observation incorrectly consumed an undelivered same-status edit: %+v", observed)
	}
	event, err := PrepareNotificationDelivery(db, current)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("same-status comment edit should be dispatched: ok=%v err=%v", ok, err)
	}
	if err := SetNotificationDeliveryDisposition(db, *attempt, DeliveryRetryable, "rate_limited"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveTaskStatus(db, current.TaskID, current.TaskUpdatedAt, current.TaskStatus, current.CommentID, current.CommentUpdatedAt); err != nil {
		t.Fatal(err)
	}
	disposition, err := NotificationDeliveryDisposition(db, event.EventID)
	if err != nil || disposition != DeliveryRetryable {
		t.Fatalf("retryable outbox intent was consumed by repeat observation: disposition=%q err=%v", disposition, err)
	}
	if err := db.Where("task_id = ?", current.TaskID).First(&observed).Error; err != nil || observed.DeliveredCommentUpdatedAt != "old-comment-time" {
		t.Fatalf("failed same-status edit advanced its delivery checkpoint: task=%+v err=%v", observed, err)
	}
}

func TestExternalReorderMutationBarrierBlocksDispatchAndSurfacesRecovery(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	revision, err := ProductionNotificationRevisionWithError(db, project.ID, project.KitsuProjectID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewProductionNotificationMutationToken()
	if err != nil {
		t.Fatal(err)
	}
	routes := ListProductionNotificationRoutes(db, project.KitsuProjectID)
	if _, err := ApplyProductionNotificationStateForExternalReorder(db, project.ID, project.KitsuProjectID, project.Name, revision, routes, nil, token); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareNotificationDelivery(db, notificationIntent()); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("dispatch intent should be blocked during external reorder: %v", err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-type-1", "Synthetic", ReviewerTargetUser, "123456789012345678"); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("direct Reviewer mutation should wait for external reorder: %v", err)
	}
	diagnostics, err := ListUnresolvedNotificationDeliveries(db, 50)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].Disposition != "mutation_reordering" {
		t.Fatalf("external mutation should be visible in safe diagnostics: %#v err=%v", diagnostics, err)
	}
	if err := FinishProductionNotificationExternalMutation(db, project.KitsuProjectID, token, true); err != nil {
		t.Fatal(err)
	}
	var quarantined ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", project.KitsuProjectID).First(&quarantined).Error; err != nil || quarantined.MutationState != NotificationMutationReorderHelp || quarantined.MutationToken != token {
		t.Fatalf("quarantined reorder must retain its durable attempt identity: lock=%+v err=%v", quarantined, err)
	}
	if _, err := PrepareNotificationDelivery(db, notificationIntent()); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("failed compensation must keep dispatch quarantined: %v", err)
	}
	diagnostics, err = ListUnresolvedNotificationDeliveries(db, 50)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].Disposition != "mutation_reorder_needs_help" {
		t.Fatalf("unresolved recovery state not surfaced: %#v err=%v", diagnostics, err)
	}
	recoveryToken, err := BeginProductionNotificationOrderRecovery(db, project.KitsuProjectID)
	if err != nil || recoveryToken == "" || recoveryToken == token {
		t.Fatalf("explicit order reconciliation should claim the persisted recovery state with a fresh token: token=%q err=%v", recoveryToken, err)
	}
	if err := FinishProductionNotificationExternalMutation(db, project.KitsuProjectID, recoveryToken, false); err != nil {
		t.Fatalf("verified reconciliation should release its matching fence: %v", err)
	}
	if _, err := PrepareNotificationDelivery(db, notificationIntent()); err != nil {
		t.Fatalf("dispatch should resume only after explicit order reconciliation completes: %v", err)
	}
}

func TestOrderRecoveryClaimsLegacyHelpStateWithoutTokenAndBlocksInFlightDelivery(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	event, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&NotificationDeliveryEvent{}).Where("event_id = ?", event.EventID).Update("disposition", DeliveryInFlight).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&ProductionNotificationWriteLock{}).Where("production_id = ?", "production-1").Updates(map[string]any{
		"mutation_state": NotificationMutationReorderHelp,
		"mutation_token": "",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := BeginProductionNotificationOrderRecovery(db, "production-1"); !errors.Is(err, ErrNotificationDeliveryInFlight) {
		t.Fatalf("order recovery must not run while a delivery is in flight: %v", err)
	}
	if err := db.Model(&NotificationDeliveryEvent{}).Where("event_id = ?", event.EventID).Update("disposition", DeliveryPending).Error; err != nil {
		t.Fatal(err)
	}
	token, err := BeginProductionNotificationOrderRecovery(db, "production-1")
	if err != nil || token == "" {
		t.Fatalf("explicit recovery should claim a persisted legacy help state with a fresh token: token=%q err=%v", token, err)
	}
	var claimed ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", "production-1").First(&claimed).Error; err != nil || claimed.MutationState != NotificationMutationReordering || claimed.MutationToken != token {
		t.Fatalf("legacy help state was not safely moved under the fresh recovery token: lock=%+v err=%v", claimed, err)
	}
	if err := FinishProductionNotificationExternalMutation(db, "production-1", token, false); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE production_notification_write_locks SET mutation_state = ?, mutation_token = NULL WHERE production_id = ?", NotificationMutationReorderHelp, "production-1").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := BeginProductionNotificationOrderRecovery(db, "production-1"); err != nil {
		t.Fatalf("explicit recovery should also claim a persisted legacy help state with a NULL token: %v", err)
	}
}

func TestDeletionBatchTombstonesAtomicallyAndBlocksAllRoutes(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	secondWebhook := ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-2", WebhookURL: "https://discord.com/api/webhooks/webhook-2/token"}
	if err := db.Create(&secondWebhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-2", DestinationWebhookID: secondWebhook.ID, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_second_tombstone BEFORE INSERT ON managed_channel_deletions WHEN NEW.webhook_row_id = 2 BEGIN SELECT RAISE(ABORT, 'injected tombstone failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	targets := []ManagedChannelDeletionTarget{{WebhookRowID: 1, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1"}, {WebhookRowID: secondWebhook.ID, GuildID: "guild-1", ChannelID: "channel-2", DiscordWebhookID: "webhook-2"}}
	if err := BeginManagedChannelDeletionBatch(db, "production-1", targets, time.Now(), true); err == nil {
		t.Fatal("injected tombstone write failure should fail the whole batch")
	}
	var count int64
	if err := db.Model(&ManagedChannelDeletion{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed batch left a partial tombstone set: count=%d err=%v", count, err)
	}
	if err := db.Exec("DROP TRIGGER fail_second_tombstone").Error; err != nil {
		t.Fatal(err)
	}
	if err := BeginManagedChannelDeletionBatch(db, "production-1", targets, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	diagnostics, err := ListUnresolvedNotificationDeliveries(db, 50)
	if err != nil || len(diagnostics) != 2 || diagnostics[0].Disposition != "channel_pending" || diagnostics[1].Disposition != "channel_pending" {
		t.Fatalf("channel tombstones should be visible in safe diagnostics: %#v err=%v", diagnostics, err)
	}
	if _, err := PrepareNotificationDelivery(db, notificationIntent()); !errors.Is(err, ErrChannelDeletionConflict) {
		t.Fatalf("dispatch should stop while any Production deletion tombstone is unresolved: %v", err)
	}
}

func TestPriorMessageCleanupClaimSerializesWithManagedChannelDeletion(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	secondWebhook := ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-2", WebhookURL: "https://discord.com/api/webhooks/webhook-2/token"}
	if err := db.Create(&secondWebhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-2", DestinationWebhookID: secondWebhook.ID, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	intent := notificationIntent()
	intent.PriorMessageID = "prior-message-1"
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("delivery claim failed: ok=%v err=%v", ok, err)
	}
	if err := CompleteNotificationDelivery(db, *attempt, "new-message-1", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := BeginPriorMessageCleanup(db, event.EventID); err != nil || !claimed {
		t.Fatalf("cleanup claim failed: claimed=%v err=%v", claimed, err)
	}
	if err := BeginManagedChannelDeletion(db, "production-1", 1, "guild-1", "channel-1", "webhook-1", time.Now()); !errors.Is(err, ErrChannelDeletionConflict) {
		t.Fatalf("channel deletion raced active/prior cleanup: %v", err)
	}
	if err := RecordPriorMessageCleanup(db, event.EventID, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := BeginManagedChannelDeletion(db, "production-1", 1, "guild-1", "channel-1", "webhook-1", time.Now()); err != nil {
		t.Fatalf("channel deletion should proceed after exact cleanup completes: %v", err)
	}
}

func TestManagedChannelDeletionTombstoneBlocksDispatchAndFinalizesAtomically(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	if err := db.Create(&ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: ReviewerTargetUser, DiscordID: "discord-user-1"}).Error; err != nil {
		t.Fatal(err)
	}
	secondWebhook := ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-2", WebhookURL: "https://discord.com/api/webhooks/webhook-2/token"}
	if err := db.Create(&secondWebhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-2", DestinationWebhookID: secondWebhook.ID, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := BeginManagedChannelDeletion(db, "production-1", 1, "guild-1", "channel-1", "webhook-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	intent, err := PrepareNotificationDelivery(db, notificationIntent())
	if err == nil || !errors.Is(err, ErrChannelDeletionConflict) || intent != nil {
		t.Fatalf("tombstoned destination must reject new delivery intent: intent=%+v err=%v", intent, err)
	}
	if err := db.Exec("CREATE TRIGGER fail_route_delete BEFORE DELETE ON production_notification_routes BEGIN SELECT RAISE(ABORT, 'injected finalize failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := FinalizeManagedChannelDeletion(db, 1, time.Now()); err == nil {
		t.Fatal("injected local-finalize failure must be reported")
	}
	if FindProjectWebhookByID(db, 1) == nil || len(ListProductionNotificationRoutes(db, "production-1")) != 2 || len(ListProjectReviewerTargets(db, project.ID)) != 1 {
		t.Fatal("failed finalization partially deleted local route state")
	}
	var tombstone ManagedChannelDeletion
	if err := db.Where("webhook_row_id = ?", 1).First(&tombstone).Error; err != nil || tombstone.Disposition != ChannelDeletionPending {
		t.Fatalf("failed finalization did not retain recoverable tombstone: row=%+v err=%v", tombstone, err)
	}
	if err := db.Exec("DROP TRIGGER fail_route_delete").Error; err != nil {
		t.Fatal(err)
	}
	if err := FinalizeManagedChannelDeletion(db, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	routes := ListProductionNotificationRoutes(db, "production-1")
	if FindProjectWebhookByID(db, 1) != nil || len(routes) != 1 || routes[0].TaskTypeID != "task-type-2" || len(ListProjectReviewerTargets(db, project.ID)) != 0 {
		t.Fatal("finalized deletion did not atomically remove local destination state")
	}
}

package model

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func rejectedNotificationCandidate(eventID string, mutate func(*NotificationDeliveryEvent)) NotificationDeliveryEvent {
	row := NotificationDeliveryEvent{
		EventID: eventID, TaskID: "task-1", ProductionID: "production-1", TaskTypeID: "task-type-1", TaskStatus: "WFA",
		TaskUpdatedAt: "2026-10-10T11:00:00Z", CommentUpdatedAt: "2026-10-10T11:00:00Z",
		PayloadJSON: `{"content":"rejected` + eventID + `"}`, PayloadSHA256: "sha-" + eventID,
		WebhookRowID: 1, DestinationGuildID: "guild-1", DestinationChannelID: "channel-1", DestinationWebhookID: "webhook-1",
		DestinationFingerprint: notificationDestinationFingerprint("guild-1", "channel-1", "webhook-1"),
		Disposition:            DeliveryRetryable, FailureClass: "rate_limited", AttemptCount: 1, AttemptID: "attempt-" + eventID,
	}
	if mutate != nil {
		mutate(&row)
	}
	return row
}

func TestSupersedeKnownRejectedEventsFailsClosedOnUnusableObservations(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	intent := notificationIntent()
	if _, err := supersedeKnownRejectedNotificationEvents(nil, intent, time.Now()); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("supersede without a transaction must fail closed: %v", err)
	}
	if _, err := supersedeKnownRejectedNotificationEvents(db, NotificationDeliveryIntent{ProductionID: "production-1"}, time.Now()); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("an observation without a task must fail closed: %v", err)
	}
	if _, err := supersedeKnownRejectedNotificationEvents(db, NotificationDeliveryIntent{TaskID: "task-1"}, time.Now()); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("an observation without a Production must fail closed: %v", err)
	}

	prior := rejectedNotificationCandidate("rejected-earlier", nil)
	if err := db.Create(&prior).Error; err != nil {
		t.Fatal(err)
	}

	unparseable := intent
	unparseable.TaskUpdatedAt = "not-a-timestamp"
	unparseable.CommentUpdatedAt = "   "
	if count, err := supersedeKnownRejectedNotificationEvents(db, unparseable, time.Now()); err != nil || count != 0 {
		t.Fatalf("an unparseable observation must not supersede anything: count=%d err=%v", count, err)
	}
	sameTime := intent
	sameTime.TaskUpdatedAt = prior.TaskUpdatedAt
	sameTime.CommentUpdatedAt = prior.CommentUpdatedAt
	if count, err := supersedeKnownRejectedNotificationEvents(db, sameTime, time.Now()); err != nil || count != 0 {
		t.Fatalf("an observation at the same timestamp is not newer: count=%d err=%v", count, err)
	}
	older := intent
	older.TaskUpdatedAt = "2026-10-10T10:00:00Z"
	older.CommentUpdatedAt = "2026-10-10T10:00:00Z"
	if count, err := supersedeKnownRejectedNotificationEvents(db, older, time.Now()); err != nil || count != 0 {
		t.Fatalf("an older observation must never supersede a rejected event: count=%d err=%v", count, err)
	}

	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", prior.EventID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Disposition != DeliveryRetryable || stored.FailureClass != "rate_limited" || stored.SupersededByEventID != "" {
		t.Fatalf("a non-newer observation must leave the rejected event retryable: %+v", stored)
	}
}

func TestSupersedeKnownRejectedEventsKeepsIneligibleEvidence(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	intent := notificationIntent()
	newEventID := NotificationEventIdentity(intent)
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	deliveredAt := now.Add(-time.Hour)
	redactedAt := now.Add(-time.Hour)
	rows := []NotificationDeliveryEvent{
		rejectedNotificationCandidate("ineligible-not-rate-limited", func(row *NotificationDeliveryEvent) { row.FailureClass = "network_error" }),
		rejectedNotificationCandidate("ineligible-already-posted", func(row *NotificationDeliveryEvent) { row.DiscordMessageID = "message-1" }),
		rejectedNotificationCandidate("ineligible-delivered", func(row *NotificationDeliveryEvent) {
			row.Disposition = DeliveryDelivered
			row.DeliveredAt = &deliveredAt
		}),
		rejectedNotificationCandidate("ineligible-redacted", func(row *NotificationDeliveryEvent) { row.PayloadRedactedAt = &redactedAt }),
		rejectedNotificationCandidate("ineligible-never-attempted", func(row *NotificationDeliveryEvent) { row.AttemptCount = 0 }),
		rejectedNotificationCandidate("ineligible-attempt-less", func(row *NotificationDeliveryEvent) { row.AttemptID = "" }),
		rejectedNotificationCandidate("ineligible-pending", func(row *NotificationDeliveryEvent) { row.Disposition = DeliveryPending }),
		rejectedNotificationCandidate("ineligible-other-task", func(row *NotificationDeliveryEvent) { row.TaskID = "task-9" }),
		rejectedNotificationCandidate("ineligible-other-production", func(row *NotificationDeliveryEvent) { row.ProductionID = "production-2" }),
		rejectedNotificationCandidate("eligible-rejection", nil),
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	seededPayloads := make(map[string]string, len(rows))
	for _, row := range rows {
		seededPayloads[row.EventID] = row.PayloadJSON
	}

	count, err := supersedeKnownRejectedNotificationEvents(db, intent, now)
	if err != nil || count != 1 {
		t.Fatalf("only the eligible known rejection may be superseded: count=%d err=%v", count, err)
	}

	var stored []NotificationDeliveryEvent
	if err := db.Order("event_id asc").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != len(rows) {
		t.Fatalf("supersede must never delete delivery evidence: got=%d want=%d", len(stored), len(rows))
	}
	for _, row := range stored {
		if row.EventID == "eligible-rejection" {
			if row.Disposition != DeliveryTerminalFailure || row.FailureClass != "superseded_by_newer_kitsu_event" || row.SupersededByEventID != newEventID {
				t.Fatalf("eligible rejection was not terminally superseded: %+v", row)
			}
			if !row.UpdatedAt.Equal(now) {
				t.Fatalf("supersede must record the observation time: %+v", row.UpdatedAt)
			}
			if row.PayloadJSON != seededPayloads[row.EventID] || row.AttemptID != "attempt-eligible-rejection" || row.AttemptCount != 1 {
				t.Fatalf("supersede must preserve the attempt and payload evidence: %+v", row)
			}
			continue
		}
		if row.SupersededByEventID != "" || row.FailureClass == "superseded_by_newer_kitsu_event" || row.PayloadJSON != seededPayloads[row.EventID] {
			t.Fatalf("ineligible delivery evidence must survive untouched: %+v", row)
		}
	}
}

func TestSupersedeObsoleteDeliveriesRejectsInvalidAndFencedRequests(t *testing.T) {
	t.Run("invalid requests", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		if _, err := SupersedeObsoleteKnownRejectedNotificationDeliveries(nil, notificationIntent()); !errors.Is(err, gorm.ErrInvalidDB) {
			t.Fatalf("supersede without a database must fail closed: %v", err)
		}
		blankProduction := notificationIntent()
		blankProduction.ProductionID = "   "
		if _, err := SupersedeObsoleteKnownRejectedNotificationDeliveries(db, blankProduction); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("supersede for a blank Production must fail closed: %v", err)
		}
		blankTask := notificationIntent()
		blankTask.TaskID = ""
		if _, err := SupersedeObsoleteKnownRejectedNotificationDeliveries(db, blankTask); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("supersede for a blank task must fail closed: %v", err)
		}
		var locks int64
		if err := db.Model(&ProductionNotificationWriteLock{}).Count(&locks).Error; err != nil || locks != 0 {
			t.Fatalf("rejected supersedes must roll back their write lock: count=%d err=%v", locks, err)
		}
	})

	t.Run("an active external mutation blocks supersede", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		prior := rejectedNotificationCandidate("rejected-fenced", nil)
		if err := db.Create(&prior).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&ProductionNotificationWriteLock{ProductionID: "production-1", Revision: 4, MutationState: NotificationMutationReordering, MutationToken: "tok-1"}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := SupersedeObsoleteKnownRejectedNotificationDeliveries(db, notificationIntent()); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
			t.Fatalf("supersede must wait for an external reorder to settle: %v", err)
		}
		var lock ProductionNotificationWriteLock
		if err := db.Where("production_id = ?", "production-1").First(&lock).Error; err != nil || lock.Revision != 4 || lock.MutationState != NotificationMutationReordering {
			t.Fatalf("the refused supersede must not disturb the active fence: lock=%+v err=%v", lock, err)
		}
		var stored NotificationDeliveryEvent
		if err := db.Where("event_id = ?", prior.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryRetryable {
			t.Fatalf("the refused supersede must leave the rejection intact: row=%+v err=%v", stored, err)
		}
	})

	t.Run("supersede stays scoped to its own Production and task", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		rows := []NotificationDeliveryEvent{
			rejectedNotificationCandidate("scoped-same-task", nil),
			rejectedNotificationCandidate("scoped-other-task", func(row *NotificationDeliveryEvent) { row.TaskID = "task-9" }),
			rejectedNotificationCandidate("scoped-other-production", func(row *NotificationDeliveryEvent) { row.ProductionID = "production-2" }),
		}
		if err := db.Create(&rows).Error; err != nil {
			t.Fatal(err)
		}
		count, err := SupersedeObsoleteKnownRejectedNotificationDeliveries(db, notificationIntent())
		if err != nil || count != 1 {
			t.Fatalf("a newer observation may only supersede its own task: count=%d err=%v", count, err)
		}
		var stored []NotificationDeliveryEvent
		if err := db.Order("event_id asc").Find(&stored).Error; err != nil {
			t.Fatal(err)
		}
		for _, row := range stored {
			if row.EventID == "scoped-same-task" {
				if row.Disposition != DeliveryTerminalFailure || row.FailureClass != "superseded_by_newer_kitsu_event" {
					t.Fatalf("the matching rejection was not superseded: %+v", row)
				}
				continue
			}
			if row.Disposition != DeliveryRetryable || row.SupersededByEventID != "" {
				t.Fatalf("another task or Production must keep its own rejection: %+v", row)
			}
		}
	})
}

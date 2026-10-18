package model

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestSetNotificationDeliveryDispositionStateGuards(t *testing.T) {
	now := time.Now()

	t.Run("invalid inputs are rejected", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		event, err := PrepareNotificationDelivery(db, notificationIntent())
		if err != nil {
			t.Fatal(err)
		}
		attempt, started, err := BeginNotificationDelivery(db, event.EventID, now)
		if err != nil || !started {
			t.Fatalf("claim delivery: started=%v err=%v", started, err)
		}
		if err := SetNotificationDeliveryDisposition(nil, *attempt, DeliveryRetryable, "rate_limited"); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("nil db must be rejected: %v", err)
		}
		for _, disposition := range []string{"", DeliveryPending, DeliveryInFlight, DeliveryDelivered, DeliveryLegacyUnknown} {
			if err := SetNotificationDeliveryDisposition(db, *attempt, disposition, "class"); !errors.Is(err, gorm.ErrInvalidData) {
				t.Fatalf("disposition %q must be refused before touching durable state: %v", disposition, err)
			}
		}
		if got, err := NotificationDeliveryDisposition(db, event.EventID); err != nil || got != DeliveryInFlight {
			t.Fatalf("refused dispositions must leave the attempt in flight: got=%q err=%v", got, err)
		}
	})

	for _, disposition := range []string{DeliveryRetryable, DeliveryUnknown, DeliveryTerminalFailure} {
		t.Run("settles "+disposition, func(t *testing.T) {
			db := notificationDeliveryTestDB(t)
			notificationDeliveryFixture(t, db)
			event, err := PrepareNotificationDelivery(db, notificationIntent())
			if err != nil {
				t.Fatal(err)
			}
			attempt, started, err := BeginNotificationDelivery(db, event.EventID, now)
			if err != nil || !started {
				t.Fatalf("claim delivery: started=%v err=%v", started, err)
			}
			if err := SetNotificationDeliveryDisposition(db, *attempt, disposition, "class-"+disposition); err != nil {
				t.Fatalf("disposition %q should settle the attempt: %v", disposition, err)
			}
			var stored NotificationDeliveryEvent
			if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Disposition != disposition || stored.FailureClass != "class-"+disposition {
				t.Fatalf("stored outcome = %q/%q, want %q/class-%s", stored.Disposition, stored.FailureClass, disposition, disposition)
			}
			if err := SetNotificationDeliveryDisposition(db, *attempt, DeliveryRetryable, "again"); !errors.Is(err, ErrDeliveryAttemptConflict) {
				t.Fatalf("a settled attempt must not settle twice: %v", err)
			}
			if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Disposition != disposition || stored.FailureClass != "class-"+disposition {
				t.Fatalf("second settlement must not rewrite the outcome: %q/%q", stored.Disposition, stored.FailureClass)
			}
			pending, err := ListPendingNotificationDeliveries(db, 0)
			if err != nil {
				t.Fatal(err)
			}
			wantQueued := 0
			if disposition == DeliveryRetryable {
				wantQueued = 1
			}
			if len(pending) != wantQueued {
				t.Fatalf("disposition %q must queue %d rows for retry, got %d", disposition, wantQueued, len(pending))
			}
		})
	}
}

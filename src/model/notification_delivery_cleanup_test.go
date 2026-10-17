package model

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

// deliveredPriorMessageEvent prepares, dispatches and completes an event that replaced
// an older Discord message, so its prior-message cleanup is pending and claimable.
func deliveredPriorMessageEvent(t *testing.T, db *gorm.DB, commentID, priorMessageID string) NotificationDeliveryEvent {
	t.Helper()
	intent := notificationIntent()
	intent.CommentID = commentID
	intent.CommentUpdatedAt = "2026-10-10T12:00:00Z"
	intent.PriorMessageID = priorMessageID
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	if event.PriorMessageID != priorMessageID {
		t.Fatalf("replaced prior message was not recorded: %+v", event)
	}
	attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("delivery attempt did not start: ok=%v err=%v", ok, err)
	}
	if err := CompleteNotificationDelivery(db, *attempt, "message-"+commentID, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.PriorCleanupPending || stored.PriorCleanupInFlight || stored.Disposition != DeliveryDelivered {
		t.Fatalf("a confirmed replacement must leave its prior message pending cleanup: %+v", stored)
	}
	return stored
}

func TestBeginPriorMessageCleanupRejectsInvalidRequests(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	if _, _, err := BeginPriorMessageCleanup(nil, "event-1"); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("cleanup without a database must fail closed: %v", err)
	}
	if _, _, err := BeginPriorMessageCleanup(db, "   "); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("cleanup without an event id must fail closed: %v", err)
	}
	claimed, ok, err := BeginPriorMessageCleanup(db, "missing-event")
	if ok || claimed != nil || err == nil {
		t.Fatalf("an unknown event must not be claimed: event=%+v ok=%v err=%v", claimed, ok, err)
	}
}

func TestBeginPriorMessageCleanupRequiresAnEligibleDeliveredEvent(t *testing.T) {
	t.Run("no prior message", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		intent := notificationIntent()
		event, err := PrepareNotificationDelivery(db, intent)
		if err != nil {
			t.Fatal(err)
		}
		attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now())
		if err != nil || !ok {
			t.Fatalf("delivery attempt did not start: ok=%v err=%v", ok, err)
		}
		if err := CompleteNotificationDelivery(db, *attempt, "message-1", "", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); ok || !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("an event that replaced nothing has no cleanup to claim: ok=%v err=%v", ok, err)
		}
	})

	t.Run("cleanup not pending yet", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		intent := notificationIntent()
		intent.PriorMessageID = "old-message"
		event, err := PrepareNotificationDelivery(db, intent)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); ok || !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("cleanup must wait for the new message to be confirmed: ok=%v err=%v", ok, err)
		}
		attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now())
		if err != nil || !ok {
			t.Fatalf("delivery attempt did not start: ok=%v err=%v", ok, err)
		}
		if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); ok || !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("an in-flight delivery must block cleanup: ok=%v err=%v", ok, err)
		}
		if err := CompleteNotificationDelivery(db, *attempt, "message-1", "", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); err != nil || !ok {
			t.Fatalf("a completed delivery must allow the claim: ok=%v err=%v", ok, err)
		}
	})

	t.Run("a second claim on the same event is refused", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		event := deliveredPriorMessageEvent(t, db, "comment-1", "old-message")
		claimed, ok, err := BeginPriorMessageCleanup(db, event.EventID)
		if err != nil || !ok || !claimed.PriorCleanupInFlight {
			t.Fatalf("prior-message cleanup was not claimed: event=%+v ok=%v err=%v", claimed, ok, err)
		}
		again, ok, err := BeginPriorMessageCleanup(db, event.EventID)
		if ok || again != nil || !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("an already claimed cleanup must not be claimed twice: event=%+v ok=%v err=%v", again, ok, err)
		}
		var stored NotificationDeliveryEvent
		if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if !stored.PriorCleanupPending || !stored.PriorCleanupInFlight {
			t.Fatalf("a refused duplicate claim must leave the original claim intact: %+v", stored)
		}
	})

	t.Run("a released claim can be retried", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		event := deliveredPriorMessageEvent(t, db, "comment-1", "old-message")
		if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); err != nil || !ok {
			t.Fatalf("prior-message cleanup was not claimed: ok=%v err=%v", ok, err)
		}
		if err := RecordPriorMessageCleanup(db, event.EventID, false, "prior_message_already_removed"); err != nil {
			t.Fatal(err)
		}
		reclaimed, ok, err := BeginPriorMessageCleanup(db, event.EventID)
		if err != nil || !ok || !reclaimed.PriorCleanupInFlight {
			t.Fatalf("a released cleanup must be claimable again: event=%+v ok=%v err=%v", reclaimed, ok, err)
		}
		if err := RecordPriorMessageCleanup(db, event.EventID, true, ""); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); ok || !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("a settled cleanup must never be claimed again: ok=%v err=%v", ok, err)
		}
	})
}

func TestBeginPriorMessageCleanupRefusesWhileAMutationFenceIsActive(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	event := deliveredPriorMessageEvent(t, db, "comment-1", "old-message")
	if err := db.Create(&ProductionNotificationWriteLock{ProductionID: "production-2", Revision: 1, MutationState: NotificationMutationReordering, MutationToken: "tok-2"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); err != nil || !ok {
		t.Fatalf("another Production's fence must not block this cleanup: ok=%v err=%v", ok, err)
	}
	if err := RecordPriorMessageCleanup(db, event.EventID, false, "prior_message_already_removed"); err != nil {
		t.Fatal(err)
	}
	if err := AcquireProductionNotificationWriteLock(db, "production-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE production_notification_write_locks SET mutation_state = ?, mutation_token = ? WHERE production_id = ?", NotificationMutationReordering, "tok-1", "production-1").Error; err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := BeginPriorMessageCleanup(db, event.EventID)
	if ok || claimed != nil || !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("an active external mutation must block cleanup: event=%+v ok=%v err=%v", claimed, ok, err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PriorCleanupInFlight {
		t.Fatalf("a refused claim must not be half-applied: %+v", stored)
	}
}

func TestBeginPriorMessageCleanupRefusesWhileAChannelDeletionIsUnresolved(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	event := deliveredPriorMessageEvent(t, db, "comment-1", "old-message")
	if err := db.Create(&ManagedChannelDeletion{
		WebhookRowID: 1, ProductionID: "production-1", GuildID: "guild-1", ChannelID: "channel-1",
		DiscordWebhookID: "webhook-1", Disposition: ChannelDeletionPending,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("a pending channel deletion must block cleanup: ok=%v err=%v", ok, err)
	}
	if err := db.Model(&ManagedChannelDeletion{}).Where("webhook_row_id = ?", 1).Update("disposition", ChannelDeletionComplete).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); err != nil || !ok {
		t.Fatalf("a completed channel deletion must not block cleanup: ok=%v err=%v", ok, err)
	}
}

func TestBeginPriorMessageCleanupRefusesWhenTheDestinationRowIsGone(t *testing.T) {
	t.Run("webhook deleted", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		event := deliveredPriorMessageEvent(t, db, "comment-1", "old-message")
		if err := db.Delete(&ProjectWebhook{}, 1).Error; err != nil {
			t.Fatal(err)
		}
		if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); ok || !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("cleanup without its destination webhook must not be claimed: ok=%v err=%v", ok, err)
		}
	})

	t.Run("webhook moved to another channel", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		event := deliveredPriorMessageEvent(t, db, "comment-1", "old-message")
		if err := db.Model(&ProjectWebhook{}).Where("id = ?", 1).Update("discord_channel_id", "channel-9").Error; err != nil {
			t.Fatal(err)
		}
		if _, ok, err := BeginPriorMessageCleanup(db, event.EventID); ok || !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("cleanup must not follow a webhook that moved channel: ok=%v err=%v", ok, err)
		}
	})
}

func TestBeginPriorMessageCleanupClaimsExactlyOnceUnderConcurrency(t *testing.T) {
	db := notificationDeliveryFileDB(t, filepath.Join(t.TempDir(), "prior-cleanup-race.db"))
	notificationDeliveryFixture(t, db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	event := deliveredPriorMessageEvent(t, db, "comment-1", "old-message")

	const workers = 4
	results := make([]error, workers)
	claimed := make([]bool, workers)
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for index := 0; index < workers; index++ {
		done.Add(1)
		go func(index int) {
			defer done.Done()
			start.Wait()
			_, ok, err := BeginPriorMessageCleanup(db, event.EventID)
			results[index] = err
			claimed[index] = ok
		}(index)
	}
	start.Done()
	done.Wait()

	winners := 0
	for index := 0; index < workers; index++ {
		switch {
		case claimed[index]:
			winners++
			if results[index] != nil {
				t.Fatalf("a winning claim must not report an error: %v", results[index])
			}
		case !errors.Is(results[index], ErrDeliveryUnavailable):
			t.Fatalf("a losing claim must report unavailability: ok=%v err=%v", claimed[index], results[index])
		}
	}
	if winners != 1 {
		t.Fatalf("exactly one worker may claim the cleanup: winners=%d", winners)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.PriorCleanupInFlight || !stored.PriorCleanupPending {
		t.Fatalf("the winning claim must be durable: %+v", stored)
	}
}

package model

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

// pendingDeletionTombstone tombstones the fixture's only destination through the
// batch entry point, leaving the channel deletion ready to finalize.
func pendingDeletionTombstone(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := BeginManagedChannelDeletionBatch(db, "production-1", teardownOnlyRoute(), time.Now(), true); err != nil {
		t.Fatalf("begin deletion batch: %v", err)
	}
}

func TestFinalizeManagedChannelDeletionGuards(t *testing.T) {
	t.Run("invalid requests", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := FinalizeManagedChannelDeletion(nil, 1, time.Now()); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("nil db must be rejected: %v", err)
		}
		if err := FinalizeManagedChannelDeletion(db, 0, time.Now()); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("zero webhook row must be rejected: %v", err)
		}
		if err := FinalizeManagedChannelDeletion(db, 999, time.Now()); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("unknown tombstone must be reported missing: %v", err)
		}
	})

	t.Run("in-flight delivery blocks finalize", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		event, err := PrepareNotificationDelivery(db, notificationIntent())
		if err != nil {
			t.Fatal(err)
		}
		if _, started, err := BeginNotificationDelivery(db, event.EventID, time.Now()); err != nil || !started {
			t.Fatalf("claim delivery: started=%v err=%v", started, err)
		}
		// The tombstone is seeded directly: the batch entry point refuses to run
		// while an unresolved delivery exists, which is the same condition this
		// guard protects against.
		if err := db.Create(&ManagedChannelDeletion{
			WebhookRowID:     1,
			ProductionID:     "production-1",
			GuildID:          "guild-1",
			ChannelID:        "channel-1",
			DiscordWebhookID: "webhook-1",
			Disposition:      ChannelDeletionPending,
			CreatedAt:        time.Now(),
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := FinalizeManagedChannelDeletion(db, 1, time.Now()); !errors.Is(err, ErrNotificationDeliveryInFlight) {
			t.Fatalf("an in-flight delivery must keep the channel deletion pending: %v", err)
		}
		if err := db.First(&ProjectWebhook{}, 1).Error; err != nil {
			t.Fatalf("webhook must survive a blocked finalize: %v", err)
		}
	})

	t.Run("guild drift blocks finalize", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		pendingDeletionTombstone(t, db)
		if err := db.Model(&Project{}).Where("kitsu_project_id = ?", "production-1").Update("discord_guild_id", "guild-9").Error; err != nil {
			t.Fatal(err)
		}
		if err := FinalizeManagedChannelDeletion(db, 1, time.Now()); !errors.Is(err, ErrChannelDeletionConflict) {
			t.Fatalf("a tombstone pointing at a foreign guild must not finalize: %v", err)
		}
		if err := db.First(&ProjectWebhook{}, 1).Error; err != nil {
			t.Fatalf("webhook must survive a conflicting finalize: %v", err)
		}
	})

	t.Run("webhook drift blocks finalize", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		pendingDeletionTombstone(t, db)
		if err := db.Model(&ProjectWebhook{}).Where("id = ?", 1).Update("webhook_url", "https://discord.com/api/webhooks/webhook-9/token").Error; err != nil {
			t.Fatal(err)
		}
		if err := FinalizeManagedChannelDeletion(db, 1, time.Now()); !errors.Is(err, ErrChannelDeletionConflict) {
			t.Fatalf("a tombstone whose webhook identity changed must not finalize: %v", err)
		}
		if err := db.First(&ProductionNotificationRoute{}, 1).Error; err != nil {
			t.Fatalf("routes must survive a conflicting finalize: %v", err)
		}
	})

	t.Run("missing webhook row blocks finalize", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		pendingDeletionTombstone(t, db)
		if err := db.Delete(&ProjectWebhook{}, 1).Error; err != nil {
			t.Fatal(err)
		}
		if err := FinalizeManagedChannelDeletion(db, 1, time.Now()); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("a vanished webhook row must be reported missing: %v", err)
		}
	})
}

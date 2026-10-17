package model

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

// teardownOnlyRoute is the single destination seeded by notificationDeliveryFixture.
func teardownOnlyRoute() []ManagedChannelDeletionTarget {
	return []ManagedChannelDeletionTarget{{WebhookRowID: 1, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1"}}
}

// teardownSecondDestination adds a second complete route to the fixture so a
// batch can cover several destinations.
func teardownSecondDestination(t *testing.T, db *gorm.DB) ProjectWebhook {
	t.Helper()
	second := ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-2", WebhookURL: "https://discord.com/api/webhooks/webhook-2/token"}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-2", DestinationWebhookID: second.ID, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	return second
}

func TestProductionTeardownDeletionBatchRequiresTokenAndFullRouteCoverage(t *testing.T) {
	t.Run("blank token", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		for _, token := range []string{"", "   "} {
			if err := BeginManagedChannelDeletionBatchForProductionTeardown(db, "production-1", teardownOnlyRoute(), time.Now(), true, token); !errors.Is(err, gorm.ErrInvalidData) {
				t.Fatalf("teardown token %q must be rejected before touching durable state: %v", token, err)
			}
		}
		var count int64
		if err := db.Model(&ManagedChannelDeletion{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("a tokenless teardown must not tombstone anything: count=%d err=%v", count, err)
		}
	})

	t.Run("last route needs an explicit teardown", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := BeginManagedChannelDeletionBatch(db, "production-1", teardownOnlyRoute(), time.Now(), false); !errors.Is(err, ErrChannelDeletionConflict) {
			t.Fatalf("removing the last notification route must demand an explicit Production teardown: %v", err)
		}
		if err := BeginManagedChannelDeletionBatchForProductionTeardown(db, "production-1", teardownOnlyRoute(), time.Now(), true, "teardown-token-1"); err != nil {
			t.Fatalf("a whole-Production teardown should tombstone its only route: %v", err)
		}
		var count int64
		if err := db.Model(&ManagedChannelDeletion{}).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("teardown should record exactly one tombstone: count=%d err=%v", count, err)
		}
	})

	t.Run("partial coverage", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		second := teardownSecondDestination(t, db)
		if err := BeginManagedChannelDeletionBatchForProductionTeardown(db, "production-1", teardownOnlyRoute(), time.Now(), true, "teardown-token-1"); !errors.Is(err, ErrChannelDeletionConflict) {
			t.Fatalf("a teardown that does not cover every route must be refused: %v", err)
		}
		full := append(teardownOnlyRoute(), ManagedChannelDeletionTarget{WebhookRowID: second.ID, GuildID: "guild-1", ChannelID: "channel-2", DiscordWebhookID: "webhook-2"})
		if err := BeginManagedChannelDeletionBatchForProductionTeardown(db, "production-1", full, time.Now(), true, "teardown-token-1"); err != nil {
			t.Fatalf("a full-coverage teardown should tombstone every destination: %v", err)
		}
		var count int64
		if err := db.Model(&ManagedChannelDeletion{}).Count(&count).Error; err != nil || count != 2 {
			t.Fatalf("a full teardown should record both tombstones: count=%d err=%v", count, err)
		}
	})
}

func TestProductionTeardownDeletionHonorsTheMutationBarrier(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	token, err := BeginProductionDeletionMutation(db, "production-1")
	if err != nil || token == "" {
		t.Fatalf("a teardown fence should start on an idle Production: token=%q err=%v", token, err)
	}
	if err := BeginManagedChannelDeletionBatch(db, "production-1", teardownOnlyRoute(), time.Now(), true); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("a plain channel deletion must not run under a whole-Production teardown fence: %v", err)
	}
	if err := BeginManagedChannelDeletionBatchForProductionTeardown(db, "production-1", teardownOnlyRoute(), time.Now(), true, "wrong-token"); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("teardown tombstones must carry the active deletion token: %v", err)
	}
	if err := BeginManagedChannelDeletionBatchForProductionTeardown(db, "production-1", teardownOnlyRoute(), time.Now(), true, token); err != nil {
		t.Fatalf("teardown tombstones should accept the matching deletion token: %v", err)
	}
	if err := FinalizeManagedChannelDeletion(db, 1, time.Now()); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("a plain finalize must not run under a whole-Production teardown fence: %v", err)
	}
	if err := FinalizeManagedChannelDeletionForProductionTeardown(db, 1, time.Now(), ""); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("a blank teardown token must be rejected: %v", err)
	}
	if err := FinalizeManagedChannelDeletionForProductionTeardown(db, 1, time.Now(), "wrong-token"); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("finalize must require the matching deletion token: %v", err)
	}
	if err := FinalizeManagedChannelDeletionForProductionTeardown(db, 1, time.Now(), token); err != nil {
		t.Fatalf("the matching deletion token should finalize the tombstone: %v", err)
	}
	if FindProjectWebhookByID(db, 1) != nil || len(ListProductionNotificationRoutes(db, "production-1")) != 0 {
		t.Fatal("teardown finalize did not remove the local destination state")
	}
	var tombstone ManagedChannelDeletion
	if err := db.Where("webhook_row_id = ?", 1).First(&tombstone).Error; err != nil || tombstone.Disposition != ChannelDeletionComplete || tombstone.CompletedAt == nil {
		t.Fatalf("teardown finalize did not complete the tombstone: row=%+v err=%v", tombstone, err)
	}
	if err := FinalizeManagedChannelDeletionForProductionTeardown(db, 1, time.Now(), token); !errors.Is(err, ErrChannelDeletionConflict) {
		t.Fatalf("a completed teardown must not finalize twice: %v", err)
	}
}

func TestManagedChannelDeletionBatchRejectsInconsistentTargets(t *testing.T) {
	t.Run("invalid requests", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		// The repeated-destination guard is only reachable once the per-channel row
		// count already matches, which needs a second webhook on the same channel.
		if err := db.Create(&ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-1", WebhookURL: "https://discord.com/api/webhooks/webhook-1b/token"}).Error; err != nil {
			t.Fatal(err)
		}
		targets := teardownOnlyRoute()
		if err := BeginManagedChannelDeletionBatch(nil, "production-1", targets, time.Now(), false); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("a nil database must be rejected: %v", err)
		}
		if err := BeginManagedChannelDeletionBatch(db, "  ", targets, time.Now(), false); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("a blank Production must be rejected: %v", err)
		}
		if err := BeginManagedChannelDeletionBatch(db, "production-1", nil, time.Now(), false); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("an empty target list must be rejected: %v", err)
		}
		if err := BeginManagedChannelDeletionBatch(db, "production-1", []ManagedChannelDeletionTarget{targets[0], targets[0]}, time.Now(), false); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("a repeated destination must be rejected: %v", err)
		}
		wrongGuild := []ManagedChannelDeletionTarget{{WebhookRowID: 1, GuildID: "guild-2", ChannelID: "channel-1", DiscordWebhookID: "webhook-1"}}
		if err := BeginManagedChannelDeletionBatch(db, "production-1", wrongGuild, time.Now(), false); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("a destination from another guild must be rejected: %v", err)
		}
		var count int64
		if err := db.Model(&ManagedChannelDeletion{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rejected requests must not tombstone anything: count=%d err=%v", count, err)
		}
	})

	t.Run("webhook identity mismatch", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		drifted := []ManagedChannelDeletionTarget{{WebhookRowID: 1, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-9"}}
		if err := BeginManagedChannelDeletionBatch(db, "production-1", drifted, time.Now(), true); !errors.Is(err, ErrChannelDeletionConflict) {
			t.Fatalf("the tombstoned Discord webhook must match the stored URL: %v", err)
		}
	})

	t.Run("channel row count mismatch", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := db.Create(&ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-1", WebhookURL: "https://discord.com/api/webhooks/webhook-1b/token"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := BeginManagedChannelDeletionBatch(db, "production-1", teardownOnlyRoute(), time.Now(), true); !errors.Is(err, ErrChannelDeletionConflict) {
			t.Fatalf("a partial deletion of a multi-webhook channel must be refused: %v", err)
		}
	})

	t.Run("tombstone drift", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := db.Create(&ManagedChannelDeletion{WebhookRowID: 1, ProductionID: "production-1", GuildID: "guild-1", ChannelID: "channel-9", DiscordWebhookID: "webhook-1", Disposition: ChannelDeletionPending}).Error; err != nil {
			t.Fatal(err)
		}
		if err := BeginManagedChannelDeletionBatch(db, "production-1", teardownOnlyRoute(), time.Now(), true); !errors.Is(err, ErrChannelDeletionConflict) {
			t.Fatalf("an existing tombstone for another destination must be refused: %v", err)
		}
	})

	t.Run("needs-help tombstone resumes", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := db.Create(&ManagedChannelDeletion{WebhookRowID: 1, ProductionID: "production-1", GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1", Disposition: ChannelDeletionNeedsHelp, FailureClass: "discord_channel_delete_failed"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := BeginManagedChannelDeletionBatch(db, "production-1", teardownOnlyRoute(), time.Now(), true); err != nil {
			t.Fatalf("a retried deletion should resume a needs-help tombstone: %v", err)
		}
		var tombstone ManagedChannelDeletion
		if err := db.Where("webhook_row_id = ?", 1).First(&tombstone).Error; err != nil || tombstone.Disposition != ChannelDeletionPending || tombstone.FailureClass != "" {
			t.Fatalf("resumed tombstone should return to pending without a stale failure class: row=%+v err=%v", tombstone, err)
		}
		if err := db.Where("webhook_row_id = ?", 1).Delete(&ManagedChannelDeletion{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&ManagedChannelDeletion{WebhookRowID: 1, ProductionID: "production-1", GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1", Disposition: ChannelDeletionComplete}).Error; err != nil {
			t.Fatal(err)
		}
		if err := BeginManagedChannelDeletionBatch(db, "production-1", teardownOnlyRoute(), time.Now(), true); !errors.Is(err, ErrChannelDeletionConflict) {
			t.Fatalf("a completed tombstone must not be reopened: %v", err)
		}
	})
}

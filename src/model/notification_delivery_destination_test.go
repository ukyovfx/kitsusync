package model

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

const resolvableWebhookURL = "https://discord.com/api/webhooks/webhook-1/token"

func TestResolveNotificationDestinationRejectsInvalidRequests(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	requests := []struct {
		name                                  string
		productionID, taskTypeID, url, hookID string
	}{
		{"blank production", "", "task-type-1", resolvableWebhookURL, "webhook-1"},
		{"blank task type", "production-1", "  ", resolvableWebhookURL, "webhook-1"},
		{"blank webhook url", "production-1", "task-type-1", "", "webhook-1"},
		{"blank webhook id", "production-1", "task-type-1", resolvableWebhookURL, "  "},
	}
	for _, request := range requests {
		if _, err := ResolveNotificationDestination(db, request.productionID, request.taskTypeID, request.url, request.hookID); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("%s must fail closed: %v", request.name, err)
		}
	}
	if _, err := ResolveNotificationDestination(nil, "production-1", "task-type-1", resolvableWebhookURL, "webhook-1"); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("resolution without a database must fail closed: %v", err)
	}
}

func TestResolveNotificationDestinationRequiresKnownRoute(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	if _, err := ResolveNotificationDestination(db, "production-9", "task-type-1", resolvableWebhookURL, "webhook-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("an unknown Production must not resolve a destination: %v", err)
	}
	if _, err := ResolveNotificationDestination(db, "production-1", "task-type-9", resolvableWebhookURL, "webhook-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("a task type without a route must not resolve a destination: %v", err)
	}
	if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", "https://discord.com/api/webhooks/webhook-1/other-token", "webhook-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("a url that is not the stored route destination must not resolve: %v", err)
	}
}

func TestResolveNotificationDestinationUnavailableForInconsistentDestinations(t *testing.T) {
	t.Run("validation-only project", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := db.Model(&Project{}).Where("kitsu_project_id = ?", "production-1").Update("validation_only", true).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", resolvableWebhookURL, "webhook-1"); !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("a validation-only project must never dispatch: %v", err)
		}
	})

	t.Run("project without a guild", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := db.Model(&Project{}).Where("kitsu_project_id = ?", "production-1").Update("discord_guild_id", "   ").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", resolvableWebhookURL, "webhook-1"); !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("a destination without a guild must never dispatch: %v", err)
		}
	})

	t.Run("webhook without a channel", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := db.Model(&ProjectWebhook{}).Where("id = ?", 1).Update("discord_channel_id", "").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", resolvableWebhookURL, "webhook-1"); !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("a webhook without a channel must never dispatch: %v", err)
		}
	})

	t.Run("route channel contradicting the webhook", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := db.Model(&ProductionNotificationRoute{}).Where("production_id = ? AND task_type_id = ?", "production-1", "task-type-1").Update("destination_channel_id", "channel-9").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", resolvableWebhookURL, "webhook-1"); !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("a route that disagrees with its webhook channel must never dispatch: %v", err)
		}
	})

	t.Run("route channel may be unset", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := db.Model(&ProductionNotificationRoute{}).Where("production_id = ? AND task_type_id = ?", "production-1", "task-type-1").Update("destination_channel_id", "").Error; err != nil {
			t.Fatal(err)
		}
		identity, err := ResolveNotificationDestination(db, "production-1", "task-type-1", resolvableWebhookURL, "webhook-1")
		if err != nil || identity.ChannelID != "channel-1" {
			t.Fatalf("an unset route channel must defer to the webhook channel: identity=%+v err=%v", identity, err)
		}
	})

	t.Run("only a completed tombstone allows dispatch again", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		if err := db.Create(&ManagedChannelDeletion{
			WebhookRowID: 1, ProductionID: "production-1", GuildID: "guild-1", ChannelID: "channel-1",
			DiscordWebhookID: "webhook-1", Disposition: ChannelDeletionComplete,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", resolvableWebhookURL, "webhook-1"); err != nil {
			t.Fatalf("a completed tombstone no longer blocks its own destination: %v", err)
		}
		if err := db.Model(&ManagedChannelDeletion{}).Where("webhook_row_id = ?", 1).Update("disposition", ChannelDeletionPending).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", resolvableWebhookURL, "webhook-1"); !errors.Is(err, ErrDeliveryUnavailable) {
			t.Fatalf("a pending tombstone must block dispatch: %v", err)
		}
	})
}

func TestResolveNotificationDestinationStaysProductionScoped(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	_, _ = seedSecondProductionDestination(t, db, notificationIntent())
	first, err := ResolveNotificationDestination(db, "production-1", "task-type-1", resolvableWebhookURL, "webhook-1")
	if err != nil || first.WebhookRowID != 1 || first.GuildID != "guild-1" || first.ChannelID != "channel-1" {
		t.Fatalf("first Production destination drifted: identity=%+v err=%v", first, err)
	}
	var secondWebhook ProjectWebhook
	if err := db.Where("kitsu_project_id = ?", "production-2").First(&secondWebhook).Error; err != nil {
		t.Fatal(err)
	}
	second, err := ResolveNotificationDestination(db, "production-2", "task-type-2", "https://discord.com/api/webhooks/webhook-2/token", "webhook-2")
	if err != nil || second.WebhookRowID != secondWebhook.ID || second.GuildID != "guild-2" || second.ChannelID != "channel-2" {
		t.Fatalf("second Production destination drifted: identity=%+v err=%v", second, err)
	}
	if second.WebhookRowID == first.WebhookRowID {
		t.Fatal("two Productions must not resolve to one webhook row")
	}
}

// The resolver derives the routed webhook row from the stored route but records the
// caller-supplied webhook id as the destination identity without cross-checking it
// against the url. Both production callers pass DiscordWebhookIDFromURL(webhookURL),
// so the divergence is unreachable today; this test pins the trust boundary. If the
// resolver is ever hardened to reject a mismatched id, assert ErrDeliveryUnavailable.
func TestResolveNotificationDestinationTrustsCallerWebhookID(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	identity, err := ResolveNotificationDestination(db, "production-1", "task-type-1", resolvableWebhookURL, "webhook-9")
	if err != nil {
		t.Fatalf("resolver rejected a non-empty caller webhook id: %v", err)
	}
	if identity.WebhookID != "webhook-9" || identity.WebhookID == notificationWebhookID(resolvableWebhookURL) {
		t.Fatalf("caller webhook id should be recorded verbatim for this fixture: %+v", identity)
	}
	deliveredAt := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	if err := db.Create(&NotificationDeliveryEvent{
		EventID: "delivered-webhook-1", TaskID: "task-1", ProductionID: "production-1", TaskTypeID: "task-type-1", TaskStatus: "WFA",
		PayloadJSON: `{"content":"prior"}`, PayloadSHA256: "sha-delivered",
		WebhookRowID: 1, DestinationGuildID: "guild-1", DestinationChannelID: "channel-1",
		DestinationWebhookID:   notificationWebhookID(resolvableWebhookURL),
		DestinationFingerprint: notificationDestinationFingerprint("guild-1", "channel-1", notificationWebhookID(resolvableWebhookURL)),
		Disposition:            DeliveryDelivered, DiscordMessageID: "prior-message", DeliveredAt: &deliveredAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	previous, err := LatestDeliveredNotificationForDestination(db, "task-1", identity)
	if err != nil {
		t.Fatal(err)
	}
	if previous != nil {
		t.Fatalf("expected the mismatched identity to miss the delivered history: %+v", previous)
	}
}

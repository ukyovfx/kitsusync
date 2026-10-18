package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func reconciliationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProductionNotificationRoute{}, &ManagedChannelDeletion{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &NotificationDeliveryReconciliationAudit{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func unknownReconciliationFixture(t *testing.T, db *gorm.DB) NotificationDeliveryEvent {
	t.Helper()
	if err := db.Create(&Project{KitsuProjectID: "production-a", DiscordGuildID: "456789012345678901"}).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: "production-a", WebhookURL: "https://discord.com/api/webhooks/123456789012345678/secret-token", DiscordChannelID: "678901234567890123"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-a", TaskTypeID: "task-type-a", DestinationWebhookID: webhook.ID, DestinationChannelID: webhook.DiscordChannelID}).Error; err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	event := NotificationDeliveryEvent{
		EventID: "delivery-event-1", TaskID: "task-a", ProductionID: "production-a", TaskTypeID: "task-type-a",
		DeliverySource: "vfx_files", SourceEventID: "plugin-event-1", PayloadJSON: `{"content":"synthetic","embeds":[{"title":"Files"}]}`,
		PayloadSHA256: strings.Repeat("a", 64), WebhookRowID: webhook.ID, DestinationGuildID: "456789012345678901",
		DestinationChannelID: webhook.DiscordChannelID, DestinationWebhookID: "123456789012345678", DestinationFingerprint: notificationDestinationFingerprint("456789012345678901", webhook.DiscordChannelID, "123456789012345678"),
		Disposition: DeliveryUnknown, FailureClass: "process_restart_during_delivery", AttemptID: "0123456789abcdef0123456789abcdef", AttemptCount: 1, AttemptStartedAt: &started,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	return event
}

func testWebhookURLFingerprint() string {
	hash := sha256.Sum256([]byte("https://discord.com/api/webhooks/123456789012345678/secret-token"))
	return hex.EncodeToString(hash[:])
}

func TestRecordProviderVerifiedUnknownDeliveryIsAttemptBoundAuditedAndIdempotent(t *testing.T) {
	db := reconciliationTestDB(t)
	event := unknownReconciliationFixture(t, db)
	evidence := VerifiedNotificationDeliveryEvidence{
		EventID: event.EventID, AttemptID: event.AttemptID, ProductionID: event.ProductionID,
		WebhookRowID: event.WebhookRowID, DestinationGuildID: event.DestinationGuildID,
		DestinationChannelID: event.DestinationChannelID, DestinationWebhookID: event.DestinationWebhookID,
		MessageID: "1234567890123456789", ThreadID: "987654321098765432", PayloadSHA256: event.PayloadSHA256, WebhookURLFingerprint: testWebhookURLFingerprint(),
		ProviderResponseSHA256: strings.Repeat("b", 64), ActorEmail: "manager@example.test", VerifiedAt: time.Date(2026, 10, 18, 12, 2, 0, 0, time.UTC),
	}
	first, err := RecordProviderVerifiedUnknownDelivery(db, evidence)
	if err != nil || !first {
		t.Fatalf("first provider verification should reconcile: changed=%t err=%v", first, err)
	}
	second, err := RecordProviderVerifiedUnknownDelivery(db, evidence)
	if err != nil || second {
		t.Fatalf("same evidence should be idempotent: changed=%t err=%v", second, err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Disposition != DeliveryDelivered || stored.DiscordMessageID != evidence.MessageID || stored.DiscordThreadID != evidence.ThreadID || stored.DeliveredAt == nil {
		t.Fatalf("verified event state=%+v", stored)
	}
	var audits []NotificationDeliveryReconciliationAudit
	if err := db.Where("event_id = ?", event.EventID).Find(&audits).Error; err != nil || len(audits) != 1 {
		t.Fatalf("expected one immutable audit row, got %d err=%v", len(audits), err)
	}
	if audits[0].Action != NotificationReconciliationProviderVerified || audits[0].ActorEmail != evidence.ActorEmail || audits[0].AttemptID != evidence.AttemptID || audits[0].ProviderResponseSHA256 != evidence.ProviderResponseSHA256 {
		t.Fatalf("audit did not preserve reconciliation evidence: %+v", audits[0])
	}
	if db.Migrator().HasColumn(&NotificationDeliveryReconciliationAudit{}, "webhook_url") {
		t.Fatal("audit schema must never add a webhook credential column")
	}
}

func TestRecordProviderVerifiedUnknownDeliveryLeavesUnknownOnUnverifiableOrStaleEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*VerifiedNotificationDeliveryEvidence)
	}{
		{name: "stale attempt", mutate: func(e *VerifiedNotificationDeliveryEvidence) { e.AttemptID = "ffffffffffffffffffffffffffffffff" }},
		{name: "wrong production", mutate: func(e *VerifiedNotificationDeliveryEvidence) { e.ProductionID = "production-b" }},
		{name: "wrong destination", mutate: func(e *VerifiedNotificationDeliveryEvidence) { e.DestinationChannelID = "different-channel" }},
		{name: "invalid evidence hash", mutate: func(e *VerifiedNotificationDeliveryEvidence) { e.ProviderResponseSHA256 = "not-a-hash" }},
		{name: "missing provider message", mutate: func(e *VerifiedNotificationDeliveryEvidence) { e.MessageID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := reconciliationTestDB(t)
			event := unknownReconciliationFixture(t, db)
			evidence := VerifiedNotificationDeliveryEvidence{
				EventID: event.EventID, AttemptID: event.AttemptID, ProductionID: event.ProductionID,
				WebhookRowID: event.WebhookRowID, DestinationGuildID: event.DestinationGuildID,
				DestinationChannelID: event.DestinationChannelID, DestinationWebhookID: event.DestinationWebhookID,
				MessageID: "1234567890123456789", ThreadID: "987654321098765432", PayloadSHA256: event.PayloadSHA256, WebhookURLFingerprint: testWebhookURLFingerprint(),
				ProviderResponseSHA256: strings.Repeat("b", 64), ActorEmail: "manager@example.test", VerifiedAt: time.Now().UTC(),
			}
			tc.mutate(&evidence)
			if _, err := RecordProviderVerifiedUnknownDelivery(db, evidence); err == nil {
				t.Fatal("invalid/stale evidence unexpectedly resolved the unknown delivery")
			}
			var stored NotificationDeliveryEvent
			if err := db.Where("event_id = ?", event.EventID).Take(&stored).Error; err != nil || stored.Disposition != DeliveryUnknown {
				t.Fatalf("invalid evidence changed unknown state: %+v err=%v", stored, err)
			}
			var count int64
			if err := db.Model(&NotificationDeliveryReconciliationAudit{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("invalid evidence wrote an audit row: count=%d err=%v", count, err)
			}
		})
	}
}

func TestRecordProviderVerifiedUnknownDeliveryDoesNotOverwriteConflictingResolution(t *testing.T) {
	db := reconciliationTestDB(t)
	event := unknownReconciliationFixture(t, db)
	evidence := VerifiedNotificationDeliveryEvidence{
		EventID: event.EventID, AttemptID: event.AttemptID, ProductionID: event.ProductionID,
		WebhookRowID: event.WebhookRowID, DestinationGuildID: event.DestinationGuildID,
		DestinationChannelID: event.DestinationChannelID, DestinationWebhookID: event.DestinationWebhookID,
		MessageID: "1234567890123456789", ThreadID: "987654321098765432", PayloadSHA256: event.PayloadSHA256, WebhookURLFingerprint: testWebhookURLFingerprint(),
		ProviderResponseSHA256: strings.Repeat("b", 64), ActorEmail: "manager@example.test", VerifiedAt: time.Now().UTC(),
	}
	if _, err := RecordProviderVerifiedUnknownDelivery(db, evidence); err != nil {
		t.Fatal(err)
	}
	other := evidence
	other.MessageID = "1234567890123456790"
	other.ProviderResponseSHA256 = strings.Repeat("c", 64)
	if _, err := RecordProviderVerifiedUnknownDelivery(db, other); !errors.Is(err, ErrDeliveryReconciliationConflict) {
		t.Fatalf("conflicting second proof should fail closed: %v", err)
	}
	var audits int64
	if err := db.Model(&NotificationDeliveryReconciliationAudit{}).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("conflicting proof changed audit count: %d %v", audits, err)
	}
}

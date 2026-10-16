package model

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func shareDisclosureTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "share-consent.db")+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &VFXFilesProductionConfig{}, &VFXFilesEventInbox{}, &VFXFilesEventNonce{}, &VFXFilesEventConflict{}, &VFXFilesDriveLinkDisclosureConsent{}, &VFXFilesDriveLinkDisclosureAudit{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	return db
}

func TestVFXFilesDriveLinkDisclosureConsentDefaultsOffAndBindsExactRouteRevisionAndIdentity(t *testing.T) {
	db := shareDisclosureTestDB(t)
	productionID, taskTypeID := vfxProductionID, "50000000-0000-4000-8000-000000000010"
	now := time.Date(2026, 10, 17, 4, 0, 0, 0, time.UTC)
	if err := db.Create(&Project{KitsuProjectID: productionID, Name: "Synthetic"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationConfig{ProductionID: productionID, Enabled: true, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	route := ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: taskTypeID, DestinationWebhookID: 1}
	if err := db.Create(&route).Error; err != nil {
		t.Fatal(err)
	}
	project := FindProjectByKitsuID(db, productionID)
	revision, err := ProductionNotificationRevisionWithError(db, project.ID, productionID)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := VFXFilesDriveLinkDisclosureAllowed(db, productionID, taskTypeID)
	if err != nil || allowed {
		t.Fatalf("missing consent should deny disclosure: allowed=%t err=%v", allowed, err)
	}
	if err := SetVFXFilesDriveLinkDisclosureConsent(db, productionID, taskTypeID, revision, "admin@example.test", true, now); err != nil {
		t.Fatal(err)
	}
	allowed, err = VFXFilesDriveLinkDisclosureAllowed(db, productionID, taskTypeID)
	if err != nil || !allowed {
		t.Fatalf("explicit consent not active: allowed=%t err=%v", allowed, err)
	}
	if err := db.Delete(&route).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: taskTypeID, DestinationWebhookID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	allowed, err = VFXFilesDriveLinkDisclosureAllowed(db, productionID, taskTypeID)
	if err != nil || allowed {
		t.Fatalf("recreated route must not resurrect old consent: allowed=%t err=%v", allowed, err)
	}
	var auditCount int64
	if err := db.Model(&VFXFilesDriveLinkDisclosureAudit{}).Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}
}

func TestVFXFilesShareIntakeDurablyQuarantinesWithoutRouteConsent(t *testing.T) {
	db := shareDisclosureTestDB(t)
	productionID, taskTypeID := vfxProductionID, "50000000-0000-4000-8000-000000000010"
	now := time.Now().UTC()
	if err := db.Create(&Project{KitsuProjectID: productionID, Name: "Synthetic"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationConfig{ProductionID: productionID, Enabled: true, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: taskTypeID, DestinationWebhookID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&VFXFilesProductionConfig{ProductionID: productionID, ActivationEpoch: 4, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	input := VFXFilesEventInput{ProducerID: "vfx-files-plugin", EventID: vfxEventID1, EventType: "file_share.created", ProductionID: productionID, ActivationEpoch: 4, RecipientTaskID: "50000000-0000-4000-8000-000000000002", RecipientTaskTypeID: taskTypeID, ShareID: "70000000-0000-4000-8000-000000000001", ShareTargetID: "80000000-0000-4000-8000-000000000001", Payload: []byte(`{"share":"synthetic"}`)}
	results, err := AcceptVFXFilesEvents(db, "key-share", "share-nonce-0000000001", []VFXFilesEventInput{input}, now)
	if err != nil || len(results) != 1 || results[0].Outcome != VFXFilesEventQuarantined || results[0].Code != "drive_link_disclosure_consent_missing" {
		t.Fatalf("no-consent intake=%+v err=%v", results, err)
	}
	var stored VFXFilesEventInbox
	if err := db.Where("event_id = ?", input.EventID).Take(&stored).Error; err != nil || stored.Outcome != VFXFilesEventQuarantined || stored.ProcessedAt == nil {
		t.Fatalf("quarantine was not durable: %+v err=%v", stored, err)
	}
	if err := SetVFXFilesDriveLinkDisclosureConsent(db, productionID, taskTypeID, ProductionNotificationRevision(db, FindProjectByKitsuID(db, productionID).ID, productionID), "admin@example.test", true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	duplicate, err := AcceptVFXFilesEvents(db, "key-share", "share-nonce-0000000002", []VFXFilesEventInput{input}, time.Now().UTC())
	if err != nil || len(duplicate) != 1 || duplicate[0].Outcome != VFXFilesEventDuplicate {
		t.Fatalf("quarantined event must remain immutable after later consent: %+v err=%v", duplicate, err)
	}
	if err := db.Where("event_id = ?", input.EventID).Take(&stored).Error; err != nil || stored.Outcome != VFXFilesEventQuarantined {
		t.Fatalf("later consent promoted quarantined event: %+v err=%v", stored, err)
	}
}

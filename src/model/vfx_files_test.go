package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	vfxProductionID = "10000000-0000-4000-8000-000000000001"
	vfxEventID1     = "60000000-0000-4000-8000-000000000001"
	vfxEventID2     = "60000000-0000-4000-8000-000000000002"
)

func vfxEventTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "vfx-events.db")+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&VFXFilesProductionConfig{}, &VFXFilesEventInbox{}, &VFXFilesEventNonce{}, &VFXFilesEventConflict{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func vfxInput(id, delivery string, revision, epoch uint64, payload string) VFXFilesEventInput {
	return VFXFilesEventInput{ProducerID: "vfx-files-plugin", EventID: id, EventType: "file_delivery.ready", ProductionID: vfxProductionID, ActivationEpoch: epoch, DeliveryID: delivery, DeliveryRevision: revision, RecipientTaskID: "50000000-0000-4000-8000-000000000002", RecipientTaskTypeID: "50000000-0000-4000-8000-000000000010", Payload: []byte(payload)}
}

func TestAcceptVFXFilesEventsPersistsDeduplicationConflictAndActivationQuarantine(t *testing.T) {
	db := vfxEventTestDB(t)
	if err := db.Create(&VFXFilesProductionConfig{ProductionID: vfxProductionID, ConfigRevision: 3, ActivationEpoch: 4, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	input := vfxInput(vfxEventID1, "90000000-0000-4000-8000-000000000001", 1, 4, `{"body":"safe synthetic"}`)
	first, err := AcceptVFXFilesEvents(db, "key-a", "nonce-000000000000001", []VFXFilesEventInput{input}, now)
	if err != nil || len(first) != 1 || first[0].Outcome != VFXFilesEventAccepted {
		t.Fatalf("first accept=%#v err=%v", first, err)
	}
	duplicate, err := AcceptVFXFilesEvents(db, "key-a", "nonce-000000000000002", []VFXFilesEventInput{input}, now.Add(time.Second))
	if err != nil || duplicate[0].Outcome != VFXFilesEventDuplicate {
		t.Fatalf("duplicate=%#v err=%v", duplicate, err)
	}
	conflict := input
	conflict.Payload = []byte(`{"body":"different"}`)
	conflicted, err := AcceptVFXFilesEvents(db, "key-a", "nonce-000000000000003", []VFXFilesEventInput{conflict}, now.Add(2*time.Second))
	if err != nil || conflicted[0].Outcome != VFXFilesEventQuarantined || conflicted[0].Code != "event_identity_conflict" {
		t.Fatalf("conflict=%#v err=%v", conflicted, err)
	}
	stale := vfxInput(vfxEventID2, input.DeliveryID, 1, 4, `{"body":"older or equal revision"}`)
	staleResult, err := AcceptVFXFilesEvents(db, "key-a", "nonce-000000000000004", []VFXFilesEventInput{stale}, now.Add(3*time.Second))
	if err != nil || staleResult[0].Code != "stale_delivery_revision" {
		t.Fatalf("stale revision=%#v err=%v", staleResult, err)
	}
	wrongEpoch := vfxInput("60000000-0000-4000-8000-000000000003", "90000000-0000-4000-8000-000000000002", 1, 3, `{"body":"old epoch"}`)
	wrongResult, err := AcceptVFXFilesEvents(db, "key-a", "nonce-000000000000005", []VFXFilesEventInput{wrongEpoch}, now.Add(4*time.Second))
	if err != nil || wrongResult[0].Code != "activation_epoch_mismatch" {
		t.Fatalf("wrong epoch=%#v err=%v", wrongResult, err)
	}
	var preserved VFXFilesEventInbox
	if err := db.Where("event_id = ?", vfxEventID1).First(&preserved).Error; err != nil || preserved.PayloadJSON != string(input.Payload) || preserved.PayloadSHA256 == "" || preserved.Outcome != VFXFilesEventAccepted {
		t.Fatalf("original event changed: %#v err=%v", preserved, err)
	}
	var conflictCount int64
	if err := db.Model(&VFXFilesEventConflict{}).Count(&conflictCount).Error; err != nil || conflictCount != 1 {
		t.Fatalf("conflict diagnostics=%d err=%v", conflictCount, err)
	}
}

func TestAcceptVFXFilesEventsNonceReuseAndTransactionRollback(t *testing.T) {
	db := vfxEventTestDB(t)
	input := vfxInput(vfxEventID1, "90000000-0000-4000-8000-000000000001", 1, 1, `{"ok":true}`)
	if _, err := AcceptVFXFilesEvents(db, "key-a", "nonce-000000000000001", []VFXFilesEventInput{input}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := AcceptVFXFilesEvents(db, "key-a", "nonce-000000000000001", []VFXFilesEventInput{input}, time.Now()); !errors.Is(err, ErrVFXFilesEventConflict) {
		t.Fatalf("reused nonce err=%v", err)
	}
	invalid := vfxInput(vfxEventID2, "90000000-0000-4000-8000-000000000002", 1, 1, "")
	if _, err := AcceptVFXFilesEvents(db, "key-b", "nonce-000000000000002", []VFXFilesEventInput{invalid}, time.Now()); !errors.Is(err, ErrVFXFilesInvalidBatch) {
		t.Fatalf("invalid batch err=%v", err)
	}
	var nonceCount int64
	if err := db.Model(&VFXFilesEventNonce{}).Where("key_id = ?", "key-b").Count(&nonceCount).Error; err != nil || nonceCount != 0 {
		t.Fatalf("rolled-back nonce count=%d err=%v", nonceCount, err)
	}
}

func TestAcceptVFXFilesEventsConcurrentDuplicatesAreDurableOnce(t *testing.T) {
	db := vfxEventTestDB(t)
	if err := db.Create(&VFXFilesProductionConfig{ProductionID: vfxProductionID, ActivationEpoch: 1, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	input := vfxInput(vfxEventID1, "90000000-0000-4000-8000-000000000001", 1, 1, `{"concurrent":true}`)
	const callers = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	var accepted, duplicate, failures int
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results, err := AcceptVFXFilesEvents(db, "key-a", fmt.Sprintf("nonce-%016d", i), []VFXFilesEventInput{input}, time.Now())
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				return
			}
			if results[0].Outcome == VFXFilesEventAccepted {
				accepted++
			} else if results[0].Outcome == VFXFilesEventDuplicate {
				duplicate++
			} else {
				failures++
			}
		}(i)
	}
	wg.Wait()
	var count int64
	if err := db.Model(&VFXFilesEventInbox{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if failures != 0 || accepted != 1 || duplicate != callers-1 || count != 1 {
		t.Fatalf("concurrent acceptance accepted=%d duplicate=%d failures=%d persisted=%d", accepted, duplicate, failures, count)
	}
}

func TestStoreVFXFilesDesiredConfigMirrorIsMonotoneAndRejectsSecrets(t *testing.T) {
	db := vfxEventTestDB(t)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", "desired-config-adopt-existing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreVFXFilesDesiredConfigMirror(db, vfxProductionID, fixture); err != nil {
		t.Fatalf("store fixture: %v", err)
	}
	if err := StoreVFXFilesDesiredConfigMirror(db, vfxProductionID, fixture); err != nil {
		t.Fatalf("idempotent store: %v", err)
	}
	var stored VFXFilesProductionConfig
	if err := db.Where("production_id = ?", vfxProductionID).First(&stored).Error; err != nil || stored.ConfigRevision != 4 || stored.Activated || stored.DesiredConfigJSON == "" {
		t.Fatalf("stored mirror=%+v err=%v", stored, err)
	}
	var older map[string]any
	if err := json.Unmarshal(fixture, &older); err != nil {
		t.Fatal(err)
	}
	older["config_revision"] = float64(3)
	olderBody, _ := json.Marshal(older)
	if err := StoreVFXFilesDesiredConfigMirror(db, vfxProductionID, olderBody); !errors.Is(err, ErrVFXFilesConfigConflict) {
		t.Fatalf("older config error=%v", err)
	}
	var malicious map[string]any
	if err := json.Unmarshal(fixture, &malicious); err != nil {
		t.Fatal(err)
	}
	malicious["refresh_token"] = "must-not-persist"
	maliciousBody, _ := json.Marshal(malicious)
	if err := StoreVFXFilesDesiredConfigMirror(db, vfxProductionID, maliciousBody); !errors.Is(err, ErrVFXFilesInvalidBatch) {
		t.Fatalf("credential-bearing config error=%v", err)
	}
	var unchanged VFXFilesProductionConfig
	if err := db.Where("production_id = ?", vfxProductionID).First(&unchanged).Error; err != nil || unchanged.ConfigRevision != 4 || strings.Contains(unchanged.DesiredConfigJSON, "must-not-persist") {
		t.Fatalf("rejected config changed persisted mirror: %+v err=%v", unchanged, err)
	}
}

func TestVFXFilesSyncCommandIdempotencySurvivesSQLiteRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vfx-sync-operations.db")
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(&VFXFilesSyncOperation{}); err != nil {
			t.Fatal(err)
		}
		return db
	}
	preview := []byte(`{"contract_version":"1.0","preview_id":"70000000-0000-4000-8000-000000000001","production_id":"` + vfxProductionID + `","config_revision":4,"state":"preview_ready","expires_at":"` + time.Now().UTC().Add(time.Hour).Format(time.RFC3339) + `"}`)
	db := open()
	if err := StoreVFXFilesSyncPreview(db, vfxProductionID, "80000000-0000-4000-8000-000000000001", 4, preview); err != nil {
		t.Fatal(err)
	}
	first, err := PrepareVFXFilesSyncCommand(db, vfxProductionID, "apply", "first-apply-key-0001")
	if err != nil || first.ApplyIdempotency != "first-apply-key-0001" {
		t.Fatalf("first Apply key=%q err=%v", first.ApplyIdempotency, err)
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

	reopened := open()
	defer func() {
		if sqlDB, err := reopened.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	retried, err := PrepareVFXFilesSyncCommand(reopened, vfxProductionID, "apply", "different-retry-key-02")
	if err != nil || retried.ApplyIdempotency != first.ApplyIdempotency {
		t.Fatalf("restart changed uncertain Apply identity: got=%q want=%q err=%v", retried.ApplyIdempotency, first.ApplyIdempotency, err)
	}
	job := func(state, updatedAt string) []byte {
		return []byte(`{"contract_version":"1.0","id":"60000000-0000-4000-8000-000000000001","production_id":"` + vfxProductionID + `","config_revision":4,"state":"` + state + `","progress":{"completed":0,"total":1},"failure_code":null,"created_at":"2026-10-13T00:00:00Z","updated_at":"` + updatedAt + `"}`)
	}
	if err := StoreVFXFilesSyncJob(reopened, vfxProductionID, job("queued", "2026-10-13T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if err := StoreVFXFilesSyncJob(reopened, vfxProductionID, job("failed", "2026-10-13T00:01:00Z")); err != nil {
		t.Fatal(err)
	}
	retry, err := PrepareVFXFilesSyncCommand(reopened, vfxProductionID, "retry", "first-retry-key-0001")
	if err != nil || retry.RetryIdempotency != "first-retry-key-0001" {
		t.Fatalf("retry command key=%q err=%v", retry.RetryIdempotency, err)
	}
	repeated, err := PrepareVFXFilesSyncCommand(reopened, vfxProductionID, "retry", "different-retry-key-02")
	if err != nil || repeated.RetryIdempotency != retry.RetryIdempotency {
		t.Fatalf("retry command key changed: got=%q want=%q err=%v", repeated.RetryIdempotency, retry.RetryIdempotency, err)
	}
}

func TestDisableVFXFilesEventIntakePausesBeforeRemoteDeactivation(t *testing.T) {
	db := vfxEventTestDB(t)
	if err := db.Create(&VFXFilesProductionConfig{ProductionID: vfxProductionID, ConfigRevision: 3, ActivationEpoch: 7, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := DisableVFXFilesEventIntake(db, vfxProductionID); err != nil {
		t.Fatal(err)
	}
	var stored VFXFilesProductionConfig
	if err := db.Where("production_id = ?", vfxProductionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Activated || stored.ActivationEpoch != 7 || stored.ConfigRevision != 3 {
		t.Fatalf("local intake was not safely paused without rewriting remote version evidence: %#v", stored)
	}
}

func TestReadVFXFilesEventDiagnosticsIsProductionScopedAndCountsOnly(t *testing.T) {
	db := vfxEventTestDB(t)
	rows := []VFXFilesEventInbox{
		{ProducerID: "vfx-files-plugin", EventID: vfxEventID1, EventType: "file_delivery.ready", ProductionID: vfxProductionID, Outcome: VFXFilesEventAccepted},
		{ProducerID: "vfx-files-plugin", EventID: vfxEventID2, EventType: "file_delivery.ready", ProductionID: vfxProductionID, Outcome: VFXFilesEventQuarantined, PayloadJSON: `{"secret":"never expose"}`},
		{ProducerID: "vfx-files-plugin", EventID: "60000000-0000-4000-8000-000000000003", EventType: "file_delivery.ready", ProductionID: "10000000-0000-4000-8000-000000000002", Outcome: VFXFilesEventQuarantined},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&VFXFilesEventConflict{ProducerID: "vfx-files-plugin", EventID: vfxEventID2, Code: "event_identity_conflict"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&VFXFilesEventConflict{ProducerID: "vfx-files-plugin", EventID: "60000000-0000-4000-8000-000000000003", Code: "event_identity_conflict"}).Error; err != nil {
		t.Fatal(err)
	}
	got, err := ReadVFXFilesEventDiagnostics(db, vfxProductionID)
	if err != nil || got.PendingDelivery != 1 || got.Quarantined != 1 || got.IdentityConflict != 1 {
		t.Fatalf("diagnostics=%+v err=%v", got, err)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), vfxEventID1) {
		t.Fatalf("diagnostics include payload or identity: %s", encoded)
	}
}

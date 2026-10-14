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
	if err := db.AutoMigrate(&VFXFilesProductionConfig{}, &VFXFilesAuthorizationIntent{}, &VFXFilesEventInbox{}, &VFXFilesEventNonce{}, &VFXFilesEventConflict{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestVFXFilesAuthorizationIntentIsProductionActorBoundAndExpires(t *testing.T) {
	db := vfxEventTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	intent := VFXFilesAuthorizationIntent{
		AuthorizationID: "30000000-0000-4000-8000-000000000001",
		ProductionID:    vfxProductionID,
		ActorID:         "20000000-0000-4000-8000-000000000001",
		ExpiresAt:       now.Add(5 * time.Minute),
	}
	if err := StoreVFXFilesAuthorizationIntent(db, intent); err != nil {
		t.Fatal(err)
	}
	if err := StoreVFXFilesAuthorizationIntent(db, intent); err != nil {
		t.Fatalf("identical retry should remain idempotent: %v", err)
	}
	if _, err := FindVFXFilesAuthorizationIntent(db, intent.AuthorizationID, now); err != nil {
		t.Fatalf("live intent was not found: %v", err)
	}
	if err := ConsumeVFXFilesAuthorizationIntent(db, intent.AuthorizationID, intent.ProductionID, "20000000-0000-4000-8000-000000000002", now); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("intent was consumed by a different actor: %v", err)
	}
	if err := ConsumeVFXFilesAuthorizationIntent(db, intent.AuthorizationID, intent.ProductionID, intent.ActorID, now); err != nil {
		t.Fatalf("authorized intent was not consumed: %v", err)
	}
	if err := ConsumeVFXFilesAuthorizationIntent(db, intent.AuthorizationID, intent.ProductionID, intent.ActorID, now); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("consumed intent was reusable: %v", err)
	}
	if err := StoreVFXFilesAuthorizationIntent(db, VFXFilesAuthorizationIntent{
		AuthorizationID: intent.AuthorizationID,
		ProductionID:    "10000000-0000-4000-8000-000000000002",
		ActorID:         intent.ActorID,
		ExpiresAt:       intent.ExpiresAt,
	}); !errors.Is(err, ErrVFXFilesAuthorizationIntentConflict) {
		t.Fatalf("intent ID was rebound to another Production: %v", err)
	}
	if _, err := FindVFXFilesAuthorizationIntent(db, intent.AuthorizationID, intent.ExpiresAt); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expired intent remained usable: %v", err)
	}
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
	stale := vfxInput(vfxEventID2, input.DeliveryID, 1, 4, `{"body":"same revision and state"}`)
	staleResult, err := AcceptVFXFilesEvents(db, "key-a", "nonce-000000000000004", []VFXFilesEventInput{stale}, now.Add(3*time.Second))
	if err != nil || staleResult[0].Code != "invalid_delivery_transition" {
		t.Fatalf("same-revision repeated state=%#v err=%v", staleResult, err)
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

func TestAcceptVFXFilesDeliveryStateTransitionsWithinPackageRevision(t *testing.T) {
	db := vfxEventTestDB(t)
	if err := db.Create(&VFXFilesProductionConfig{ProductionID: vfxProductionID, ActivationEpoch: 4, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	deliveryID := "90000000-0000-4000-8000-000000000021"
	events := []VFXFilesEventInput{
		vfxInput("60000000-0000-4000-8000-000000000021", deliveryID, 1, 4, `{"status":"created"}`),
		vfxInput("60000000-0000-4000-8000-000000000022", deliveryID, 1, 4, `{"status":"needs_fix"}`),
		vfxInput("60000000-0000-4000-8000-000000000023", deliveryID, 2, 4, `{"status":"revised"}`),
		vfxInput("60000000-0000-4000-8000-000000000024", deliveryID, 2, 4, `{"status":"accepted"}`),
	}
	events[0].EventType = "file_delivery.created"
	events[1].EventType = "file_delivery.needs_fix"
	events[2].EventType = "file_delivery.revised"
	events[3].EventType = "file_delivery.accepted"
	for i, event := range events {
		results, err := AcceptVFXFilesEvents(db, "key-a", fmt.Sprintf("state-transition-nonce-%04d", i), []VFXFilesEventInput{event}, now.Add(time.Duration(i)*time.Second))
		if err != nil || len(results) != 1 || results[0].Outcome != VFXFilesEventAccepted {
			t.Fatalf("transition %s revision=%d result=%#v err=%v", event.EventType, event.DeliveryRevision, results, err)
		}
		if i == 1 {
			retry, retryErr := AcceptVFXFilesEvents(db, "key-a", "state-transition-retry-nonce", []VFXFilesEventInput{event}, now.Add(1500*time.Millisecond))
			if retryErr != nil || len(retry) != 1 || retry[0].Outcome != VFXFilesEventDuplicate {
				t.Fatalf("fresh-nonce retry of the same state event=%#v err=%v", retry, retryErr)
			}
		}
	}
	var accepted int64
	if err := db.Model(&VFXFilesEventInbox{}).Where("delivery_id = ? AND outcome = ?", deliveryID, VFXFilesEventAccepted).Count(&accepted).Error; err != nil || accepted != int64(len(events)) {
		t.Fatalf("accepted transition count=%d err=%v", accepted, err)
	}
}

func TestAcceptVFXFilesDeliveryRejectsRevisionRegressionAndOutOfOrderTransition(t *testing.T) {
	db := vfxEventTestDB(t)
	if err := db.Create(&VFXFilesProductionConfig{ProductionID: vfxProductionID, ActivationEpoch: 4, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 2, 0, 0, 0, time.UTC)
	deliveryID := "90000000-0000-4000-8000-000000000022"
	created := vfxInput("60000000-0000-4000-8000-000000000031", deliveryID, 1, 4, `{"status":"created"}`)
	created.EventType = "file_delivery.created"
	needsFix := vfxInput("60000000-0000-4000-8000-000000000032", deliveryID, 1, 4, `{"status":"needs_fix"}`)
	needsFix.EventType = "file_delivery.needs_fix"
	for i, event := range []VFXFilesEventInput{created, needsFix} {
		if result, err := AcceptVFXFilesEvents(db, "key-a", fmt.Sprintf("revision-seed-nonce-%04d", i), []VFXFilesEventInput{event}, now.Add(time.Duration(i)*time.Second)); err != nil || result[0].Outcome != VFXFilesEventAccepted {
			t.Fatalf("seed %s result=%#v err=%v", event.EventType, result, err)
		}
	}
	// An accepted response at the old package revision cannot skip the
	// required revised event that advances the revision.
	outOfOrder := vfxInput("60000000-0000-4000-8000-000000000033", deliveryID, 2, 4, `{"status":"accepted"}`)
	outOfOrder.EventType = "file_delivery.accepted"
	accepted, err := AcceptVFXFilesEvents(db, "key-a", "revision-order-nonce-001", []VFXFilesEventInput{outOfOrder}, now.Add(3*time.Second))
	if err != nil || accepted[0].Outcome != VFXFilesEventQuarantined || accepted[0].Code != "invalid_delivery_transition" {
		t.Fatalf("out-of-order state=%#v err=%v", accepted, err)
	}
	revised := vfxInput("60000000-0000-4000-8000-000000000034", deliveryID, 2, 4, `{"status":"revised"}`)
	revised.EventType = "file_delivery.revised"
	if result, err := AcceptVFXFilesEvents(db, "key-a", "revision-revised-nonce-01", []VFXFilesEventInput{revised}, now.Add(4*time.Second)); err != nil || result[0].Outcome != VFXFilesEventAccepted {
		t.Fatalf("revised transition=%#v err=%v", result, err)
	}
	oldRevision := vfxInput("60000000-0000-4000-8000-000000000034", deliveryID, 1, 4, `{"status":"accepted"}`)
	oldRevision.EventID = "60000000-0000-4000-8000-000000000036"
	oldRevision.EventType = "file_delivery.accepted"
	stale, err := AcceptVFXFilesEvents(db, "key-a", "revision-regression-nonce-001", []VFXFilesEventInput{oldRevision}, now.Add(5*time.Second))
	if err != nil || stale[0].Outcome != VFXFilesEventQuarantined || stale[0].Code != "stale_delivery_revision" {
		t.Fatalf("revision regression=%#v err=%v", stale, err)
	}
}

func TestAcceptVFXFilesDeliveryBatchPreservesSameRevisionTransitionOrder(t *testing.T) {
	db := vfxEventTestDB(t)
	if err := db.Create(&VFXFilesProductionConfig{ProductionID: vfxProductionID, ActivationEpoch: 4, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	deliveryID := "90000000-0000-4000-8000-000000000023"
	created := vfxInput("60000000-0000-4000-8000-000000000099", deliveryID, 1, 4, `{"status":"created"}`)
	created.EventType = "file_delivery.created"
	needsFix := vfxInput("60000000-0000-4000-8000-000000000001", deliveryID, 1, 4, `{"status":"needs_fix"}`)
	needsFix.EventType = "file_delivery.needs_fix"
	results, err := AcceptVFXFilesEvents(db, "key-a", "same-revision-batch-nonce-01", []VFXFilesEventInput{created, needsFix}, time.Now().UTC())
	if err != nil || len(results) != 2 || results[0].EventID != created.EventID || results[0].Outcome != VFXFilesEventAccepted || results[1].EventID != needsFix.EventID || results[1].Outcome != VFXFilesEventAccepted {
		t.Fatalf("ordered same-revision batch=%#v err=%v", results, err)
	}
}

func TestAcceptVFXFilesDeliveryBatchQuarantinesRevisionOrderInversion(t *testing.T) {
	db := vfxEventTestDB(t)
	if err := db.Create(&VFXFilesProductionConfig{ProductionID: vfxProductionID, ActivationEpoch: 4, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	deliveryID := "90000000-0000-4000-8000-000000000024"
	revised := vfxInput("60000000-0000-4000-8000-000000000041", deliveryID, 2, 4, `{"status":"revised"}`)
	revised.EventType = "file_delivery.revised"
	created := vfxInput("60000000-0000-4000-8000-000000000042", deliveryID, 1, 4, `{"status":"created"}`)
	created.EventType = "file_delivery.created"
	results, err := AcceptVFXFilesEvents(db, "key-a", "revision-inversion-batch-nonce", []VFXFilesEventInput{revised, created}, time.Now().UTC())
	if err != nil || len(results) != 2 || results[0].Outcome != VFXFilesEventQuarantined || results[0].Code != "delivery_revision_gap" || results[1].Outcome != VFXFilesEventQuarantined || results[1].Code != "delivery_order_conflict" {
		t.Fatalf("inverted revision batch=%#v err=%v", results, err)
	}
	var accepted int64
	if err := db.Model(&VFXFilesEventInbox{}).Where("delivery_id = ? AND outcome = ?", deliveryID, VFXFilesEventAccepted).Count(&accepted).Error; err != nil || accepted != 0 {
		t.Fatalf("inverted revision batch advanced accepted history: count=%d err=%v", accepted, err)
	}
}

func TestAcceptVFXFilesDeliveryBatchQuarantinesSameRevisionOrderInversion(t *testing.T) {
	db := vfxEventTestDB(t)
	if err := db.Create(&VFXFilesProductionConfig{ProductionID: vfxProductionID, ActivationEpoch: 4, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	deliveryID := "90000000-0000-4000-8000-000000000025"
	needsFix := vfxInput("60000000-0000-4000-8000-000000000051", deliveryID, 1, 4, `{"status":"needs_fix"}`)
	needsFix.EventType = "file_delivery.needs_fix"
	created := vfxInput("60000000-0000-4000-8000-000000000052", deliveryID, 1, 4, `{"status":"created"}`)
	created.EventType = "file_delivery.created"
	results, err := AcceptVFXFilesEvents(db, "key-a", "state-inversion-batch-nonce", []VFXFilesEventInput{needsFix, created}, time.Now().UTC())
	if err != nil || len(results) != 2 || results[0].Outcome != VFXFilesEventQuarantined || results[0].Code != "delivery_revision_gap" || results[1].Outcome != VFXFilesEventQuarantined || results[1].Code != "delivery_order_conflict" {
		t.Fatalf("inverted state batch=%#v err=%v", results, err)
	}
}

func TestValidateVFXFilesDeliveryTransitionSeparatesRevisionAndStatus(t *testing.T) {
	previous := func(status string, revision uint64) *VFXFilesEventInbox {
		return &VFXFilesEventInbox{EventType: "file_delivery." + status, DeliveryRevision: revision}
	}
	cases := []struct {
		name     string
		previous *VFXFilesEventInbox
		status   string
		revision uint64
		wantCode string
	}{
		{name: "initial created", status: "created", revision: 1},
		{name: "initial ready", status: "ready", revision: 1},
		{name: "created to needs fix without revision bump", previous: previous("created", 1), status: "needs_fix", revision: 1},
		{name: "needs fix to revised advances package revision", previous: previous("needs_fix", 1), status: "revised", revision: 2},
		{name: "revised to accepted keeps package revision", previous: previous("revised", 2), status: "accepted", revision: 2},
		{name: "lower revision is stale", previous: previous("revised", 2), status: "accepted", revision: 1, wantCode: "stale_delivery_revision"},
		{name: "revision gap is rejected", previous: previous("needs_fix", 1), status: "revised", revision: 3, wantCode: "delivery_revision_gap"},
		{name: "response cannot skip revised package", previous: previous("needs_fix", 1), status: "accepted", revision: 2, wantCode: "invalid_delivery_transition"},
		{name: "duplicate state with a new event is rejected", previous: previous("created", 1), status: "created", revision: 1, wantCode: "invalid_delivery_transition"},
		{name: "unknown event state is rejected", previous: previous("created", 1), status: "deleted", revision: 1, wantCode: "invalid_delivery_transition"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := VFXFilesEventInput{EventType: "file_delivery." + test.status, DeliveryRevision: test.revision}
			if got := validateVFXFilesDeliveryTransition(test.previous, input); got != test.wantCode {
				t.Fatalf("transition diagnostic=%q want %q", got, test.wantCode)
			}
		})
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

func TestActivationMirrorHoldsOnlyTheExplicitNextEpochAndReleasesAfterReadback(t *testing.T) {
	db := vfxEventTestDB(t)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", "desired-config-adopt-existing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreVFXFilesDesiredConfigMirror(db, vfxProductionID, fixture); err != nil {
		t.Fatal(err)
	}
	if err := BeginVFXFilesActivationMirror(db, vfxProductionID); err != nil {
		t.Fatalf("begin authorized activation mirror transition: %v", err)
	}
	input := vfxInput(vfxEventID1, "90000000-0000-4000-8000-000000000001", 1, 1, `{"body":"activation race"}`)
	if _, err := AcceptVFXFilesEvents(db, "key-a", "activation-pending-nonce-0001", []VFXFilesEventInput{input}, time.Now().UTC()); !errors.Is(err, ErrVFXFilesActivationPending) {
		t.Fatalf("only the exact pending next epoch should be transiently held, got %v", err)
	}
	var nonceCount, inboxCount int64
	if err := db.Model(&VFXFilesEventNonce{}).Count(&nonceCount).Error; err != nil || nonceCount != 1 {
		t.Fatalf("activation retry did not consume its signed-request nonce: count=%d err=%v", nonceCount, err)
	}
	if err := db.Model(&VFXFilesEventInbox{}).Count(&inboxCount).Error; err != nil || inboxCount != 0 {
		t.Fatalf("held event was stored/quarantined: count=%d err=%v", inboxCount, err)
	}

	var active map[string]any
	if err := json.Unmarshal(fixture, &active); err != nil {
		t.Fatal(err)
	}
	active["config_revision"] = float64(5)
	activation := active["activation"].(map[string]any)
	activation["enabled"] = true
	activation["notification_epoch"] = float64(1)
	activation["activated_at"] = "2026-10-14T08:00:00Z"
	activeBody, err := json.Marshal(active)
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreVFXFilesDesiredConfigMirror(db, vfxProductionID, activeBody); err != nil {
		t.Fatalf("persist Plugin activation readback: %v", err)
	}
	var stored VFXFilesProductionConfig
	if err := db.Where("production_id = ?", vfxProductionID).First(&stored).Error; err != nil || stored.PendingActivationEpoch != 0 || !stored.Activated || stored.ActivationEpoch != 1 {
		t.Fatalf("activation readback did not clear the transition marker: %+v err=%v", stored, err)
	}
	accepted, err := AcceptVFXFilesEvents(db, "key-a", "activation-retry-nonce-0001", []VFXFilesEventInput{input}, time.Now().UTC())
	if err != nil || len(accepted) != 1 || accepted[0].Outcome != VFXFilesEventAccepted {
		t.Fatalf("fresh-nonce retry after mirror convergence was not accepted: results=%+v err=%v", accepted, err)
	}

	// A disabled configuration without an in-flight marker still quarantines
	// even an otherwise next epoch; old or arbitrary future epochs do likewise.
	if err := db.Model(&VFXFilesProductionConfig{}).Where("production_id = ?", vfxProductionID).Update("activated", false).Error; err != nil {
		t.Fatal(err)
	}
	for index, epoch := range []uint64{1, 2} {
		candidate := vfxInput(fmt.Sprintf("60000000-0000-4000-8000-%012d", index+10), fmt.Sprintf("90000000-0000-4000-8000-%012d", index+10), 1, epoch, `{"body":"must quarantine"}`)
		results, err := AcceptVFXFilesEvents(db, "key-a", fmt.Sprintf("activation-closed-nonce-%04d", index), []VFXFilesEventInput{candidate}, time.Now().UTC())
		if err != nil || len(results) != 1 || results[0].Outcome != VFXFilesEventQuarantined {
			t.Fatalf("disabled or unmarked epoch %d was not fail-closed: results=%+v err=%v", epoch, results, err)
		}
	}
}

func TestStoreVFXFilesDesiredConfigMirrorAcceptsEquivalentJSONKeyOrder(t *testing.T) {
	db := vfxEventTestDB(t)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", "desired-config-adopt-existing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreVFXFilesDesiredConfigMirror(db, vfxProductionID, fixture); err != nil {
		t.Fatalf("store initial response: %v", err)
	}
	var reordered map[string]any
	if err := json.Unmarshal(fixture, &reordered); err != nil {
		t.Fatal(err)
	}
	reorderedBody, err := json.Marshal(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if string(fixture) == string(reorderedBody) {
		t.Fatal("test did not produce a different JSON key order")
	}
	if err := StoreVFXFilesDesiredConfigMirror(db, vfxProductionID, reorderedBody); err != nil {
		t.Fatalf("equivalent response with reordered object keys must be idempotent: %v", err)
	}
	var changed map[string]any
	if err := json.Unmarshal(reorderedBody, &changed); err != nil {
		t.Fatal(err)
	}
	policy := changed["sync_policy"].(map[string]any)
	policy["notify_new_deliveries"] = false
	changedBody, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreVFXFilesDesiredConfigMirror(db, vfxProductionID, changedBody); !errors.Is(err, ErrVFXFilesConfigConflict) {
		t.Fatalf("same revision with changed desired behavior must conflict, got %v", err)
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

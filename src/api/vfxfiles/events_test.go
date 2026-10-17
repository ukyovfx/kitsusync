package vfxfiles

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func eventHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "events.db")+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectReviewerTarget{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.VFXFilesDriveLinkDisclosureConsent{}, &model.VFXFilesDriveLinkDisclosureAudit{}, &model.VFXFilesProductionConfig{}, &model.VFXFilesEventInbox{}, &model.VFXFilesEventNonce{}, &model.VFXFilesEventConflict{}, &model.NotificationDeliveryEvent{}, &model.ProductionNotificationWriteLock{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VFXFilesProductionConfig{ProductionID: "10000000-0000-4000-8000-000000000001", ActivationEpoch: 1, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Project{KitsuProjectID: "10000000-0000-4000-8000-000000000001", Name: "Synthetic"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationConfig{ProductionID: "10000000-0000-4000-8000-000000000001", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func signedEventRequest(t *testing.T, body []byte, key, nonce, idempotency string, now time.Time) *http.Request {
	t.Helper()
	timestamp := now.UTC().Format(time.RFC3339)
	sum := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(sum[:])
	canonical := timestamp + "\n" + nonce + "\n" + bodyHash + "\n" + idempotency
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(canonical))
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/vfx-files/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-VFX-Key-Id", "fake-key-1")
	req.Header.Set("X-VFX-Timestamp", timestamp)
	req.Header.Set("X-VFX-Nonce", nonce)
	req.Header.Set("X-VFX-Content-SHA256", bodyHash)
	req.Header.Set("X-VFX-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Idempotency-Key", idempotency)
	return req
}

func TestPluginEventHandlerVerifiesAndDurablyAcceptsContractFixture(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", "delivery-created-multi-target.json"))
	if err != nil {
		t.Fatal(err)
	}
	db := eventHandlerTestDB(t)
	key := "synthetic-event-signing-key-material-0001"
	keys := map[string][]byte{"fake-key-1": []byte(key)}
	clock := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	handler := EventHandler(db, keys, func() time.Time { return clock })
	request := signedEventRequest(t, body, key, "synthetic-nonce-0000001", "synthetic-idempotency-0001", clock)
	response := httptest.NewRecorder()
	handler(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("valid batch status=%d body=%s", response.Code, response.Body.String())
	}
	var accepted struct {
		ContractVersion string                      `json:"contract_version"`
		Results         []model.VFXFilesEventResult `json:"results"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil || accepted.ContractVersion != "1.0" || len(accepted.Results) != 2 || accepted.Results[0].Outcome != model.VFXFilesEventAccepted || accepted.Results[1].Outcome != model.VFXFilesEventAccepted {
		t.Fatalf("accepted response=%+v err=%v", accepted, err)
	}
	var events int64
	if err := db.Model(&model.VFXFilesEventInbox{}).Count(&events).Error; err != nil || events != 2 {
		t.Fatalf("durable event rows=%d err=%v", events, err)
	}
	response = httptest.NewRecorder()
	handler(response, signedEventRequest(t, body, key, "synthetic-nonce-0000001", "synthetic-idempotency-0001", clock))
	if response.Code != http.StatusConflict {
		t.Fatalf("replayed nonce status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestParseFileShareV11FixtureAndRejectsUnsafeDriveURLVariants(t *testing.T) {
	path := filepath.Join("..", "..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", "file-share-created-v1.1-two-recipient.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var batch eventBatch
	if err := json.Unmarshal(body, &batch); err != nil || batch.ContractVersion != "1.1" || len(batch.Events) != 2 {
		t.Fatalf("fixture batch invalid: %v", err)
	}
	var inputs []model.VFXFilesEventInput
	for _, raw := range batch.Events {
		input, err := parsePluginEvent(raw)
		if err != nil || input.EventType != "file_share.created" || input.RecipientTaskID == "" || input.ShareTargetID == "" {
			t.Fatalf("share event parse=%+v err=%v", input, err)
		}
		inputs = append(inputs, input)
	}
	if !validShareBatchSemantics(batch.Events, inputs) {
		t.Fatal("positive two-recipient batch semantics failed")
	}
	duplicateEvents := append([]json.RawMessage(nil), batch.Events...)
	var first, second map[string]json.RawMessage
	_ = json.Unmarshal(duplicateEvents[0], &first)
	_ = json.Unmarshal(duplicateEvents[1], &second)
	var firstShare, secondShare map[string]json.RawMessage
	_ = json.Unmarshal(first["share"], &firstShare)
	_ = json.Unmarshal(second["share"], &secondShare)
	firstShare["share_target_id"] = secondShare["share_target_id"]
	first["share"], _ = json.Marshal(firstShare)
	duplicateEvents[0], _ = json.Marshal(first)
	if validShareBatchSemantics(duplicateEvents, inputs) {
		t.Fatal("duplicate share target was accepted")
	}
	var versionMismatch map[string]any
	if err := json.Unmarshal(batch.Events[0], &versionMismatch); err != nil {
		t.Fatal(err)
	}
	versionMismatch["event_type"] = "file_delivery.ready"
	encodedMismatch, _ := json.Marshal(versionMismatch)
	if _, err := parsePluginEvent(encodedMismatch); err == nil {
		t.Fatal("file delivery was accepted under contract 1.1")
	}
	var event map[string]any
	if err := json.Unmarshal(batch.Events[0], &event); err != nil {
		t.Fatal(err)
	}
	share := event["share"].(map[string]any)
	obj := share["object"].(map[string]any)
	for _, unsafe := range []string{
		"https://drive.google.com/drive/folders/1FzExampleDriveFolder_opaqueID?resourcekey=secret",
		"https://attacker.example/drive/folders/1FzExampleDriveFolder_opaqueID",
		"https://user@drive.google.com/drive/folders/1FzExampleDriveFolder_opaqueID",
		"https://drive.google.com/drive/folders/another-id",
	} {
		obj["url"] = unsafe
		encoded, _ := json.Marshal(event)
		if _, err := parsePluginEvent(encoded); err == nil {
			t.Fatalf("unsafe Drive URL accepted: %s", unsafe)
		}
	}
}

func TestPluginEventHandlerDurablyReportsMissingShareDisclosureConsent(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", "file-share-created-v1.1-two-recipient.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Bind the frozen synthetic fixture to this test Production and its active
	// epoch while retaining the contract's two-recipient homogeneous batch.
	body = []byte(strings.ReplaceAll(string(body), "9cb8f70d-0b63-40b7-a73d-3237e6f50d3d", "10000000-0000-4000-8000-000000000001"))
	body = []byte(strings.ReplaceAll(string(body), `"activation_epoch": 7`, `"activation_epoch": 1`))
	db := eventHandlerTestDB(t)
	key := "synthetic-event-signing-key-material-0001"
	clock := time.Date(2026, 10, 17, 5, 0, 0, 0, time.UTC)
	handler := EventHandler(db, map[string][]byte{"fake-key-1": []byte(key)}, func() time.Time { return clock })
	response := httptest.NewRecorder()
	handler(response, signedEventRequest(t, body, key, "share-consent-nonce-000001", "share-consent-idempotency-000001", clock))
	if response.Code != http.StatusAccepted {
		t.Fatalf("no-consent share batch status=%d body=%s", response.Code, response.Body.String())
	}
	var accepted struct {
		ContractVersion string                      `json:"contract_version"`
		Results         []model.VFXFilesEventResult `json:"results"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.ContractVersion != "1.0" || len(accepted.Results) != 2 {
		t.Fatalf("response contract/results=%+v", accepted)
	}
	for _, result := range accepted.Results {
		if result.Outcome != model.VFXFilesEventQuarantined || result.Code != "drive_link_disclosure_consent_missing" {
			t.Fatalf("no-consent event result=%+v", result)
		}
	}
	if strings.Contains(response.Body.String(), "drive.google.com") || strings.Contains(response.Body.String(), "opaqueID") {
		t.Fatalf("response leaked Drive reference: %s", response.Body.String())
	}
	var quarantined int64
	if err := db.Model(&model.VFXFilesEventInbox{}).Where("event_type = ? AND outcome = ? AND diagnostic_code = ?", "file_share.created", model.VFXFilesEventQuarantined, "drive_link_disclosure_consent_missing").Count(&quarantined).Error; err != nil || quarantined != 2 {
		t.Fatalf("durable no-consent quarantine count=%d err=%v", quarantined, err)
	}
}

func TestPluginEventHandlerAcceptsAndDeduplicatesConsentedMultiRecipientShareBatch(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", "file-share-created-v1.1-two-recipient.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Bind the frozen synthetic fixture to the test Production and its active
	// epoch while retaining the contract's two-recipient homogeneous batch.
	productionID := "10000000-0000-4000-8000-000000000001"
	body = []byte(strings.ReplaceAll(string(body), "9cb8f70d-0b63-40b7-a73d-3237e6f50d3d", productionID))
	body = []byte(strings.ReplaceAll(string(body), `"activation_epoch": 7`, `"activation_epoch": 1`))
	var batch eventBatch
	if err := json.Unmarshal(body, &batch); err != nil || batch.ContractVersion != "1.1" || len(batch.Events) != 2 {
		t.Fatalf("two-recipient share fixture is invalid: batch=%+v err=%v", batch, err)
	}
	inputs := make([]model.VFXFilesEventInput, 0, len(batch.Events))
	for _, raw := range batch.Events {
		input, err := parsePluginEvent(raw)
		if err != nil {
			t.Fatalf("parse share fixture event: %v", err)
		}
		inputs = append(inputs, input)
	}

	db := eventHandlerTestDB(t)
	clock := time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC)
	taskTypes := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		taskTypes[input.RecipientTaskTypeID] = struct{}{}
	}
	for taskTypeID := range taskTypes {
		route := model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: taskTypeID, DestinationWebhookID: 1, CreatedAt: clock}
		if err := db.Create(&route).Error; err != nil {
			t.Fatalf("create consent-bound route for Task Type %s: %v", taskTypeID, err)
		}
	}
	project := model.FindProjectByKitsuID(db, productionID)
	if project == nil {
		t.Fatal("test Production was not created")
	}
	revision, err := model.ProductionNotificationRevisionWithError(db, project.ID, productionID)
	if err != nil {
		t.Fatalf("read notification route revision: %v", err)
	}
	for taskTypeID := range taskTypes {
		if err := model.SetVFXFilesDriveLinkDisclosureConsent(db, productionID, taskTypeID, revision, "admin@example.test", true, clock); err != nil {
			t.Fatalf("enable exact-revision Drive link disclosure consent for Task Type %s: %v", taskTypeID, err)
		}
	}

	key := "synthetic-event-signing-key-material-0001"
	handler := EventHandler(db, map[string][]byte{"fake-key-1": []byte(key)}, func() time.Time { return clock })
	firstResponse := httptest.NewRecorder()
	handler(firstResponse, signedEventRequest(t, body, key, "share-accept-nonce-000001", "share-accept-idempotency-0001", clock))
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("consented share batch status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	var firstAccepted struct {
		ContractVersion string                      `json:"contract_version"`
		Results         []model.VFXFilesEventResult `json:"results"`
	}
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &firstAccepted); err != nil {
		t.Fatalf("decode first share response: %v", err)
	}
	if firstAccepted.ContractVersion != contractVersion || len(firstAccepted.Results) != len(inputs) {
		t.Fatalf("first response contract/results=%+v", firstAccepted)
	}
	for i, input := range inputs {
		result := firstAccepted.Results[i]
		if result.EventID != input.EventID || result.Outcome != model.VFXFilesEventAccepted || result.Code != "accepted" {
			t.Fatalf("first response result[%d]=%+v, expected ordered accepted event %s", i, result, input.EventID)
		}
	}
	var stored int64
	if err := db.Model(&model.VFXFilesEventInbox{}).Where("event_type = ? AND production_id = ?", "file_share.created", productionID).Count(&stored).Error; err != nil || stored != int64(len(inputs)) {
		t.Fatalf("durable share inbox rows=%d err=%v", stored, err)
	}

	retryResponse := httptest.NewRecorder()
	handler(retryResponse, signedEventRequest(t, body, key, "share-accept-nonce-000002", "share-accept-idempotency-0001", clock))
	if retryResponse.Code != http.StatusAccepted {
		t.Fatalf("fresh-nonce share retry status=%d body=%s", retryResponse.Code, retryResponse.Body.String())
	}
	var retried struct {
		ContractVersion string                      `json:"contract_version"`
		Results         []model.VFXFilesEventResult `json:"results"`
	}
	if err := json.Unmarshal(retryResponse.Body.Bytes(), &retried); err != nil {
		t.Fatalf("decode retried share response: %v", err)
	}
	if retried.ContractVersion != contractVersion || len(retried.Results) != len(inputs) {
		t.Fatalf("retry response contract/results=%+v", retried)
	}
	for i, input := range inputs {
		result := retried.Results[i]
		if result.EventID != input.EventID || result.Outcome != model.VFXFilesEventDuplicate || result.Code != "duplicate_event" {
			t.Fatalf("retry response result[%d]=%+v, expected ordered duplicate event %s", i, result, input.EventID)
		}
	}
	stored = 0
	if err := db.Model(&model.VFXFilesEventInbox{}).Where("event_type = ? AND production_id = ?", "file_share.created", productionID).Count(&stored).Error; err != nil || stored != int64(len(inputs)) {
		t.Fatalf("retry changed durable share inbox row count to %d err=%v", stored, err)
	}
}

func TestSignedNonDeliveryLifecycleEventsAreDurableButNeverPendingDiscord(t *testing.T) {
	db := eventHandlerTestDB(t)
	key := "synthetic-event-signing-key-material-0001"
	clock := time.Date(2026, 10, 12, 16, 0, 0, 0, time.UTC)
	handler := EventHandler(db, map[string][]byte{"fake-key-1": []byte(key)}, func() time.Time { return clock })
	fixtures := []struct {
		name      string
		eventType string
	}{
		{name: "folder-provisioning-completed.json", eventType: "folder.provisioning.completed"},
		{name: "output-package-created.json", eventType: "output_package.created"},
		{name: "output-package-ready.json", eventType: "output_package.ready"},
	}
	for i, fixture := range fixtures {
		t.Run(fixture.eventType, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", fixture.name))
			if err != nil {
				t.Fatal(err)
			}
			request := signedEventRequest(t, body, key, fmt.Sprintf("non-delivery-nonce-%04d", i), fmt.Sprintf("non-delivery-idemp-%04d", i), clock)
			response := httptest.NewRecorder()
			handler(response, request)
			if response.Code != http.StatusAccepted {
				t.Fatalf("signed %s event status=%d body=%s", fixture.eventType, response.Code, response.Body.String())
			}
			var accepted struct {
				Results []model.VFXFilesEventResult `json:"results"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil || len(accepted.Results) != 1 || accepted.Results[0].Outcome != model.VFXFilesEventAccepted {
				t.Fatalf("signed %s event result=%+v err=%v", fixture.eventType, accepted.Results, err)
			}
			var stored model.VFXFilesEventInbox
			if err := db.Where("event_id = ?", accepted.Results[0].EventID).First(&stored).Error; err != nil || stored.EventType != fixture.eventType || stored.Outcome != model.VFXFilesEventAccepted || stored.PayloadJSON == "" {
				t.Fatalf("event was not durably journaled: row=%+v err=%v", stored, err)
			}
			// Simulate a lost first 202: the Provider retries the exact same
			// Event ID and body with a fresh request nonce.
			replay := httptest.NewRecorder()
			replayBody := signedEventRequest(t, body, key, fmt.Sprintf("non-delivery-replay-%04d", i), fmt.Sprintf("non-delivery-replay-key-%04d", i), clock)
			handler(replay, replayBody)
			var replayed struct {
				Results []model.VFXFilesEventResult `json:"results"`
			}
			if replay.Code != http.StatusAccepted || json.Unmarshal(replay.Body.Bytes(), &replayed) != nil || len(replayed.Results) != 1 || replayed.Results[0].Outcome != model.VFXFilesEventDuplicate {
				t.Fatalf("identical event retry was not acknowledged idempotently: status=%d body=%s", replay.Code, replay.Body.String())
			}
			var eventCount int64
			if err := db.Model(&model.VFXFilesEventInbox{}).Where("event_id = ?", stored.EventID).Count(&eventCount).Error; err != nil || eventCount != 1 {
				t.Fatalf("event retry created duplicate durable rows: count=%d err=%v", eventCount, err)
			}
		})
	}
	rows, err := model.PendingVFXFilesDeliveryEvents(db, 100)
	if err != nil || len(rows) != 0 {
		t.Fatalf("non-delivery lifecycle events entered Discord delivery queue: rows=%+v err=%v", rows, err)
	}
	diagnostics, err := model.ReadVFXFilesEventDiagnostics(db, "10000000-0000-4000-8000-000000000001")
	if err != nil || diagnostics.PendingDelivery != 0 {
		t.Fatalf("non-delivery lifecycle events counted as pending Discord deliveries: diagnostics=%+v err=%v", diagnostics, err)
	}
}

func TestSignedAuthorityBarrierDrainsOldEpochAndAllowsActivationAfterAck(t *testing.T) {
	db := eventHandlerTestDB(t)
	key := "synthetic-event-signing-key-material-0001"
	clock := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	handler := EventHandler(db, map[string][]byte{"fake-key-1": []byte(key)}, func() time.Time { return clock })
	if err := db.Create(&model.VFXFilesEventInbox{
		ProducerID: "vfx-files-plugin", EventID: "60000000-0000-4000-8000-000000000090",
		PayloadSHA256: strings.Repeat("a", 64), PayloadJSON: `{"stale":"pending"}`,
		EventType: "file_delivery.ready", ProductionID: "10000000-0000-4000-8000-000000000001",
		ActivationEpoch: 1, Outcome: model.VFXFilesEventAccepted, CreatedAt: clock,
	}).Error; err != nil {
		t.Fatal(err)
	}
	requestFor := func(eventID, eventType string, revision, previous, next, eventEpoch uint64, nonce string) *http.Request {
		body, err := json.Marshal(map[string]any{"contract_version": "1.0", "events": []any{map[string]any{
			"contract_version": "1.0", "producer_id": "vfx-files-plugin", "event_id": eventID,
			"event_type": eventType, "occurred_at": clock.Format(time.RFC3339),
			"production_id": "10000000-0000-4000-8000-000000000001", "activation_epoch": eventEpoch,
			"config_revision": revision, "previous_activation_epoch": previous, "new_activation_epoch": next,
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return signedEventRequest(t, body, key, nonce, eventID, clock)
	}
	deactivatedID := "60000000-0000-4000-8000-000000000091"
	response := httptest.NewRecorder()
	handler(response, requestFor(deactivatedID, "storage_authority.deactivation_requested", 4, 1, 2, 1, "authority-nonce-deactivate-01"))
	if response.Code != http.StatusAccepted {
		t.Fatalf("deactivation barrier=%d %s", response.Code, response.Body.String())
	}
	var config model.VFXFilesProductionConfig
	if err := db.Where("production_id = ?", "10000000-0000-4000-8000-000000000001").Take(&config).Error; err != nil || config.Activated || config.ActivationEpoch != 2 || config.ConfigRevision != 4 {
		t.Fatalf("deactivated mirror=%+v err=%v", config, err)
	}
	var stored model.VFXFilesEventInbox
	if err := db.Where("event_id = ?", deactivatedID).Take(&stored).Error; err != nil || stored.ProcessedAt == nil || stored.DiagnosticCode != "activation_barrier_complete" {
		t.Fatalf("barrier receipt=%+v err=%v", stored, err)
	}
	var fenced model.VFXFilesEventInbox
	if err := db.Where("event_id = ?", "60000000-0000-4000-8000-000000000090").Take(&fenced).Error; err != nil || fenced.Outcome != model.VFXFilesEventQuarantined || fenced.ProcessedAt == nil || fenced.DiagnosticCode != "activation_epoch_fenced" {
		t.Fatalf("queued old-epoch delivery was not durably fenced: %+v err=%v", fenced, err)
	}
	response = httptest.NewRecorder()
	handler(response, requestFor("60000000-0000-4000-8000-000000000092", "storage_authority.activation_requested", 5, 2, 3, 3, "authority-nonce-activate-0001"))
	if response.Code != http.StatusAccepted {
		t.Fatalf("activation barrier=%d %s", response.Code, response.Body.String())
	}
	if err := db.Where("production_id = ?", "10000000-0000-4000-8000-000000000001").Take(&config).Error; err != nil || !config.Activated || config.ActivationEpoch != 3 || config.ConfigRevision != 5 {
		t.Fatalf("activated mirror=%+v err=%v", config, err)
	}
	response = httptest.NewRecorder()
	handler(response, requestFor(deactivatedID, "storage_authority.deactivation_requested", 4, 1, 2, 1, "authority-nonce-retry-0001"))
	if response.Code != http.StatusConflict {
		t.Fatalf("stale deactivation must not overwrite a newer epoch: %d %s", response.Code, response.Body.String())
	}
}

func TestSignedDeactivationBarrierAdvancesAlreadyInactiveLegacyEpoch(t *testing.T) {
	db := eventHandlerTestDB(t)
	productionID := "10000000-0000-4000-8000-000000000001"
	key := "synthetic-event-signing-key-material-0001"
	clock := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	if result := db.Model(&model.VFXFilesProductionConfig{}).Where("production_id = ?", productionID).
		Updates(map[string]any{"config_revision": 7, "activation_epoch": 4, "activated": false}); result.Error != nil || result.RowsAffected != 1 {
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		t.Fatalf("legacy config rows updated = %d, want 1", result.RowsAffected)
	}
	eventID := "60000000-0000-4000-8000-000000000093"
	body, err := json.Marshal(map[string]any{"contract_version": "1.0", "events": []any{map[string]any{
		"contract_version": "1.0", "producer_id": "vfx-files-plugin", "event_id": eventID,
		"event_type": "storage_authority.deactivation_requested", "occurred_at": clock.Format(time.RFC3339),
		"production_id": productionID, "activation_epoch": 4, "config_revision": 8,
		"previous_activation_epoch": 4, "new_activation_epoch": 5,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	req := signedEventRequest(t, body, key, "authority-nonce-inactive-0001", eventID, clock)
	response := httptest.NewRecorder()
	EventHandler(db, map[string][]byte{"fake-key-1": []byte(key)}, func() time.Time { return clock })(response, req)
	if response.Code != http.StatusAccepted {
		t.Fatalf("inactive deactivation barrier=%d %s", response.Code, response.Body.String())
	}
	var config model.VFXFilesProductionConfig
	if err := db.Where("production_id = ?", productionID).Take(&config).Error; err != nil || config.Activated || config.ActivationEpoch != 5 || config.ConfigRevision != 8 {
		t.Fatalf("inactive legacy mirror was not fenced: %+v err=%v", config, err)
	}
	var receipt model.VFXFilesEventInbox
	if err := db.Where("event_id = ?", eventID).Take(&receipt).Error; err != nil || receipt.ProcessedAt == nil || receipt.DiagnosticCode != "activation_barrier_complete" {
		t.Fatalf("inactive barrier receipt=%+v err=%v", receipt, err)
	}
}

// TestPluginLegacyImportReceivesDurableKitsuSyncBarrierAck runs the Plugin's
// real outbox client against this package's signed HTTPS receiver. CI enables
// it only after checking out the exact Plugin PR head and preparing its small
// Python test environment; normal Go unit runs remain repository-local.
func TestPluginLegacyImportReceivesDurableKitsuSyncBarrierAck(t *testing.T) {
	pluginRoot := strings.TrimSpace(os.Getenv("VFX_FILES_CROSS_REPO_PLUGIN_ROOT"))
	if pluginRoot == "" {
		t.Skip("set VFX_FILES_CROSS_REPO_PLUGIN_ROOT to run the pinned Plugin integration")
	}
	pluginSHA := strings.TrimSpace(os.Getenv("VFX_FILES_CROSS_REPO_PLUGIN_SHA"))
	if decoded, err := hex.DecodeString(pluginSHA); err != nil || len(decoded) != sha256.Size {
		t.Fatal("VFX_FILES_CROSS_REPO_PLUGIN_SHA must pin the 40-character Plugin commit SHA")
	}
	pluginHead, err := exec.Command("git", "-C", pluginRoot, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("read pinned Plugin HEAD: %v: %s", err, pluginHead)
	}
	if got := strings.TrimSpace(string(pluginHead)); got != pluginSHA {
		t.Fatalf("Plugin checkout HEAD = %s, want pinned SHA %s", got, pluginSHA)
	}

	db := eventHandlerTestDB(t)
	productionID := "10000000-0000-4000-8000-000000000001"
	fileEventID := "60000000-0000-4000-8000-000000000090"
	clock := time.Now().UTC()
	if result := db.Model(&model.VFXFilesProductionConfig{}).Where("production_id = ?", productionID).
		Updates(map[string]any{"config_revision": 7, "activation_epoch": 4, "activated": true}); result.Error != nil || result.RowsAffected != 1 {
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		t.Fatalf("legacy config rows updated = %d, want 1", result.RowsAffected)
	}
	if err := db.Create(&model.VFXFilesEventInbox{
		ProducerID: "vfx-files-plugin", EventID: fileEventID, PayloadSHA256: strings.Repeat("a", 64),
		PayloadJSON: `{"synthetic":"prior-delivery"}`, EventType: "file_delivery.ready", ProductionID: productionID,
		ActivationEpoch: 4, Outcome: model.VFXFilesEventAccepted, CreatedAt: clock,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NotificationDeliveryEvent{
		EventID: "70000000-0000-4000-8000-000000000090", TaskID: "synthetic-task", ProductionID: productionID,
		TaskTypeID: "synthetic-task-type", TaskStatus: "FILE_READY", DeliverySource: "vfx_files", SourceEventID: fileEventID,
		PayloadJSON: `{}`, PayloadSHA256: strings.Repeat("b", 64), WebhookRowID: 1,
		DestinationGuildID: "synthetic-guild", DestinationChannelID: "synthetic-channel", DestinationWebhookID: "synthetic-webhook",
		DestinationFingerprint: strings.Repeat("c", 64), Disposition: model.DeliveryUnknown,
	}).Error; err != nil {
		t.Fatal(err)
	}

	controlSecret := make([]byte, 32)
	if _, err := rand.Read(controlSecret); err != nil {
		t.Fatal(err)
	}
	controlKey := hex.EncodeToString(controlSecret)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/integrations/vfx-files/v1/events", EventHandler(
		db, map[string][]byte{"vfx-files-plugin": []byte("synthetic-event-signing-key-material-0001")}, time.Now,
	))
	mux.HandleFunc("POST /__test__/resolve", func(w http.ResponseWriter, r *http.Request) {
		if !hmac.Equal([]byte(r.Header.Get("X-Test-Control-Key")), []byte(controlKey)) {
			http.Error(w, "unauthorized test control", http.StatusUnauthorized)
			return
		}
		var mirror model.VFXFilesProductionConfig
		if err := db.Where("production_id = ?", productionID).Take(&mirror).Error; err != nil || !mirror.Activated || mirror.ActivationEpoch != 4 {
			http.Error(w, "unresolved barrier was not fail-closed", http.StatusConflict)
			return
		}
		var unresolved model.NotificationDeliveryEvent
		if err := db.Where("event_id = ?", "70000000-0000-4000-8000-000000000090").Take(&unresolved).Error; err != nil || unresolved.Disposition != model.DeliveryUnknown {
			http.Error(w, "synthetic Discord outcome was not left unresolved", http.StatusConflict)
			return
		}
		result := db.Model(&model.NotificationDeliveryEvent{}).
			Where("event_id = ? AND disposition = ?", "70000000-0000-4000-8000-000000000090", model.DeliveryUnknown).
			Update("disposition", model.DeliveryPending)
		if result.Error != nil {
			http.Error(w, "synthetic dispatch resolution failed", http.StatusInternalServerError)
			return
		}
		if result.RowsAffected != 1 {
			http.Error(w, "synthetic dispatch was not unresolved", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	tempDir := t.TempDir()
	keyPath := filepath.Join(tempDir, "event-hmac.key")
	if err := os.WriteFile(keyPath, []byte("synthetic-event-signing-key-material-0001"), 0o600); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(tempDir, "receiver-ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(certPath, certificate, 0o600); err != nil {
		t.Fatal(err)
	}

	python := strings.TrimSpace(os.Getenv("VFX_FILES_CROSS_REPO_TEST_PYTHON"))
	if python == "" {
		python = "python3"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-m", "pytest", "-q", "tests/integrations/test_oauth_vault.py::test_native_integrated_legacy_import_waits_for_durable_barrier_ack")
	cmd.Dir = pluginRoot
	cmd.Env = append(os.Environ(),
		"VFX_FILES_CROSS_REPO_PLUGIN_SHA="+pluginSHA,
		"VFX_FILES_CROSS_REPO_TEST_ENDPOINT="+server.URL+"/api/integrations/vfx-files/v1/events",
		"VFX_FILES_CROSS_REPO_TEST_HMAC_KEY_FILE="+keyPath,
		"VFX_FILES_CROSS_REPO_TEST_KEY_ID=vfx-files-plugin",
		"VFX_FILES_CROSS_REPO_TEST_CONTROL_URL="+server.URL+"/__test__/resolve",
		"VFX_FILES_CROSS_REPO_TEST_CONTROL_KEY="+controlKey,
		"SSL_CERT_FILE="+certPath,
	)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("Plugin HTTP integration timed out: %s", output)
	}
	if err != nil {
		t.Fatalf("Plugin HTTP integration failed: %v\n%s", err, output)
	}
	var mirror model.VFXFilesProductionConfig
	if err := db.Where("production_id = ?", productionID).Take(&mirror).Error; err != nil || mirror.Activated || mirror.ActivationEpoch != 5 || mirror.ConfigRevision != 8 {
		t.Fatalf("durable barrier mirror=%+v err=%v", mirror, err)
	}
	var resolved model.NotificationDeliveryEvent
	if err := db.Where("event_id = ?", "70000000-0000-4000-8000-000000000090").Take(&resolved).Error; err != nil || resolved.Disposition != model.DeliveryTerminalFailure || resolved.FailureClass != "activation_epoch_fenced" {
		t.Fatalf("old-epoch Discord journal=%+v err=%v", resolved, err)
	}
	var receipt model.VFXFilesEventInbox
	if err := db.Where("production_id = ? AND event_type = ? AND diagnostic_code = ?", productionID, "storage_authority.deactivation_requested", "activation_barrier_complete").Take(&receipt).Error; err != nil || receipt.ProcessedAt == nil {
		t.Fatalf("durable barrier receipt=%+v err=%v", receipt, err)
	}
	t.Logf("Plugin HEAD %s passed durable retry, receiver ACK, and authenticated fresh-confirmation test: %s", pluginSHA, strings.TrimSpace(string(output)))
}

func TestAuthorityBarrierDefersForUnknownDiscordOutcomeAndFencesPendingJournal(t *testing.T) {
	db := eventHandlerTestDB(t)
	key := "synthetic-event-signing-key-material-0001"
	clock := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	handler := EventHandler(db, map[string][]byte{"fake-key-1": []byte(key)}, func() time.Time { return clock })
	productionID := "10000000-0000-4000-8000-000000000001"
	fileEventID := "60000000-0000-4000-8000-000000000090"
	if err := db.Create(&model.VFXFilesEventInbox{
		ProducerID: "vfx-files-plugin", EventID: fileEventID, PayloadSHA256: strings.Repeat("a", 64),
		PayloadJSON: `{"stale":"pending"}`, EventType: "file_delivery.ready", ProductionID: productionID,
		ActivationEpoch: 1, Outcome: model.VFXFilesEventAccepted, CreatedAt: clock,
	}).Error; err != nil {
		t.Fatal(err)
	}
	journal := model.NotificationDeliveryEvent{
		EventID: "70000000-0000-4000-8000-000000000090", TaskID: "task-a", ProductionID: productionID,
		TaskTypeID: "task-type-a", TaskStatus: "FILE_READY", DeliverySource: "vfx_files", SourceEventID: fileEventID,
		PayloadJSON: `{}`, PayloadSHA256: strings.Repeat("b", 64), WebhookRowID: 1,
		DestinationGuildID: "guild-a", DestinationChannelID: "channel-a", DestinationWebhookID: "webhook-a",
		DestinationFingerprint: strings.Repeat("c", 64), Disposition: model.DeliveryUnknown,
	}
	if err := db.Create(&journal).Error; err != nil {
		t.Fatal(err)
	}
	requestFor := func(nonce string) *http.Request {
		body, err := json.Marshal(map[string]any{"contract_version": "1.0", "events": []any{map[string]any{
			"contract_version": "1.0", "producer_id": "vfx-files-plugin",
			"event_id":   "60000000-0000-4000-8000-000000000091",
			"event_type": "storage_authority.deactivation_requested", "occurred_at": clock.Format(time.RFC3339),
			"production_id": productionID, "activation_epoch": 1, "config_revision": 4,
			"previous_activation_epoch": 1, "new_activation_epoch": 2,
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return signedEventRequest(t, body, key, nonce, "60000000-0000-4000-8000-000000000091", clock)
	}
	response := httptest.NewRecorder()
	handler(response, requestFor("authority-nonce-unresolved-01"))
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "30" {
		t.Fatalf("unresolved Discord outcome must defer barrier ACK: status=%d retry=%q body=%s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
	var config model.VFXFilesProductionConfig
	if err := db.Where("production_id = ?", productionID).Take(&config).Error; err != nil || !config.Activated || config.ActivationEpoch != 1 {
		t.Fatalf("unresolved dispatch changed mirror authority: %+v err=%v", config, err)
	}
	if err := db.Model(&model.NotificationDeliveryEvent{}).Where("event_id = ?", journal.EventID).Update("disposition", model.DeliveryPending).Error; err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler(response, requestFor("authority-nonce-resolved-0001"))
	if response.Code != http.StatusAccepted {
		t.Fatalf("resolved old delivery barrier=%d %s", response.Code, response.Body.String())
	}
	var fenced model.NotificationDeliveryEvent
	if err := db.Where("event_id = ?", journal.EventID).Take(&fenced).Error; err != nil || fenced.Disposition != model.DeliveryTerminalFailure || fenced.FailureClass != "activation_epoch_fenced" {
		t.Fatalf("pending old dispatch was not durably terminalized: %+v err=%v", fenced, err)
	}
}

func TestSignedEventHandlerQuarantinesWrongProductionStaleEpochAndDisabledIntegration(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", "folder-provisioning-completed.json"))
	if err != nil {
		t.Fatal(err)
	}
	key := "synthetic-event-signing-key-material-0001"
	clock := time.Date(2026, 10, 12, 16, 30, 0, 0, time.UTC)
	cases := []struct {
		name         string
		mutateBody   func([]byte) []byte
		deactivate   bool
		currentEpoch uint64
	}{
		{name: "wrong production", mutateBody: func(body []byte) []byte {
			return []byte(strings.Replace(string(body), "10000000-0000-4000-8000-000000000001", "10000000-0000-4000-8000-000000000099", 1))
		}},
		{name: "stale activation epoch", mutateBody: func(body []byte) []byte {
			return []byte(strings.Replace(string(body), `"activation_epoch": 1`, `"activation_epoch": 2`, 1))
		}},
		{name: "past activation epoch", currentEpoch: 2, mutateBody: func(body []byte) []byte { return body }},
		{name: "disabled integration", mutateBody: func(body []byte) []byte { return body }, deactivate: true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := eventHandlerTestDB(t)
			if tc.currentEpoch != 0 {
				if err := db.Model(&model.VFXFilesProductionConfig{}).Where("production_id = ?", "10000000-0000-4000-8000-000000000001").Update("activation_epoch", tc.currentEpoch).Error; err != nil {
					t.Fatal(err)
				}
			}
			if tc.deactivate {
				if err := db.Model(&model.VFXFilesProductionConfig{}).Where("production_id = ?", "10000000-0000-4000-8000-000000000001").Update("activated", false).Error; err != nil {
					t.Fatal(err)
				}
			}
			body := tc.mutateBody(fixture)
			handler := EventHandler(db, map[string][]byte{"fake-key-1": []byte(key)}, func() time.Time { return clock })
			response := httptest.NewRecorder()
			handler(response, signedEventRequest(t, body, key, fmt.Sprintf("quarantine-nonce-%04d", i), fmt.Sprintf("quarantine-idemp-%04d", i), clock))
			var result struct {
				Results []model.VFXFilesEventResult `json:"results"`
			}
			if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Results) != 1 || result.Results[0].Outcome != model.VFXFilesEventQuarantined || result.Results[0].Code != "activation_epoch_mismatch" {
				t.Fatalf("unsafe-scope event was not durably quarantined: status=%d body=%s", response.Code, response.Body.String())
			}
			var eventCount int64
			if err := db.Model(&model.VFXFilesEventInbox{}).Where("outcome = ? AND diagnostic_code = ?", model.VFXFilesEventQuarantined, "activation_epoch_mismatch").Count(&eventCount).Error; err != nil || eventCount != 1 {
				t.Fatalf("quarantine was not durable: count=%d err=%v", eventCount, err)
			}
		})
	}
}

func TestSignedEventDuringActivationMirrorGapIsRetryableWithFreshNonce(t *testing.T) {
	db := eventHandlerTestDB(t)
	if err := db.Model(&model.VFXFilesProductionConfig{}).Where("production_id = ?", "10000000-0000-4000-8000-000000000001").Update("pending_activation_epoch", 2).Error; err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", "folder-provisioning-completed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtureBatch eventBatch
	if err := json.Unmarshal(fixture, &fixtureBatch); err != nil || fixtureBatch.ContractVersion != contractVersion || len(fixtureBatch.Events) != 1 {
		t.Fatalf("invalid folder event fixture: contract=%q events=%d err=%v", fixtureBatch.ContractVersion, len(fixtureBatch.Events), err)
	}
	validNextEpoch := json.RawMessage(strings.Replace(string(fixtureBatch.Events[0]), `"activation_epoch": 1`, `"activation_epoch": 2`, 1))
	var pendingEvent map[string]any
	if err := json.Unmarshal(validNextEpoch, &pendingEvent); err != nil {
		t.Fatal(err)
	}
	unknownProductionEvent := make(map[string]any, len(pendingEvent))
	for key, value := range pendingEvent {
		unknownProductionEvent[key] = value
	}
	unknownProductionEvent["event_id"] = "60000000-0000-4000-8000-000000000099"
	unknownProductionEvent["production_id"] = "10000000-0000-4000-8000-000000000099"
	batchBody, err := json.Marshal(map[string]any{"contract_version": "1.0", "events": []any{pendingEvent, unknownProductionEvent}})
	if err != nil {
		t.Fatal(err)
	}
	key := "synthetic-event-signing-key-material-0001"
	clock := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	handler := EventHandler(db, map[string][]byte{"fake-key-1": []byte(key)}, func() time.Time { return clock })
	first := httptest.NewRecorder()
	handler(first, signedEventRequest(t, batchBody, key, "activation-gap-nonce-000001", "activation-gap-idempotency-000001", clock))
	if first.Code != http.StatusServiceUnavailable || first.Header().Get("Retry-After") == "" {
		t.Fatalf("ahead event during marked activation gap must request retry: status=%d headers=%v body=%s", first.Code, first.Header(), first.Body.String())
	}
	var nonceCount, eventCount int64
	if err := db.Model(&model.VFXFilesEventNonce{}).Count(&nonceCount).Error; err != nil || nonceCount != 1 {
		t.Fatalf("retryable batch did not durably consume its request nonce: count=%d err=%v", nonceCount, err)
	}
	if err := db.Model(&model.VFXFilesEventInbox{}).Count(&eventCount).Error; err != nil || eventCount != 1 {
		t.Fatalf("retryable batch should persist only its non-pending event: count=%d err=%v", eventCount, err)
	}
	var quarantined model.VFXFilesEventInbox
	if err := db.Where("event_id = ?", "60000000-0000-4000-8000-000000000099").First(&quarantined).Error; err != nil || quarantined.Outcome != model.VFXFilesEventQuarantined {
		t.Fatalf("unknown Production was not durably quarantined while the valid next epoch retried: %+v err=%v", quarantined, err)
	}
	pendingEventID := pendingEvent["event_id"].(string)
	var pendingCount int64
	if err := db.Model(&model.VFXFilesEventInbox{}).Where("event_id = ?", pendingEventID).Count(&pendingCount).Error; err != nil || pendingCount != 0 {
		t.Fatalf("pending next-epoch event was persisted during activation: count=%d err=%v", pendingCount, err)
	}

	if err := db.Model(&model.VFXFilesProductionConfig{}).Where("production_id = ?", "10000000-0000-4000-8000-000000000001").Updates(map[string]any{
		"activation_epoch": 2, "activated": true, "pending_activation_epoch": 0,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VFXFilesProductionConfig{ProductionID: "10000000-0000-4000-8000-000000000099", ActivationEpoch: 2, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	// Provider retries use a fresh nonce while preserving the stable Event ID.
	second := httptest.NewRecorder()
	handler(second, signedEventRequest(t, batchBody, key, "activation-gap-nonce-000002", "activation-gap-idempotency-000002", clock))
	if second.Code != http.StatusAccepted {
		t.Fatalf("same Event ID with fresh nonce was not durably accepted after mirror convergence: status=%d body=%s", second.Code, second.Body.String())
	}
	var accepted int64
	if err := db.Model(&model.VFXFilesEventInbox{}).Where("outcome = ?", model.VFXFilesEventAccepted).Count(&accepted).Error; err != nil || accepted != 1 {
		t.Fatalf("activation-gap retry was not accepted exactly once: count=%d err=%v", accepted, err)
	}
	if err := db.Where("event_id = ?", "60000000-0000-4000-8000-000000000099").First(&quarantined).Error; err != nil || quarantined.Outcome != model.VFXFilesEventQuarantined {
		t.Fatalf("previously unknown Production event was silently rebound after retry: %+v err=%v", quarantined, err)
	}

	// Outside the marked activation transition, disabled and unknown scopes
	// retain the existing durable quarantine behavior.
	if err := db.Model(&model.VFXFilesProductionConfig{}).Where("production_id = ?", "10000000-0000-4000-8000-000000000001").Update("activated", false).Error; err != nil {
		t.Fatal(err)
	}
	otherEvent := json.RawMessage(strings.Replace(string(validNextEpoch), pendingEventID, "60000000-0000-4000-8000-000000000099", 1))
	otherBody, err := json.Marshal(eventBatch{ContractVersion: contractVersion, Events: []json.RawMessage{otherEvent}})
	if err != nil {
		t.Fatal(err)
	}
	third := httptest.NewRecorder()
	handler(third, signedEventRequest(t, otherBody, key, "activation-gap-nonce-000003", "activation-gap-idempotency-000003", clock))
	var quarantineResponse struct {
		Results []model.VFXFilesEventResult `json:"results"`
	}
	if third.Code != http.StatusAccepted || json.Unmarshal(third.Body.Bytes(), &quarantineResponse) != nil || len(quarantineResponse.Results) != 1 || quarantineResponse.Results[0].Outcome != model.VFXFilesEventQuarantined {
		t.Fatalf("disabled integration did not remain fail-closed: status=%d body=%s", third.Code, third.Body.String())
	}
}

func TestPluginEventHandlerFailsClosedOnSignatureTimestampAndMalformedPayload(t *testing.T) {
	db := eventHandlerTestDB(t)
	key := "synthetic-event-signing-key-material-0001"
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	handler := EventHandler(db, map[string][]byte{"fake-key-1": []byte(key)}, func() time.Time { return now })
	good := []byte(`{"contract_version":"1.0","events":[{"contract_version":"1.0","producer_id":"vfx-files-plugin","event_id":"60000000-0000-4000-8000-000000000001","event_type":"folder.provisioning.completed","occurred_at":"2026-10-12T12:00:00Z","production_id":"10000000-0000-4000-8000-000000000001","activation_epoch":1,"folder":{"object_type":"production","object_id":"10000000-0000-4000-8000-000000000001","drive_file_id":"fake-drive-id","binding_state":"ready"}}]}`)
	request := signedEventRequest(t, good, key, "synthetic-nonce-0000002", "synthetic-idempotency-0002", now)
	request.Header.Set("X-VFX-Signature", "sha256="+strings.Repeat("0", 64))
	response := httptest.NewRecorder()
	handler(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid HMAC status=%d", response.Code)
	}
	request = signedEventRequest(t, good, key, "synthetic-nonce-0000003", "synthetic-idempotency-0003", now.Add(-6*time.Minute))
	response = httptest.NewRecorder()
	handler(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("stale timestamp status=%d", response.Code)
	}
	malformed := []byte(`{"contract_version":"1.0","events":[{"contract_version":"1.0","producer_id":"vfx-files-plugin","event_id":"60000000-0000-4000-8000-000000000001","event_type":"file_delivery.ready","occurred_at":"2026-10-12T12:00:00Z","production_id":"10000000-0000-4000-8000-000000000001","activation_epoch":1,"unexpected_secret":"must never echo"}]}`)
	request = signedEventRequest(t, malformed, key, "synthetic-nonce-0000004", "synthetic-idempotency-0004", now)
	response = httptest.NewRecorder()
	handler(response, request)
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "must never echo") {
		t.Fatalf("malformed payload status/body=%d %s", response.Code, response.Body.String())
	}
	var nonceCount int64
	if err := db.Model(&model.VFXFilesEventNonce{}).Count(&nonceCount).Error; err != nil || nonceCount != 0 {
		t.Fatalf("invalid events persisted nonce count=%d err=%v", nonceCount, err)
	}
}

func TestDecodeFileDeliveryNotificationValidatesTaskTypeSeparatesRevisionAndOmitsDriveReferences(t *testing.T) {
	body := []byte(`{"contract_version":"1.0","producer_id":"vfx-files-plugin","event_id":"60000000-0000-4000-8000-000000000001","event_type":"file_delivery.ready","occurred_at":"2026-10-12T12:00:00Z","production_id":"10000000-0000-4000-8000-000000000001","activation_epoch":1,"production_name":"Synthetic","entity":{"type":"shot","id":"30000000-0000-4000-8000-000000000001","name":"sh010"},"source":{"id":"50000000-0000-4000-8000-000000000001","task_type_id":"50000000-0000-4000-8000-000000000010","task_type_name":"Compositing"},"sender":{"id":"40000000-0000-4000-8000-000000000001","display_name":"Synthetic Artist"},"output_package":{"id":"70000000-0000-4000-8000-000000000001","version":3,"classification":"Low-res EXR","manifest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","objects":[{"drive_file_id":"fake-drive-file-01","classification":"EXR","size_bytes":10,"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},"delivery":{"id":"90000000-0000-4000-8000-000000000001","revision":1,"status":"ready","recipient_task_id":"50000000-0000-4000-8000-000000000002","recipient_task_type_id":"50000000-0000-4000-8000-000000000011","recipient_task_type_name":"FX"}}`)
	input, err := parsePluginEvent(body)
	if err != nil || input.DeliveryID != "90000000-0000-4000-8000-000000000001" || input.DeliveryRevision != 1 {
		t.Fatalf("parse delivery stream=%s revision=%d err=%v", input.DeliveryID, input.DeliveryRevision, err)
	}
	decoded, err := DecodeFileDeliveryNotification(body)
	if err != nil || decoded.EventID != "60000000-0000-4000-8000-000000000001" || decoded.DeliveryID != input.DeliveryID || decoded.RecipientTaskTypeName != "FX" || decoded.OutputVersion != 3 || int(input.DeliveryRevision) == decoded.OutputVersion {
		t.Fatalf("decode=%+v err=%v", decoded, err)
	}
	if strings.Contains(strings.Join([]string{decoded.EventID, decoded.EntityName, decoded.OutputClassification}, " "), "fake-drive-file-01") {
		t.Fatal("Drive object reference leaked into notification projection")
	}
	missingTaskTypeName := strings.Replace(string(body), `,"task_type_name":"Compositing"`, "", 1)
	if _, err := DecodeFileDeliveryNotification([]byte(missingTaskTypeName)); err == nil {
		t.Fatal("event missing contract-required task_type_name was accepted")
	}
}

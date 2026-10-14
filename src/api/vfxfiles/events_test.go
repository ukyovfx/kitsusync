package vfxfiles

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	if err := db.AutoMigrate(&model.VFXFilesProductionConfig{}, &model.VFXFilesEventInbox{}, &model.VFXFilesEventNonce{}, &model.VFXFilesEventConflict{}, &model.NotificationDeliveryEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VFXFilesProductionConfig{ProductionID: "10000000-0000-4000-8000-000000000001", ActivationEpoch: 1, Activated: true}).Error; err != nil {
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
	otherBody := []byte(strings.Replace(string(validNextEpoch), "60000000-0000-4000-8000-000000000001", "60000000-0000-4000-8000-000000000099", 1))
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

func TestDecodeFileDeliveryNotificationRequiresContractValidTaskTypeAndOmitsDriveReferences(t *testing.T) {
	body := []byte(`{"contract_version":"1.0","producer_id":"vfx-files-plugin","event_id":"60000000-0000-4000-8000-000000000001","event_type":"file_delivery.ready","occurred_at":"2026-10-12T12:00:00Z","production_id":"10000000-0000-4000-8000-000000000001","activation_epoch":1,"production_name":"Synthetic","entity":{"type":"shot","id":"30000000-0000-4000-8000-000000000001","name":"sh010"},"source":{"id":"50000000-0000-4000-8000-000000000001","task_type_id":"50000000-0000-4000-8000-000000000010","task_type_name":"Compositing"},"sender":{"id":"40000000-0000-4000-8000-000000000001","display_name":"Synthetic Artist"},"output_package":{"id":"70000000-0000-4000-8000-000000000001","version":3,"classification":"Low-res EXR","manifest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","objects":[{"drive_file_id":"fake-drive-file-01","classification":"EXR","size_bytes":10,"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},"delivery":{"id":"90000000-0000-4000-8000-000000000001","revision":1,"status":"ready","recipient_task_id":"50000000-0000-4000-8000-000000000002","recipient_task_type_id":"50000000-0000-4000-8000-000000000011","recipient_task_type_name":"FX"}}`)
	decoded, err := DecodeFileDeliveryNotification(body)
	if err != nil || decoded.EventID != "60000000-0000-4000-8000-000000000001" || decoded.DeliveryID != "90000000-0000-4000-8000-000000000001" || decoded.RecipientTaskTypeName != "FX" || decoded.OutputVersion != 3 {
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

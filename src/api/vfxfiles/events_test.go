package vfxfiles

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	if err := db.AutoMigrate(&model.VFXFilesProductionConfig{}, &model.VFXFilesEventInbox{}, &model.VFXFilesEventNonce{}, &model.VFXFilesEventConflict{}); err != nil {
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

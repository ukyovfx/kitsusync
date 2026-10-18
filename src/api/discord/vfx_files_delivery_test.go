package discord

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

	"app/src/api/vfxfiles"
	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestVFXFilesJournalTaskIDIsStableAndSeparateFromKitsuTaskIDs(t *testing.T) {
	deliveryID := "90000000-0000-4000-8000-000000000001"
	first := vfxFilesJournalTaskID(deliveryID)
	if first != vfxFilesJournalTaskID(" 90000000-0000-4000-8000-000000000001 ") {
		t.Fatal("same recipient delivery did not map to a stable journal identity")
	}
	if first == deliveryID || first == "50000000-0000-4000-8000-000000000002" {
		t.Fatal("Plugin delivery journal identity can collide with a native Kitsu Task ID")
	}
	if first == vfxFilesJournalTaskID("90000000-0000-4000-8000-000000000002") {
		t.Fatal("distinct recipient deliveries share journal identity")
	}
}

func TestFileSharePayloadMarksSourceEventAndDisablesMentions(t *testing.T) {
	const eventID = "60000000-0000-4000-8000-000000000090"
	payload := fileSharePayload(nil, vfxfiles.FileShareNotification{
		EventID: eventID, ProductionID: "10000000-0000-4000-8000-000000000001",
		EntityType: "shot", EntityID: "30000000-0000-4000-8000-000000000001", RecipientTaskID: "50000000-0000-4000-8000-000000000002",
		ObjectID: "opaque-id", ObjectType: "file", URL: "https://drive.google.com/file/d/opaque-id/view", Name: "plates.exr",
	})
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, expected := range []string{"https://drive.google.com/file/d/opaque-id/view", "existing Google Drive ACLs", "No Drive ACLs were changed", "plates.exr", vfxFilesDeliveryMarkerPrefix + eventID} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Drive link notification omitted %q: %s", expected, text)
		}
	}
	if payload.AllowedMentions == nil || len(payload.AllowedMentions.Parse) != 0 || len(payload.AllowedMentions.Users) != 0 || len(payload.AllowedMentions.Roles) != 0 {
		t.Fatalf("share message mentions were not disabled: %+v", payload.AllowedMentions)
	}
}

func TestSignedFileShareV11ConsentGatesDurableDiscordDispatch(t *testing.T) {
	const (
		productionID = "10000000-0000-4000-8000-000000000001"
		taskTypeID   = "90d30435-7cff-495c-a316-1f10aa6aa0d8"
	)
	var posts []Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/webhooks/12345/fake-token" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":1,"id":"12345","guild_id":"45678","channel_id":"78901"}`))
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload Payload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode synthetic webhook payload: %v", err)
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		posts = append(posts, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"112233445566778899","channel_id":"78901"}`))
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "file-share-e2e.db")+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationRoute{},
		&model.ProductionNotificationConfig{}, &model.ProjectReviewerTarget{}, &model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{},
		&model.ProductionNotificationWriteLock{}, &model.VFXFilesProductionConfig{}, &model.VFXFilesEventInbox{},
		&model.VFXFilesEventNonce{}, &model.VFXFilesEventConflict{}, &model.VFXFilesDriveLinkDisclosureConsent{}, &model.VFXFilesDriveLinkDisclosureAudit{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	project := model.Project{KitsuProjectID: productionID, Name: "Synthetic Share Production", DiscordGuildID: "45678", Language: "en"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationConfig{ProductionID: productionID, ProductionName: project.Name, Enabled: true, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	webhook := model.ProjectWebhook{KitsuProjectID: productionID, ChannelName: "fx", TaskType: "FX", WebhookURL: server.URL + "/api/webhooks/12345/fake-token", DiscordChannelID: "78901"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	route := model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: taskTypeID, TaskTypeName: "FX", DestinationWebhookID: webhook.ID, DestinationChannelID: "78901"}
	if err := db.Create(&route).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VFXFilesProductionConfig{ProductionID: productionID, ActivationEpoch: 7, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	project = *model.FindProjectByKitsuID(db, productionID)
	revision, err := model.ProductionNotificationRevisionWithError(db, project.ID, productionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.SetVFXFilesDriveLinkDisclosureConsent(db, productionID, taskTypeID, revision, "synthetic-admin@example.test", true, now); err != nil {
		t.Fatalf("enable exact route consent: %v", err)
	}

	fixtureBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "integrations", "vfx-files-plugin", "v1", "fixtures", "file-share-created-v1.1-two-recipient.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ContractVersion string            `json:"contract_version"`
		Events          []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil || fixture.ContractVersion != "1.1" || len(fixture.Events) != 2 {
		t.Fatalf("synthetic v1.1 fixture: version=%q events=%d err=%v", fixture.ContractVersion, len(fixture.Events), err)
	}
	shareBody := func(raw json.RawMessage) []byte {
		var event map[string]any
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		event["production_id"] = productionID
		event["activation_epoch"] = 7
		recipient := event["recipient"].(map[string]any)
		recipient["task_type_id"] = taskTypeID
		recipient["task_type_name"] = "FX"
		share := event["share"].(map[string]any)
		object := share["object"].(map[string]any)
		object["object_type"] = "file"
		object["object_id"] = "synthetic-drive-file-01"
		object["url"] = "https://drive.google.com/file/d/synthetic-drive-file-01/view"
		metadata := share["metadata"].(map[string]any)
		metadata["name"] = "plates_v003.exr"
		body, err := json.Marshal(map[string]any{"contract_version": "1.1", "events": []any{event}})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	oldResolver := KitsuPublicURLResolver
	KitsuPublicURLResolver = func() string { return "https://kitsu.example.invalid" }
	t.Cleanup(func() { KitsuPublicURLResolver = oldResolver })

	const signingKey = "synthetic-plugin-event-key-material-0001"
	keyID := "synthetic-plugin-key"
	var nonceNumber int
	sendSigned := func(body []byte) *httptest.ResponseRecorder {
		nonceNumber++
		nonce := fmt.Sprintf("synthetic-share-nonce-%06d", nonceNumber)
		idempotency := fmt.Sprintf("synthetic-share-idempotency-%06d", nonceNumber)
		timestamp := now.Format(time.RFC3339)
		bodyHash := sha256.Sum256(body)
		canonical := timestamp + "\n" + nonce + "\n" + hex.EncodeToString(bodyHash[:]) + "\n" + idempotency
		mac := hmac.New(sha256.New, []byte(signingKey))
		_, _ = mac.Write([]byte(canonical))
		request := httptest.NewRequest(http.MethodPost, "/api/integrations/vfx-files/v1/events", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-VFX-Key-Id", keyID)
		request.Header.Set("X-VFX-Timestamp", timestamp)
		request.Header.Set("X-VFX-Nonce", nonce)
		request.Header.Set("X-VFX-Content-SHA256", hex.EncodeToString(bodyHash[:]))
		request.Header.Set("X-VFX-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		request.Header.Set("Idempotency-Key", idempotency)
		response := httptest.NewRecorder()
		vfxfiles.EventHandler(db, map[string][]byte{keyID: []byte(signingKey)}, func() time.Time { return now })(response, request)
		return response
	}

	firstBody := shareBody(fixture.Events[0])
	firstResponse := sendSigned(firstBody)
	var accepted struct {
		ContractVersion string                      `json:"contract_version"`
		Results         []model.VFXFilesEventResult `json:"results"`
	}
	if firstResponse.Code != http.StatusAccepted || json.Unmarshal(firstResponse.Body.Bytes(), &accepted) != nil || accepted.ContractVersion != "1.0" || len(accepted.Results) != 1 || accepted.Results[0].Outcome != model.VFXFilesEventAccepted {
		t.Fatalf("signed share intake: status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	firstEventID := accepted.Results[0].EventID
	processed, deferred, err := ProcessVFXFilesDeliveryEvents(db, 10)
	if err != nil || processed != 1 || deferred != 0 || len(posts) != 1 {
		t.Fatalf("first share dispatch: processed=%d deferred=%d posts=%d err=%v", processed, deferred, len(posts), err)
	}
	payloadText, err := json.Marshal(posts[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(posts[0].Embeds) != 1 || posts[0].Embeds[0].Url != "https://drive.google.com/file/d/synthetic-drive-file-01/view" ||
		!strings.Contains(posts[0].Embeds[0].Description, "existing Google Drive ACLs") || !strings.Contains(posts[0].Embeds[0].Description, "No Drive ACLs were changed") ||
		strings.Contains(string(payloadText), "synthetic-admin@example.test") {
		t.Fatalf("share message omitted authorized link/ACL wording or leaked consent actor: %s", payloadText)
	}
	if posts[0].AllowedMentions == nil || len(posts[0].AllowedMentions.Parse) != 0 || len(posts[0].AllowedMentions.Users) != 0 || len(posts[0].AllowedMentions.Roles) != 0 {
		t.Fatalf("share message mentions were not disabled: %+v", posts[0].AllowedMentions)
	}
	var journal model.NotificationDeliveryEvent
	if err := db.Where("source_event_id = ? AND delivery_source = ?", firstEventID, "vfx_files").First(&journal).Error; err != nil || journal.Disposition != model.DeliveryDelivered {
		t.Fatalf("successful share journal=%+v err=%v", journal, err)
	}
	processed, deferred, err = ProcessVFXFilesDeliveryEvents(db, 10)
	if err != nil || processed != 0 || deferred != 0 || len(posts) != 1 {
		t.Fatalf("idempotent second pass: processed=%d deferred=%d posts=%d err=%v", processed, deferred, len(posts), err)
	}

	// Accept a second event while consent is active, then revoke before the
	// worker begins it. The worker must quarantine without another webhook POST.
	secondResponse := sendSigned(shareBody(fixture.Events[1]))
	if secondResponse.Code != http.StatusAccepted {
		t.Fatalf("second signed share intake: status=%d body=%s", secondResponse.Code, secondResponse.Body.String())
	}
	var secondAccepted struct {
		Results []model.VFXFilesEventResult `json:"results"`
	}
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &secondAccepted); err != nil || len(secondAccepted.Results) != 1 || secondAccepted.Results[0].Outcome != model.VFXFilesEventAccepted {
		t.Fatalf("second share was not accepted before revoke: %+v err=%v", secondAccepted, err)
	}
	if err := model.SetVFXFilesDriveLinkDisclosureConsent(db, productionID, taskTypeID, revision, "synthetic-admin@example.test", false, now.Add(time.Second)); err != nil {
		t.Fatalf("revoke route consent: %v", err)
	}
	processed, deferred, err = ProcessVFXFilesDeliveryEvents(db, 10)
	if err != nil || processed != 1 || deferred != 0 || len(posts) != 1 {
		t.Fatalf("revoked share dispatch was not fenced: processed=%d deferred=%d posts=%d err=%v", processed, deferred, len(posts), err)
	}
	var quarantined model.VFXFilesEventInbox
	if err := db.Where("event_id = ?", secondAccepted.Results[0].EventID).First(&quarantined).Error; err != nil || quarantined.Outcome != model.VFXFilesEventQuarantined || quarantined.DiagnosticCode != "drive_link_disclosure_consent_revoked" {
		t.Fatalf("revoked share was not durably quarantined: %+v err=%v", quarantined, err)
	}
}

func TestVFXFilesEventUsesProductionTaskTypeRouteAndDurableJournal(t *testing.T) {
	const (
		productionID = "10000000-0000-4000-8000-000000000001"
		taskTypeID   = "50000000-0000-4000-8000-000000000011"
		eventID      = "60000000-0000-4000-8000-000000000001"
		deliveryID   = "90000000-0000-4000-8000-000000000001"
	)
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/webhooks/12345/fake-token") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":1,"id":"12345","guild_id":"45678","channel_id":"78901"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/webhooks/12345/fake-token") && r.Method == http.MethodPost {
			posts++
			if posts == 1 {
				// A long Retry-After is a definitive, retryable rejection. SendMessage
				// leaves the durable notification intent retryable without sleeping,
				// so the next event-processing pass must resume it safely.
				w.Header().Set("Retry-After", "31")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			var payload Payload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode synthetic Discord payload: %v", err)
			}
			if payload.AllowedMentions == nil || len(payload.AllowedMentions.Parse) != 0 || len(payload.AllowedMentions.Users) != 0 || len(payload.AllowedMentions.Roles) != 0 {
				t.Errorf("file delivery payload enabled mentions: %+v", payload.AllowedMentions)
			}
			encoded, _ := json.Marshal(payload)
			if strings.Contains(string(encoded), "fake-drive-file") || strings.Contains(string(encoded), "manifest:") {
				t.Errorf("private Plugin storage reference leaked into Discord payload: %s", encoded)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"112233445566778899","channel_id":"78901"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "files-delivery.db")
	db, err := gorm.Open(sqlite.Open(dbPath+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	defer func() { _ = sqlDB.Close() }()
	if err := db.AutoMigrate(
		&model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationRoute{},
		&model.ProductionNotificationConfig{}, &model.ProjectReviewerTarget{}, &model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{},
		&model.ProductionNotificationWriteLock{}, &model.VFXFilesProductionConfig{}, &model.VFXFilesEventInbox{},
		&model.VFXFilesEventNonce{}, &model.VFXFilesEventConflict{},
	); err != nil {
		t.Fatal(err)
	}
	project := model.Project{KitsuProjectID: productionID, Name: "Synthetic Production", DiscordGuildID: "45678", Language: "en"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationConfig{ProductionID: productionID, ProductionName: project.Name, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	webhookURL := server.URL + "/api/webhooks/12345/fake-token"
	webhook := model.ProjectWebhook{KitsuProjectID: productionID, ChannelName: "fx", TaskType: "FX", WebhookURL: webhookURL, DiscordChannelID: "78901"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: taskTypeID, TaskTypeName: "FX", DestinationWebhookID: webhook.ID, DestinationChannelID: "78901"}).Error; err != nil {
		t.Fatal(err)
	}
	pluginConfigBody := []byte(fmt.Sprintf(`{"contract_version":"1.0","production_id":%q,"config_revision":4,"connection_id":"20000000-0000-4000-8000-000000000001","drive":{"account_mode":"my_drive","owner_subject":"synthetic-drive-owner","root_id":"synthetic-root-01","root_state":"verified"},"template_selection":{"selections":[{"scope":"production","template_id":"vfx-files.production.default","revision":1,"overrides":{}}]},"sync_policy":{"mode":"new_production","scope":{"episodes":false,"sequences":false,"shots":false,"assets":false,"tasks":false,"task_type_ids":[],"include_ids":[],"exclude_ids":[]},"notify_new_deliveries":false},"activation":{"enabled":true,"activated_at":"2026-10-13T00:00:00Z","notification_epoch":2}}`, productionID))
	if err := model.StoreVFXFilesDesiredConfigMirror(db, productionID, pluginConfigBody); err != nil {
		t.Fatalf("store Plugin-owned Drive authority mirror: %v", err)
	}
	oldResolver := KitsuPublicURLResolver
	KitsuPublicURLResolver = func() string { return "https://kitsu.example.invalid" }
	defer func() { KitsuPublicURLResolver = oldResolver }()

	payload := []byte(fmt.Sprintf(`{"contract_version":"1.0","producer_id":"vfx-files-plugin","event_id":%q,"event_type":"file_delivery.ready","occurred_at":"2026-10-13T00:00:00Z","production_id":%q,"activation_epoch":2,"production_name":"Synthetic Production","entity":{"type":"shot","id":"30000000-0000-4000-8000-000000000001","name":"sh010"},"source":{"id":"50000000-0000-4000-8000-000000000001","task_type_id":"50000000-0000-4000-8000-000000000010","task_type_name":"Compositing"},"sender":{"id":"40000000-0000-4000-8000-000000000001","display_name":"Synthetic Artist"},"output_package":{"id":"70000000-0000-4000-8000-000000000001","version":3,"classification":"Low-res EXR","manifest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","objects":[{"drive_file_id":"fake-drive-file-01","classification":"EXR","size_bytes":10,"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},"delivery":{"id":%q,"revision":1,"status":"ready","recipient_task_id":"50000000-0000-4000-8000-000000000002","recipient_task_type_id":%q,"recipient_task_type_name":"FX"}}`, eventID, productionID, deliveryID, taskTypeID))
	const signingKey = "synthetic-plugin-event-key-material-0001"
	requestBody, err := json.Marshal(map[string]any{"contract_version": "1.0", "events": []json.RawMessage{payload}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	timestamp := now.Format(time.RFC3339)
	nonce := "synthetic-kitsu-sync-event-nonce-0001"
	idempotencyKey := "synthetic-kitsu-sync-event-idempotency-0001"
	bodyHash := sha256.Sum256(requestBody)
	canonical := timestamp + "\n" + nonce + "\n" + hex.EncodeToString(bodyHash[:]) + "\n" + idempotencyKey
	mac := hmac.New(sha256.New, []byte(signingKey))
	_, _ = mac.Write([]byte(canonical))
	request := httptest.NewRequest(http.MethodPost, "/api/integrations/vfx-files/v1/events", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-VFX-Key-Id", "synthetic-plugin-key")
	request.Header.Set("X-VFX-Timestamp", timestamp)
	request.Header.Set("X-VFX-Nonce", nonce)
	request.Header.Set("X-VFX-Content-SHA256", hex.EncodeToString(bodyHash[:]))
	request.Header.Set("X-VFX-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response := httptest.NewRecorder()
	vfxfiles.EventHandler(db, map[string][]byte{"synthetic-plugin-key": []byte(signingKey)}, func() time.Time { return now })(response, request)
	var acceptance struct {
		Results []model.VFXFilesEventResult `json:"results"`
	}
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &acceptance) != nil || len(acceptance.Results) != 1 || acceptance.Results[0].Outcome != model.VFXFilesEventAccepted {
		t.Fatalf("signed synthetic Plugin event acceptance: status=%d body=%s", response.Code, response.Body.String())
	}
	processed, deferred, err := ProcessVFXFilesDeliveryEvents(db, 10)
	if err != nil || processed != 0 || deferred != 1 || posts != 1 {
		t.Fatalf("delivery pass processed=%d deferred=%d posts=%d err=%v", processed, deferred, posts, err)
	}
	var journal model.NotificationDeliveryEvent
	if err := db.Where("source_event_id = ? AND delivery_source = ?", eventID, "vfx_files").First(&journal).Error; err != nil || journal.Disposition != model.DeliveryRetryable || journal.FailureClass != "rate_limited" || journal.DiscordMessageID != "" {
		t.Fatalf("retryable notification journal=%+v err=%v", journal, err)
	}
	if journal.RetryAfterAt == nil || !journal.RetryAfterAt.After(time.Now().UTC().Add(25*time.Second)) {
		t.Fatalf("Discord Retry-After was not durably recorded: retry_after_at=%v", journal.RetryAfterAt)
	}
	var inbox model.VFXFilesEventInbox
	if err := db.Where("event_id = ?", eventID).First(&inbox).Error; err != nil || inbox.ProcessedAt != nil {
		t.Fatalf("rate-limited inbox event must remain pending: %+v err=%v", inbox, err)
	}
	processed, deferred, err = ProcessVFXFilesDeliveryEvents(db, 10)
	if err != nil || processed != 0 || deferred != 0 || posts != 1 {
		t.Fatalf("immediate retry ignored Discord Retry-After: processed=%d deferred=%d posts=%d err=%v", processed, deferred, posts, err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close SQLite before retry recovery: %v", err)
	}
	db, err = gorm.Open(sqlite.Open(dbPath+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatalf("reopen durable SQLite state before retry: %v", err)
	}
	sqlDB, err = db.DB()
	if err != nil {
		t.Fatalf("reopen SQLite pool before retry: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Model(&model.NotificationDeliveryEvent{}).Where("source_event_id = ? AND delivery_source = ?", eventID, "vfx_files").Update("retry_after_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatalf("make retry due after simulating elapsed Retry-After: %v", err)
	}
	processed, deferred, err = ProcessVFXFilesDeliveryEvents(db, 10)
	if err != nil || processed != 1 || deferred != 0 || posts != 2 {
		t.Fatalf("retry pass processed=%d deferred=%d posts=%d err=%v", processed, deferred, posts, err)
	}
	if err := db.Where("source_event_id = ? AND delivery_source = ?", eventID, "vfx_files").First(&journal).Error; err != nil || journal.Disposition != model.DeliveryDelivered || journal.DiscordMessageID != "112233445566778899" {
		t.Fatalf("durable notification journal=%+v err=%v", journal, err)
	}
	if err := db.Where("event_id = ?", eventID).First(&inbox).Error; err != nil || inbox.ProcessedAt == nil {
		t.Fatalf("successful retry must mark inbox event processed: %+v err=%v", inbox, err)
	}
	processed, deferred, err = ProcessVFXFilesDeliveryEvents(db, 10)
	if err != nil || processed != 0 || deferred != 0 || posts != 2 {
		t.Fatalf("second pass duplicated delivery processed=%d deferred=%d posts=%d err=%v", processed, deferred, posts, err)
	}
}

func TestVFXFilesDeferredFirstPageDoesNotStarveLaterEvents(t *testing.T) {
	const (
		productionID = "10000000-0000-4000-8000-000000000001"
		taskTypeID   = "50000000-0000-4000-8000-000000000011"
	)
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/webhooks/12345/fake-token") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":1,"id":"12345","guild_id":"45678","channel_id":"78901"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/webhooks/12345/fake-token") && r.Method == http.MethodPost {
			posts++
			if posts <= 25 {
				w.Header().Set("Retry-After", "31")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"112233445566778899","channel_id":"78901"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "files-backlog.db")+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationRoute{}, &model.ProductionNotificationConfig{}, &model.ProjectReviewerTarget{}, &model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{}, &model.ProductionNotificationWriteLock{}, &model.VFXFilesProductionConfig{}, &model.VFXFilesEventInbox{}, &model.VFXFilesEventNonce{}, &model.VFXFilesEventConflict{}); err != nil {
		t.Fatal(err)
	}
	project := model.Project{KitsuProjectID: productionID, Name: "Synthetic Backlog Production", DiscordGuildID: "45678", Language: "en"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationConfig{ProductionID: productionID, ProductionName: project.Name, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	webhookURL := server.URL + "/api/webhooks/12345/fake-token"
	webhook := model.ProjectWebhook{KitsuProjectID: productionID, ChannelName: "fx", TaskType: "FX", WebhookURL: webhookURL, DiscordChannelID: "78901"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: taskTypeID, TaskTypeName: "FX", DestinationWebhookID: webhook.ID, DestinationChannelID: "78901"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VFXFilesProductionConfig{ProductionID: productionID, ConfigRevision: 4, ActivationEpoch: 2, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	inputs := make([]model.VFXFilesEventInput, 0, 26)
	for i := 1; i <= 26; i++ {
		eventID := fmt.Sprintf("60000000-0000-4000-8000-%012d", i)
		deliveryID := fmt.Sprintf("90000000-0000-4000-8000-%012d", i)
		payload := []byte(fmt.Sprintf(`{"contract_version":"1.0","producer_id":"vfx-files-plugin","event_id":%q,"event_type":"file_delivery.ready","occurred_at":"2026-10-13T00:00:00Z","production_id":%q,"activation_epoch":2,"production_name":"Synthetic Backlog Production","entity":{"type":"shot","id":"30000000-0000-4000-8000-000000000001","name":"sh010"},"source":{"id":"50000000-0000-4000-8000-000000000001","task_type_id":"50000000-0000-4000-8000-000000000010","task_type_name":"Compositing"},"sender":{"id":"40000000-0000-4000-8000-000000000001","display_name":"Synthetic Artist"},"output_package":{"id":"70000000-0000-4000-8000-000000000001","version":3,"classification":"Low-res EXR","manifest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","objects":[{"drive_file_id":"fake-drive-file-01","classification":"EXR","size_bytes":10,"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},"delivery":{"id":%q,"revision":1,"status":"ready","recipient_task_id":"50000000-0000-4000-8000-000000000002","recipient_task_type_id":%q,"recipient_task_type_name":"FX"}}`, eventID, productionID, deliveryID, taskTypeID))
		inputs = append(inputs, model.VFXFilesEventInput{ProducerID: "vfx-files-plugin", EventID: eventID, EventType: "file_delivery.ready", ProductionID: productionID, ActivationEpoch: 2, DeliveryID: deliveryID, DeliveryRevision: 1, RecipientTaskID: "50000000-0000-4000-8000-000000000002", RecipientTaskTypeID: taskTypeID, Payload: payload})
	}
	if results, err := model.AcceptVFXFilesEvents(db, "test-key", "backlog-batch-nonce-000001", inputs, time.Now().UTC()); err != nil || len(results) != 26 {
		t.Fatalf("accept synthetic backlog: results=%d err=%v", len(results), err)
	}
	processed, deferred, err := ProcessVFXFilesDeliveryEvents(db, 25)
	if err != nil || processed != 1 || deferred != 25 || posts != 26 {
		t.Fatalf("deferred first page starved later events: processed=%d deferred=%d posts=%d err=%v", processed, deferred, posts, err)
	}
	var first, last model.VFXFilesEventInbox
	if err := db.Where("id = 1").First(&first).Error; err != nil || first.ProcessedAt != nil {
		t.Fatalf("first rate-limited event should remain pending: %+v err=%v", first, err)
	}
	if err := db.Where("id = 26").First(&last).Error; err != nil || last.ProcessedAt == nil {
		t.Fatalf("later event should complete despite deferred earlier rows: %+v err=%v", last, err)
	}
	var firstJournal model.NotificationDeliveryEvent
	if err := db.Where("source_event_id = ? AND delivery_source = ?", first.EventID, "vfx_files").First(&firstJournal).Error; err != nil || firstJournal.Disposition != model.DeliveryRetryable || firstJournal.RetryAfterAt == nil || !firstJournal.RetryAfterAt.After(time.Now().UTC()) {
		t.Fatalf("first event lost its Discord retry schedule: %+v err=%v", firstJournal, err)
	}
	if err := db.Model(&model.VFXFilesEventInbox{}).Where("id <= 25").Update("created_at", time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)).Error; err != nil {
		t.Fatal(err)
	}
	processed, deferred, err = ProcessVFXFilesDeliveryEvents(db, 25)
	if err != nil || processed != 0 || deferred != 0 || posts != 26 {
		t.Fatalf("pre-deadline scan retried a rate-limited event: processed=%d deferred=%d posts=%d err=%v", processed, deferred, posts, err)
	}
	if err := db.Model(&model.NotificationDeliveryEvent{}).Where("delivery_source = ? AND failure_class = ?", "vfx_files", "rate_limited").Update("retry_after_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	processed, deferred, err = ProcessVFXFilesDeliveryEvents(db, 25)
	if err != nil || processed != 25 || deferred != 0 || posts != 51 {
		t.Fatalf("old pending events were lost after Retry-After: processed=%d deferred=%d posts=%d err=%v", processed, deferred, posts, err)
	}
}

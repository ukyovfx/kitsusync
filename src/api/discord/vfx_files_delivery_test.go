package discord

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if err := db.Create(&model.VFXFilesProductionConfig{ProductionID: productionID, ConfigRevision: 4, ActivationEpoch: 2, Activated: true}).Error; err != nil {
		t.Fatal(err)
	}
	oldResolver := KitsuPublicURLResolver
	KitsuPublicURLResolver = func() string { return "https://kitsu.example.invalid" }
	defer func() { KitsuPublicURLResolver = oldResolver }()

	payload := []byte(fmt.Sprintf(`{"contract_version":"1.0","producer_id":"vfx-files-plugin","event_id":%q,"event_type":"file_delivery.ready","occurred_at":"2026-10-13T00:00:00Z","production_id":%q,"activation_epoch":2,"production_name":"Synthetic Production","entity":{"type":"shot","id":"30000000-0000-4000-8000-000000000001","name":"sh010"},"source":{"id":"50000000-0000-4000-8000-000000000001","task_type_id":"50000000-0000-4000-8000-000000000010","task_type_name":"Compositing"},"sender":{"id":"40000000-0000-4000-8000-000000000001","display_name":"Synthetic Artist"},"output_package":{"id":"70000000-0000-4000-8000-000000000001","version":3,"classification":"Low-res EXR","manifest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","objects":[{"drive_file_id":"fake-drive-file-01","classification":"EXR","size_bytes":10,"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},"delivery":{"id":%q,"revision":1,"status":"ready","recipient_task_id":"50000000-0000-4000-8000-000000000002","recipient_task_type_id":%q,"recipient_task_type_name":"FX"}}`, eventID, productionID, deliveryID, taskTypeID))
	results, err := model.AcceptVFXFilesEvents(db, "test-key", "synthetic-nonce-00000001", []model.VFXFilesEventInput{{ProducerID: "vfx-files-plugin", EventID: eventID, EventType: "file_delivery.ready", ProductionID: productionID, ActivationEpoch: 2, DeliveryID: deliveryID, DeliveryRevision: 1, RecipientTaskID: "50000000-0000-4000-8000-000000000002", RecipientTaskTypeID: taskTypeID, Payload: payload}}, time.Now().UTC())
	if err != nil || len(results) != 1 || results[0].Outcome != model.VFXFilesEventAccepted {
		t.Fatalf("synthetic event acceptance=%+v err=%v", results, err)
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

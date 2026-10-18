package discord

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func reconciliationServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationRoute{}, &model.ProductionNotificationWriteLock{}, &model.ManagedChannelDeletion{}, &model.NotificationDeliveryEvent{}, &model.NotificationDeliveryReconciliationAudit{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func testSnowflakeAt(at time.Time) string {
	const discordEpochMillis = int64(1420070400000)
	ms := at.UTC().UnixMilli() - discordEpochMillis
	return strconv.FormatUint(uint64(ms)<<22|1, 10)
}

func TestVerifyUnknownVFXFilesDeliveryReadsBackExactThreadAndNeverPosts(t *testing.T) {
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	messageID := testSnowflakeAt(started.Add(5 * time.Second))
	threadID := "987654321098765432"
	payload := Payload{Username: "KitsuSync", Content: "", Embeds: []Embed{{Title: "Files · shot010", Description: "Shared file reference", Color: 0x64748B, Footer: EmbedFooter{Text: vfxFilesDeliveryMarkerPrefix + "plugin-event-1"}, Fields: []EmbedField{{Name: "Recipient Task", Value: "https://kitsu.example/tasks/shot010"}}}}, AllowedMentions: &AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}}}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	payloadHash := sha256.Sum256(payloadJSON)
	const productionID = "production-a"
	const guildID = "456789012345678901"
	const parentChannelID = "678901234567890123"
	const webhookID = "123456789012345678"
	var gets, posts int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			w.WriteHeader(http.StatusCreated)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/webhooks/123456789012345678/synthetic-token/messages/"+messageID || r.URL.Query().Get("thread_id") != threadID {
			t.Errorf("unexpected provider readback request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gets++
		response := verifiedWebhookMessage{ID: messageID, ChannelID: threadID, WebhookID: webhookID, Content: payload.Content, Embeds: payload.Embeds}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer provider.Close()
	webhookURL := provider.URL + "/api/webhooks/" + webhookID + "/synthetic-token"
	db := reconciliationServiceDB(t)
	if err := db.Create(&model.Project{KitsuProjectID: productionID, DiscordGuildID: guildID}).Error; err != nil {
		t.Fatal(err)
	}
	webhook := model.ProjectWebhook{KitsuProjectID: productionID, WebhookURL: webhookURL, DiscordChannelID: parentChannelID}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: "task-type-a", DestinationWebhookID: webhook.ID, DestinationChannelID: parentChannelID}).Error; err != nil {
		t.Fatal(err)
	}
	fingerprintData, _ := json.Marshal([]string{guildID, parentChannelID, webhookID})
	fingerprint := sha256.Sum256(fingerprintData)
	event := model.NotificationDeliveryEvent{
		EventID: "delivery-event-1", AttemptID: "0123456789abcdef0123456789abcdef", DeliverySource: "vfx_files",
		SourceEventID: "plugin-event-1", ProductionID: productionID, TaskID: "task-a", TaskTypeID: "task-type-a",
		TaskStatus: "FILE_SHARE_CREATED", PayloadJSON: string(payloadJSON), PayloadSHA256: hex.EncodeToString(payloadHash[:]),
		WebhookRowID: webhook.ID, DestinationGuildID: guildID, DestinationChannelID: parentChannelID,
		DestinationWebhookID: webhookID, DestinationFingerprint: hex.EncodeToString(fingerprint[:]),
		Disposition: model.DeliveryUnknown, FailureClass: "process_restart_during_delivery", AttemptCount: 1,
		AttemptStartedAt: &started, ThreadName: "Files · shot010",
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	verifiedAt := started.Add(time.Minute)
	if err := VerifyUnknownVFXFilesDelivery(db, event.EventID, event.AttemptID, messageID, threadID, "manager@example.test", verifiedAt); err != nil {
		t.Fatal(err)
	}
	if gets != 1 || posts != 0 {
		t.Fatalf("provider requests GET=%d POST=%d; reconciliation must only read back", gets, posts)
	}
	var stored model.NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).Take(&stored).Error; err != nil || stored.Disposition != model.DeliveryDelivered || stored.DiscordMessageID != messageID || stored.DiscordThreadID != threadID {
		t.Fatalf("verified state=%+v err=%v", stored, err)
	}
	var audit model.NotificationDeliveryReconciliationAudit
	if err := db.Where("event_id = ?", event.EventID).Take(&audit).Error; err != nil || audit.Action != model.NotificationReconciliationProviderVerified || audit.ActorEmail != "manager@example.test" || audit.PayloadSHA256 != event.PayloadSHA256 || len(audit.ProviderResponseSHA256) != 64 {
		t.Fatalf("proof audit=%+v err=%v", audit, err)
	}
	if strings.Contains(audit.ProviderResponseSHA256, "synthetic-token") {
		t.Fatal("audit fingerprint unexpectedly contains webhook credentials")
	}
	if err := VerifyUnknownVFXFilesDelivery(db, event.EventID, event.AttemptID, messageID, threadID, "manager@example.test", verifiedAt); err != nil || gets != 1 || posts != 0 {
		t.Fatalf("same confirmation should be idempotent without a second provider read: GET=%d POST=%d err=%v", gets, posts, err)
	}
}

func TestVerifyUnknownVFXFilesDeliveryKeepsUnknownOnMissingOrMismatchedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		mutate func(*verifiedWebhookMessage)
	}{
		{name: "not found", status: http.StatusNotFound},
		{name: "provider unavailable", status: http.StatusServiceUnavailable},
		{name: "wrong payload", status: http.StatusOK, mutate: func(m *verifiedWebhookMessage) { m.Embeds[0].Title = "different message" }},
		{name: "wrong channel", status: http.StatusOK, mutate: func(m *verifiedWebhookMessage) { m.ChannelID = "987654321098765431" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
			messageID := testSnowflakeAt(started.Add(5 * time.Second))
			const webhookID = "123456789012345678"
			const threadID = "987654321098765432"
			payload := Payload{Content: "", Embeds: []Embed{{Title: "Expected", Footer: EmbedFooter{Text: vfxFilesDeliveryMarkerPrefix + "source-1"}}}}
			payloadJSON, _ := json.Marshal(payload)
			payloadHash := sha256.Sum256(payloadJSON)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if tc.status != http.StatusOK {
					return
				}
				message := verifiedWebhookMessage{ID: messageID, ChannelID: threadID, WebhookID: webhookID, Embeds: payload.Embeds}
				tc.mutate(&message)
				_ = json.NewEncoder(w).Encode(message)
			}))
			defer provider.Close()
			db := reconciliationServiceDB(t)
			if err := db.Create(&model.Project{KitsuProjectID: "production-a", DiscordGuildID: "456789012345678901"}).Error; err != nil {
				t.Fatal(err)
			}
			webhook := model.ProjectWebhook{KitsuProjectID: "production-a", WebhookURL: provider.URL + "/api/webhooks/" + webhookID + "/token", DiscordChannelID: "678901234567890123"}
			if err := db.Create(&webhook).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ProductionNotificationRoute{ProductionID: "production-a", TaskTypeID: "task-type-a", DestinationWebhookID: webhook.ID, DestinationChannelID: webhook.DiscordChannelID}).Error; err != nil {
				t.Fatal(err)
			}
			fingerprintData, _ := json.Marshal([]string{"456789012345678901", webhook.DiscordChannelID, webhookID})
			fingerprint := sha256.Sum256(fingerprintData)
			event := model.NotificationDeliveryEvent{EventID: "event-1", AttemptID: "0123456789abcdef0123456789abcdef", DeliverySource: "vfx_files", SourceEventID: "source-1", ProductionID: "production-a", TaskID: "task-a", TaskTypeID: "task-type-a", TaskStatus: "FILE_SHARE_CREATED", PayloadJSON: string(payloadJSON), PayloadSHA256: hex.EncodeToString(payloadHash[:]), WebhookRowID: webhook.ID, DestinationGuildID: "456789012345678901", DestinationChannelID: webhook.DiscordChannelID, DestinationWebhookID: webhookID, DestinationFingerprint: hex.EncodeToString(fingerprint[:]), Disposition: model.DeliveryUnknown, FailureClass: "process_restart_during_delivery", AttemptCount: 1, AttemptStartedAt: &started, ThreadName: "Files"}
			if err := db.Create(&event).Error; err != nil {
				t.Fatal(err)
			}
			err := VerifyUnknownVFXFilesDelivery(db, event.EventID, event.AttemptID, messageID, threadID, "manager@example.test", started.Add(time.Minute))
			if err == nil {
				t.Fatal("missing or mismatched provider evidence resolved the unknown row")
			}
			var stored model.NotificationDeliveryEvent
			if err := db.Where("event_id = ?", event.EventID).Take(&stored).Error; err != nil || stored.Disposition != model.DeliveryUnknown {
				t.Fatalf("unverified delivery no longer unknown: %+v err=%v", stored, err)
			}
			var count int64
			if err := db.Model(&model.NotificationDeliveryReconciliationAudit{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("failed proof created audit: %d %v", count, err)
			}
		})
	}
}

func TestVerifyUnknownVFXFilesDeliveryRejectsLegacyUncorrelatedPayloadWithoutProviderCall(t *testing.T) {
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	messageID := testSnowflakeAt(started.Add(5 * time.Second))
	var requests int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer provider.Close()
	const webhookID = "123456789012345678"
	payload := Payload{Content: "", Embeds: []Embed{{Title: "Old delivery without marker"}}}
	payloadJSON, _ := json.Marshal(payload)
	payloadHash := sha256.Sum256(payloadJSON)
	db := reconciliationServiceDB(t)
	if err := db.Create(&model.Project{KitsuProjectID: "production-a", DiscordGuildID: "456789012345678901"}).Error; err != nil {
		t.Fatal(err)
	}
	webhook := model.ProjectWebhook{KitsuProjectID: "production-a", WebhookURL: provider.URL + "/api/webhooks/" + webhookID + "/token", DiscordChannelID: "678901234567890123"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationRoute{ProductionID: "production-a", TaskTypeID: "task-type-a", DestinationWebhookID: webhook.ID, DestinationChannelID: webhook.DiscordChannelID}).Error; err != nil {
		t.Fatal(err)
	}
	fingerprintData, _ := json.Marshal([]string{"456789012345678901", webhook.DiscordChannelID, webhookID})
	fingerprint := sha256.Sum256(fingerprintData)
	event := model.NotificationDeliveryEvent{EventID: "legacy-event", AttemptID: "0123456789abcdef0123456789abcdef", DeliverySource: "vfx_files", SourceEventID: "source-legacy", ProductionID: "production-a", TaskID: "task-a", TaskTypeID: "task-type-a", PayloadJSON: string(payloadJSON), PayloadSHA256: hex.EncodeToString(payloadHash[:]), WebhookRowID: webhook.ID, DestinationGuildID: "456789012345678901", DestinationChannelID: webhook.DiscordChannelID, DestinationWebhookID: webhookID, DestinationFingerprint: hex.EncodeToString(fingerprint[:]), Disposition: model.DeliveryUnknown, FailureClass: "process_restart_during_delivery", AttemptCount: 1, AttemptStartedAt: &started}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	if err := VerifyUnknownVFXFilesDelivery(db, event.EventID, event.AttemptID, messageID, "", "manager@example.test", started.Add(time.Minute)); err == nil {
		t.Fatal("legacy event without unique event marker unexpectedly verified")
	}
	var stored model.NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).Take(&stored).Error; err != nil || stored.Disposition != model.DeliveryUnknown {
		t.Fatalf("legacy delivery did not remain unknown: %+v err=%v", stored, err)
	}
	if requests != 0 {
		t.Fatalf("uncorrelated legacy delivery contacted provider %d times", requests)
	}
}

func TestVerifyUnknownVFXFilesDeliveryKeepsUnknownWhenDestinationChangesDuringReadback(t *testing.T) {
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	messageID := testSnowflakeAt(started.Add(5 * time.Second))
	const webhookID = "123456789012345678"
	const threadID = "987654321098765432"
	payload := Payload{Content: "", Embeds: []Embed{{Title: "Expected", Footer: EmbedFooter{Text: vfxFilesDeliveryMarkerPrefix + "source-1"}}}}
	payloadJSON, _ := json.Marshal(payload)
	payloadHash := sha256.Sum256(payloadJSON)
	db := reconciliationServiceDB(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected provider method %s", r.Method)
		}
		if err := db.Model(&model.ProjectWebhook{}).Where("id = ?", 1).Update("webhook_url", "https://discord.com/api/webhooks/123456789012345678/replaced-token").Error; err != nil {
			t.Errorf("could not simulate concurrent destination change: %v", err)
		}
		_ = json.NewEncoder(w).Encode(verifiedWebhookMessage{ID: messageID, ChannelID: threadID, WebhookID: webhookID, Embeds: payload.Embeds})
	}))
	defer provider.Close()
	if err := db.Create(&model.Project{KitsuProjectID: "production-a", DiscordGuildID: "456789012345678901"}).Error; err != nil {
		t.Fatal(err)
	}
	webhook := model.ProjectWebhook{ID: 1, KitsuProjectID: "production-a", WebhookURL: provider.URL + "/api/webhooks/" + webhookID + "/token", DiscordChannelID: "678901234567890123"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationRoute{ProductionID: "production-a", TaskTypeID: "task-type-a", DestinationWebhookID: webhook.ID, DestinationChannelID: webhook.DiscordChannelID}).Error; err != nil {
		t.Fatal(err)
	}
	fingerprintData, _ := json.Marshal([]string{"456789012345678901", webhook.DiscordChannelID, webhookID})
	fingerprint := sha256.Sum256(fingerprintData)
	event := model.NotificationDeliveryEvent{EventID: "event-1", AttemptID: "0123456789abcdef0123456789abcdef", DeliverySource: "vfx_files", SourceEventID: "source-1", ProductionID: "production-a", TaskID: "task-a", TaskTypeID: "task-type-a", PayloadJSON: string(payloadJSON), PayloadSHA256: hex.EncodeToString(payloadHash[:]), WebhookRowID: webhook.ID, DestinationGuildID: "456789012345678901", DestinationChannelID: webhook.DiscordChannelID, DestinationWebhookID: webhookID, DestinationFingerprint: hex.EncodeToString(fingerprint[:]), Disposition: model.DeliveryUnknown, FailureClass: "process_restart_during_delivery", AttemptCount: 1, AttemptStartedAt: &started, ThreadName: "Files"}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	if err := VerifyUnknownVFXFilesDelivery(db, event.EventID, event.AttemptID, messageID, threadID, "manager@example.test", started.Add(time.Minute)); err == nil {
		t.Fatal("destination change during readback unexpectedly resolved the event")
	}
	var stored model.NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).Take(&stored).Error; err != nil || stored.Disposition != model.DeliveryUnknown {
		t.Fatalf("concurrent destination change did not preserve unknown: %+v err=%v", stored, err)
	}
	var auditCount int64
	if err := db.Model(&model.NotificationDeliveryReconciliationAudit{}).Count(&auditCount).Error; err != nil || auditCount != 0 {
		t.Fatalf("concurrent destination change wrote audit: count=%d err=%v", auditCount, err)
	}
}

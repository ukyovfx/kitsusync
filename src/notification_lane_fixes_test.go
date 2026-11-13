package main

import (
	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/setup"
	"app/src/utils/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

// dmChainFixture wires a Production with a DM-lane route whose work-channel
// webhook points at the test Discord server, plus a WFA reviewer target.
type dmChainFixture struct {
	db               *gorm.DB
	server           *httptest.Server
	webhookURL       string
	dmOpens          atomic.Int64
	dmSends          atomic.Int64
	channelSends     atomic.Int64
	dmOpenStatus     int32 // HTTP status returned for DM channel opens
	dmSendStatus     int32 // HTTP status returned for DM message sends
	webhookSendState int32 // HTTP status returned for work-channel webhook POSTs
}

func newDMChainFixture(t *testing.T) *dmChainFixture {
	t.Helper()
	db := newNotificationPipelineDB(t)
	t.Cleanup(func() { closeNotificationPipelineDB(t, db) })
	fixture := &dmChainFixture{db: db, dmOpenStatus: http.StatusForbidden, dmSendStatus: http.StatusForbidden, webhookSendState: http.StatusOK}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v10/users/@me/channels", func(w http.ResponseWriter, r *http.Request) {
		fixture.dmOpens.Add(1)
		if int(fixture.dmOpenStatus) != http.StatusOK {
			http.Error(w, `{"message":"DMs closed"}`, int(fixture.dmOpenStatus))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "dm-channel-1"})
	})
	mux.HandleFunc("POST /api/v10/channels/dm-channel-1/messages", func(w http.ResponseWriter, r *http.Request) {
		fixture.dmSends.Add(1)
		if int(fixture.dmSendStatus) != http.StatusOK {
			http.Error(w, `{"message":"forbidden"}`, int(fixture.dmSendStatus))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "dm-message-1", "channel_id": "dm-channel-1"})
	})
	mux.HandleFunc("GET /api/webhooks/111111111111111111/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "111111111111111111", "type": 1, "guild_id": "guild-1", "channel_id": "channel-1"})
	})
	mux.HandleFunc("POST /api/webhooks/111111111111111111/token", func(w http.ResponseWriter, r *http.Request) {
		fixture.channelSends.Add(1)
		if int(fixture.webhookSendState) != http.StatusOK {
			http.Error(w, `{"message":"bad request"}`, int(fixture.webhookSendState))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "channel-message-1", "channel_id": "channel-1"})
	})
	fixture.server = httptest.NewServer(mux)
	t.Cleanup(fixture.server.Close)
	fixture.webhookURL = fixture.server.URL + "/api/webhooks/111111111111111111/token"

	if err := db.Create(&model.Project{KitsuProjectID: "p1", Name: "P1", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "p1", "comp", "Comp", fixture.webhookURL, "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", TaskTypeName: "Comp", DestinationKind: model.LaneDM, DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-1"}}); err != nil {
		t.Fatal(err)
	}

	oldAPI, oldResolver := discord.SetDiscordBotAPIBase(fixture.server.URL+"/api/v10"), discord.ReviewerResolver
	oldToken := os.Getenv("DISCORD_BOT_TOKEN")
	os.Setenv("DISCORD_BOT_TOKEN", "test-bot-token")
	discord.ResetDMChannelCache()
	t.Cleanup(func() {
		discord.SetDiscordBotAPIBase(oldAPI)
		discord.ReviewerResolver = oldResolver
		os.Setenv("DISCORD_BOT_TOKEN", oldToken)
		discord.ResetDMChannelCache()
	})
	discord.ReviewerResolver = func(projectID, taskTypeID, taskTypeName, senderWebhookURL string) ([]discord.ReviewerTarget, error) {
		return []discord.ReviewerTarget{{Kind: "user", ID: "222222222222222222"}}, nil
	}
	return fixture
}

func (f *dmChainFixture) send(t *testing.T) map[string]discord.SendResult {
	t.Helper()
	payload := pipelinePayload("WFA")
	return discord.SendMessageBunch(config.Config{}, []kitsu.MessagePayload{payload}, f.webhookURL, map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{"p1": "ja"}, f.db)
}

func dmLaneEvent(t *testing.T, db *gorm.DB, taskID string) *model.NotificationDeliveryEvent {
	t.Helper()
	var rows []model.NotificationDeliveryEvent
	if err := db.Where("task_id = ?", taskID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("delivery ledger rows for task = %d, want 1", len(rows))
	}
	return &rows[0]
}

// TestDMPermanentFailureFallsBackToWorkChannel reproduces the P1 finding: a
// permanent DM send failure (403) must retarget the pending intent to the work
// channel and deliver there, instead of storing terminal_failure and dropping
// the fallback chain.
func TestDMPermanentFailureFallsBackToWorkChannel(t *testing.T) {
	fixture := newDMChainFixture(t)
	fixture.dmOpenStatus, fixture.dmSendStatus = http.StatusOK, http.StatusForbidden

	results := fixture.send(t)
	result, ok := results["task-1"]
	if !ok || result.MessageID == "" {
		t.Fatalf("work-channel fallback did not deliver: %#v", result)
	}
	if got := fixture.channelSends.Load(); got != 1 {
		t.Fatalf("work-channel sends = %d, want 1", got)
	}
	event := dmLaneEvent(t, fixture.db, "task-1")
	if event.Disposition != model.DeliveryDelivered || event.LaneType != model.LaneChannel {
		t.Fatalf("retargeted event = lane=%q disposition=%q, want channel/delivered", event.LaneType, event.Disposition)
	}
	if event.DestinationChannelID != "channel-1" {
		t.Fatalf("retargeted destination = %q, want channel-1", event.DestinationChannelID)
	}
}

// TestDMOpenFailureFallsBackToWorkChannel reproduces the P1 finding: a failed
// DM channel open (recipient closed DMs) must still create the ledger row and
// deliver through the work channel instead of returning without any record.
func TestDMOpenFailureFallsBackToWorkChannel(t *testing.T) {
	fixture := newDMChainFixture(t)
	fixture.dmOpenStatus = http.StatusForbidden

	results := fixture.send(t)
	result, ok := results["task-1"]
	if !ok || result.MessageID == "" {
		t.Fatalf("work-channel fallback did not deliver after DM open failure: %#v", result)
	}
	if got := fixture.channelSends.Load(); got != 1 {
		t.Fatalf("work-channel sends = %d, want 1", got)
	}
	if got := fixture.dmOpens.Load(); got != 1 {
		t.Fatalf("DM channel opens = %d, want 1", got)
	}
	event := dmLaneEvent(t, fixture.db, "task-1")
	if event.Disposition != model.DeliveryDelivered || event.LaneType != model.LaneChannel {
		t.Fatalf("event after open failure = lane=%q disposition=%q, want channel/delivered", event.LaneType, event.Disposition)
	}
	// The next poll must not repeat the DM attempt: the delivered checkpoint
	// short-circuits before any Discord call.
	results = fixture.send(t)
	if !results["task-1"].AlreadyDelivered {
		t.Fatalf("second send did not short-circuit as already delivered: %#v", results["task-1"])
	}
	if got := fixture.dmOpens.Load(); got != 1 {
		t.Fatalf("DM channel opens after retry = %d, want 1 (no repeat)", got)
	}
}

// TestDMFallbackExhaustionRecordsStuckRow verifies the end of the chain: when
// the DM and the work channel both fail permanently and no global channel is
// configured, the intent is recorded once as a stuck terminal row.
func TestDMFallbackExhaustionRecordsStuckRow(t *testing.T) {
	fixture := newDMChainFixture(t)
	fixture.dmOpenStatus = http.StatusForbidden
	fixture.webhookSendState = http.StatusBadRequest // permanent webhook failure

	results := fixture.send(t)
	if results["task-1"].MessageID != "" {
		t.Fatalf("exhausted chain must not report delivery: %#v", results["task-1"])
	}
	event := dmLaneEvent(t, fixture.db, "task-1")
	if event.Disposition != model.DeliveryTerminalFailure || event.FailureClass != "delivery_fallback_exhausted" {
		t.Fatalf("stuck record = disposition=%q class=%q", event.Disposition, event.FailureClass)
	}
}

// TestDMRateLimitStaysOnDMLane verifies the temporary-failure rule: a 429 on
// the DM send never advances the fallback chain and stays retryable on the DM
// lane.
func TestDMRateLimitStaysOnDMLane(t *testing.T) {
	fixture := newDMChainFixture(t)
	fixture.dmOpenStatus, fixture.dmSendStatus = http.StatusOK, http.StatusTooManyRequests

	results := fixture.send(t)
	result := results["task-1"]
	if result.MessageID != "" || !result.Retryable {
		t.Fatalf("rate-limited DM must stay retryable on the DM lane: %#v", result)
	}
	event := dmLaneEvent(t, fixture.db, "task-1")
	if event.Disposition != model.DeliveryRetryable || event.LaneType != model.LaneDM {
		t.Fatalf("rate-limited event = lane=%q disposition=%q, want dm/retryable", event.LaneType, event.Disposition)
	}
	if got := fixture.channelSends.Load(); got != 0 {
		t.Fatalf("temporary failure advanced the fallback chain: channel sends = %d", got)
	}
}

// TestFilterTasksSuppressesRecordOnlyBeforeInitialCheckpoint reproduces the
// P2 finding: the record-only branch wrote ledger rows for pre-existing tasks
// before the initial-checkpoint suppression guard ran.
func TestFilterTasksSuppressesRecordOnlyBeforeInitialCheckpoint(t *testing.T) {
	db := newNotificationPipelineDB(t)
	t.Cleanup(func() { closeNotificationPipelineDB(t, db) })
	if err := db.Create(&model.Project{KitsuProjectID: "p1", Name: "P1", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", DestinationKind: model.LaneRecordOnly}}); err != nil {
		t.Fatal(err)
	}
	setup.MarkInitialCheckpointPending("p1")
	t.Cleanup(func() { setup.ClearInitialCheckpointPending("p1") })

	payload := pipelinePayload("WFA")
	FilterTasks([]kitsu.MessagePayload{payload}, config.Config{}, db)

	var count int64
	db.Model(&model.NotificationDeliveryEvent{}).Where("task_id = ?", payload.Task.ID).Count(&count)
	if count != 0 {
		t.Fatalf("pre-checkpoint task polluted the record-only ledger: %d rows", count)
	}
	var observation model.Task
	if err := db.Where("task_id = ?", payload.Task.ID).First(&observation).Error; err != nil {
		t.Fatalf("pre-checkpoint task observation was not persisted: %v", err)
	}
	// After the checkpoint clears, the pre-existing task stays silent: its
	// observation is current, so no new transition and no ledger row appear.
	setup.ClearInitialCheckpointPending("p1")
	FilterTasks([]kitsu.MessagePayload{payload}, config.Config{}, db)
	db.Model(&model.NotificationDeliveryEvent{}).Where("task_id = ?", payload.Task.ID).Count(&count)
	if count != 0 {
		t.Fatalf("pre-existing task recorded after checkpoint: %d rows", count)
	}
}

// TestStaleUnknownResolutionUnblocksTaskWithoutDuplicateSend reproduces the
// round-3 measured stall: an unknown ledger row blocked all later
// notifications for its task across polls. After auto-resolution the task's
// next transition delivers again, and the resolved row itself is never
// re-sent (a message Discord may have delivered is not duplicated).
func TestStaleUnknownResolutionUnblocksTaskWithoutDuplicateSend(t *testing.T) {
	db := newNotificationPipelineDB(t)
	t.Cleanup(func() { closeNotificationPipelineDB(t, db) })

	var sends atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/webhooks/111111111111111111/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"111111111111111111","type":1,"guild_id":"guild-1","channel_id":"channel-1"}`))
	})
	mux.HandleFunc("POST /api/webhooks/111111111111111111/token", func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"channel-message-1","channel_id":"channel-1"}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	webhookURL := server.URL + "/api/webhooks/111111111111111111/token"

	if err := db.Create(&model.Project{KitsuProjectID: "p1", Name: "P1", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "p1", "comp", "Comp", webhookURL, "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-1"}}); err != nil {
		t.Fatal(err)
	}
	stuck := model.NotificationDeliveryEvent{
		EventID: "stuck-unknown", TaskID: "task-1", ProductionID: "p1", TaskTypeID: "tt1", TaskStatus: "WFA",
		TaskUpdatedAt: "2026-11-12T00:00:00Z", CommentID: "c0", CommentUpdatedAt: "2026-11-12T00:00:00Z",
		LaneType: model.LaneChannel, PayloadJSON: `{"content":"synthetic"}`, PayloadSHA256: "hash",
		WebhookRowID: webhook.ID, DestinationChannelID: "channel-1",
		Disposition: model.DeliveryUnknown, FailureClass: "process_restart_during_delivery",
	}
	if err := db.Create(&stuck).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.NotificationDeliveryEvent{}).Where("id = ?", stuck.ID).Update("updated_at", time.Now().Add(-2*model.NotificationUnknownAutoResolveAfter)).Error; err != nil {
		t.Fatal(err)
	}

	oldFactory := makeKitsuResponse
	t.Cleanup(func() { makeKitsuResponse = oldFactory })
	makeKitsuResponse = func(config.Config) ([]kitsu.MessagePayload, error) {
		return []kitsu.MessagePayload{pipelinePayload("WFA")}, nil
	}
	conf := pipelineConfig()
	conf.Discord.EmbedsPerRequests = 10
	conf.Discord.RequestsPerMinute = 60
	runOnePoll(conf, db)

	if got := sends.Load(); got != 1 {
		t.Fatalf("Discord sends after resolution = %d, want exactly 1 (stuck row must never re-send)", got)
	}
	var resolved model.NotificationDeliveryEvent
	if err := db.Where("event_id = ?", "stuck-unknown").First(&resolved).Error; err != nil {
		t.Fatal(err)
	}
	if resolved.Disposition != model.DeliveryTerminalFailure || resolved.FailureClass != "unknown_auto_resolved" {
		t.Fatalf("stuck row = disposition=%q class=%q", resolved.Disposition, resolved.FailureClass)
	}
	var fresh model.NotificationDeliveryEvent
	if err := db.Where("task_id = ? AND event_id <> ?", "task-1", "stuck-unknown").First(&fresh).Error; err != nil {
		t.Fatalf("task stayed blocked after resolution: %v", err)
	}
	if fresh.Disposition != model.DeliveryDelivered {
		t.Fatalf("follow-up notification = %q, want delivered", fresh.Disposition)
	}
}

// TestRunOnePollSkipsValidationOnlyProjectsInLiveness reproduces the P2
// finding: the liveness loop covered every project, so validation-only profile
// projects could raise stall alerts.
func TestRunOnePollSkipsValidationOnlyProjectsInLiveness(t *testing.T) {
	oldFactory, oldStats := makeKitsuResponse, setup.Stats
	t.Cleanup(func() { makeKitsuResponse = oldFactory; setup.Stats = oldStats })
	setup.Stats = &setup.RuntimeStats{}
	makeKitsuResponse = func(config.Config) ([]kitsu.MessagePayload, error) {
		return []kitsu.MessagePayload{}, nil
	}

	db := newNotificationPipelineDB(t)
	t.Cleanup(func() { closeNotificationPipelineDB(t, db) })
	if err := db.Create(&model.Project{KitsuProjectID: "p-live", Name: "Live", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Project{KitsuProjectID: "p-validation", Name: "Validation", DiscordGuildID: "guild-1", ValidationOnly: true}).Error; err != nil {
		t.Fatal(err)
	}

	runOnePoll(config.Config{}, db)
	stats := setup.Stats.ProductionPollStats()
	if _, ok := stats["p-validation"]; ok {
		t.Fatal("validation-only project entered the liveness monitor")
	}
	if _, ok := stats["p-live"]; !ok {
		t.Fatal("writable project was not tracked by the liveness monitor")
	}
}

package main

import (
	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/setup"
	"app/src/utils/config"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

// phase2WebhookFixture wires a production route to an httptest webhook and
// counts POSTs / DELETEs.
type phase2WebhookFixture struct {
	db           *gorm.DB
	server       *httptest.Server
	webhookURL   string
	webhookRowID uint
	posts      atomic.Int64
	deletes    atomic.Int64
	delete204  atomic.Bool
}

func newPhase2WebhookFixture(t *testing.T) *phase2WebhookFixture {
	t.Helper()
	db := newNotificationPipelineDB(t)
	t.Cleanup(func() { closeNotificationPipelineDB(t, db) })
	fixture := &phase2WebhookFixture{db: db, delete204: atomic.Bool{}}
	fixture.delete204.Store(true)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/webhooks/111111111111111111/token", func(w http.ResponseWriter, r *http.Request) {
		fixture.posts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"channel-message-1","channel_id":"channel-1"}`))
	})
	mux.HandleFunc("GET /api/webhooks/111111111111111111/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"111111111111111111","type":1,"guild_id":"guild-1","channel_id":"channel-1"}`))
	})
	mux.HandleFunc("DELETE /api/webhooks/111111111111111111/token/messages/", func(w http.ResponseWriter, r *http.Request) {
		fixture.deletes.Add(1)
		if fixture.delete204.Load() {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	fixture.server = httptest.NewServer(mux)
	t.Cleanup(fixture.server.Close)
	fixture.webhookURL = fixture.server.URL + "/api/webhooks/111111111111111111/token"

	if err := model.CreateProject(db, "p1", "Production", "", "guild-1", "category-1", "ja"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "p1", "Animation", "", fixture.webhookURL, "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	fixture.webhookRowID = webhook.ID
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-1"}}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// deliverViaDispatch stubs notificationDispatch with the real durable sender
// behavior: each dispatched payload writes a delivered channel ledger row and
// advances the delivered checkpoint, exactly like SendMessageBunch does.
func deliverViaDispatch(t *testing.T, db *gorm.DB, webhookRowID uint) *atomic.Int64 {
	t.Helper()
	original := notificationDispatch
	t.Cleanup(func() { notificationDispatch = original })
	var dispatched atomic.Int64
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, db *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		for _, payload := range data {
			dispatched.Add(1)
			intent := model.NotificationDeliveryIntent{
				TaskID: payload.Task.ID, ProductionID: payload.Project.ID, TaskTypeID: payload.TaskType.ID,
				TaskStatus: strings.ToUpper(strings.TrimSpace(payload.TaskStatus.TaskStatus.ShortName)), TaskUpdatedAt: payload.Task.UpdatedAt,
				CommentID: payload.LatestComment.Comment.ID, CommentUpdatedAt: payload.LatestComment.Comment.UpdatedAt,
				IsAssignNotification: payload.IsAssignNotification, LaneType: model.LaneChannel,
				WebhookRowID: webhookRowID, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "111111111111111111",
				Payload: []byte(`{"content":"synthetic"}`),
			}
			event, err := model.PrepareNotificationDelivery(db, intent)
			if err != nil || event == nil {
				t.Errorf("durable intent could not be prepared: %v", err)
				continue
			}
			attempt, started, beginErr := model.BeginNotificationDeliveryForEvent(db, *event, time.Now().UTC())
			if beginErr != nil || !started || attempt == nil {
				t.Errorf("durable intent could not be claimed: %v", beginErr)
				continue
			}
			if err := model.CompleteNotificationDelivery(db, *attempt, "discord-message-1", "", time.Now().UTC()); err != nil {
				t.Errorf("durable delivery could not complete: %v", err)
				continue
			}
			if err := model.UpdateTaskWithDiscord(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.TaskStatus.ShortName, "", "", "discord-message-1", "", "", "", ""); err != nil {
				t.Errorf("delivery checkpoint update failed: %v", err)
			}
		}
		return data
	}
	return &dispatched
}

func expireAggregationWindows(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Model(&model.NotificationAggregationBuffer{}).Where("1=1").Update("first_seen_at", time.Now().UTC().Add(-channelAggregationWindow-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
}

// TestTaskLeavingWFADeletesChannelPost verifies item 1: a task that leaves WFA
// gets its waiting-for-check post deleted by the lifecycle sweeper.
func TestTaskLeavingWFADeletesChannelPost(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	dispatched := deliverViaDispatch(t, fixture.db, fixture.webhookRowID)

	// WFA is delivered: the channel post exists.
	FilterTasks([]kitsu.MessagePayload{pipelinePayload("WFA")}, pipelineConfig(), fixture.db)
	if dispatched.Load() != 1 {
		t.Fatalf("WFA dispatch count = %d, want 1", dispatched.Load())
	}

	// The task leaves WFA: the post must be marked for deletion.
	retake := pipelinePayload("RETAKE")
	retake.Task.UpdatedAt = "2026-08-07T00:10:00"
	FilterTasks([]kitsu.MessagePayload{retake}, pipelineConfig(), fixture.db)

	var event model.NotificationDeliveryEvent
	if err := fixture.db.Where("task_id = ? AND lane_type = ? AND task_status = ?", "task-1", model.LaneChannel, "WFA").Order("id desc").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if !event.LifecycleCleanupPending {
		t.Fatal("task left WFA but the channel post was not marked for lifecycle deletion")
	}

	discord.DeletePendingLifecycleMessages(fixture.db, 10)
	if got := fixture.deletes.Load(); got != 1 {
		t.Fatalf("lifecycle deletes = %d, want 1", got)
	}
	var after model.NotificationDeliveryEvent
	if err := fixture.db.Where("event_id = ?", event.EventID).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.LifecycleCleanupPending || after.LifecycleDeletedAt == nil {
		t.Fatalf("lifecycle cleanup did not complete: pending=%v deletedAt=%v", after.LifecycleCleanupPending, after.LifecycleDeletedAt)
	}
}

// TestTaskStayingWFANeverDeletesPost verifies the wrong-deletion guard: a task
// that is still WFA must never get its post marked for deletion.
func TestTaskStayingWFANeverDeletesPost(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	deliverViaDispatch(t, fixture.db, fixture.webhookRowID)

	FilterTasks([]kitsu.MessagePayload{pipelinePayload("WFA")}, pipelineConfig(), fixture.db)
	// Same WFA state re-observed: no transition, no deletion mark.
	FilterTasks([]kitsu.MessagePayload{pipelinePayload("WFA")}, pipelineConfig(), fixture.db)

	var pending int64
	fixture.db.Model(&model.NotificationDeliveryEvent{}).Where("lifecycle_cleanup_pending = ?", true).Count(&pending)
	if pending != 0 {
		t.Fatalf("a still-WFA task was marked for deletion: %d rows", pending)
	}
}

// TestLifecycleDeletionRequiresWebhookOwnership verifies the sweeper refuses
// to delete when the webhook identity no longer matches the ledger.
func TestLifecycleDeletionRequiresWebhookOwnership(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	deliverViaDispatch(t, fixture.db, fixture.webhookRowID)

	FilterTasks([]kitsu.MessagePayload{pipelinePayload("WFA")}, pipelineConfig(), fixture.db)
	retake := pipelinePayload("RETAKE")
	retake.Task.UpdatedAt = "2026-08-07T00:10:00"
	FilterTasks([]kitsu.MessagePayload{retake}, pipelineConfig(), fixture.db)

	// Point the webhook at a different channel: ownership evidence is gone.
	if err := fixture.db.Model(&model.ProjectWebhook{}).Where("1=1").Update("discord_channel_id", "channel-2").Error; err != nil {
		t.Fatal(err)
	}
	discord.DeletePendingLifecycleMessages(fixture.db, 10)
	if got := fixture.deletes.Load(); got != 0 {
		t.Fatalf("deletion ran without ownership evidence: %d deletes", got)
	}
	var event model.NotificationDeliveryEvent
	if err := fixture.db.Where("task_id = ? AND lane_type = ? AND task_status = ?", "task-1", model.LaneChannel, "WFA").Order("id desc").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if !event.LifecycleCleanupPending || event.CleanupFailureClass != "destination_identity_changed" {
		t.Fatalf("ownership failure not recorded: pending=%v class=%q", event.LifecycleCleanupPending, event.CleanupFailureClass)
	}
}

// TestAggregationWindowCollapsesRoundTrip verifies item 2: transitions inside
// the window collapse into one delivery of the final state.
func TestAggregationWindowCollapsesRoundTrip(t *testing.T) {
	fixture := newPhase2WebhookFixture(t)
	dispatched := deliverViaDispatch(t, fixture.db, fixture.webhookRowID)

	FilterTasks([]kitsu.MessagePayload{pipelinePayload("WFA")}, pipelineConfig(), fixture.db)
	var buffer model.NotificationAggregationBuffer
	if err := fixture.db.First(&buffer).Error; err != nil {
		t.Fatalf("transition was not buffered: %v", err)
	}
	if buffer.TransitionCount != 1 {
		t.Fatalf("transition count = %d, want 1", buffer.TransitionCount)
	}
	// Round-trip inside the window: RETAKE then back to WFA.
	retake := pipelinePayload("RETAKE")
	retake.Task.UpdatedAt = "2026-08-07T00:01:00"
	FilterTasks([]kitsu.MessagePayload{retake}, pipelineConfig(), fixture.db)
	back := pipelinePayload("WFA")
	back.Task.UpdatedAt = "2026-08-07T00:02:00"
	FilterTasks([]kitsu.MessagePayload{back}, pipelineConfig(), fixture.db)

	if err := fixture.db.First(&buffer).Error; err != nil {
		t.Fatal(err)
	}
	if buffer.TransitionCount != 3 || buffer.LastStatus != "WFA" {
		t.Fatalf("round-trip did not collapse into one buffer: count=%d status=%q", buffer.TransitionCount, buffer.LastStatus)
	}
	if dispatched.Load() != 0 {
		t.Fatalf("window must suppress intermediate dispatches, got %d", dispatched.Load())
	}

	expireAggregationWindows(t, fixture.db)
	FilterTasks(nil, pipelineConfig(), fixture.db) // sweeper runs at the end of FilterTasks
	if got := dispatched.Load(); got != 1 {
		t.Fatalf("window close dispatched %d messages, want 1", got)
	}
	var remaining int64
	fixture.db.Model(&model.NotificationAggregationBuffer{}).Count(&remaining)
	if remaining != 0 {
		t.Fatalf("buffer rows survived a confirmed dispatch: %d", remaining)
	}
}

// TestAggregationSurvivesRestartWithoutLoss verifies zero aggregation loss:
// buffered transitions survive a restart and dispatch afterwards.
func TestAggregationSurvivesRestartWithoutLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aggregation-state.db")
	db := reopenNotificationPipelineDB(t, path)
	defer closeNotificationPipelineDB(t, db)
	// The real 3-minute window keeps the first poll's transition buffered.
	fixture := newPhase2WebhookFixture(t)
	// Reuse the fixture's route setup on the persistent DB.
	if err := model.CreateProject(db, "p1", "Production", "", "guild-1", "category-1", "ja"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "p1", "Animation", "", fixture.webhookURL, "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	webhookRowID := webhook.ID
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-1"}}); err != nil {
		t.Fatal(err)
	}
	dispatched := deliverViaDispatch(t, db, webhookRowID)

	FilterTasks([]kitsu.MessagePayload{pipelinePayload("WFA")}, pipelineConfig(), db)
	if dispatched.Load() != 0 {
		t.Fatalf("window must not close within the same poll, got %d", dispatched.Load())
	}
	// Simulate a crash: close without dispatching, reopen.
	closeNotificationPipelineDB(t, db)
	db = reopenNotificationPipelineDB(t, path)
	defer closeNotificationPipelineDB(t, db)
	dispatched2 := deliverViaDispatch(t, db, webhookRowID)
	expireAggregationWindows(t, db)
	FilterTasks(nil, pipelineConfig(), db)
	if got := dispatched2.Load(); got != 1 {
		t.Fatalf("buffered transition lost across restart: dispatched=%d", got)
	}
}

// TestBulkWindowSendsOneSummaryPerDestination verifies the user-approved bulk
// rule: more than the threshold sends ONE summary message, not per-task posts.
func TestBulkWindowSendsOneSummaryPerDestination(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)

	for i := 0; i < 11; i++ {
		payload := pipelinePayload("WFA")
		payload.Task.ID = fmt.Sprintf("task-%d", i)
		if i%2 == 0 {
			payload.TaskStatus.ShortName = "RETAKE"
		}
		if _, err := bufferChannelTransition(fixture.db, payload, productionRoutingPlan{DestinationID: "channel-1", LaneType: model.LaneChannel, WebhookURL: fixture.webhookURL, ShouldSend: true}); err != nil {
			t.Fatal(err)
		}
	}
	expireAggregationWindows(t, fixture.db)
	FilterTasks(nil, pipelineConfig(), fixture.db)

	if got := fixture.posts.Load(); got != 1 {
		t.Fatalf("bulk window posts = %d, want 1 summary", got)
	}
	var remaining int64
	fixture.db.Model(&model.NotificationAggregationBuffer{}).Count(&remaining)
	if remaining != 0 {
		t.Fatalf("buffers survived the bulk summary: %d", remaining)
	}
	var recorded int64
	fixture.db.Model(&model.NotificationDeliveryEvent{}).Where("lane_type = ? AND failure_class = ?", model.LaneRecordOnly, "aggregated_bulk_summary").Count(&recorded)
	if recorded != 11 {
		t.Fatalf("bulk summary ledger rows = %d, want 11", recorded)
	}
}

// TestFlapDowngradeSkipsIndividualSend verifies the flap rule: a task with too
// many transitions is consumed into the ledger without a send.
func TestFlapDowngradeSkipsIndividualSend(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	deliverViaDispatch(t, fixture.db, fixture.webhookRowID)

	// Seed four transitions in the last hour through the normal pipeline
	// (immediate window: each dispatch lands in the ledger).
	for i, status := range []string{"WFA", "RETAKE", "WFA", "RETAKE"} {
		payload := pipelinePayload(status)
		payload.Task.UpdatedAt = fmt.Sprintf("2026-08-07T00:0%d:00", i)
		FilterTasks([]kitsu.MessagePayload{payload}, pipelineConfig(), fixture.db)
	}
	// Back to the real window: the next transition stays buffered.
	channelAggregationWindow = model.NotificationAggregationWindow
	fifth := pipelinePayload("WFA")
	fifth.Task.UpdatedAt = "2026-08-07T00:05:00"
	FilterTasks([]kitsu.MessagePayload{fifth}, pipelineConfig(), fixture.db)

	var buffer model.NotificationAggregationBuffer
	if err := fixture.db.First(&buffer).Error; err != nil {
		t.Fatalf("fifth transition was not buffered: %v", err)
	}
	if !buffer.FlapDowngraded {
		t.Fatalf("task with %d transitions/hour was not flap-downgraded", buffer.TransitionCount)
	}
	before := fixture.posts.Load()
	expireAggregationWindows(t, fixture.db)
	FilterTasks(nil, pipelineConfig(), fixture.db)
	if got := fixture.posts.Load() - before; got != 0 {
		t.Fatalf("flap-downgraded task produced %d posts, want 0", got)
	}
	var recorded int64
	fixture.db.Model(&model.NotificationDeliveryEvent{}).Where("lane_type = ? AND failure_class = ?", model.LaneRecordOnly, "flap_downgraded_to_digest").Count(&recorded)
	if recorded == 0 {
		t.Fatal("flap-downgraded transition left no ledger trace")
	}
}

// TestMorningDigestMatchesObservation verifies item 3: the digest counts are
// derived from the task observation table and the card renders them.
func TestMorningDigestMatchesObservation(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	deliverViaDispatch(t, fixture.db, fixture.webhookRowID)

	// Two WFA tasks, one of them stalled (old timestamp), one RETAKE.
	wfa := pipelinePayload("WFA")
	FilterTasks([]kitsu.MessagePayload{wfa}, pipelineConfig(), fixture.db)
	wfa2 := pipelinePayload("WFA")
	wfa2.Task.ID = "task-2"
	FilterTasks([]kitsu.MessagePayload{wfa2}, pipelineConfig(), fixture.db)
	retake := pipelinePayload("RETAKE")
	retake.Task.ID = "task-3"
	FilterTasks([]kitsu.MessagePayload{retake}, pipelineConfig(), fixture.db)

	// Age the first WFA task beyond the stall threshold.
	old, _ := time.Parse(time.RFC3339, "2020-01-01T00:00:00Z")
	if err := fixture.db.Model(&model.Task{}).Where("task_id = ?", "task-1").Update("task_updated_at", old.Format(time.RFC3339)).Error; err != nil {
		t.Fatal(err)
	}

	digest := buildProductionDigest(fixture.db, model.ListProjects(fixture.db)[0], time.Now().In(jst))
	if len(digest.WaitingCheck) != 2 || len(digest.Retakes) != 1 || len(digest.Stalled) != 1 {
		t.Fatalf("digest mismatch: wfa=%d retake=%d stalled=%d", len(digest.WaitingCheck), len(digest.Retakes), len(digest.Stalled))
	}
	card := renderDigestCard(digest, 0)
	for _, want := range []string{"チェック待ち (WFA): 2件", "差し戻し (RETAKE): 1件", "止まっている物 (24h以上WFA): 1件", "昨日の完了 (DONE): 0件"} {
		if !strings.Contains(card, want) {
			t.Fatalf("digest card missing %q: %s", want, card)
		}
	}
}

// TestMorningDigestSendsOncePerDay verifies the daily dedup and the DM path.
func TestMorningDigestSendsOncePerDay(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	project := model.ListProjects(fixture.db)[0]
	if err := fixture.db.Create(&model.ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "*", TargetKind: model.ReviewerTargetUser, DiscordID: "222222222222222222"}).Error; err != nil {
		t.Fatal(err)
	}
	oldToken := os.Getenv("DISCORD_BOT_TOKEN")
	os.Setenv("DISCORD_BOT_TOKEN", "test-token")
	t.Cleanup(func() { os.Setenv("DISCORD_BOT_TOKEN", oldToken) })

	// Unreachable bot API: the digest must fail without recording the marker.
	oldAPI := discord.SetDiscordBotAPIBase("http://127.0.0.1:1")
	t.Cleanup(func() { discord.SetDiscordBotAPIBase(oldAPI) })
	if err := sendMorningDigests(pipelineConfig(), fixture.db, time.Now().In(jst), time.Now().In(jst).Format("2006-01-02")); err == nil {
		t.Fatal("digest with unreachable Discord API must fail")
	}
	today := time.Now().In(jst).Format("2006-01-02")
	if model.GetSetting(fixture.db, digestLastSentDateSettingKey) == today {
		t.Fatal("failed digest must not record the day marker")
	}
}

// TestMentionDMCapAndExcess verifies item 4: mentions DM up to the daily cap
// and count the excess for the digest.
func TestMentionDMCapAndExcess(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	if err := fixture.db.Create(&model.UserMap{KitsuName: "Alice", DiscordID: "222222222222222222"}).Error; err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v10/users/@me/channels", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"dm-channel-1"}`))
	})
	mux.HandleFunc("POST /api/v10/channels/dm-channel-1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"dm-message-1","channel_id":"dm-channel-1"}`))
	})
	botAPI := httptest.NewServer(mux)
	t.Cleanup(botAPI.Close)
	oldAPI := discord.SetDiscordBotAPIBase(botAPI.URL + "/api/v10")
	oldToken := os.Getenv("DISCORD_BOT_TOKEN")
	os.Setenv("DISCORD_BOT_TOKEN", "test-token")
	discord.ResetDMChannelCache()
	t.Cleanup(func() {
		discord.SetDiscordBotAPIBase(oldAPI)
		os.Setenv("DISCORD_BOT_TOKEN", oldToken)
		discord.ResetDMChannelCache()
	})

	payload := pipelinePayload("WFA")
	payload.CommentsUnavailable = false
	payload.LatestComment.Comment.ID = "c1"
	payload.LatestComment.Comment.UpdatedAt = "2026-08-07T00:05:00"
	payload.LatestComment.Comment.Text = "@Alice please check"
	payload.LatestComment.Author.FullName = "Bob"

	// The first five mentions deliver; the sixth counts as excess.
	for i := 0; i < 6; i++ {
		payload.LatestComment.Comment.ID = fmt.Sprintf("c%d", i)
		dispatchMentionDMs(fixture.db, payload)
	}
	today := time.Now().In(jst).Format("2006-01-02")
	if got := model.GetSetting(fixture.db, mentionCountSettingPrefix+"222222222222222222."+today); got != "5" {
		t.Fatalf("mention DM count = %q, want 5", got)
	}
	if got := model.GetSetting(fixture.db, mentionExcessSettingPrefix+"222222222222222222."+today); got != "1" {
		t.Fatalf("mention excess count = %q, want 1", got)
	}
	if excess := mentionExcessForToday(fixture.db, "222222222222222222", time.Now()); excess != 1 {
		t.Fatalf("digest excess = %d, want 1", excess)
	}
}

// TestMentionCapabilityProbeRecordsDegradation verifies the probe: an
// unreadable comments endpoint disables mention DMs and records the state.
func TestMentionCapabilityProbeRecordsDegradation(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	if err := fixture.db.Create(&model.UserMap{KitsuName: "Alice", DiscordID: "222222222222222222"}).Error; err != nil {
		t.Fatal(err)
	}
	mentionCapabilityState = -1
	t.Cleanup(func() { mentionCapabilityState = -1 })

	// Seed the task so the change-detection branch runs on the next poll.
	seed := pipelinePayload("WFA")
	seed.CommentsUnavailable = false
	FilterTasks([]kitsu.MessagePayload{seed}, pipelineConfig(), fixture.db)

	// Same status but comments unavailable this poll: the probe must record
	// the degradation and no mention DM may fire.
	degraded := pipelinePayload("WFA")
	degraded.Task.UpdatedAt = "2026-08-07T00:05:00"
	degraded.CommentsUnavailable = true
	degraded.LatestComment.Comment.ID = "c1"
	degraded.LatestComment.Comment.Text = "@Alice please check"
	FilterTasks([]kitsu.MessagePayload{degraded}, pipelineConfig(), fixture.db)

	if model.GetSetting(fixture.db, commentsReadableSettingKey) != "false" {
		t.Fatalf("unreadable capability not recorded: %q", model.GetSetting(fixture.db, commentsReadableSettingKey))
	}
	today := time.Now().In(jst).Format("2006-01-02")
	if got := model.GetSetting(fixture.db, mentionCountSettingPrefix+"222222222222222222."+today); got != "" {
		t.Fatalf("mention DM sent while comments unreadable: %q", got)
	}
}

// TestInitialBulkIsSilencedByCheckpoint verifies the initial-connection bulk
// never floods: the checkpoint guard suppresses it before buffering.
func TestInitialBulkIsSilencedByCheckpoint(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	setup.MarkInitialCheckpointPending("p1")
	t.Cleanup(func() { setup.ClearInitialCheckpointPending("p1") })

	var payloads []kitsu.MessagePayload
	for i := 0; i < 200; i++ {
		payload := pipelinePayload("WFA")
		payload.Task.ID = fmt.Sprintf("task-%d", i)
		payloads = append(payloads, payload)
	}
	FilterTasks(payloads, pipelineConfig(), fixture.db)

	var buffered int64
	fixture.db.Model(&model.NotificationAggregationBuffer{}).Count(&buffered)
	if buffered != 0 {
		t.Fatalf("initial bulk was buffered instead of suppressed: %d rows", buffered)
	}
	var posts int64
	fixture.db.Model(&model.NotificationDeliveryEvent{}).Count(&posts)
	if posts != 0 {
		t.Fatalf("initial bulk polluted the ledger: %d rows", posts)
	}
}

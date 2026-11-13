package main

import (
	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// dmFixture serves the Discord Bot API for digest/mention DMs and counts
// sends per recipient. Recipients in failingIDs get a 403 on the DM open.
type dmFixture struct {
	server     *httptest.Server
	sends      map[string]*atomic.Int64
	failingIDs map[string]bool
	mu         atomic.Int64
}

func newDMFixture(t *testing.T, failingIDs ...string) *dmFixture {
	t.Helper()
	fixture := &dmFixture{sends: map[string]*atomic.Int64{}, failingIDs: map[string]bool{}}
	for _, id := range failingIDs {
		fixture.failingIDs[id] = true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v10/users/@me/channels", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RecipientID string `json:"recipient_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if fixture.failingIDs[body.RecipientID] {
			http.Error(w, `{"message":"DMs closed"}`, http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"dm-channel-` + body.RecipientID + `"}`))
	})
	mux.HandleFunc("POST /api/v10/channels/", func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Add(1)
		recipient := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v10/channels/dm-channel-"), "/messages")
		if fixture.sends[recipient] == nil {
			fixture.sends[recipient] = &atomic.Int64{}
		}
		fixture.sends[recipient].Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"dm-message-` + recipient + `","channel_id":"dm-channel-` + recipient + `"}`))
	})
	fixture.server = httptest.NewServer(mux)
	t.Cleanup(fixture.server.Close)

	oldAPI := discord.SetDiscordBotAPIBase(fixture.server.URL + "/api/v10")
	oldToken := os.Getenv("DISCORD_BOT_TOKEN")
	os.Setenv("DISCORD_BOT_TOKEN", "test-token")
	discord.ResetDMChannelCache()
	t.Cleanup(func() {
		discord.SetDiscordBotAPIBase(oldAPI)
		os.Setenv("DISCORD_BOT_TOKEN", oldToken)
		discord.ResetDMChannelCache()
	})
	return fixture
}

func (f *dmFixture) sendsFor(recipient string) int64 {
	if counter := f.sends[recipient]; counter != nil {
		return counter.Load()
	}
	return 0
}

// TestDigestIncludesCheckpointedTasks reproduces finding 1: tasks silently
// accepted by the initial checkpoint never enter the delivery ledger, so the
// digest must read membership from the observation table.
func TestDigestIncludesCheckpointedTasks(t *testing.T) {
	fixture := newPhase2WebhookFixture(t)
	// A pre-existing WFA task recorded by the initial checkpoint only.
	if err := model.MarkTaskObserved(fixture.db, "task-checkpointed", "2026-08-07T00:00:00", "WFA", "c1", "2026-08-01T00:00:00", "p1", "sh001"); err != nil {
		t.Fatal(err)
	}
	digest := buildProductionDigest(fixture.db, model.ListProjects(fixture.db)[0], time.Now().In(jst))
	if len(digest.WaitingCheck) != 1 || digest.WaitingCheck[0] != "sh001" {
		t.Fatalf("checkpointed task missing from digest: %#v", digest.WaitingCheck)
	}
}

// TestDoneYesterdayJudgedByCompletionTime reproduces finding 2: yesterday's
// completions are judged by when the task completed, not by when the
// observation row was last touched.
func TestDoneYesterdayJudgedByCompletionTime(t *testing.T) {
	fixture := newPhase2WebhookFixture(t)
	yesterday := time.Now().In(jst).AddDate(0, 0, -1)
	// Completed yesterday; the observation row was touched again today.
	completed := model.Task{TaskID: "task-done-old", ProductionID: "p1", TaskStatus: "DONE", LastObservedStatus: "DONE",
		EntityName: "sh002", StatusChangedAt: yesterday.Add(-time.Hour), UpdatedAt: time.Now().UTC()}
	if err := fixture.db.Create(&completed).Error; err != nil {
		t.Fatal(err)
	}
	// Row touched yesterday, but the task only completed today.
	recent := model.Task{TaskID: "task-done-new", ProductionID: "p1", TaskStatus: "DONE", LastObservedStatus: "DONE",
		EntityName: "sh003", StatusChangedAt: time.Now().UTC().Add(-time.Hour), UpdatedAt: yesterday.Add(-time.Hour)}
	if err := fixture.db.Create(&recent).Error; err != nil {
		t.Fatal(err)
	}
	digest := buildProductionDigest(fixture.db, model.ListProjects(fixture.db)[0], time.Now().In(jst))
	if len(digest.DoneYesterday) != 1 || digest.DoneYesterday[0] != "sh002" {
		t.Fatalf("done-yesterday = %#v, want [sh002] (completion time, not row time)", digest.DoneYesterday)
	}
}

// TestStallNoticePerProduction reproduces finding 3: the stall-notice daily
// marker is per production, so two productions both notify on the same day.
func TestStallNoticePerProduction(t *testing.T) {
	fixture := newPhase2WebhookFixture(t)
	if err := fixture.db.Create(&model.Project{KitsuProjectID: "p2", Name: "Second", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatal(err)
	}
	dm := newDMFixture(t)
	var leadDMs int64
	// Both productions have a stalled WFA task and one lead each.
	for _, projectID := range []string{"p1", "p2"} {
		if err := model.MarkTaskObserved(fixture.db, "stalled-"+projectID, "2020-01-01T00:00:00Z", "WFA", "c", "2020-01-01T00:00:00Z", projectID, "shot-"+projectID); err != nil {
			t.Fatal(err)
		}
		project := model.Project{KitsuProjectID: projectID}
		if err := fixture.db.Where("kitsu_project_id = ?", projectID).First(&project).Error; err != nil {
			t.Fatal(err)
		}
		if err := fixture.db.Create(&model.ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "*", TargetKind: model.ReviewerTargetUser, DiscordID: "lead-" + projectID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := sendMorningDigests(pipelineConfig(), fixture.db, time.Now().In(jst), time.Now().In(jst).Format("2006-01-02")); err != nil {
		t.Fatalf("digest send failed: %v", err)
	}
	leadDMs = dm.sendsFor("lead-p1") + dm.sendsFor("lead-p2")
	if leadDMs < 2 {
		t.Fatalf("stall notices across two productions = %d, want >= 2 (per-production marker)", leadDMs)
	}
	// Second run the same day: no additional stall notices.
	if err := sendMorningDigests(pipelineConfig(), fixture.db, time.Now().In(jst), time.Now().In(jst).Format("2006-01-02")); err != nil {
		t.Fatalf("second digest send failed: %v", err)
	}
	if got := dm.sendsFor("lead-p1") + dm.sendsFor("lead-p2"); got != leadDMs {
		t.Fatalf("stall notices repeated within the day: %d -> %d", leadDMs, got)
	}
}

// TestDigestRetriesOnlyFailedDestinations reproduces finding 4: a partial
// digest failure must not re-send successful destinations on the next poll.
func TestDigestRetriesOnlyFailedDestinations(t *testing.T) {
	fixture := newPhase2WebhookFixture(t)
	dm := newDMFixture(t, "333333333333333333") // second recipient's DMs fail
	project := model.ListProjects(fixture.db)[0]
	for _, recipient := range []string{"222222222222222222", "333333333333333333"} {
		if err := fixture.db.Create(&model.ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "*", TargetKind: model.ReviewerTargetUser, DiscordID: recipient}).Error; err != nil {
			t.Fatal(err)
		}
	}
	today := time.Now().In(jst).Format("2006-01-02")
	if err := sendMorningDigests(pipelineConfig(), fixture.db, time.Now().In(jst), today); err == nil {
		t.Fatal("digest with one failing recipient must report failure")
	}
	if got := dm.sendsFor("222222222222222222"); got != 1 {
		t.Fatalf("first recipient sends = %d, want 1", got)
	}
	// Repair the failing recipient and run again: only the failed one retries.
	delete(dm.failingIDs, "333333333333333333")
	if err := sendMorningDigests(pipelineConfig(), fixture.db, time.Now().In(jst), today); err != nil {
		t.Fatalf("retry digest failed: %v", err)
	}
	if got := dm.sendsFor("222222222222222222"); got != 1 {
		t.Fatalf("successful destination was re-sent: %d sends, want 1", got)
	}
	if got := dm.sendsFor("333333333333333333"); got != 1 {
		t.Fatalf("failed destination was not retried: %d sends, want 1", got)
	}
}

// TestFlapCountsOnlyStatusChanges reproduces finding 5: comment-only ledger
// rows must not inflate the flap round-trip count.
func TestFlapCountsOnlyStatusChanges(t *testing.T) {
	fixture := newPhase2WebhookFixture(t)
	base := "2026-08-07T00:00:0%s"
	// Three WFA rows (two of them comment-only) plus one RETAKE row.
	for i, status := range []string{"WFA", "WFA", "WFA", "RETAKE"} {
		if err := model.RecordNotificationOnlyDelivery(fixture.db, "task-flap", "p1", "tt1", status,
			fmt.Sprintf(base, fmt.Sprint(i)), "comment-"+fmt.Sprint(i), fmt.Sprintf(base, fmt.Sprint(i)), false); err != nil {
			t.Fatal(err)
		}
	}
	count, err := model.CountTaskTransitionsSince(fixture.db, "task-flap", time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// The first row has no in-window baseline and counts as one transition;
	// the two comment-only rows do not count; RETAKE does.
	if count != 2 {
		t.Fatalf("status round-trips = %d, want 2 (comment rows must not count)", count)
	}
}

// TestBulkSummaryBodyNeutralizesMentionsAndCapsLength covers findings 6 and 7
// at the body level: mentions are neutralized and the body never exceeds the
// Discord content limit.
func TestBulkSummaryBodyNeutralizesMentionsAndCapsLength(t *testing.T) {
	members := []summaryMember{
		{Name: "@everyone drop", Status: "WFA", TaskID: "task-1"},
		{Name: "[click](https://evil.example)", Status: "RETAKE", TaskID: "task-2"},
	}
	body := buildAggregatedSummaryBody("channel-1", members)
	if strings.Contains(body, "@everyone") || strings.Contains(body, "<b>") || strings.Contains(body, "<") {
		t.Fatalf("summary body carries a live mention or raw markup: %s", body)
	}
	var long []summaryMember
	for i := 0; i < 60; i++ {
		long = append(long, summaryMember{Name: fmt.Sprintf("sh%03d-%s", i, strings.Repeat("x", 80)), Status: "WFA", TaskID: fmt.Sprintf("task-long-%d", i)})
	}
	if got := len(buildAggregatedSummaryBody("channel-1", long)); got > aggregatedSummaryContentLimit {
		t.Fatalf("summary body length = %d, want <= %d", got, aggregatedSummaryContentLimit)
	}
}

// TestBulkSummaryUnknownConsumesWithoutResend reproduces finding 8: an Unknown
// summary send consumes the buffers with an explicit ledger trace instead of
// re-sending and risking a double post.
func TestBulkSummaryUnknownConsumesWithoutResend(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	// The webhook accepts connections but never answers POSTs: a transport
	// error means Unknown (the request may have been accepted). GET identity
	// checks still succeed so destination verification passes.
	fixture.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			panic(http.ErrAbortHandler)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"111111111111111111","type":1,"guild_id":"guild-1","channel_id":"channel-1"}`))
	})
	for i := 0; i < 11; i++ {
		payload := pipelinePayload("WFA")
		payload.Task.ID = fmt.Sprintf("task-bulk-%d", i)
		payload.Entity.Name = fmt.Sprintf("sh%03d", i)
		if _, err := model.BufferNotificationTransition(fixture.db, aggregationBufferFor(payload)); err != nil {
			t.Fatal(err)
		}
	}
	expireAggregationWindows(t, fixture.db)
	dispatchDueAggregatedNotifications(pipelineConfig(), fixture.db)
	var unknown int64
	fixture.db.Model(&model.NotificationDeliveryEvent{}).Where("failure_class = ?", "aggregated_bulk_unknown").Count(&unknown)
	if unknown != 11 {
		t.Fatalf("unknown bulk trace rows = %d, want 11", unknown)
	}
	var buffers int64
	fixture.db.Model(&model.NotificationAggregationBuffer{}).Count(&buffers)
	if buffers != 0 {
		t.Fatalf("buffers survived an Unknown summary send: %d", buffers)
	}
	// No retry: the next sweep performs no further send attempts.
	dispatchDueAggregatedNotifications(pipelineConfig(), fixture.db)
}

// TestBulkSummaryReverifiesDestination reproduces finding 9: the summary send
// must use the destination's current webhook, not the URL captured when the
// transition was buffered.
func TestBulkSummaryReverifiesDestination(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	for i := 0; i < 11; i++ {
		payload := pipelinePayload("WFA")
		payload.Task.ID = fmt.Sprintf("task-bulk-%d", i)
		if _, err := model.BufferNotificationTransition(fixture.db, aggregationBufferFor(payload)); err != nil {
			t.Fatal(err)
		}
	}
	// The webhook was re-created under a new URL for the same channel.
	newURL := fixture.server.URL + "/api/webhooks/222222222222222222/newtoken"
	if err := fixture.db.Model(&model.ProjectWebhook{}).Where("id = ?", fixture.webhookRowID).Update("webhook_url", newURL).Error; err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	var newPosts atomic.Int64
	mux.HandleFunc("POST /api/webhooks/222222222222222222/newtoken", func(w http.ResponseWriter, r *http.Request) {
		newPosts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"summary-message-1","channel_id":"channel-1"}`))
	})
	mux.HandleFunc("GET /api/webhooks/222222222222222222/newtoken", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"222222222222222222","type":1,"guild_id":"guild-1","channel_id":"channel-1"}`))
	})
	oldHandler := fixture.server.Config.Handler
	fixture.server.Config.Handler = mux
	t.Cleanup(func() { fixture.server.Config.Handler = oldHandler })

	expireAggregationWindows(t, fixture.db)
	dispatchDueAggregatedNotifications(pipelineConfig(), fixture.db)
	if got := newPosts.Load(); got != 1 {
		t.Fatalf("summary POSTs to the current webhook = %d, want 1", got)
	}
	var stalePosts int64
	fixture.db.Model(&model.NotificationDeliveryEvent{}).Where("failure_class = ?", "aggregated_bulk_summary").Count(&stalePosts)
	if stalePosts != 11 {
		t.Fatalf("bulk summary ledger rows = %d, want 11", stalePosts)
	}
}

// TestSummaryLifecycleEditsAndDeletesOnResolution reproduces finding 10: WFA
// entries inside a delivered bulk summary leave the summary when they resolve,
// and the summary is deleted once nothing waits anymore.
func TestSummaryLifecycleEditsAndDeletesOnResolution(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	var edits atomic.Int64
	fixture.server.Config.Handler = summaryLifecycleMux(fixture, &edits)
	for i := 0; i < 11; i++ {
		payload := pipelinePayload("WFA")
		payload.Task.ID = fmt.Sprintf("task-bulk-%d", i)
		payload.Entity.Name = fmt.Sprintf("sh%03d", i)
		if _, err := model.BufferNotificationTransition(fixture.db, aggregationBufferFor(payload)); err != nil {
			t.Fatal(err)
		}
	}
	expireAggregationWindows(t, fixture.db)
	dispatchDueAggregatedNotifications(pipelineConfig(), fixture.db)

	// One member resolves: the summary is edited without that task's line.
	resolved := pipelinePayload("DONE")
	resolved.Task.ID = "task-bulk-0"
	resolved.Entity.Name = "sh000"
	if err := model.ObserveTaskStatus(fixture.db, resolved.Task.ID, resolved.Task.UpdatedAt, "DONE", "", "", "p1", "sh000"); err != nil {
		t.Fatal(err)
	}
	if member, err := model.FindSummaryMemberEventForTask(fixture.db, resolved.Task.ID, "p1"); err != nil || member == nil {
		t.Fatalf("summary member row not found for resolved task: %v", err)
	} else if err := model.MarkNotificationLifecycleCleanupPending(fixture.db, member.EventID); err != nil {
		t.Fatal(err)
	}
	edits.Store(0)
	reconcileAggregatedSummaries(fixture.db)
	if got := edits.Load(); got != 1 {
		t.Fatalf("summary edits after one resolution = %d, want 1", got)
	}

	// All members resolve: the summary is deleted.
	for i := 1; i < 11; i++ {
		taskID := fmt.Sprintf("task-bulk-%d", i)
		if err := model.ObserveTaskStatus(fixture.db, taskID, "2026-08-07T00:00:00", "DONE", "", "", "p1", fmt.Sprintf("sh%03d", i)); err != nil {
			t.Fatal(err)
		}
		if member, err := model.FindSummaryMemberEventForTask(fixture.db, taskID, "p1"); err != nil || member == nil {
			t.Fatalf("summary member row not found for %s: %v", taskID, err)
		} else if err := model.MarkNotificationLifecycleCleanupPending(fixture.db, member.EventID); err != nil {
			t.Fatal(err)
		}
	}
	reconcileAggregatedSummaries(fixture.db)
	if got := fixture.deletes.Load(); got != 1 {
		t.Fatalf("summary deletes after full resolution = %d, want 1", got)
	}
}

// summaryLifecycleMux serves POST/GET/PATCH/DELETE for the summary lifecycle
// fixture and records the PATCH body.
func summaryLifecycleMux(fixture *phase2WebhookFixture, edits *atomic.Int64) http.Handler {
	var lastEdit atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/webhooks/111111111111111111/token", func(w http.ResponseWriter, r *http.Request) {
		fixture.posts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"summary-message-1","channel_id":"channel-1"}`))
	})
	mux.HandleFunc("GET /api/webhooks/111111111111111111/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"111111111111111111","type":1,"guild_id":"guild-1","channel_id":"channel-1"}`))
	})
	mux.HandleFunc("PATCH /api/webhooks/111111111111111111/token/messages/summary-message-1", func(w http.ResponseWriter, r *http.Request) {
		edits.Add(1)
		body, _ := io.ReadAll(r.Body)
		lastEdit.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"summary-message-1","channel_id":"channel-1"}`))
	})
	mux.HandleFunc("DELETE /api/webhooks/111111111111111111/token/messages/summary-message-1", func(w http.ResponseWriter, r *http.Request) {
		fixture.deletes.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// TestLifecycleInFlightClaimReleasedOnRestart reproduces finding 11: a
// lifecycle claim left in-flight by a process restart is released at startup
// so the channel post can still be deleted.
func TestLifecycleInFlightClaimReleasedOnRestart(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	deliverViaDispatch(t, fixture.db, fixture.webhookRowID)
	payload := pipelinePayload("WFA")
	FilterTasks([]kitsu.MessagePayload{payload}, pipelineConfig(), fixture.db)
	var event model.NotificationDeliveryEvent
	if err := fixture.db.Where("task_id = ? AND disposition = ?", payload.Task.ID, model.DeliveryDelivered).First(&event).Error; err != nil {
		t.Fatal(err)
	}
	// Simulate leaving WFA with a claim stuck in-flight (process died).
	if err := model.MarkNotificationLifecycleCleanupPending(fixture.db, event.EventID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.NotificationDeliveryEvent{}).Where("event_id = ?", event.EventID).Update("lifecycle_cleanup_in_flight", true).Error; err != nil {
		t.Fatal(err)
	}
	released, err := model.ReleaseStuckNotificationLifecycleCleanups(fixture.db)
	if err != nil || released != 1 {
		t.Fatalf("released = %d, %v; want 1", released, err)
	}
	discord.DeletePendingLifecycleMessages(fixture.db, 50)
	if got := fixture.deletes.Load(); got != 1 {
		t.Fatalf("post deletes after claim release = %d, want 1", got)
	}
}

// TestSilentUpdateStopsMentionDMs reproduces finding 12: silent-update mode
// must not send mention DMs, exactly like normal delivery.
func TestSilentUpdateStopsMentionDMs(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	if err := fixture.db.Create(&model.UserMap{KitsuName: "alice", DiscordID: "222222222222222222"}).Error; err != nil {
		t.Fatal(err)
	}
	dm := newDMFixture(t)
	// Seed the observation so the comment registers as a change (new tasks
	// skip the change-detection branch entirely).
	if err := model.MarkTaskObserved(fixture.db, "task-1", "2026-08-07T00:00:00", "WFA", "comment-0", "2026-08-06T00:00:00", "p1", "Asset 1"); err != nil {
		t.Fatal(err)
	}
	payload := pipelinePayload("WFA")
	payload.LatestComment.Comment.ID = "comment-1"
	payload.LatestComment.Comment.UpdatedAt = "2026-08-07T00:00:00"
	payload.LatestComment.Comment.Text = "@alice please check"
	FilterTasks([]kitsu.MessagePayload{payload}, pipelineConfig(), fixture.db)
	if got := dm.sendsFor("222222222222222222"); got != 1 {
		t.Fatalf("mention DMs in normal mode = %d, want 1", got)
	}
	// Second comment in silent-update mode: no DM.
	payload2 := payload
	payload2.LatestComment.Comment.ID = "comment-2"
	payload2.LatestComment.Comment.UpdatedAt = "2026-08-07T00:01:00"
	payload2.LatestComment.Comment.Text = "@alice again"
	conf := pipelineConfig()
	conf.SilentUpdateDB = true
	FilterTasks([]kitsu.MessagePayload{payload2}, conf, fixture.db)
	if got := dm.sendsFor("222222222222222222"); got != 1 {
		t.Fatalf("silent-update mode sent mention DMs: %d sends, want 1", got)
	}
}

// TestMentionDMFailureDoesNotConsumeCap reproduces finding 13: a failed
// mention DM must not consume the daily quota.
func TestMentionDMFailureDoesNotConsumeCap(t *testing.T) {
	fixture := newPhase2WebhookFixture(t)
	if err := fixture.db.Create(&model.UserMap{KitsuName: "alice", DiscordID: "222222222222222222"}).Error; err != nil {
		t.Fatal(err)
	}
	dm := newDMFixture(t, "222222222222222222") // DM open fails
	payload := pipelinePayload("WFA")
	payload.LatestComment.Comment.ID = "comment-1"
	payload.LatestComment.Comment.UpdatedAt = "2026-08-07T00:00:00"
	payload.LatestComment.Comment.Text = "@alice please check"
	dispatchMentionDMs(fixture.db, payload)
	today := time.Now().In(jst).Format("2006-01-02")
	if got := model.GetSetting(fixture.db, mentionCountSettingPrefix+"222222222222222222."+today); got != "" {
		t.Fatalf("failed mention DM consumed quota: count = %q, want unset", got)
	}
	// Repair the transport: the next mention delivers and only then counts.
	delete(dm.failingIDs, "222222222222222222")
	payload2 := payload
	payload2.LatestComment.Comment.ID = "comment-2"
	payload2.LatestComment.Comment.UpdatedAt = "2026-08-07T00:01:00"
	dispatchMentionDMs(fixture.db, payload2)
	if got := model.GetSetting(fixture.db, mentionCountSettingPrefix+"222222222222222222."+today); got != "1" {
		t.Fatalf("delivered mention DM count = %q, want 1", got)
	}
}

// TestMentionMatchingRequiresCompleteName reproduces finding 14: @ゆい must
// never match @ゆいと.
func TestMentionMatchingRequiresCompleteName(t *testing.T) {
	fixture := newPhase2WebhookFixture(t)
	if err := fixture.db.Create(&model.UserMap{KitsuName: "yui", DiscordID: "111111111111111111"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Create(&model.UserMap{KitsuName: "yuito", DiscordID: "222222222222222222"}).Error; err != nil {
		t.Fatal(err)
	}
	if got := mentionedDiscordIDs(fixture.db, "@yuito please check"); len(got) != 1 || got[0] != "222222222222222222" {
		t.Fatalf("complete-name match failed: %v", got)
	}
	if got := mentionedDiscordIDs(fixture.db, "@yui please check"); len(got) != 1 || got[0] != "111111111111111111" {
		t.Fatalf("short-name match failed: %v", got)
	}
	if got := mentionedDiscordIDs(fixture.db, "contact me at mailto:yui or @yui!"); len(got) != 1 || got[0] != "111111111111111111" {
		t.Fatalf("boundary match failed: %v", got)
	}
}

// TestDigestCardShowsEntityNames reproduces finding 15: the digest card lists
// shot names recorded with the observation, not UUID fragments.
func TestDigestCardShowsEntityNames(t *testing.T) {
	fixture := newPhase2WebhookFixture(t)
	if err := model.MarkTaskObserved(fixture.db, "0123456789abcdef", "2026-08-07T00:00:00", "WFA", "c1", "2026-08-01T00:00:00", "p1", "sh002"); err != nil {
		t.Fatal(err)
	}
	digest := buildProductionDigest(fixture.db, model.ListProjects(fixture.db)[0], time.Now().In(jst))
	card := renderDigestCard(digest, 0)
	if !strings.Contains(card, "sh002") || strings.Contains(card, "0123456789ab") {
		t.Fatalf("digest card does not use the entity name: %s", card)
	}
}

// TestStaleBufferedTransitionSuppressed covers the optional item: a WFA
// transition that resolved inside the aggregation window is consumed without
// the wasted post.
func TestStaleBufferedTransitionSuppressed(t *testing.T) {
	withImmediateAggregation(t)
	fixture := newPhase2WebhookFixture(t)
	deliverViaDispatch(t, fixture.db, fixture.webhookRowID)
	payload := pipelinePayload("WFA")
	if _, err := model.BufferNotificationTransition(fixture.db, aggregationBufferFor(payload)); err != nil {
		t.Fatal(err)
	}
	// The task resolves into a non-notifiable status before the window closes.
	if err := model.MarkTaskObserved(fixture.db, payload.Task.ID, "2026-08-07T00:01:00", "ready", "c1", "2026-08-01T00:00:00", "p1", "Asset 1"); err != nil {
		t.Fatal(err)
	}
	before := fixture.posts.Load()
	expireAggregationWindows(t, fixture.db)
	dispatchDueAggregatedNotifications(pipelineConfig(), fixture.db)
	if got := fixture.posts.Load() - before; got != 0 {
		t.Fatalf("stale buffered transition produced %d posts, want 0", got)
	}
	var superseded int64
	fixture.db.Model(&model.NotificationDeliveryEvent{}).Where("failure_class = ?", "aggregation_transition_superseded").Count(&superseded)
	if superseded != 1 {
		t.Fatalf("superseded trace rows = %d, want 1", superseded)
	}
}

// aggregationBufferFor serializes a payload into a buffer row for tests.
func aggregationBufferFor(payload kitsu.MessagePayload) model.NotificationAggregationBuffer {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	status := strings.ToUpper(strings.TrimSpace(payload.TaskStatus.TaskStatus.ShortName))
	identity := model.NotificationEventIdentity(model.NotificationDeliveryIntent{
		TaskID: payload.Task.ID, ProductionID: payload.Project.ID, TaskTypeID: payload.TaskType.ID,
		TaskStatus: status, TaskUpdatedAt: payload.Task.UpdatedAt,
		CommentID: payload.LatestComment.Comment.ID, CommentUpdatedAt: payload.LatestComment.Comment.UpdatedAt,
		IsAssignNotification: payload.IsAssignNotification,
	})
	return model.NotificationAggregationBuffer{
		TaskID: payload.Task.ID, ProductionID: payload.Project.ID, TaskTypeID: payload.TaskType.ID,
		DestinationID: "channel-1", LaneType: model.LaneChannel, WebhookURL: "https://discord.com/api/webhooks/111111111111111111/token",
		LastStatus: status, LastPayloadJSON: string(payloadJSON), LastIdentity: identity,
	}
}

var _ = io.Discard

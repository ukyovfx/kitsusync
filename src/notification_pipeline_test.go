package main

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/config"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newNotificationPipelineDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.Task{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}, &model.ProjectReviewerTarget{}, &model.ProductionNotificationWriteLock{}, &model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func reopenNotificationPipelineDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.Task{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}, &model.ProjectReviewerTarget{}, &model.ProductionNotificationWriteLock{}, &model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func closeNotificationPipelineDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
}

func pipelinePayload(status string) kitsu.MessagePayload {
	var payload kitsu.MessagePayload
	payload.Project.ID, payload.Project.Name = "p1", "Production"
	payload.Task.ID, payload.Task.UpdatedAt = "task-1", "2026-08-07T00:00:00"
	payload.TaskType.ID, payload.TaskType.Name = "tt1", "Animation"
	payload.TaskStatus.ShortName = status
	payload.Entity.Name = "Asset 1"
	return payload
}

func pipelineConfig() config.Config {
	var conf config.Config
	conf.Notification.NotifyOnAssign = true
	return conf
}

func configurePipelineRoute(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := model.CreateProject(db, "p1", "Production", "", "guild-1", "category-1", "ja"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "p1", "Animation", "", "https://example.invalid/webhook", "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
}

func TestFilterTasksLeavesUnroutedEventAvailableForLaterRouting(t *testing.T) {
	db := newNotificationPipelineDB(t)
	original := notificationDispatch
	t.Cleanup(func() { notificationDispatch = original })
	dispatched := 0
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, _ *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		dispatched += len(data)
		for _, payload := range data {
			model.UpdateTaskWithDiscord(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.ShortName, "", "", "discord-message", "", "")
		}
		return data
	}

	payload := pipelinePayload("WFA")
	FilterTasks([]kitsu.MessagePayload{payload}, pipelineConfig(), db)
	if got := model.FindTask(db, payload.Task.ID); got.ID != 0 {
		t.Fatal("unrouted event was marked handled before a route existed")
	}

	configurePipelineRoute(t, db)
	FilterTasks([]kitsu.MessagePayload{payload}, pipelineConfig(), db)
	if dispatched != 1 {
		t.Fatalf("expected event to dispatch after routing was configured, got %d", dispatched)
	}
}

func TestFilterTasksRetriesAfterFailedDispatch(t *testing.T) {
	db := newNotificationPipelineDB(t)
	configurePipelineRoute(t, db)
	original := notificationDispatch
	t.Cleanup(func() { notificationDispatch = original })
	attempts := 0
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, _ *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		attempts++
		if attempts > 1 {
			for _, payload := range data {
				model.UpdateTaskWithDiscord(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.ShortName, "", "", "discord-message", "", "")
			}
		}
		return data
	}

	payload := pipelinePayload("WFA")
	FilterTasks([]kitsu.MessagePayload{payload}, pipelineConfig(), db)
	FilterTasks([]kitsu.MessagePayload{payload}, pipelineConfig(), db)
	if attempts != 2 {
		t.Fatalf("expected failed delivery to be retried once, got %d attempts", attempts)
	}
}

func TestFilterTasksDoesNotResendUnknownDiscordDelivery(t *testing.T) {
	db := newNotificationPipelineDB(t)
	if err := db.AutoMigrate(&model.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	configurePipelineRoute(t, db)
	attempts := 0
	const webhookID = "123456789012345678"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"123456789012345678","type":1,"guild_id":"guild-1","channel_id":"channel-1"}`))
		case http.MethodPost:
			attempts++
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected webhook request method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	webhookURL := server.URL + "/api/webhooks/" + webhookID + "/test-token"
	if err := db.Model(&model.ProjectWebhook{}).Where("id = ?", webhook.ID).Update("webhook_url", webhookURL).Error; err != nil {
		t.Fatal(err)
	}
	conf := pipelineConfig()
	conf.Discord.EmbedsPerRequests = 1
	conf.Discord.RequestsPerMinute = 1000
	payload := pipelinePayload("WFA")

	FilterTasks([]kitsu.MessagePayload{payload}, conf, db)
	FilterTasks([]kitsu.MessagePayload{payload}, conf, db)

	if attempts != 1 {
		t.Fatalf("unknown delivery was re-sent %d times, want once", attempts)
	}
	if task := model.FindTask(db, payload.Task.ID); task.ID == 0 || task.TaskUpdatedAt != payload.Task.UpdatedAt {
		t.Fatalf("unknown delivery was not marked observed: %+v", task)
	}
}

func TestFilterTasksReachesConfiguredAssignmentNotification(t *testing.T) {
	db := newNotificationPipelineDB(t)
	configurePipelineRoute(t, db)
	original := notificationDispatch
	t.Cleanup(func() { notificationDispatch = original })
	assigned := false
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, _ *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		assigned = len(data) == 1 && data[0].IsAssignNotification
		for _, payload := range data {
			model.UpdateTaskWithDiscord(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.ShortName, "", "", "discord-message", "", "")
		}
		return data
	}

	FilterTasks([]kitsu.MessagePayload{pipelinePayload("none")}, pipelineConfig(), db)
	if !assigned {
		t.Fatal("configured assignment notification did not reach dispatch")
	}
}

func TestFilterTasksPersistsStatusTransitionsAcrossPollsAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notification-state.db")
	db := reopenNotificationPipelineDB(t, path)
	configurePipelineRoute(t, db)
	original := notificationDispatch
	t.Cleanup(func() { notificationDispatch = original })
	var transitions []string
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, db *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		for _, payload := range data {
			transitions = append(transitions, payload.PreviousStatusName+" -> "+payload.TaskStatus.TaskStatus.ShortName)
			model.UpdateTaskWithDiscord(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.TaskStatus.ShortName, "", "", "discord-message", "", "")
		}
		return data
	}

	wfa := pipelinePayload("WFA")
	FilterTasks([]kitsu.MessagePayload{wfa}, pipelineConfig(), db)
	for i := 0; i < 8; i++ {
		FilterTasks([]kitsu.MessagePayload{wfa}, pipelineConfig(), db)
	}
	closeNotificationPipelineDB(t, db)

	db = reopenNotificationPipelineDB(t, path)
	retake := pipelinePayload("RETAKE")
	retake.Task.UpdatedAt = "2026-08-07T00:10:00"
	FilterTasks([]kitsu.MessagePayload{retake}, pipelineConfig(), db)
	closeNotificationPipelineDB(t, db)

	db = reopenNotificationPipelineDB(t, path)
	done := pipelinePayload("DONE")
	done.Task.UpdatedAt = "2026-08-07T00:20:00"
	FilterTasks([]kitsu.MessagePayload{done}, pipelineConfig(), db)
	closeNotificationPipelineDB(t, db)

	want := []string{" -> WFA", "WFA -> RETAKE", "RETAKE -> DONE"}
	if fmt.Sprint(transitions) != fmt.Sprint(want) {
		t.Fatalf("unexpected persisted transitions: got %v, want %v", transitions, want)
	}
}

func TestFilterTasksRetriesKnownTransitionAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retry-state.db")
	db := reopenNotificationPipelineDB(t, path)
	configurePipelineRoute(t, db)
	original := notificationDispatch
	t.Cleanup(func() { notificationDispatch = original })
	var transitions []string
	failRetake := true
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, db *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		for _, payload := range data {
			transitions = append(transitions, payload.PreviousStatusName+" -> "+payload.TaskStatus.TaskStatus.ShortName)
			if payload.TaskStatus.TaskStatus.ShortName == "RETAKE" && failRetake {
				failRetake = false
				continue
			}
			model.UpdateTaskWithDiscord(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.TaskStatus.ShortName, "", "", "discord-message", "", "")
		}
		return data
	}

	wfa := pipelinePayload("WFA")
	FilterTasks([]kitsu.MessagePayload{wfa}, pipelineConfig(), db)
	retake := pipelinePayload("RETAKE")
	retake.Task.UpdatedAt = "2026-08-07T00:10:00"
	FilterTasks([]kitsu.MessagePayload{retake}, pipelineConfig(), db)
	closeNotificationPipelineDB(t, db)

	db = reopenNotificationPipelineDB(t, path)
	FilterTasks([]kitsu.MessagePayload{retake}, pipelineConfig(), db)
	closeNotificationPipelineDB(t, db)
	want := []string{" -> WFA", "WFA -> RETAKE", "WFA -> RETAKE"}
	if fmt.Sprint(transitions) != fmt.Sprint(want) {
		t.Fatalf("retry lost durable previous status: got %v, want %v", transitions, want)
	}
}

func TestFilterTasksSupersedesKnownRejectedOlderEventAndUnblocksCurrentEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "superseded-notification.db")
	db, err := gorm.Open(sqlite.Open("file:"+filepath.ToSlash(path)+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.Task{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}, &model.ProjectReviewerTarget{}, &model.ProductionNotificationWriteLock{}, &model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	configurePipelineRoute(t, db)
	old := pipelinePayload("WFA")
	old.Task.UpdatedAt = "2026-08-07T00:00:00"
	if err := model.UpdateTaskWithDiscord(db, old.Task.ID, old.Task.UpdatedAt, "WFA", "comment-1", "2026-08-07T00:00:00", "", "", ""); err != nil {
		t.Fatal(err)
	}
	project := model.FindProjectByKitsuID(db, "p1")
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	revision, err := model.ProductionNotificationRevisionWithError(db, project.ID, project.KitsuProjectID)
	if err != nil {
		t.Fatal(err)
	}
	oldIntent := model.NotificationDeliveryIntent{
		TaskID: old.Task.ID, ProductionID: old.Project.ID, TaskTypeID: old.TaskType.ID,
		TaskStatus: "WFA", TaskUpdatedAt: old.Task.UpdatedAt, CommentID: "comment-1", CommentUpdatedAt: "2026-08-07T00:00:00",
		Payload:      []byte(`{"content":"older WFA event","allowed_mentions":{"parse":[]}}`),
		WebhookRowID: webhook.ID, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1", NotificationRevision: revision,
	}
	oldEvent, err := model.PrepareNotificationDelivery(db, oldIntent)
	if err != nil {
		t.Fatal(err)
	}
	attempt, started, err := model.BeginNotificationDelivery(db, oldEvent.EventID, time.Now().UTC())
	if err != nil || !started {
		t.Fatalf("begin old attempt: started=%v err=%v", started, err)
	}
	if err := model.SetNotificationDeliveryDisposition(db, *attempt, model.DeliveryRetryable, "rate_limited"); err != nil {
		t.Fatal(err)
	}

	originalDispatch := notificationDispatch
	t.Cleanup(func() { notificationDispatch = originalDispatch })
	dispatched := 0
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, _ *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		dispatched += len(data)
		if len(data) == 1 && data[0].TaskStatus.TaskStatus.ShortName != "RETAKE" {
			t.Errorf("expected the current Kitsu event after old rejection, got %s", data[0].TaskStatus.TaskStatus.ShortName)
		}
		return data
	}

	current := pipelinePayload("RETAKE")
	current.Task.UpdatedAt = "2026-08-07T00:10:00"
	current.LatestComment.Comment.ID = "comment-2"
	current.LatestComment.Comment.UpdatedAt = "2026-08-07T00:10:00"
	FilterTasks([]kitsu.MessagePayload{current}, pipelineConfig(), db)
	if dispatched != 1 {
		t.Fatalf("current Kitsu event was still blocked by an older known rejection: dispatches=%d", dispatched)
	}
	var storedOld model.NotificationDeliveryEvent
	if err := db.Where("event_id = ?", oldEvent.EventID).First(&storedOld).Error; err != nil {
		t.Fatal(err)
	}
	currentIntent := model.NotificationDeliveryIntent{
		TaskID: current.Task.ID, ProductionID: current.Project.ID, TaskTypeID: current.TaskType.ID,
		TaskStatus: "RETAKE", TaskUpdatedAt: current.Task.UpdatedAt, CommentID: current.LatestComment.Comment.ID, CommentUpdatedAt: current.LatestComment.Comment.UpdatedAt,
		Payload:      []byte(`{"content":"current RETAKE event","allowed_mentions":{"parse":[]}}`),
		WebhookRowID: webhook.ID, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1", NotificationRevision: revision,
	}
	currentEvent, err := model.PrepareNotificationDelivery(db, currentIntent)
	if err != nil {
		t.Fatal(err)
	}
	if storedOld.Disposition != model.DeliveryTerminalFailure || storedOld.FailureClass != "superseded_by_newer_kitsu_event" || storedOld.SupersededByEventID != currentEvent.EventID || storedOld.PayloadJSON != string(oldIntent.Payload) {
		t.Fatalf("old event was not retained with explicit successor evidence: %+v", storedOld)
	}
	if _, started, err := model.BeginNotificationDelivery(db, currentEvent.EventID, time.Now().UTC()); err != nil || !started {
		t.Fatalf("current event should be claimable after predecessor supersession: started=%v err=%v", started, err)
	}
}

func TestPreviousTaskStatusHasNoInventedFallback(t *testing.T) {
	if got := model.PreviousTaskStatus(model.Task{}, "WFA"); got != "" {
		t.Fatalf("unknown prior status must remain empty, got %q", got)
	}
}

package main

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/config"
	"fmt"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newNotificationPipelineDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.Task{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}); err != nil {
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
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.Task{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}); err != nil {
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

func TestPreviousTaskStatusHasNoInventedFallback(t *testing.T) {
	if got := model.PreviousTaskStatus(model.Task{}, "WFA"); got != "" {
		t.Fatalf("unknown prior status must remain empty, got %q", got)
	}
}

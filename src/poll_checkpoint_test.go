package main

import (
	"path/filepath"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
	"app/src/setup"
	"app/src/utils/config"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// A poll that races the production connection must not report work that
// already existed; once the initial checkpoint finishes, changes notify again.
func TestFilterTasksStaysSilentWhileInitialCheckpointRuns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "poll.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.ProjectWebhook{}, &model.NotificationDeliveryEvent{}, &model.NotificationRoutingDiagnosis{}); err != nil {
		t.Fatal(err)
	}
	webhook := model.ProjectWebhook{KitsuProjectID: "p1", DiscordChannelID: "channel-1", WebhookURL: "https://discord.invalid/hook"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: "p1", TaskTypeID: "tt1", DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-1"}}); err != nil {
		t.Fatal(err)
	}
	payloads := []kitsu.MessagePayload{notifiablePayload("t1")}
	conf := config.Config{}

	setup.MarkInitialCheckpointPending("p1")
	FilterTasks(payloads, conf, db)
	if checkpoint, _ := model.FindTaskWithError(db, "t1"); checkpoint.DeliveredTaskStatus != "" {
		t.Fatalf("a poll during the initial checkpoint observed a task: %#v", checkpoint)
	}

	setup.ClearInitialCheckpointPending("p1")
	FilterTasks(payloads, conf, db)
	checkpoint, _ := model.FindTaskWithError(db, "t1")
	if checkpoint.LastObservedStatus != "wfa" || checkpoint.DeliveredTaskStatus != "" {
		t.Fatalf("polling did not resume after the initial checkpoint: %#v", checkpoint)
	}
}

func notifiablePayload(taskID string) kitsu.MessagePayload {
	var payload kitsu.MessagePayload
	payload.Task.ID = taskID
	payload.Task.UpdatedAt = "2026-01-01T00:00:00"
	payload.TaskStatus.TaskStatus.ShortName = "wfa"
	payload.Project.ID = "p1"
	payload.TaskType.ID = "tt1"
	return payload
}

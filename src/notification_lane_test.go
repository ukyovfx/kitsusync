package main

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/config"
	"testing"
)

func TestPlanProductionNotificationRecordOnlyLane(t *testing.T) {
	db := newNotificationPipelineDB(t)
	t.Cleanup(func() { closeNotificationPipelineDB(t, db) })
	if err := db.Create(&model.Project{KitsuProjectID: "p1", Name: "P1", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", DestinationKind: model.LaneRecordOnly}}); err != nil {
		t.Fatal(err)
	}
	payload := pipelinePayload("WFA")
	plan := planProductionNotification(db, payload)
	if plan.LaneType != model.LaneRecordOnly {
		t.Fatalf("plan lane = %q, want record_only", plan.LaneType)
	}
	if plan.ShouldSend {
		t.Fatal("record-only plan must not dispatch a network send")
	}
}

func TestPlanProductionNotificationDMKeepsWebhookFallbackDestination(t *testing.T) {
	db := newNotificationPipelineDB(t)
	t.Cleanup(func() { closeNotificationPipelineDB(t, db) })
	if err := db.Create(&model.Project{KitsuProjectID: "p1", Name: "P1", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatal(err)
	}
	webhook := model.ProjectWebhook{KitsuProjectID: "p1", ChannelName: "comp", TaskType: "Comp", WebhookURL: "https://example.invalid/webhook", DiscordChannelID: "channel-1"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", DestinationKind: model.LaneDM, DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-1"}}); err != nil {
		t.Fatal(err)
	}
	payload := pipelinePayload("WFA")
	plan := planProductionNotification(db, payload)
	if plan.LaneType != model.LaneDM {
		t.Fatalf("plan lane = %q, want dm", plan.LaneType)
	}
	if !plan.ShouldSend || plan.WebhookURL != webhook.WebhookURL {
		t.Fatalf("dm plan must keep the work-channel webhook fallback: %+v", plan)
	}
}

func TestFilterTasksRecordsRecordOnlyDeliveryWithoutDispatch(t *testing.T) {
	db := newNotificationPipelineDB(t)
	t.Cleanup(func() { closeNotificationPipelineDB(t, db) })
	if err := db.Create(&model.Project{KitsuProjectID: "p1", Name: "P1", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", DestinationKind: model.LaneRecordOnly}}); err != nil {
		t.Fatal(err)
	}
	payload := pipelinePayload("WFA")
	FilterTasks([]kitsu.MessagePayload{payload}, config.Config{}, db)

	var count int64
	db.Model(&model.NotificationDeliveryEvent{}).Where("task_id = ? AND lane_type = ?", payload.Task.ID, model.LaneRecordOnly).Count(&count)
	if count != 1 {
		t.Fatalf("record-only journal rows = %d, want 1", count)
	}
	var diagnosisCount int64
	db.Model(&model.NotificationRoutingDiagnosis{}).Where("task_id = ?", payload.Task.ID).Count(&diagnosisCount)
	if diagnosisCount != 0 {
		t.Fatalf("record-only lane must not be recorded as a routing skip, got %d diagnoses", diagnosisCount)
	}
}

package main

import (
	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newNotificationRoutingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func notificationPayload(projectID, projectName, taskTypeID, taskTypeName string) kitsu.MessagePayload {
	var payload kitsu.MessagePayload
	payload.Project.ID, payload.Project.Name = projectID, projectName
	payload.TaskType.ID, payload.TaskType.Name = taskTypeID, taskTypeName
	payload.Task.ID = "task-1"
	payload.Entity.Name = "Asset"
	payload.TaskStatus.ShortName = "wfa"
	return payload
}

func TestDryRunUsesTaskTypeIDAndMakesNoDiscordCall(t *testing.T) {
	db := newNotificationRoutingTestDB(t)
	if err := model.CreateProject(db, "p1", "Same Name", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "p1", "A", "", "https://example.invalid/a", "11111111111111111"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", ProductionName: "Same Name", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	result := dryRunProductionNotification(db, notificationPayload("p1", "Same Name", "tt1", "Compositing"), map[string]struct{}{"tt1": {}})
	if result.SkipReason != "" || result.DestinationID == "" || result.MatchedRule == "" {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	if result.RenderedPreview == "" {
		t.Fatal("expected preview")
	}
	if resultContainsSecret(result) {
		t.Fatal("dry-run result contains a secret field")
	}
}

func resultContainsSecret(result ProductionRoutingDryRun) bool {
	return result.DestinationID == "https://example.invalid/a" || result.RenderedPreview == "https://example.invalid/a"
}

func TestUnmatchedTaskTypeFailsClosedAndRecordsDiagnosis(t *testing.T) {
	db := newNotificationRoutingTestDB(t)
	if err := model.CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, nil); err != nil {
		t.Fatal(err)
	}
	payload := notificationPayload("p1", "P", "unknown-id", "Same Name")
	plan := planProductionNotification(db, payload)
	if plan.ShouldSend || plan.WebhookURL != "" {
		t.Fatalf("expected fail-closed plan: %+v", plan)
	}
	recordProductionRoutingSkip(db, payload, plan.SkipReason)
	if got := len(model.ListNotificationRoutingDiagnoses(db, "p1", 10)); got != 1 {
		t.Fatalf("expected one diagnosis, got %d", got)
	}
}

func TestSameNameTaskTypesRemainIsolatedByID(t *testing.T) {
	db := newNotificationRoutingTestDB(t)
	if err := model.CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "p1", "A", "", "https://example.invalid/a", "11111111111111111"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", TaskTypeName: "Same Name", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	plan := planProductionNotification(db, notificationPayload("p1", "P", "tt2", "Same Name"))
	if plan.ShouldSend {
		t.Fatal("same-name Task Type with a different ID was incorrectly routed")
	}
}

func TestProductionTaskLinkUsesPublicKitsuURLOnly(t *testing.T) {
	previous := discord.KitsuPublicURLResolver
	discord.KitsuPublicURLResolver = func() string { return "https://kitsu.example.test" }
	defer func() { discord.KitsuPublicURLResolver = previous }()
	payload := notificationPayload("production-1", "P", "task-type-1", "Animation")
	payload.EntityType.Name = "Shot"
	if got := productionTaskLink(payload); got != "https://kitsu.example.test/productions/production-1/shots/tasks/task-1" {
		t.Fatalf("public task link = %q", got)
	}
	discord.KitsuPublicURLResolver = func() string { return "http://host.docker.internal:8080" }
	if got := productionTaskLink(payload); got != "not supplied" {
		t.Fatalf("internal configured URL must not be exposed publicly, got %q", got)
	}
	discord.KitsuPublicURLResolver = func() string { return "" }
	if got := productionTaskLink(payload); got != "not supplied" {
		t.Fatalf("missing public URL = %q, want not supplied", got)
	}
}

func TestDryRunDetectsStaleTaskTypeID(t *testing.T) {
	db := newNotificationRoutingTestDB(t)
	if err := model.CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	result := dryRunProductionNotification(db, notificationPayload("p1", "P", "old-id", "Renamed"), map[string]struct{}{"new-id": {}})
	if result.StaleIDDiagnosis == "" {
		t.Fatal("expected stale ID diagnosis")
	}
}

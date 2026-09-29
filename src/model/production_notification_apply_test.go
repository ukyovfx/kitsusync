package model

import (
	"errors"
	"testing"
)

func TestApplyProductionNotificationStateRejectsReviewerDeltaForRemovedRoute(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	project := Project{KitsuProjectID: "apply-delete-route", Name: "Apply Delete Route"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "comp", DiscordChannelID: "123456789012345678"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	routes := []ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}}
	if err := SaveProductionNotificationConfig(db, &ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, routes); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", ReviewerTargetUser, "123456789012345679"); err != nil {
		t.Fatal(err)
	}
	revision := ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	_, err := ApplyProductionNotificationState(db, project.ID, project.KitsuProjectID, project.Name, revision, nil, []ProductionReviewerDelta{{TaskTypeID: "task-comp", AddUserIDs: []string{"123456789012345680"}}})
	if !errors.Is(err, ErrReviewerDeltaForRemovedRoute) {
		t.Fatalf("expected removed-route delta rejection, got %v", err)
	}
	if got := len(ListProductionNotificationRoutes(db, project.KitsuProjectID)); got != 1 {
		t.Fatalf("route changed after rejected request: %d", got)
	}
	if got := len(ListProjectReviewerTargets(db, project.ID)); got != 1 {
		t.Fatalf("targets changed after rejected request: %d", got)
	}
}

func TestApplyProductionNotificationStateDeletesRemovedRouteTargetsAtomically(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	project := Project{KitsuProjectID: "apply-remove-targets", Name: "Apply Remove Targets"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "comp", DiscordChannelID: "123456789012345678"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	routes := []ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}}
	if err := SaveProductionNotificationConfig(db, &ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, routes); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", ReviewerTargetRole, "123456789012345679"); err != nil {
		t.Fatal(err)
	}
	revision := ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	if _, err := ApplyProductionNotificationState(db, project.ID, project.KitsuProjectID, project.Name, revision, []ProductionNotificationRoute{{TaskTypeID: "task-new", DestinationWebhookID: webhook.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := len(ListProjectReviewerTargets(db, project.ID)); got != 0 {
		t.Fatalf("deleted route retained additional targets: %d", got)
	}
	got := ListProductionNotificationRoutes(db, project.KitsuProjectID)
	if len(got) != 1 || got[0].TaskTypeID != "task-new" {
		t.Fatalf("unexpected final routes: %#v", got)
	}
}

func TestApplyProductionNotificationStateCommitsRoutesAndUserRoleDeltasTogether(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	project := Project{KitsuProjectID: "apply-combined-state", Name: "Combined Apply"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "comp", DiscordChannelID: "123456789012345678"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	initial := []ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}}
	if err := SaveProductionNotificationConfig(db, &ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, initial); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", ReviewerTargetUser, "123456789012345679"); err != nil {
		t.Fatal(err)
	}
	revision := ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	routes := []ProductionNotificationRoute{
		{TaskTypeID: "task-comp", DestinationWebhookID: webhook.ID},
		{TaskTypeID: "task-animation", DestinationWebhookID: webhook.ID},
	}
	changes := []ProductionReviewerDelta{
		{TaskTypeID: "task-comp", AddRoleIDs: []string{"123456789012345680"}, RemoveUserIDs: []string{"123456789012345679"}},
		{TaskTypeID: "task-animation", AddUserIDs: []string{"123456789012345681"}},
	}
	if _, err := ApplyProductionNotificationState(db, project.ID, project.KitsuProjectID, project.Name, revision, routes, changes); err != nil {
		t.Fatalf("combined route and recipient Apply failed: %v", err)
	}
	gotRoutes := ListProductionNotificationRoutes(db, project.KitsuProjectID)
	if len(gotRoutes) != 2 || gotRoutes[0].TaskTypeID != "task-comp" || gotRoutes[1].TaskTypeID != "task-animation" {
		t.Fatalf("combined Apply did not preserve submitted route order: %#v", gotRoutes)
	}
	gotTargets := ListProjectReviewerTargets(db, project.ID)
	if len(gotTargets) != 2 {
		t.Fatalf("combined Apply produced %d targets, want 2: %#v", len(gotTargets), gotTargets)
	}
	want := map[string]bool{"task-comp:" + ReviewerTargetRole + ":123456789012345680": true, "task-animation:" + ReviewerTargetUser + ":123456789012345681": true}
	for _, target := range gotTargets {
		key := target.TaskTypeID + ":" + target.TargetKind + ":" + target.DiscordID
		if !want[key] {
			t.Errorf("unexpected additional target after combined Apply: %s", key)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("combined Apply is missing targets: %#v", want)
	}
}

func TestApplyProductionNotificationStateRejectsStaleRevisionWithoutWrites(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	project := Project{KitsuProjectID: "apply-stale", Name: "Apply Stale"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "comp", DiscordChannelID: "123456789012345678"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	initial := []ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}}
	if err := SaveProductionNotificationConfig(db, &ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, initial); err != nil {
		t.Fatal(err)
	}
	revision := ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", ReviewerTargetUser, "123456789012345679"); err != nil {
		t.Fatal(err)
	}
	_, err := ApplyProductionNotificationState(db, project.ID, project.KitsuProjectID, project.Name, revision, []ProductionNotificationRoute{{TaskTypeID: "task-comp", DestinationWebhookID: webhook.ID}}, nil)
	if !errors.Is(err, ErrProductionNotificationRevisionConflict) {
		t.Fatalf("expected stale revision conflict, got %v", err)
	}
	got := ListProductionNotificationRoutes(db, project.KitsuProjectID)
	if len(got) != 1 || got[0].TaskTypeName != "Compositing" {
		t.Fatalf("stale request changed routing: %#v", got)
	}
	if len(ListProjectReviewerTargets(db, project.ID)) != 1 {
		t.Fatal("stale request changed reviewer targets")
	}
}

func TestApplyProductionNotificationStateRollsBackInvalidTargetAfterRouteReplacement(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	project := Project{KitsuProjectID: "apply-rollback", Name: "Apply Rollback"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "comp", DiscordChannelID: "123456789012345678"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	initial := []ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-before", TaskTypeName: "Before", DestinationWebhookID: webhook.ID}}
	if err := SaveProductionNotificationConfig(db, &ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, initial); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-before", "Before", ReviewerTargetRole, "123456789012345679"); err != nil {
		t.Fatal(err)
	}
	revision := ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	_, err := ApplyProductionNotificationState(db, project.ID, project.KitsuProjectID, project.Name, revision,
		[]ProductionNotificationRoute{{TaskTypeID: "task-after", DestinationWebhookID: webhook.ID}},
		[]ProductionReviewerDelta{{TaskTypeID: "task-after", AddUserIDs: []string{"not-a-discord-id"}}},
	)
	if err == nil {
		t.Fatal("invalid reviewer target was committed")
	}
	routes := ListProductionNotificationRoutes(db, project.KitsuProjectID)
	if len(routes) != 1 || routes[0].TaskTypeID != "task-before" {
		t.Fatalf("failed Apply partially replaced routes: %#v", routes)
	}
	targets := ListProjectReviewerTargets(db, project.ID)
	if len(targets) != 1 || targets[0].TaskTypeID != "task-before" {
		t.Fatalf("failed Apply partially changed additional targets: %#v", targets)
	}
}

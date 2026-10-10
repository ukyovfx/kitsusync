package model

import (
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestProductionNotificationRevisionReturnsReadFailures(t *testing.T) {
	for _, table := range []string{"production_notification_routes", "project_reviewer_targets"} {
		t.Run(table, func(t *testing.T) {
			db := newRoutingTestDB(t)
			if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
				t.Fatal(err)
			}
			readErr := errors.New("injected revision read failure")
			name := "test:fail_revision_read:" + table
			if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					tx.AddError(readErr)
				}
			}); err != nil {
				t.Fatal(err)
			}
			revision, err := ProductionNotificationRevisionWithError(db, 1, "project")
			if !errors.Is(err, readErr) || revision != "" {
				t.Fatalf("revision=%q err=%v, want propagated read error and no digest", revision, err)
			}
		})
	}
}

func TestProductionNotificationRevisionLegacyWrapperFailsClosed(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	readErr := errors.New("injected revision read failure")
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail_revision_read_legacy", func(tx *gorm.DB) {
		if tx.Statement.Table == "production_notification_routes" {
			tx.AddError(readErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if got := ProductionNotificationRevision(db, 1, "project"); got != "" {
		t.Fatalf("legacy wrapper must not return a valid digest on read failure: %q", got)
	}
}

func TestRestoreProductionNotificationStateStopsOnRevisionReadFailure(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	project := Project{KitsuProjectID: "restore-read-failure", Name: "Restore Read Failure"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "task", WebhookURL: "https://discord.com/api/webhooks/12345678901234567/synthetic", DiscordChannelID: "channel-1"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	route := ProductionNotificationRoute{ProductionID: project.KitsuProjectID, TaskTypeID: "tt-1", DestinationWebhookID: webhook.ID, DestinationChannelID: webhook.DiscordChannelID}
	if err := db.Create(&route).Error; err != nil {
		t.Fatal(err)
	}
	before := ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	readErr := errors.New("injected restore revision read failure")
	failureInjected := false
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail_restore_revision", func(tx *gorm.DB) {
		if tx.Statement.Table == "production_notification_routes" && !failureInjected {
			failureInjected = true
			tx.AddError(readErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := RestoreProductionNotificationState(db, project.ID, project.KitsuProjectID, before, nil, nil, nil); !errors.Is(err, readErr) {
		t.Fatalf("restore must propagate revision read failure before writes: %v", err)
	}
	var count int64
	if err := db.Model(&ProductionNotificationRoute{}).Where("production_id = ?", project.KitsuProjectID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("restore modified route state despite failed revision read: count=%d err=%v", count, err)
	}
}

func TestApplyProductionNotificationStateRejectsReviewerDeltaForRemovedRoute(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	project := Project{KitsuProjectID: "apply-delete-route", Name: "Apply Delete Route"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "comp", WebhookURL: "https://discord.invalid/webhooks/comp", DiscordChannelID: "123456789012345678"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	routes := []ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}, {ProductionID: project.KitsuProjectID, TaskTypeID: "task-animation", TaskTypeName: "Animation", DestinationWebhookID: webhook.ID}}
	if err := SaveProductionNotificationConfig(db, &ProductionNotificationConfig{ProductionID: project.KitsuProjectID, Enabled: true}, routes); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", ReviewerTargetUser, "123456789012345679"); err != nil {
		t.Fatal(err)
	}
	revision := ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	_, err := ApplyProductionNotificationState(db, project.ID, project.KitsuProjectID, project.Name, revision, []ProductionNotificationRoute{{TaskTypeID: "task-animation", DestinationWebhookID: webhook.ID}}, []ProductionReviewerDelta{{TaskTypeID: "task-comp", AddUserIDs: []string{"123456789012345680"}}})
	if !errors.Is(err, ErrReviewerDeltaForRemovedRoute) {
		t.Fatalf("expected removed-route delta rejection, got %v", err)
	}
	if got := len(ListProductionNotificationRoutes(db, project.KitsuProjectID)); got != 2 {
		t.Fatalf("route changed after rejected request: %d", got)
	}
	if got := len(ListProjectReviewerTargets(db, project.ID)); got != 1 {
		t.Fatalf("targets changed after rejected request: %d", got)
	}
}

func TestApplyProductionNotificationStateDeletesRemovedRouteTargetsAtomically(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	project := Project{KitsuProjectID: "apply-remove-targets", Name: "Apply Remove Targets"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "comp", WebhookURL: "https://discord.invalid/webhooks/comp", DiscordChannelID: "123456789012345678"}
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

func TestApplyProductionNotificationStateCreatesWebhookAndRouteAtomically(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	project := Project{KitsuProjectID: "apply-new-channel", Name: "New Channel"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	revision := ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	created := &ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "compositing", TaskType: "task-comp", WebhookURL: "https://discord.invalid/webhook/secret", DiscordChannelID: "123456789012345678"}
	if _, err := ApplyProductionNotificationState(db, project.ID, project.KitsuProjectID, project.Name, revision, []ProductionNotificationRoute{{TaskTypeID: "task-comp", DestinationWebhookID: 0}}, nil, created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatal("Apply must return the persisted managed webhook identity")
	}
	routes := ListProductionNotificationRoutes(db, project.KitsuProjectID)
	if len(routes) != 1 || routes[0].DestinationWebhookID != created.ID {
		t.Fatalf("route did not use the atomically created webhook: %#v, webhook=%#v", routes, created)
	}
	if got := len(ListProjectWebhooks(db, project.KitsuProjectID)); got != 1 {
		t.Fatalf("expected exactly one managed webhook row, got %d", got)
	}
	newRevision := ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	if err := RestoreProductionNotificationState(db, project.ID, project.KitsuProjectID, newRevision, nil, nil, nil, created.ID); err != nil {
		t.Fatal(err)
	}
	if len(ListProductionNotificationRoutes(db, project.KitsuProjectID)) != 0 || len(ListProjectWebhooks(db, project.KitsuProjectID)) != 0 {
		t.Fatal("transactional compensation must remove the new route and managed webhook row")
	}
}

func TestApplyProductionNotificationStateCommitsRoutesAndUserRoleDeltasTogether(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	project := Project{KitsuProjectID: "apply-combined-state", Name: "Combined Apply"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: project.KitsuProjectID, ChannelName: "comp", WebhookURL: "https://discord.invalid/webhooks/comp", DiscordChannelID: "123456789012345678"}
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
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
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
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProjectReviewerTarget{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
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

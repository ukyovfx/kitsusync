package model

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestSnapshotProductionNotificationStateReadsDestinationSet(t *testing.T) {
	if _, _, _, err := SnapshotProductionNotificationState(nil, 1, "production-1"); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("a nil database must be reported, not silently treated as an empty configuration: %v", err)
	}

	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	if project == nil {
		t.Fatal("fixture project missing")
	}

	config, routes, targets, err := SnapshotProductionNotificationState(db, project.ID, "production-1")
	if err != nil || config != nil || len(routes) != 1 || routes[0].TaskTypeID != "task-type-1" || len(targets) != 0 {
		t.Fatalf("a Production without a config row must still snapshot its routes: config=%+v routes=%+v targets=%+v err=%v", config, routes, targets, err)
	}
	if _, _, targets, err = SnapshotProductionNotificationState(db, project.ID, "  "); err != nil || len(targets) != 0 {
		t.Fatalf("a blank Production should snapshot no routes and no targets: targets=%+v err=%v", targets, err)
	}

	if err := db.Create(&ProductionNotificationConfig{ProductionID: "production-1", ProductionName: "Prod One", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: ReviewerTargetUser, DiscordID: "11111111111111111"}).Error; err != nil {
		t.Fatal(err)
	}
	config, routes, targets, err = SnapshotProductionNotificationState(db, project.ID, "production-1")
	if err != nil || config == nil || config.ProductionName != "Prod One" || !config.Enabled || len(routes) != 1 || len(targets) != 1 || targets[0].DiscordID != "11111111111111111" {
		t.Fatalf("snapshot did not read the full destination set: config=%+v routes=%+v targets=%+v err=%v", config, routes, targets, err)
	}
}

func TestExternalReorderRestoreRequiresTokenAndMatchingRevision(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	if project == nil {
		t.Fatal("fixture project missing")
	}
	if err := db.Create(&ProductionNotificationConfig{ProductionID: "production-1", ProductionName: "Prod One", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: ReviewerTargetUser, DiscordID: "11111111111111111"}).Error; err != nil {
		t.Fatal(err)
	}
	// A webhook created by the failed Discord reorder must be cleaned up by the restore.
	stray := ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-9", WebhookURL: "https://discord.com/api/webhooks/webhook-9/token"}
	if err := db.Create(&stray).Error; err != nil {
		t.Fatal(err)
	}
	config, routes, targets, err := SnapshotProductionNotificationState(db, project.ID, "production-1")
	if err != nil {
		t.Fatal(err)
	}
	revision := ProductionNotificationRevision(db, project.ID, "production-1")
	if revision == "" {
		t.Fatal("expected a canonical routing revision")
	}

	if err := RestoreProductionNotificationStateForExternalReorder(db, project.ID, "production-1", revision, config, routes, targets, "  ", stray.ID); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("a tokenless external reorder restore must be refused: %v", err)
	}
	if err := RestoreProductionNotificationStateForExternalReorder(db, project.ID, "production-1", revision, config, routes, targets, "tok-1", stray.ID); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("an external reorder restore must hold the reorder fence: %v", err)
	}
	if err := AcquireProductionNotificationWriteLock(db, "production-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE production_notification_write_locks SET mutation_state = ?, mutation_token = ?, mutation_at = ? WHERE production_id = ?", NotificationMutationReordering, "tok-1", time.Now().UTC(), "production-1").Error; err != nil {
		t.Fatal(err)
	}
	if err := RestoreProductionNotificationStateForExternalReorder(db, project.ID, "production-1", "stale-revision", config, routes, targets, "tok-1", stray.ID); !errors.Is(err, ErrProductionNotificationRevisionConflict) {
		t.Fatalf("a stale expected revision must refuse the restore: %v", err)
	}
	if FindProductionNotificationConfig(db, "production-1") == nil || len(ListProductionNotificationRoutes(db, "production-1")) != 1 || FindProjectWebhookByID(db, stray.ID) == nil {
		t.Fatal("a refused restore must leave the current routing state untouched")
	}

	if err := RestoreProductionNotificationStateForExternalReorder(db, project.ID, "production-1", revision, config, routes, targets, "tok-1", stray.ID); err != nil {
		t.Fatalf("the fenced restore should reconcile local routing state: %v", err)
	}
	restoredConfig := FindProductionNotificationConfig(db, "production-1")
	restoredRoutes := ListProductionNotificationRoutes(db, "production-1")
	restoredTargets := ListProjectReviewerTargets(db, project.ID)
	if restoredConfig == nil || restoredConfig.ProductionName != "Prod One" {
		t.Fatalf("restore did not re-create the snapshot config: %+v", restoredConfig)
	}
	if len(restoredRoutes) != 1 || restoredRoutes[0].TaskTypeID != "task-type-1" || restoredRoutes[0].DestinationWebhookID != 1 {
		t.Fatalf("restore did not re-create the snapshot routes: %+v", restoredRoutes)
	}
	if len(restoredTargets) != 1 || restoredTargets[0].DiscordID != "11111111111111111" {
		t.Fatalf("restore did not re-create the snapshot reviewer targets: %+v", restoredTargets)
	}
	if FindProjectWebhookByID(db, stray.ID) != nil {
		t.Fatal("restore did not clean up the webhook created by the failed reorder")
	}
}

func TestValidateReviewerDeltasForRemovedRoutesRejectsUnknownTaskTypes(t *testing.T) {
	if err := ValidateReviewerDeltasForRemovedRoutes(nil, nil); err != nil {
		t.Fatalf("no routes and no changes must be valid: %v", err)
	}
	routes := []ProductionNotificationRoute{{TaskTypeID: "task-type-1"}}
	if err := ValidateReviewerDeltasForRemovedRoutes(routes, []ProductionReviewerDelta{{TaskTypeID: " task-type-1 ", AddUserIDs: []string{"11111111111111111"}}}); err != nil {
		t.Fatalf("a delta on a live route must be valid: %v", err)
	}
	err := ValidateReviewerDeltasForRemovedRoutes(routes, []ProductionReviewerDelta{{TaskTypeID: "task-type-2"}})
	if !errors.Is(err, ErrReviewerDeltaForRemovedRoute) {
		t.Fatalf("a delta on a removed route must be refused: %v", err)
	}
	if err := ValidateReviewerDeltasForRemovedRoutes(nil, []ProductionReviewerDelta{{TaskTypeID: "task-type-1"}}); !errors.Is(err, ErrReviewerDeltaForRemovedRoute) {
		t.Fatalf("a delta with every route removed must be refused: %v", err)
	}
}

func TestDeleteProjectReviewerTargetsForDiscordUserIsScopedAndGated(t *testing.T) {
	t.Run("scoping", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		project := FindProjectByKitsuID(db, "production-1")
		if project == nil {
			t.Fatal("fixture project missing")
		}
		seed := []ProjectReviewerTarget{
			{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: ReviewerTargetUser, DiscordID: "11111111111111111"},
			{ProjectID: project.ID, TaskTypeID: "task-type-2", TargetKind: ReviewerTargetUser, DiscordID: "11111111111111111"},
			{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: ReviewerTargetUser, DiscordID: "22222222222222222"},
			{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: ReviewerTargetRole, DiscordID: "11111111111111111"},
		}
		for i := range seed {
			if err := db.Create(&seed[i]).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := DeleteProjectReviewerTargetsForDiscordUser(nil, project.ID, "11111111111111111"); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("a nil database must be rejected: %v", err)
		}
		if err := DeleteProjectReviewerTargetsForDiscordUser(db, 0, "11111111111111111"); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("a zero project must be rejected: %v", err)
		}
		if err := DeleteProjectReviewerTargetsForDiscordUser(db, project.ID, "   "); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("a blank Discord user must be rejected: %v", err)
		}
		if err := DeleteProjectReviewerTargetsForDiscordUser(db, 999, "11111111111111111"); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("an unknown project must be reported: %v", err)
		}
		if err := DeleteProjectReviewerTargetsForDiscordUser(db, project.ID, "11111111111111111"); err != nil {
			t.Fatalf("scoped deletion failed: %v", err)
		}
		remaining := ListProjectReviewerTargets(db, project.ID)
		if len(remaining) != 2 {
			t.Fatalf("deletion must remove only that user's targets: %+v", remaining)
		}
		for _, target := range remaining {
			if target.TargetKind != ReviewerTargetRole && target.DiscordID == "11111111111111111" {
				t.Fatalf("a user target survived the scoped deletion: %+v", target)
			}
			if target.DiscordID == "22222222222222222" && target.TargetKind != ReviewerTargetUser {
				t.Fatalf("another user's target changed: %+v", target)
			}
		}
	})

	t.Run("unresolved delivery gate", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		project := FindProjectByKitsuID(db, "production-1")
		if err := db.Create(&ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: ReviewerTargetUser, DiscordID: "11111111111111111"}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareNotificationDelivery(db, notificationIntent()); err != nil {
			t.Fatal(err)
		}
		if err := DeleteProjectReviewerTargetsForDiscordUser(db, project.ID, "11111111111111111"); !errors.Is(err, ErrNotificationDeliveryInFlight) {
			t.Fatalf("reviewer targets must not change while a delivery is unresolved: %v", err)
		}
		if len(ListProjectReviewerTargets(db, project.ID)) != 1 {
			t.Fatal("a blocked deletion must not remove reviewer targets")
		}
	})
}

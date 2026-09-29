package setup

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

func TestSameProductionNotificationRoutesSkipsDiscordReorderForReviewerOnlyApply(t *testing.T) {
	before := []model.ProductionNotificationRoute{{TaskTypeID: "task-comp", TaskTypeName: "Old Name", DestinationWebhookID: 7}}
	after := []model.ProductionNotificationRoute{{TaskTypeID: "task-comp", TaskTypeName: "New Name", DestinationWebhookID: 7}}
	if !sameProductionNotificationRoutes(before, after) {
		t.Fatal("Task Type display-name refresh should not require a Discord reorder")
	}
	after[0].DestinationWebhookID = 8
	if sameProductionNotificationRoutes(before, after) {
		t.Fatal("destination change must require a Discord reorder")
	}
}

func TestProductionNotificationCompensationAttemptsDatabaseAndDiscordRestores(t *testing.T) {
	dbErr := errors.New("database restore failed")
	discordErr := errors.New("discord restore failed")
	dbCalled, discordCalled := false, false
	gotDB, gotDiscord := compensateProductionNotificationApply(func() error { dbCalled = true; return dbErr }, func() error { discordCalled = true; return discordErr })
	if !dbCalled || !discordCalled {
		t.Fatalf("both compensations must be attempted: database=%v discord=%v", dbCalled, discordCalled)
	}
	if !errors.Is(gotDB, dbErr) || !errors.Is(gotDiscord, discordErr) {
		t.Fatalf("compensation failures not reported independently: database=%v discord=%v", gotDB, gotDiscord)
	}
}

func TestProductionNotificationCompensationStillAttemptsDiscordAfterDatabaseRestoreFailure(t *testing.T) {
	dbErr := errors.New("database restore failed")
	discordCalled := false
	gotDB, gotDiscord := compensateProductionNotificationApply(func() error { return dbErr }, func() error { discordCalled = true; return nil })
	if !discordCalled || !errors.Is(gotDB, dbErr) || gotDiscord != nil {
		t.Fatalf("unexpected compensation result: database=%v discord=%v called=%v", gotDB, gotDiscord, discordCalled)
	}
}

func TestProductionNotificationRecoveryFailuresReturnExplicitNonSuccessDiagnostics(t *testing.T) {
	databaseErr := errors.New("db recovery failed")
	discordErr := errors.New("discord recovery failed")
	for _, tc := range []struct {
		name    string
		db      error
		discord error
		key     string
	}{
		{name: "database", db: databaseErr, key: "database_recovery"},
		{name: "Discord order", discord: discordErr, key: "discord_order_recovery"},
		{name: "both", db: databaseErr, discord: discordErr, key: "database_recovery"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := productionNotificationRecoveryDiagnostic(tc.db, tc.discord)
			if status < 400 || body["recovery"] != "incomplete" || body[tc.key] != "failed" || body["error"] == "" {
				t.Fatalf("recovery failure was not a fail-closed diagnostic: status=%d body=%#v", status, body)
			}
		})
	}
}

func TestProductionNotificationApplyRejectsReviewerDeltaForDeletedRouteBeforeExternalReads(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "apply-deleted-delta", Name: "Apply Deleted Delta"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	oldTaskReader := reviewerTaskTypesForProduction
	reads := 0
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType { reads++; return nil }
	defer func() { reviewerTaskTypesForProduction = oldTaskReader }()
	body := []byte(`{"project_id":"apply-deleted-delta","expected_revision":"stale-but-present","routes":[],"reviewer_changes":[{"task_type_id":"removed-task","add_user_ids":["123456789012345678"]}]}`)
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	if !handleProductionNotificationApply(response, request, "en", db) {
		t.Fatal("Apply endpoint did not handle the request")
	}
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("reviewer_delta_for_removed_route")) {
		t.Fatalf("deleted-route reviewer delta was not explicitly rejected: status=%d body=%s", response.Code, response.Body.String())
	}
	if reads != 0 || len(model.ListProductionNotificationRoutes(db, project.KitsuProjectID)) != 0 || len(model.ListProjectReviewerTargets(db, project.ID)) != 0 {
		t.Fatalf("invalid delta caused external reads or writes: reads=%d routes=%#v targets=%#v", reads, model.ListProductionNotificationRoutes(db, project.KitsuProjectID), model.ListProjectReviewerTargets(db, project.ID))
	}
}

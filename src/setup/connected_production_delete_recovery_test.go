package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/gorm"
)

func addDeleteRecoveryFixture(t *testing.T, db *gorm.DB, productionID string) *model.Project {
	t.Helper()
	if err := model.CreateProject(db, productionID, "Delete Recovery", "kitsu", "", "", "en"); err != nil {
		t.Fatalf("create project: %v", err)
	}
	project := model.FindProjectByKitsuID(db, productionID)
	if project == nil {
		t.Fatal("project fixture missing")
	}
	db.Create(&model.ProductionChannelMapping{ProductionID: productionID, GuildID: "guild-1", TaskTypeID: "tt-1", ChannelID: "channel-delete-recovery", Active: true, State: model.ChannelMappingStateCurrent, MigrationState: model.ChannelMappingStateCurrent})
	db.Create(&model.ProductionNotificationConfig{ProductionID: productionID, ProductionName: project.Name, Enabled: true})
	db.Create(&model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: "tt-1", DestinationChannelID: "channel-delete-recovery"})
	return project
}

func TestStaleProductionDeleteBypassesOnlyRuntimeReadinessGate(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	const staleID = "stale-production-404"
	addDeleteRecoveryFixture(t, db, staleID)
	addDeleteRecoveryFixture(t, db, "unrelated-production")

	var projectReads int
	kitsu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"runtime-token"}`))
		case "/api/data/projects/" + staleID:
			projectReads++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer kitsu.Close()

	form := url.Values{
		"action":           {"delete_final"},
		"kitsu_project_id": {staleID},
		"project_name":     {"Delete Recovery"},
		"admin_email":      {"admin@example.test"},
		"admin_password":   {"test-password"},
	}
	request := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	Handler(kitsu.URL+"/", "", "", db, func() bool { return false }, nil)(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("stale Production delete returned %d: %s", response.Code, response.Body.String())
	}
	if projectReads != 0 {
		t.Fatalf("delete unexpectedly retried stale Kitsu Production reads: %d", projectReads)
	}
	assertStrongDeleteComplete(t, db, staleID)
	if model.FindProjectByKitsuID(db, "unrelated-production") == nil {
		t.Fatal("unrelated Production was removed")
	}
}

func TestConnectedProductionStrongDeleteHTTPStatusReflectsLocalCleanup(t *testing.T) {
	t.Run("healthy cleanup returns success and removes exact rows", func(t *testing.T) {
		project, db, _ := newStrongDeleteFixture(t, 1, true)
		installStrongDeleteTestSeams(t, func(string, string) error { return nil }, DeleteProjectConnectionOnly)

		form := url.Values{"action": {"execute_validated_channel_delete"}, "project_id": {project.KitsuProjectID}, "confirm_text": {"delete"}}
		request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?lang=en", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		AdminProjectsHandler(db, "guild-1", "bot-token")(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("healthy delete returned %d: %s", response.Code, response.Body.String())
		}
		assertStrongDeleteComplete(t, db, project.KitsuProjectID)
	})

	t.Run("local cleanup failure is not false HTTP success", func(t *testing.T) {
		project, db, _ := newStrongDeleteFixture(t, 1, true)
		installStrongDeleteTestSeams(t, func(string, string) error { return nil }, func(string, *gorm.DB) error {
			return gorm.ErrInvalidDB
		})

		form := url.Values{"action": {"execute_validated_channel_delete"}, "project_id": {project.KitsuProjectID}, "confirm_text": {"delete"}}
		request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?lang=en", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		AdminProjectsHandler(db, "guild-1", "bot-token")(response, request)

		if response.Code != http.StatusConflict {
			t.Fatalf("local cleanup failure returned %d instead of %d", response.Code, http.StatusConflict)
		}
		if model.FindProjectByKitsuID(db, project.KitsuProjectID) == nil {
			t.Fatal("project was removed despite simulated local cleanup failure")
		}
	})
}

package setup

import (
	"app/src/model"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestHasReadyProductionRoutingRequiresForumRoute(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:readiness-routing?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProductionForumConfig{}, &model.ProductionForumRoute{}, &model.ProductionForumStatusRole{}, &model.ProductionForumTag{}, &model.ForumBaseline{}, &model.ForumTask{}); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p1", "Same name", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p2", "Same name", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if hasReadyProductionRouting(db) {
		t.Fatal("connected Productions alone must not be ready")
	}
	settings := reviewerBrowserForumSettings("p1", "guild-1", []model.ProductionForumRoute{
		{ProductionID: "p1", TaskTypeID: "tt1", TaskTypeName: "Alerts", ForumChannelID: "channel-1", ForumChannelName: "alerts"},
	})
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatal(err)
	}
	if !hasReadyProductionRouting(db) {
		t.Fatal("valid forum routing should be ready")
	}
}

func TestSharedBotRuntimeReadinessIsSharedBySetupAndSettings(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:shared-readiness?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}, &model.Project{}, &model.ProductionForumConfig{}, &model.ProductionForumRoute{}, &model.ProductionForumStatusRole{}, &model.ProductionForumTag{}, &model.ForumBaseline{}, &model.ForumTask{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	previous := os.Getenv(RuntimeKitsuPasswordEnv)
	defer os.Setenv(RuntimeKitsuPasswordEnv, previous)
	_ = os.Setenv(RuntimeKitsuPasswordEnv, "configured-in-test")
	if got := sharedBotRuntimeReadiness(db, "https://kitsu.invalid", ""); got.OverallReady || got.DiscordConfigured {
		t.Fatalf("token-less shared readiness must be incomplete: %#v", got)
	}
	if err := setRuntimeKitsuToken(db, "test-token"); err != nil {
		t.Fatal(err)
	}
	if got := sharedBotRuntimeReadiness(db, "https://kitsu.invalid", "test-token"); got.OverallReady || !got.KitsuConfigured || !got.DiscordConfigured {
		t.Fatalf("connection prerequisites alone must remain blocked: %#v", got)
	}
	if err := model.CreateProject(db, "ready-production", "Ready Production", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	settings := reviewerBrowserForumSettings("ready-production", "guild-1", []model.ProductionForumRoute{
		{ProductionID: "ready-production", TaskTypeID: "task-type-1", TaskTypeName: "Alerts", ForumChannelID: "channel-1", ForumChannelName: "alerts"},
	})
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatal(err)
	}
	if got := sharedBotRuntimeReadiness(db, "https://kitsu.invalid", "test-token"); !got.OverallReady {
		t.Fatalf("valid Production routing should complete shared readiness: %#v", got)
	}
}

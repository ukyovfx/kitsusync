package setup

import (
	"app/src/model"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func readinessMatrixDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:readiness-matrix-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}, &model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSharedReadinessStateMatrix(t *testing.T) {
	cases := []struct {
		name               string
		host, email, token string
		projects, routes   bool
		want               ReadinessState
	}{
		{"kitsu missing", "", "", "", false, false, ReadinessSetupRequired},
		{"discord missing", "https://kitsu.invalid", "manager", "", false, false, ReadinessBotSetupRequired},
		{"production missing", "https://kitsu.invalid", "manager", "token", false, false, ReadinessProductionRequired},
		{"routes missing", "https://kitsu.invalid", "manager", "token", true, false, ReadinessRoutingRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := readinessMatrixDB(t)
			if tc.host != "" {
				model.SetSetting(db, "kitsu.hostname", tc.host)
			}
			if tc.email != "" {
				model.SetSetting(db, RuntimeKitsuEmailSettingKey, tc.email)
			}
			t.Setenv(RuntimeKitsuPasswordEnv, "configured")
			if tc.projects {
				_ = model.CreateProject(db, "p", "Production", "", "", "", "")
			}
			got := sharedBotRuntimeReadiness(db, tc.host, tc.token)
			if got.State != tc.want {
				t.Fatalf("state=%q want %q", got.State, tc.want)
			}
		})
	}
}

func TestSharedReadinessRequiresEnabledRoute(t *testing.T) {
	db := readinessMatrixDB(t)
	model.SetSetting(db, "kitsu.hostname", "https://kitsu.invalid")
	model.SetSetting(db, RuntimeKitsuEmailSettingKey, "manager")
	t.Setenv(RuntimeKitsuPasswordEnv, "configured")
	if err := model.CreateProject(db, "p", "Production", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := sharedBotRuntimeReadiness(db, "https://kitsu.invalid", "token"); got.State != ReadinessRoutingRequired {
		t.Fatalf("connected Production must remain blocked: %#v", got)
	}
}

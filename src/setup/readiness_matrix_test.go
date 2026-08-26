package setup

import (
	"app/src/model"
	"path/filepath"
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
		name                    string
		host, kitsuToken, token string
		projects, routes        bool
		want                    ReadinessState
	}{
		{"kitsu missing", "", "", "", false, false, ReadinessSetupRequired},
		{"discord missing", "https://kitsu.invalid", "kitsu-token", "", false, false, ReadinessBotSetupRequired},
		{"production missing", "https://kitsu.invalid", "kitsu-token", "token", false, false, ReadinessProductionRequired},
		{"routes missing", "https://kitsu.invalid", "kitsu-token", "token", true, false, ReadinessRoutingRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := readinessMatrixDB(t)
			t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
			if tc.host != "" {
				model.SetSetting(db, "kitsu.hostname", tc.host)
			}
			if tc.kitsuToken != "" {
				if err := setRuntimeKitsuToken(db, tc.kitsuToken); err != nil {
					t.Fatal(err)
				}
			}
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
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	if err := setRuntimeKitsuToken(db, "token"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p", "Production", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := sharedBotRuntimeReadiness(db, "https://kitsu.invalid", "token"); got.State != ReadinessRoutingRequired {
		t.Fatalf("connected Production must remain blocked: %#v", got)
	}
}

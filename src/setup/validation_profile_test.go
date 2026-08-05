package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/src/model"
	"app/src/utils/config"
)

func TestValidationOnlyProjectRendersReadOnlyRealData(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{
		KitsuProjectID: "real-production-id",
		Name:           "Real Production",
		ValidationOnly: true,
	}
	project.ValidationDataJSON = `{"task_types":[{"id":"task-type-1","name":"Animation"}],"participants":[{"id":"person-1","full_name":"Real Person","email":"person@example.invalid"}]}`
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	class, label, hint := iaStatus(db, project, "en")
	if class != "warning" || label != "Validation only" || !strings.Contains(hint, "No Discord server") {
		t.Fatalf("unexpected validation-only status: %q %q %q", class, label, hint)
	}
	for _, tab := range []string{"notifications", "danger-zone"} {
		r := httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=real-production-id&tab="+tab+"&lang=en", nil)
		w := httptest.NewRecorder()
		renderIAProductionList(w, r, db, "")
		body := w.Body.String()
		if !strings.Contains(body, "Validation only") || strings.Contains(body, "method=\"POST\"") {
			t.Fatalf("validation-only %s panel is not read-only", tab)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=real-production-id&tab=users&lang=en", nil)
	w := httptest.NewRecorder()
	renderIAProductionList(w, r, db, "")
	if body := w.Body.String(); !strings.Contains(body, "Real Person") || !strings.Contains(body, "Not linked") {
		t.Fatal("validation-only participants were not rendered")
	}
}

func TestFixtureConfigRequiresExplicitMode(t *testing.T) {
	t.Setenv("KITSUSYNC_FIXTURE_MODE", "")
	if FixtureModeEnabled() {
		t.Fatal("fixture mode enabled without explicit flag")
	}
	t.Setenv("KITSUSYNC_FIXTURE_MODE", "1")
	if !FixtureModeEnabled() {
		t.Fatal("fixture mode did not enable with explicit flag")
	}
}

func TestSampleConfigIsIgnoredUnlessFixtureModeIsExplicit(t *testing.T) {
	db := newIAViewDB(t)
	conf := config.Config{}
	conf.Mention.UserMap = []config.UserMapEntry{{KitsuName: "Sample User", DiscordID: "123456789012345678"}}
	conf.Mention.Checkers = []config.CheckerEntry{{TaskType: "Animation", DiscordID: "123456789012345679"}}
	t.Setenv("KITSUSYNC_FIXTURE_MODE", "")
	SeedConfigIfFixture(db, conf)
	var sample model.UserMap
	if db.Where("kitsu_name = ?", "Sample User").First(&sample).Error == nil {
		t.Fatal("sample user mapping populated without fixture mode")
	}
	t.Setenv("KITSUSYNC_FIXTURE_MODE", "1")
	SeedConfigIfFixture(db, conf)
	if db.Where("kitsu_name = ?", "Sample User").First(&sample).Error != nil {
		t.Fatal("fixture mode did not populate sample mapping")
	}
}

func TestValidationOnlyRejectsMutations(t *testing.T) {
	db := newIAViewDB(t)
	if err := db.Create(&model.Project{KitsuProjectID: "validation-mutation-production", Name: "Real Production", ValidationOnly: true}).Error; err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader("project_id=validation-mutation-production&action=remove_connection"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	AdminProjectsHandler(db, "", "")(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("validation-only mutation status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

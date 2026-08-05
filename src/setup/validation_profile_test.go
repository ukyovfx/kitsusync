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

func TestLiveProductionPreviewIsReadOnlyAndDoesNotPersist(t *testing.T) {
	db := newIAViewDB(t)
	preview := model.Project{
		KitsuProjectID:     "live-production-id",
		Name:               "Live Production",
		ReadOnlyPreview:    true,
		ValidationDataJSON: `{"task_types":[{"id":"task-type-1","name":"Animation"}],"participants":[{"id":"person-1","full_name":"Live Person"}]}`,
	}
	class, label, hint := iaStatus(db, preview, "en")
	if class != "warning" || label != "Not connected" || !strings.Contains(hint, "Notifications are unavailable") {
		t.Fatalf("unexpected live preview status: %q %q %q", class, label, hint)
	}
	for _, tab := range []string{"notifications", "users", "storage-settings", "danger-zone"} {
		w := httptest.NewRecorder()
		renderIASelectedProduction(w, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?tab="+tab+"&lang=en", nil), db, preview, "")
		body := w.Body.String()
		if strings.Contains(body, `method="POST"`) {
			t.Fatalf("live preview %s panel exposes a mutation form", tab)
		}
	}
	w := httptest.NewRecorder()
	renderIASelectedProduction(w, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang=en", nil), db, preview, "")
	if !strings.Contains(w.Body.String(), "Configure connection") || !strings.Contains(w.Body.String(), "Live Production") {
		t.Fatal("live preview did not expose the safe connection entry point")
	}
	var before, after int64
	if err := db.Model(&model.Project{}).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Project{}).Count(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("rendering live preview changed project rows from %d to %d", before, after)
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

func TestNormalStartupDoesNotSeedFixtureOrValidationData(t *testing.T) {
	t.Setenv("KITSUSYNC_FIXTURE_MODE", "")
	t.Setenv(validationOnlyEnv, "")
	if FixtureModeEnabled() || ValidationOnlyModeEnabled() {
		t.Fatal("normal startup unexpectedly enabled an isolated data mode")
	}
	db := newIAViewDB(t)
	SeedConfigIfFixture(db, config.Config{Mention: config.MentionConfig{UserMap: []config.UserMapEntry{{KitsuName: "Sample User", DiscordID: "synthetic"}}}})
	for _, table := range []string{"projects", "user_maps", "production_notification_routes", "production_channel_mappings", "audit_logs"} {
		var before, after int64
		if err := db.Table(table).Count(&before).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table(table).Count(&after).Error; err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("normal startup changed %s rows from %d to %d", table, before, after)
		}
	}
	var sample model.UserMap
	if db.Where("kitsu_name = ?", "Sample User").First(&sample).Error == nil {
		t.Fatal("normal startup imported a sample user")
	}
}

func TestLiveKitsuPersonsMatchActiveUserSourceRule(t *testing.T) {
	persons := []KitsuPerson{
		{FullName: "kitsu bot", Email: "kitsu-bot@example.invalid", Active: true},
		{FullName: "Super Admin", Email: "admin@example.invalid", Active: true},
		{FullName: "KitsuSync Bot", Email: "kitsusync-bot@google.com", Active: true, IsBot: true},
		{FullName: "Archived User", Email: "archived@example.invalid", Active: false},
	}
	visible := filterAssignablePersons(persons, "kitsusync-bot@google.com")
	if len(visible) != 2 {
		t.Fatalf("visible Kitsu user count = %d, want 2", len(visible))
	}
	for _, person := range visible {
		if person.FullName == "KitsuSync Bot" || person.FullName == "Archived User" {
			t.Fatalf("stale or inactive Kitsu person remained visible: %q", person.FullName)
		}
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

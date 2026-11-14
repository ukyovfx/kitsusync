package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func newForumPageDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newIAViewDB(t)
	// Close the SQLite file before the temp dir is removed (Windows keeps it locked).
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.ProductionForumConfig{}, &model.ProductionForumRoute{}, &model.ProductionForumStatusRole{}, &model.ProductionForumTag{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Project{KitsuProjectID: "p1", Name: "Synthetic Production", DiscordGuildID: "g1"}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

// stubForumLookups replaces the Kitsu and Discord lookups for one test.
func stubForumLookups(t *testing.T, discordErr error) {
	t.Helper()
	oldTypes, oldStatuses, oldForums := forumTaskTypeLister, forumStatusLister, forumChannelLister
	t.Cleanup(func() { forumTaskTypeLister, forumStatusLister, forumChannelLister = oldTypes, oldStatuses, oldForums })
	forumTaskTypeLister = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "tt-anim", Name: "Animation"}, {ID: "tt-comp", Name: "Compositing"}}
	}
	forumStatusLister = func(*gorm.DB) ([]kitsu.TaskStatus, error) {
		return []kitsu.TaskStatus{{ID: "s-wfa", Name: "Waiting For Approval"}, {ID: "s-retake", Name: "Retake"}, {ID: "s-done", Name: "Done"}, {ID: "s-wip", Name: "WIP"}}, nil
	}
	forumChannelLister = func(string, string) ([]forumChannelInfo, error) {
		if discordErr != nil {
			return nil, discordErr
		}
		tags := []forumTagInfo{{ID: "t1", Name: "WFA"}, {ID: "t2", Name: "Retake"}, {ID: "t3", Name: "Done"}, {ID: "t4", Name: "WIP"}}
		return []forumChannelInfo{{ID: "f-anim", Name: "animation", Tags: tags}, {ID: "f-comp", Name: "compositing", Tags: tags}}, nil
	}
}

func forumHandler(db *gorm.DB) http.HandlerFunc {
	return ForumSettingsHandler(db, func() (string, string) { return "bot-token", "g-fallback" })
}

func forumPost(t *testing.T, db *gorm.DB, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/bot/admin/forum?lang=en", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	forumHandler(db)(w, r)
	return w
}

func completeForumForm() url.Values {
	form := url.Values{"project": {"p1"}, "mode": {"forum"}, "guild_id": {"g1"}}
	form["tt_id"], form["tt_name"], form["tt_forum"] = []string{"tt-anim", "tt-comp"}, []string{"Animation", "Compositing"}, []string{"f-anim", "f-comp"}
	form["st_id"] = []string{"s-wfa", "s-retake", "s-done", "s-wip"}
	form["st_name"] = []string{"WFA", "Retake", "Done", "WIP"}
	form["st_role"] = []string{"wfa", "retake", "done", "wip"}
	for _, forum := range []string{"f-anim", "f-comp"} {
		for i, role := range []string{"wfa", "retake", "done", "wip"} {
			form.Add("tag_forum", forum)
			form.Add("tag_role", role)
			form.Add("tag_id", []string{"t1", "t2", "t3", "t4"}[i])
		}
	}
	return form
}

func TestForumSettingsPageRendersLookupsAndDefaultsToLane(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	for _, want := range []string{"Animation", "Compositing", `value="f-anim"`, "Waiting For Approval", `name="mode" value="lane" checked`} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
}

func TestForumSettingsSaveStoresSettingsAndSwitchesMode(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	w := forumPost(t, db, completeForumForm())
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	if got := model.NotificationModeForProduction(db, "p1"); got != model.NotificationModeForum {
		t.Fatalf("mode = %q, want forum", got)
	}
	loaded, ok, err := model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok || len(loaded.Routes) != 2 || len(loaded.StatusRoles) != 4 || len(loaded.Tags) != 8 {
		t.Fatalf("unexpected stored settings: ok=%v err=%v %+v", ok, err, loaded)
	}
}

func TestForumSettingsIncompleteForumModeIsRejectedWithReasons(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	form := completeForumForm()
	form["tag_id"] = make([]string, len(form["tag_id"])) // no tags chosen
	w := forumPost(t, db, form)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "has no tag for the role") {
		t.Fatal("the page must say what is missing")
	}
	if got := model.NotificationModeForProduction(db, "p1"); got != model.NotificationModeLane {
		t.Fatalf("a rejected save must stay in lane mode, got %q", got)
	}
}

func TestForumSettingsLaneDraftSavesWithoutTags(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	form := completeForumForm()
	form.Set("mode", "lane")
	delete(form, "tag_forum")
	delete(form, "tag_role")
	delete(form, "tag_id")
	if w := forumPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("a lane-mode draft must save, status = %d: %s", w.Code, w.Body.String())
	}
	if got := model.NotificationModeForProduction(db, "p1"); got != model.NotificationModeLane {
		t.Fatalf("mode = %q, want lane", got)
	}
}

func TestForumSettingsRepointedTaskTypeDropsStaleTagRows(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	form := completeForumForm()
	form["tt_forum"] = []string{"f-anim", "f-anim"} // f-comp is no longer routed, its tag rows remain in the form
	if w := forumPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	loaded, _, _ := model.LoadProductionForumSettings(db, "p1")
	if len(loaded.Tags) != 4 {
		t.Fatalf("tags of the unrouted forum must be dropped, got %d", len(loaded.Tags))
	}
}

func TestForumSettingsSaveWorksWhenDiscordLookupFails(t *testing.T) {
	stubForumLookups(t, forumTestError("discord down"))
	db := newForumPageDB(t)
	if w := forumPost(t, db, completeForumForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("saving must not depend on the Discord lookup, status = %d", w.Code)
	}
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	body := w.Body.String()
	if !strings.Contains(body, "Could not read the Discord forum list") {
		t.Fatal("the page must explain the failed Discord lookup")
	}
	if !strings.Contains(body, `<option value="f-anim" selected>f-anim</option>`) {
		t.Fatal("a saved forum must stay selected even when the live list is unavailable")
	}
}

func TestForumSettingsRejectsUnknownProductionAndValidationOnly(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=nope", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown Production status = %d", w.Code)
	}
	if err := db.Create(&model.Project{KitsuProjectID: "v1", Name: "Validation", ValidationOnly: true}).Error; err != nil {
		t.Fatal(err)
	}
	form := completeForumForm()
	form.Set("project", "v1")
	if w := forumPost(t, db, form); w.Code != http.StatusForbidden {
		t.Fatalf("validation-only Production must be read-only, status = %d", w.Code)
	}
	if got := model.NotificationModeForProduction(db, "v1"); got != model.NotificationModeLane {
		t.Fatalf("mode = %q", got)
	}
}

func TestForumSettingsEscapesUntrustedNames(t *testing.T) {
	stubForumLookups(t, nil)
	forumTaskTypeLister = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "tt-x", Name: `<script>alert(1)</script>`}}
	}
	db := newForumPageDB(t)
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	if strings.Contains(w.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("Kitsu names must be HTML-escaped")
	}
}

func TestNotificationsTabLinksToForumSettings(t *testing.T) {
	db := newForumPageDB(t)
	var project model.Project
	db.Where("kitsu_project_id = ?", "p1").First(&project)
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=p1&tab=notifications&lang=en", nil)
	got := renderForumSettingsLink(db, r, project, "en")
	if !strings.Contains(got, "/bot/admin/forum?") || !strings.Contains(got, "project=p1") || !strings.Contains(got, "Lane (current)") {
		t.Fatalf("link missing or wrong mode: %s", got)
	}
}

type forumTestError string

func (e forumTestError) Error() string { return string(e) }

func TestForumSettingsErrorsAreWordedInJapanese(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	form := completeForumForm()
	form["tag_id"] = make([]string, len(form["tag_id"]))
	form["st_role"] = []string{"wfa", "retake", "", ""} // no status for Done
	r := httptest.NewRequest(http.MethodPost, "/bot/admin/forum?lang=ja", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	forumHandler(db)(w, r)
	body := w.Body.String()
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	for _, want := range []string{"保存できませんでした", "notice notice-error", "役割「完了」に割り当てるステータスがありません。", "のタグがありません。", "フォーラム「animation」に"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Japanese message missing %q", want)
		}
	}
	for _, english := range []string{"has no tag for", "forum mode needs", "invalid forum settings"} {
		if strings.Contains(body, english) {
			t.Fatalf("English validation text %q leaked into the Japanese page", english)
		}
	}
}

func TestForumSettingsEveryProblemCodeHasLocalizedText(t *testing.T) {
	codes := []string{model.ProblemUnknownMode, model.ProblemProductionRequired, model.ProblemRouteIncomplete, model.ProblemTaskTypeDuplicate, model.ProblemStatusNoRole, model.ProblemStatusDuplicate, model.ProblemTagUnroutedForum, model.ProblemTagInvalid, model.ProblemTagRoleDuplicate, model.ProblemTagReused, model.ProblemGuildRequired, model.ProblemRoutesRequired, model.ProblemRoleStatusMissing, model.ProblemForumTagMissing}
	for _, code := range codes {
		problem := model.ForumSettingsProblem{Code: code, Args: []string{"x", "wfa"}}
		for _, lang := range []string{"ja", "en"} {
			got := forumProblemText(lang, problem, nil)
			if strings.TrimSpace(got) == "" || got == problem.English() && lang == "ja" {
				t.Fatalf("code %s has no %s text: %q", code, lang, got)
			}
			if lang == "en" && strings.ContainsAny(got, "あいうえおのをがは") {
				t.Fatalf("code %s English text contains Japanese: %q", code, got)
			}
		}
	}
}

func TestForumSettingsInternalErrorsAreNotShownVerbatim(t *testing.T) {
	got := forumSaveErrorMarkup("en", forumTestError("UNIQUE constraint failed: secret table detail"), nil)
	if strings.Contains(got, "UNIQUE") || strings.Contains(got, "secret") {
		t.Fatalf("internal error leaked: %s", got)
	}
}

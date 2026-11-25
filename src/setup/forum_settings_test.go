package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"app/src/api/kitsu"
	"app/src/model"
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
	if err := db.AutoMigrate(&model.ProductionForumConfig{}, &model.ProductionForumRoute{}, &model.ProductionForumStatusRole{}, &model.ProductionForumTag{}, &model.ProductionHandoffConfig{}, &model.ProductionHandoffProductType{}, &model.ProductionHandoffRoute{}, &model.ForumBaseline{}, &model.ForumTask{}, &model.StudioStatusPolicy{}, &model.StudioTaskRoute{}, &model.ProductionTaskRoute{}, &model.AdminException{}, &model.NotificationDecision{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Project{KitsuProjectID: "p1", Name: "Synthetic Production", DiscordGuildID: "g1"}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

// stubForumLookups replaces the Kitsu and Discord lookups for one test. Saves
// read the forum list through forumChannelLister; page rendering reads the
// guild channel cache, so both seams are replaced together.
func stubForumLookups(t *testing.T, discordErr error) {
	t.Helper()
	oldTypes, oldStatuses, oldForums, oldCache := forumTaskTypeLister, forumStatusLister, forumChannelLister, guildChannelListCache
	t.Cleanup(func() {
		forumTaskTypeLister, forumStatusLister, forumChannelLister, guildChannelListCache = oldTypes, oldStatuses, oldForums, oldCache
	})
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
	body := []byte(`[{"id":"f-anim","name":"animation","type":15,"available_tags":[{"id":"t1","name":"WFA"},{"id":"t2","name":"Retake"},{"id":"t3","name":"Done"},{"id":"t4","name":"WIP"}]},{"id":"f-comp","name":"compositing","type":15,"available_tags":[{"id":"t1","name":"WFA"},{"id":"t2","name":"Retake"},{"id":"t3","name":"Done"},{"id":"t4","name":"WIP"}]}]`)
	cache := &guildChannelsCache{
		ttl:     guildChannelCacheTTL,
		entries: map[string]*guildChannelCacheEntry{},
		loads:   map[string]*guildChannelLoad{},
		fetch: func(guildID, botToken string) ([]byte, int, error) {
			if discordErr != nil {
				return nil, http.StatusServiceUnavailable, discordErr
			}
			return body, http.StatusOK, nil
		},
		now: time.Now,
	}
	if discordErr == nil {
		// The page fixture project points at guild "g1"; serve the same list
		// the old forumChannelLister stub returned, already cached.
		cache.entries["g1"] = &guildChannelCacheEntry{body: body, fetchedAt: time.Now()}
	}
	guildChannelListCache = cache
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
	return form
}

// The forum page keeps only the routing section: the status -> role mapping
// moved to the Bot settings page and the comment notification toggle was
// abolished (redesign front half, 2026-11-23).
func TestForumSettingsPageRendersTwoSectionsWithoutTagAssignment(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(body, "<h2>Task Type → forum</h2>") {
		t.Fatal("page is missing the routing section")
	}
	for _, gone := range []string{"Kitsu status → role", "Comment notifications", `name="st_role"`, `name="comment_notify_assignee"`} {
		if strings.Contains(body, gone) {
			t.Fatalf("page still carries the removed section %q", gone)
		}
	}
	if strings.Contains(body, "Role → forum tag") || strings.Contains(body, `name="tag_id"`) || strings.Contains(body, `name="tag_forum"`) {
		t.Fatal("the per-forum tag assignment section must be gone from the page")
	}
	if !strings.Contains(body, `name="guild_id"`) {
		t.Fatal("the Discord server ID field must stay on the page")
	}
}

// The template pre-fill of status roles now lives on the Bot settings page
// section; see status_roles_section_test.go.

func TestForumSettingsPageFetchesStatusesOnce(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	calls := 0
	oldStatuses := forumStatusLister
	t.Cleanup(func() { forumStatusLister = oldStatuses })
	forumStatusLister = func(*gorm.DB) ([]kitsu.TaskStatus, error) {
		calls++
		return []kitsu.TaskStatus{{ID: "s-wfa", Name: "Waiting For Approval"}, {ID: "s-retake", Name: "Retake"}, {ID: "s-done", Name: "Done"}}, nil
	}
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if calls != 1 {
		t.Fatalf("status list fetched %d times, want 1", calls)
	}
}

func TestForumSettingsSaveStoresSettingsAndSwitchesMode(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	w := forumPost(t, db, completeForumForm())
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	loaded, ok, err := model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok || len(loaded.Routes) != 2 || len(loaded.StatusRoles) != 4 || len(loaded.Tags) != 8 {
		t.Fatalf("unexpected stored settings: ok=%v err=%v %+v", ok, err, loaded)
	}
	// The tag mapping is derived from the forums' own tag names, not the form.
	got := map[string]string{}
	for _, tag := range loaded.Tags {
		got[tag.ForumChannelID+"|"+tag.Role] = tag.TagID
	}
	for _, forum := range []string{"f-anim", "f-comp"} {
		for role, tagID := range map[string]string{"wfa": "t1", "retake": "t2", "done": "t3", "wip": "t4"} {
			if got[forum+"|"+role] != tagID {
				t.Fatalf("derived tag %s|%s = %q, want %q", forum, role, got[forum+"|"+role], tagID)
			}
		}
	}
}

func TestForumSettingsForumMissingStandardTagsIsRejectedWithReasons(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	// The compositing forum only carries a WFA tag, so the Retake and Done
	// mappings cannot be derived from its tag names.
	oldForums := forumChannelLister
	t.Cleanup(func() { forumChannelLister = oldForums })
	forumChannelLister = func(string, string) ([]forumChannelInfo, error) {
		return []forumChannelInfo{
			{ID: "f-anim", Name: "animation", Tags: []forumTagInfo{{ID: "t1", Name: "WFA"}, {ID: "t2", Name: "Retake"}, {ID: "t3", Name: "Done"}, {ID: "t4", Name: "WIP"}}},
			{ID: "f-comp", Name: "compositing", Tags: []forumTagInfo{{ID: "c1", Name: "WFA"}}},
		}, nil
	}
	w := forumPost(t, db, completeForumForm())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "has no tag for the role") || !strings.Contains(body, "compositing") {
		t.Fatal("the page must say which forum is missing which role's tag")
	}
	if strings.Contains(body, `name="tag_id"`) {
		t.Fatal("the refusal must not bring the tag assignment inputs back")
	}
	if _, ok, _ := model.LoadProductionForumSettings(db, "p1"); ok {
		t.Fatal("a refused save must not store anything")
	}
}

func TestForumSettingsRepointedTaskTypeDropsStaleTagRows(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	form := completeForumForm()
	form["tt_forum"] = []string{"f-anim", "f-anim"} // f-comp is no longer routed
	if w := forumPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	loaded, _, _ := model.LoadProductionForumSettings(db, "p1")
	if len(loaded.Tags) != 4 {
		t.Fatalf("tags of the unrouted forum must be dropped, got %d", len(loaded.Tags))
	}
}

// A Task Type pointed at a forum without the standard tags must be refused
// with the forum and roles named; the page offers no tag inputs to fill.
func TestForumSettingsRefusalNamesNewlyRoutedForumTags(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	form := completeForumForm()
	form["tt_forum"] = []string{"f-anim", "f-new"} // f-new has no standard tags
	w := forumPost(t, db, form)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "has no tag for the role") || !strings.Contains(body, "f-new") {
		t.Fatal("the page must say which forum's tags are missing")
	}
	if strings.Contains(body, `name="tag_id"`) {
		t.Fatal("the page must not render tag assignment inputs")
	}
}

func TestForumSettingsSaveWorksWhenDiscordLookupFails(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	if w := forumPost(t, db, completeForumForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("seed save failed: %d", w.Code)
	}
	// Discord is now unreachable. Re-saving the page exactly as it is rendered
	// must keep the saved tag mapping instead of refusing the save.
	stubForumLookups(t, forumTestError("discord down"))
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	if !strings.Contains(w.Body.String(), "Could not read the Discord forum list") {
		t.Fatal("the page must explain the failed Discord lookup")
	}
	if !strings.Contains(w.Body.String(), `<option value="f-anim" selected>f-anim</option>`) {
		t.Fatal("a saved forum must stay selected even when the live list is unavailable")
	}
	if resp := forumPost(t, db, completeForumForm()); resp.Code != http.StatusSeeOther {
		t.Fatalf("re-save with Discord down must keep the saved tags, status = %d: %s", resp.Code, resp.Body.String())
	}
	loaded, _, _ := model.LoadProductionForumSettings(db, "p1")
	if len(loaded.Tags) != 8 {
		t.Fatalf("saved tags were lost: %d left", len(loaded.Tags))
	}
}

func TestForumSettingsSaveWithoutDiscordListAndNoSavedTagsIsRefused(t *testing.T) {
	stubForumLookups(t, forumTestError("discord down"))
	db := newForumPageDB(t)
	w := forumPost(t, db, completeForumForm())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Could not read the Discord forum list", "needs the Discord forum list"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the refusal must explain the missing forum list, missing %q in: %s", want, body)
		}
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
	if !strings.Contains(got, "/bot/admin/forum?") || !strings.Contains(got, "project=p1") {
		t.Fatalf("link to forum settings missing: %s", got)
	}
}

type forumTestError string

func (e forumTestError) Error() string { return string(e) }

func TestForumSettingsErrorsAreWordedInJapanese(t *testing.T) {
	stubForumLookups(t, nil)
	// The compositing forum only carries a WFA tag, so deriving its Retake and
	// Done mappings fails alongside the missing Done status role.
	oldForums := forumChannelLister
	t.Cleanup(func() { forumChannelLister = oldForums })
	forumChannelLister = func(string, string) ([]forumChannelInfo, error) {
		return []forumChannelInfo{
			{ID: "f-anim", Name: "animation", Tags: []forumTagInfo{{ID: "t1", Name: "WFA"}, {ID: "t2", Name: "Retake"}, {ID: "t3", Name: "Done"}, {ID: "t4", Name: "WIP"}}},
			{ID: "f-comp", Name: "compositing", Tags: []forumTagInfo{{ID: "c1", Name: "WFA"}}},
		}, nil
	}
	db := newForumPageDB(t)
	form := completeForumForm()
	r := httptest.NewRequest(http.MethodPost, "/bot/admin/forum?lang=ja", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	forumHandler(db)(w, r)
	body := w.Body.String()
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	for _, want := range []string{"保存できませんでした", "notice notice-error", "のタグがありません。", "フォーラム「compositing」に"} {
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
	codes := []string{model.ProblemProductionRequired, model.ProblemRouteIncomplete, model.ProblemTaskTypeDuplicate, model.ProblemStatusNoRole, model.ProblemStatusDuplicate, model.ProblemTagUnroutedForum, model.ProblemTagInvalid, model.ProblemTagRoleDuplicate, model.ProblemTagReused, model.ProblemGuildRequired, model.ProblemRoutesRequired, model.ProblemRoleStatusMissing, model.ProblemForumTagMissing, model.ProblemForumListUnavailable}
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

func TestForumSettingsStatusListFailureKeepsSavedStatusRoles(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	if w := forumPost(t, db, completeForumForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("seed save failed: %d", w.Code)
	}
	for _, p := range []struct{ id, name, nature string }{
		{"s-wfa", "Waiting For Approval", model.StatusNatureReview},
		{"s-retake", "Retake", model.StatusNatureRetake},
		{"s-done", "Done", model.StatusNatureDone},
		{"s-wip", "WIP", model.StatusNatureWIP},
	} {
		if err := model.SetStudioStatusPolicyNature(db, p.id, p.name, p.nature); err != nil {
			t.Fatalf("seed policy: %v", err)
		}
	}
	// Kitsu's status list is now unreachable. Re-saving the page exactly as it is
	// rendered must not erase the saved status roles: the forum page carries
	// them over, and the Bot page section keeps showing the saved studio-wide rows.
	forumStatusLister = func(*gorm.DB) ([]kitsu.TaskStatus, error) { return nil, forumTestError("kitsu down") }
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	body := w.Body.String()
	if !strings.Contains(body, "Saved status settings are kept") {
		t.Fatal("the page must say the saved status settings are kept")
	}
	section := renderStatusRolesSection(db, "en", httptest.NewRequest(http.MethodGet, "/bot/admin/bot/statuses?lang=en", nil))
	for _, id := range []string{"s-wfa", "s-retake", "s-done", "s-wip"} {
		if !strings.Contains(section, `name="srole_id" value="`+id+`"`) {
			t.Fatalf("saved status %s must stay in the Bot page section while Kitsu is unreachable", id)
		}
	}
	if !strings.Contains(section, `data-role-box="done"`) || !strings.Contains(section, `data-role-chip="s-done"`) {
		t.Fatal("saved roles must stay placed in their role boxes in the Bot page section")
	}

	// Submit the same values the page rendered.
	form := completeForumForm()
	if resp := forumPost(t, db, form); resp.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", resp.Code)
	}
	loaded, _, _ := model.LoadProductionForumSettings(db, "p1")
	if len(loaded.StatusRoles) != 4 {
		t.Fatalf("saved status roles were lost: %d left", len(loaded.StatusRoles))
	}
}

func TestForumSettingsKeepsSavedStatusMissingFromLiveList(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	forumPost(t, db, completeForumForm())
	for _, p := range []struct{ id, name, nature string }{
		{"s-wfa", "Waiting For Approval", model.StatusNatureReview},
		{"s-retake", "Retake", model.StatusNatureRetake},
		{"s-done", "Done", model.StatusNatureDone},
		{"s-wip", "WIP", model.StatusNatureWIP},
	} {
		if err := model.SetStudioStatusPolicyNature(db, p.id, p.name, p.nature); err != nil {
			t.Fatalf("seed policy: %v", err)
		}
	}
	forumStatusLister = func(*gorm.DB) ([]kitsu.TaskStatus, error) {
		return []kitsu.TaskStatus{{ID: "s-wfa", Name: "Waiting For Approval"}}, nil // others vanished in Kitsu
	}
	section := renderStatusRolesSection(db, "en", httptest.NewRequest(http.MethodGet, "/bot/admin/bot/statuses?lang=en", nil))
	for _, id := range []string{"s-retake", "s-done", "s-wip"} {
		if !strings.Contains(section, `name="srole_id" value="`+id+`"`) {
			t.Fatalf("saved status %s missing from the live list must stay in the Bot page section", id)
		}
	}
}

func TestForumSettingsGlobalValidationOnlyFlagRefusesWrites(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	t.Setenv(validationOnlyEnv, "1")
	if w := forumPost(t, db, completeForumForm()); w.Code != http.StatusForbidden {
		t.Fatalf("a global validation-only profile must refuse writes, status = %d", w.Code)
	}
	if _, ok, _ := model.LoadProductionForumSettings(db, "p1"); ok {
		t.Fatal("nothing may be stored")
	}
	// Reading stays possible.
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("read status = %d", w.Code)
	}
}

func TestForumSettingsSavesWithoutAnyKitsuOrDiscordLookup(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	if w := forumPost(t, db, completeForumForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("seed save failed: %d", w.Code)
	}
	// Every lookup is now down. Re-saving the page exactly as it is rendered
	// must keep the saved routes, status roles and derived tags.
	oldTypes, oldStatuses, oldForums := forumTaskTypeLister, forumStatusLister, forumChannelLister
	t.Cleanup(func() { forumTaskTypeLister, forumStatusLister, forumChannelLister = oldTypes, oldStatuses, oldForums })
	forumTaskTypeLister = func(*gorm.DB, string) []kitsu.TaskType { return nil }
	forumStatusLister = func(*gorm.DB) ([]kitsu.TaskStatus, error) { return nil, forumTestError("not connected") }
	forumChannelLister = func(string, string) ([]forumChannelInfo, error) { return nil, forumTestError("not connected") }
	if resp := forumPost(t, db, completeForumForm()); resp.Code != http.StatusSeeOther {
		t.Fatalf("re-saving must not need Kitsu or Discord, status = %d: %s", resp.Code, resp.Body.String())
	}
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `name="tt_id" value="tt-anim"`) {
		t.Fatalf("saved rows must render with every lookup down (status %d)", w.Code)
	}
	loaded, _, _ := model.LoadProductionForumSettings(db, "p1")
	if len(loaded.Routes) != 2 || len(loaded.StatusRoles) != 4 || len(loaded.Tags) != 8 {
		t.Fatalf("re-save with every lookup down lost settings: %+v", loaded)
	}
}

func TestForumSettingsOfflineReuseRefusedAfterGuildChange(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	if w := forumPost(t, db, completeForumForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("seed save failed: %d", w.Code)
	}
	// Discord is unreachable and the operator changes the server ID in the
	// same save. The saved tag rows belong to the old guild, so they must not
	// be reused under the new guild ID.
	stubForumLookups(t, forumTestError("discord down"))
	form := completeForumForm()
	form.Set("guild_id", "g2")
	w := forumPost(t, db, form)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	loaded, ok, _ := model.LoadProductionForumSettings(db, "p1")
	if !ok {
		t.Fatal("the refused save must not delete the saved settings")
	}
	if loaded.Config.GuildID != "g1" {
		t.Fatalf("saved guild = %q, want g1: the refused save must not store the new guild with the old guild's tags", loaded.Config.GuildID)
	}
}

func TestForumSettingsCreateButtonVisibility(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)

	getPage := func() string {
		t.Helper()
		w := httptest.NewRecorder()
		forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		return w.Body.String()
	}

	// 1. No routes assigned yet -> button must be present.
	if body := getPage(); !strings.Contains(body, `value="create_forums"`) {
		t.Fatal("create_forums button must be rendered when a task type has no forum assigned")
	}

	// 2. All task types assigned and no failure mark -> button must be omitted, Save kept.
	if w := forumPost(t, db, completeForumForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("seed save failed: %d: %s", w.Code, w.Body.String())
	}
	body := getPage()
	if strings.Contains(body, `value="create_forums"`) {
		t.Fatal("create_forums button must be omitted when all task types have forums and there is no failure mark")
	}
	if !strings.Contains(body, `<button type="submit" class="btn-ghost">Save</button>`) {
		t.Fatal("Save button must remain rendered")
	}

	// 3. Auto-create failure mark present -> button must be rendered even when all task types have forums.
	model.SetSetting(db, forumAutoCreateCheckKey("p1"), "previous auto-create failed")
	if body := getPage(); !strings.Contains(body, `value="create_forums"`) {
		t.Fatal("create_forums button must be rendered when auto-create failure mark is present")
	}
}

func TestForumSettingsEmptyTaskTypesHidesEmptyTable(t *testing.T) {
	stubForumLookups(t, nil)
	forumTaskTypeLister = func(*gorm.DB, string) []kitsu.TaskType { return nil }
	db := newForumPageDB(t)
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, `class="data-table forum-map-table"`) {
		t.Fatal("forum page must not render an empty mapping table when there are no task-type rows")
	}
	if !strings.Contains(body, "No task types were found for this Production") {
		t.Fatal("forum page must render an empty-state message when there are no task-type rows")
	}
}

func TestForumSettingsTagTableVisibility(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	model.SetSetting(db, forumAutoCreateCheckKey("p1"), "previous auto-create failed")

	renderPanel := func() (string, string) {
		t.Helper()
		w := httptest.NewRecorder()
		forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=ja", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		body := w.Body.String()
		start := strings.Index(body, `id="forum-result"`)
		if start < 0 {
			t.Fatalf("missing forum-result panel: %s", body)
		}
		rest := body[start:]
		return body, rest[:strings.Index(rest, "</section>")]
	}

	// 1. No forums created yet -> summary and create button stay, tag table omitted.
	full, panel := renderPanel()
	if !strings.Contains(panel, "作成結果と現在の状態") || !strings.Contains(full, `value="create_forums"`) {
		t.Fatalf("summary or create button missing when no forums exist")
	}
	if strings.Contains(panel, "<table") || strings.Contains(panel, "なし") {
		t.Fatalf("tag table and なし pills must not render when no forums exist: %s", panel)
	}

	// 2. At least one forum exists -> table renders with あり for present tags,
	// empty cells for missing tags, and 未作成 only in the フォーラム column.
	if err := db.Create(&model.ProductionForumConfig{ProductionID: "p1", GuildID: "g1"}).Error; err != nil {
		t.Fatalf("create config: %v", err)
	}
	if err := db.Create(&model.ProductionForumRoute{ProductionID: "p1", TaskTypeID: "tt-anim", TaskTypeName: "Animation", ForumChannelID: "f-anim", ForumChannelName: "animation"}).Error; err != nil {
		t.Fatalf("create route: %v", err)
	}
	if err := db.Create(&model.ProductionForumTag{ProductionID: "p1", ForumChannelID: "f-anim", Role: model.ForumRoleWFA, TagID: "t1"}).Error; err != nil {
		t.Fatalf("create tag: %v", err)
	}
	_, panel = renderPanel()
	if !strings.Contains(panel, "<table") {
		t.Fatalf("tag table must render when at least one forum exists: %s", panel)
	}
	if strings.Contains(panel, "なし") {
		t.Fatalf("missing tags must render as empty cells without なし pill: %s", panel)
	}
	if strings.Count(panel, `status-pill success">あり`) != 1 {
		t.Fatalf("expected 1 あり pill, got panel: %s", panel)
	}
	if strings.Count(panel, `status-pill warning">未作成`) != 1 {
		t.Fatalf("expected 1 未作成 marker in forum column, got panel: %s", panel)
	}
	if strings.Count(panel, `<td></td>`) != 7 {
		t.Fatalf("expected 7 empty tag cells (3 on Animation + 4 on Compositing), got panel: %s", panel)
	}
}

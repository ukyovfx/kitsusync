package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
)

// settingsSourceFiles returns the non-test Go sources of this package, so a
// rule about user-facing wording or markup can be checked in one place.
func settingsSourceFiles(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(body)
	}
	return files
}

// The wiring page has task types down the side and one checkbox per
// receiving task type, in a studio table and a per-Production table.
func TestHandoffRoutesTableRendersBothLayers(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	if err := model.SaveStudioTaskRoute(db, model.StudioTaskRoute{FromTaskTypeID: "tt-lay", FromTaskTypeName: "Layout", NotifyTaskTypeIDs: "tt-anim,tt-light"}); err != nil {
		t.Fatal(err)
	}
	body := handoffGet(t, db).Body.String()
	if got := strings.Count(body, `<table class="data-table handoff-routes">`); got != 2 {
		t.Fatalf("routes tables = %d, want 2 (studio + production)", got)
	}
	for _, taskType := range handoffTaskTypes() {
		if !strings.Contains(body, `<th scope="row">`+taskType.Name+`</th>`) {
			t.Fatalf("row header %q missing", taskType.Name)
		}
	}
	if !strings.Contains(body, `name="st_notify_tt-lay" value="tt-anim" checked`) || !strings.Contains(body, `name="st_notify_tt-lay" value="tt-light" checked`) {
		t.Fatal("saved studio wiring did not render checked")
	}
	if strings.Contains(body, `name="st_notify_tt-lay" value="tt-comp" checked`) {
		t.Fatal("an unwired receiver rendered checked")
	}
	// The production table shows what it inherits.
	if !strings.Contains(body, "Animation, Lighting") {
		t.Fatal("the production table must show the inherited wiring")
	}
}

func TestHandoffRoutesSaveRoundTrip(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	form := url.Values{"project": {"p1"}, "no_assignee": {"admin"}}
	form["st_task"] = []string{"tt-lay"}
	form["st_name"] = []string{"Layout"}
	form["st_notify_tt-lay"] = []string{"tt-anim"}
	if w := handoffPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d: %s", w.Code, w.Body.String())
	}
	body := handoffGet(t, db).Body.String()
	if !strings.Contains(body, `name="st_notify_tt-lay" value="tt-anim" checked`) || strings.Contains(body, `name="st_notify_tt-lay" value="tt-comp" checked`) {
		t.Fatalf("saved wiring did not reload as saved: %s", body)
	}
}

func notificationsPanelFixture(t *testing.T, query string) string {
	t.Helper()
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "focus-production", Name: "Focus Production", Language: "en", DiscordGuildID: "123456789012345678"}
	db.Create(&p)
	if err := model.SaveProductionForumSettings(db, reviewerBrowserForumSettings(p.KitsuProjectID, p.DiscordGuildID,
		[]model.ProductionForumRoute{{ProductionID: p.KitsuProjectID, TaskTypeID: "compositing", TaskTypeName: "Compositing", ForumChannelID: "forum-comp", ForumChannelName: "compositing"}})); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=focus-production&tab=notifications&lang=en"+query, nil)
	return renderSelectedProductionPanel(db, r, p, "en", "notifications", "ok", "Healthy", "")
}

// While one section (the WFA recipients) is being edited, the other sections of
// the tab are hidden; outside editing all three are shown.
func TestNotificationsEditingShowsOnlyTheEditedSection(t *testing.T) {
	reading := notificationsPanelFixture(t, "")
	for _, want := range []string{"Forum notifications", "Material handoff", `id="wfa-recipients"`} {
		if !strings.Contains(reading, want) {
			t.Fatalf("read view missing %q", want)
		}
	}
	editing := notificationsPanelFixture(t, "&edit_routing=1")
	for _, hidden := range []string{"Forum notifications", "Open forum settings", "Material handoff", "Open handoff mapping"} {
		if strings.Contains(editing, hidden) {
			t.Fatalf("editing view still shows %q", hidden)
		}
	}
	if !strings.Contains(editing, `class="production-routing-editor"`) {
		t.Fatalf("editing view lost the section being edited: %s", editing)
	}
}

// Keyboard focus must be visible on every input, select and textarea, whatever
// a local rule did to the control's border or shadow.
func TestFocusRingIsAlwaysVisibleOnFormControls(t *testing.T) {
	css := adminThemeCSS
	last := css[strings.LastIndex(css, "Keyboard focus is always visible"):]
	for _, want := range []string{
		"input:focus-visible,select:focus-visible,textarea:focus-visible{outline:2px solid var(--border-focus)!important;outline-offset:2px!important}",
		"select:focus-visible,textarea:focus-visible{box-shadow:0 0 0 3px rgba(109,195,255,.35)!important}",
	} {
		if !strings.Contains(last, want) {
			t.Fatalf("final focus rule missing %q", want)
		}
	}
	// No rule after the focus rule may switch the outline off again.
	if strings.Contains(last, "outline:none") || strings.Contains(last, "outline:0") {
		t.Fatal("a rule after the focus rule removes the outline")
	}
	// The control's own rules never use !important to flatten the shadow, so the
	// focus rule above always wins.
	if regexp.MustCompile(`(input|select|textarea)[^{]*:focus[^{]*\{[^}]*(outline|box-shadow):\s*none\s*!important`).MatchString(css) {
		t.Fatal("a rule forces focus styles off with !important")
	}
}

// Between 761px and 1100px the team table keeps room for the @name and has no
// action column that could be clipped at the right edge.
func TestTeamTableBandKeepsDiscordColumnReadable(t *testing.T) {
	css := adminThemeCSS
	band := "@media(min-width:761px) and (max-width:1100px){"
	i := strings.Index(css, band)
	if i < 0 {
		t.Fatal("team table band rule is missing")
	}
	rule := css[i : i+strings.Index(css[i:], "\n")]
	for _, want := range []string{"production-team-col-role{width:15%}", "production-team-col-discord{width:28%}", "production-team-col-status{width:17%}"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("band rule missing %q: %s", want, rule)
		}
	}
	if strings.Contains(css, "production-team-col-actions") {
		// The column group no longer has an actions column; leftover widths would be dead CSS.
		t.Log("note: leftover production-team-col-actions rule")
	}
}

// Every confirmation uses the one shared dialog: no native window.confirm is
// left, scripts reach the dialog through kitsusyncConfirm, the cancel button
// comes before the danger-coloured confirm button, and the wording is shared.
func TestConfirmationsUseTheSharedDialog(t *testing.T) {
	for name, body := range settingsSourceFiles(t) {
		if strings.Contains(body, "window.confirm") || regexp.MustCompile(`[^.\w]confirm\(`).MatchString(body) {
			t.Errorf("%s still uses a native confirm", name)
		}
	}
	for _, lang := range []string{"ja", "en"} {
		js := baseAdminJS(lang)
		for _, want := range []string{"window.kitsusyncConfirm = function", "openConfirm(", "activeCallback"} {
			if !strings.Contains(js, want) {
				t.Fatalf("%s: shared dialog script missing %q", lang, want)
			}
		}
	}
	files := settingsSourceFiles(t)
	var modal string
	for _, body := range files {
		if i := strings.Index(body, `id="deleteModal"`); i >= 0 {
			modal = body[i : i+strings.Index(body[i:], `</div></div></div>`)]
		}
	}
	if modal == "" {
		t.Fatal("shared confirmation dialog markup not found")
	}
	cancel, confirm := strings.Index(modal, `id="deleteCancelBtn"`), strings.Index(modal, `id="deleteConfirmBtn"`)
	if cancel < 0 || confirm < 0 || cancel > confirm {
		t.Fatalf("cancel must come before confirm in the dialog: %s", modal)
	}
	if !strings.Contains(modal, `id="deleteConfirmBtn" type="button" class="btn-danger"`) {
		t.Fatal("the confirm button must use the danger colour")
	}
	if !strings.Contains(files["pr199_user_linking_render.go"], "window.kitsusyncConfirm(unlink.dataset.confirm") {
		t.Fatal("the unlink action does not use the shared dialog")
	}
}

// Back / cancel comes first and the main action last, on every screen.
func TestButtonOrderIsBackThenPrimary(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	w := httptest.NewRecorder()
	forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	forum := w.Body.String()
	order := func(name, body string, markers ...string) {
		t.Helper()
		last := -1
		for _, marker := range markers {
			i := strings.Index(body, marker)
			if i < 0 {
				t.Fatalf("%s: %q missing", name, marker)
			}
			if i < last {
				t.Fatalf("%s: %q is out of order (back → secondary → primary expected)", name, marker)
			}
			last = i
		}
	}
	order("forum settings", forum, "Back to notifications", `<button type="submit" class="btn-ghost">Save</button>`, `value="create_forums" class="btn">`)

	stubHandoffTaskTypes(t, handoffTaskTypes())
	order("handoff settings", handoffGet(t, db).Body.String(), "Back to notifications", `<button type="submit" class="btn">Save</button>`)

	files := settingsSourceFiles(t)
	for _, check := range []struct{ file, pattern string }{
		{"admin.go", `Cancel"\) \+ ` + "`" + `</a><button type="submit" class="btn">`},
		{"ia_views.go", `Cancel"\)\) \+ ` + "`" + `</a><button class="btn" type="submit"`},
		{"current_routing.go", `Cancel"\)\) \+ ` + "`" + `</a><button type="submit" class="btn" data-apply-submit`},
		{"handler.go", `Cancel"\)\) \+ ` + "`" + `</a>` + "`" + ` \+\s*` + "`" + `<button type="submit" class="btn-danger">`},
	} {
		if !regexp.MustCompile(check.pattern).MatchString(files[check.file]) {
			t.Errorf("%s: the cancel link must come before the primary button", check.file)
		}
	}
}

// One wording for "needs a look": 要確認 / Needs review. The old variants and the
// untranslated catalog label are gone.
func TestStatusWordingIsUnified(t *testing.T) {
	for name, body := range settingsSourceFiles(t) {
		for _, variant := range []string{"確認が必要", "再確認", "Recheck"} {
			if strings.Contains(body, variant) {
				t.Errorf("%s still uses the variant %q", name, variant)
			}
		}
	}
	if got := tr("ja", "ia.danger"); got != "危険な操作" {
		t.Fatalf("ja catalog label for the danger zone = %q, want 危険な操作", got)
	}
	if got := tr("en", "ia.danger"); got != "Danger Zone" {
		t.Fatalf("en catalog label for the danger zone = %q", got)
	}
}

// The audit log shows the local date and time without the IANA zone name.
func TestAuditLogTimeHasNoZoneSuffix(t *testing.T) {
	db := newIAViewDB(t)
	model.WriteAuditLog(db, model.AuditLog{TaskID: "t1", ProjectID: "p1", ProjectName: "Audit Production", Success: true, ActorKind: model.AuditActorSystem, ActorName: "test"})
	w := httptest.NewRecorder()
	renderIAAudit(w, httptest.NewRequest("GET", "/bot/admin/audit?lang=en", nil), db)
	body := w.Body.String()
	if !strings.Contains(body, `class="audit-time"`) {
		t.Fatalf("audit time element missing: %s", body)
	}
	for _, forbidden := range []string{"resolvedOptions", `+" "+zone`, "Asia/Tokyo"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("audit time script still adds the zone name (%q)", forbidden)
		}
	}
}

// C13: a Discord read that failed outright (nothing cached) also keeps the
// notification badge off Healthy.
func TestNotificationBadgeReflectsDiscordReadFailure(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "badge-cold-failure", Name: "Badge Cold", DiscordGuildID: "123456789012345678", DiscordCategoryID: "123456789012345679"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { invalidateGuildDirectory(project.DiscordGuildID) })
	guildMemberListCache.mu.Lock()
	guildMemberListCache.entries[project.DiscordGuildID] = &guildDirectoryCacheEntry{lastErr: errors.New("synthetic discord failure")}
	guildMemberListCache.mu.Unlock()
	body := renderSelectedProductionNotifications(db, httptest.NewRequest("GET", "/bot/admin/projects?project="+project.KitsuProjectID+"&tab=notifications&lang=en", nil), project, "en", "ok", "Healthy", "", "synthetic-discord-token")
	if strings.Contains(body, ">Healthy<") || !strings.Contains(body, "Needs review") {
		t.Fatalf("badge reports Healthy although the Discord read failed: %s", body)
	}
}

// After the create button, and after the connection's automatic creation, the
// forum page shows the result and the current state: which forums and tags exist
// and what failed.
func TestForumResultPanelShowsCreatedStateAndFailures(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	if w := forumPost(t, db, completeForumForm()); w.Code != http.StatusBadRequest && w.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d: %s", w.Code, w.Body.String())
	}
	render := func(query string) string {
		w := httptest.NewRecorder()
		forumHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en"+query, nil))
		return w.Body.String()
	}

	if body := render(""); strings.Contains(body, `id="forum-result"`) {
		t.Fatal("the result panel should not appear on an ordinary visit")
	}
	body := render("&msg=forums_created")
	panel := body[strings.Index(body, `id="forum-result"`):]
	panel = panel[:strings.Index(panel, "</section>")]
	for _, want := range []string{"Creation result and current state", "Result: created", "#animation", "#compositing", "WFA", "RETAKE", "DONE", "WIP", "Task Types with a forum: 2, not created: 0"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("result panel missing %q: %s", want, panel)
		}
	}
	if strings.Contains(panel, "Not created") || strings.Contains(panel, `>No<`) {
		t.Fatalf("every forum and tag exists, but the panel reports a gap: %s", panel)
	}

	// An automatic-creation failure left a mark: the page shows what failed and
	// that the Task Types still have no forum.
	empty := newForumPageDB(t)
	model.SetSetting(empty, forumAutoCreateCheckKey("p1"), "フォーラム「Animation」を作成できませんでした: missing permission")
	w := httptest.NewRecorder()
	forumHandler(empty)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	failed := w.Body.String()
	panel = failed[strings.Index(failed, `id="forum-result"`):]
	panel = panel[:strings.Index(panel, "</section>")]
	for _, want := range []string{"Result: failed", "What failed", "missing permission", "Not created", "Task Types with a forum: 0, not created: 2"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("failure panel missing %q: %s", want, panel)
		}
	}
}

// A creation that stopped halfway shows the forums it did make next to the
// error, so the operator sees the real state.
func TestForumResultPanelAfterPartialCreateFailure(t *testing.T) {
	stubForumLookups(t, nil)
	settings := model.ProductionForumSettings{Config: model.ProductionForumConfig{ProductionID: "p1", GuildID: "g1"}, Routes: []model.ProductionForumRoute{{ProductionID: "p1", TaskTypeID: "tt-anim", TaskTypeName: "Animation", ForumChannelID: "f-anim", ForumChannelName: "animation"}}}
	for _, role := range forumRoleOptions[:2] {
		settings.Tags = append(settings.Tags, model.ProductionForumTag{ProductionID: "p1", ForumChannelID: "f-anim", Role: role, TagID: "tag-" + role})
	}
	taskTypes := []kitsu.TaskType{{ID: "tt-anim", Name: "Animation"}, {ID: "tt-comp", Name: "Compositing"}}
	panel := renderForumResultPanel("en", settings, taskTypes, nil, "Could not create forum \"Compositing\"", false)
	for _, want := range []string{"Result: failed", "Could not create forum", "#animation", "Not created", "Task Types with a forum: 1, not created: 1"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("partial-failure panel missing %q: %s", want, panel)
		}
	}
	if strings.Count(panel, `status-pill success">Yes`) != 2 || strings.Count(panel, `status-pill warning">No<`) != 6 {
		t.Fatalf("tag states are wrong for a forum with two of four tags: %s", panel)
	}
}

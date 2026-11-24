package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/model"
)

// The status -> role page (/bot/admin/bot/statuses) edits ONE studio-wide
// assignment stored in studio_status_policies, with no per-production picker.
// Dictionary-derived placements display in the boxes when no manual policy is
// saved yet, and manual saves win over both the dictionary and any legacy
// per-production status-role rows.

func TestStatusRolesSectionPreFillsTemplate(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/bot/statuses?lang=en", nil)
	body := renderStatusRolesSection(db, "en", r)
	if !strings.Contains(body, `id="status-roles"`) {
		t.Fatalf("section missing: %s", body)
	}
	for _, gone := range []string{"role-summary-line", "all three roles are assigned", "a role with no status is never notified", "<h2>Task statuses</h2>", `<select name="srole_role">`, `name="srole_project"`, "Choose a Production", "プロダクションを選ぶ"} {
		if strings.Contains(body, gone) {
			t.Fatalf("section must omit %q: %s", gone, body)
		}
	}
	for _, want := range []string{`data-role-box="wfa"`, `data-role-box="retake"`, `data-role-box="done"`, `data-role-add-select`, `data-role-add`, `data-role-remove`, `data-role-tray-item="s-wip"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("section missing %q: %s", want, body)
		}
	}
	if strings.Count(body, `<select data-role-add-select`) != 3 || strings.Count(body, `data-role-add>`) != 3 {
		t.Fatalf("expected one add control per role box, got body: %s", body)
	}
	trayStart := strings.Index(body, `data-role-tray`)
	if trayStart < 0 || strings.Contains(body[trayStart:], `<select`) {
		t.Fatalf("unassigned tray must be a plain list with no select: %s", body)
	}
}

func TestSaveStatusRolesStoresStudioWidePolicyAndIgnoresProductionRows(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	// Seed a conflicting per-production status-role row; the studio-wide page
	// must ignore it and save directly to studio_status_policies.
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "p1", GuildID: "g1"},
		Routes: []model.ProductionForumRoute{{ProductionID: "p1", TaskTypeID: "tt-comp", TaskTypeName: "Compositing", ForumChannelID: "f-comp"}},
		Tags: []model.ProductionForumTag{
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleWFA, TagID: "t1"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleRetake, TagID: "t2"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleDone, TagID: "t3"},
		},
		StatusRoles: []model.ProductionForumStatusRole{
			{StatusID: "s-wfa", StatusName: "Waiting For Approval", Role: model.ForumRoleDone},
			{StatusID: "s-retake", StatusName: "Retake", Role: model.ForumRoleWFA},
			{StatusID: "s-done", StatusName: "Done", Role: model.ForumRoleRetake},
		},
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	form := url.Values{
		"srole_id":   {"s-wfa", "s-retake", "s-done", "s-wip"},
		"srole_name": {"Waiting For Approval", "Retake", "Done", "WIP"},
		"srole_role": {"wfa", "retake", "done", ""},
	}
	r := httptest.NewRequest(http.MethodPost, "/bot/admin/bot/statuses", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := saveStatusRoles(db, r); err != nil {
		t.Fatalf("saveStatusRoles: %v", err)
	}
	policies, err := model.LoadStudioStatusPolicies(db)
	if err != nil || len(policies) != 4 {
		t.Fatalf("stored policies = %+v err=%v", policies, err)
	}
	if policies["s-wfa"].Nature != model.StatusNatureReview || !policies["s-wfa"].Manual {
		t.Fatalf("s-wfa policy = %+v, want manual review", policies["s-wfa"])
	}
	if policies["s-wip"].Nature != model.StatusNatureNone || !policies["s-wip"].Manual {
		t.Fatalf("s-wip policy = %+v, want manual none", policies["s-wip"])
	}
	// Rendering must reflect the saved studio-wide policy, not the per-production row.
	body := renderStatusRolesSection(db, "en", httptest.NewRequest(http.MethodGet, "/bot/admin/bot/statuses?lang=en", nil))
	wfaBoxStart := strings.Index(body, `data-role-box="wfa"`)
	retakeBoxStart := strings.Index(body, `data-role-box="retake"`)
	if wfaBoxStart < 0 || retakeBoxStart < 0 || !strings.Contains(body[wfaBoxStart:retakeBoxStart], `data-role-chip="s-wfa"`) {
		t.Fatalf("s-wfa must be rendered in the wfa box from studio_status_policies: %s", body)
	}
}

func TestStatusRolesDisplayOmitsProductionHeadingAndSummaryLine(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "p1", GuildID: "g1"},
		Routes: []model.ProductionForumRoute{{ProductionID: "p1", TaskTypeID: "tt-comp", TaskTypeName: "Compositing", ForumChannelID: "f-comp"}},
		Tags: []model.ProductionForumTag{
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleWFA, TagID: "t1"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleRetake, TagID: "t2"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleDone, TagID: "t3"},
		},
		StatusRoles: []model.ProductionForumStatusRole{
			{StatusID: "s-wfa", StatusName: "Waiting For Approval", Role: model.ForumRoleWFA},
			{StatusID: "s-retake", StatusName: "Retake", Role: model.ForumRoleRetake},
			{StatusID: "s-done", StatusName: "Done", Role: model.ForumRoleDone},
		},
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?lang=en", nil)
	body := renderStatusRolesDisplay(db, "en", r)
	for _, want := range []string{"Task statuses", `class="role-boxes"`, `class="role-chip role-chip-static"`, "Waiting For Approval"} {
		if !strings.Contains(body, want) {
			t.Fatalf("display section missing %q: %s", want, body)
		}
	}
	for _, gone := range []string{"<h3>", "role-summary-line", "all three roles are assigned", "Alpha"} {
		if strings.Contains(body, gone) {
			t.Fatalf("display section must omit %q: %s", gone, body)
		}
	}
}

func TestConnectionsPageRendersDirectlyAndSubpageRedirects(t *testing.T) {
	db := newSetupStateTestDB(t)
	w := httptest.NewRecorder()
	BotHandler(db, nil)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/bot?lang=ja", nil))
	body := w.Body.String()
	for _, want := range []string{
		"接続設定", "Kitsu接続", "Discord Bot接続", "接続設定を編集",
		`/bot/admin/bot?edit=1`, `/bot/admin/bot/statuses`, `/bot/admin/bot/wiring`,
		"タスクステータス", "Kitsuのステータスを「確認待ち」「リテイク」「完了」「未割り当て」に割り当てます。", "タスクステータスを開く",
		"受け渡しの配線", "タスク完了時に次工程へ通知する基本の配線を設定します。", "受け渡しの配線を開く",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("connections page missing %q", want)
		}
	}
	if strings.Contains(body, `data-bot-hub`) || strings.Contains(body, `connections-sublinks`) {
		t.Fatalf("connections page must not render hub or plain sublinks")
	}

	redir := httptest.NewRecorder()
	BotHandler(db, nil)(redir, httptest.NewRequest(http.MethodGet, "/bot/admin/bot/connections?edit=1&lang=en", nil))
	if redir.Code != http.StatusSeeOther || redir.Header().Get("Location") != "/bot/admin/bot?edit=1&lang=en" {
		t.Fatalf("expected redirect from /bot/admin/bot/connections to /bot/admin/bot?edit=1&lang=en, got status=%d location=%q", redir.Code, redir.Header().Get("Location"))
	}
}

func TestBotSubpagesOmitDuplicateHeadingAndSummaryLine(t *testing.T) {
	stubForumLookups(t, nil)
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "p1", GuildID: "g1"},
		Routes: []model.ProductionForumRoute{{ProductionID: "p1", TaskTypeID: "tt-comp", TaskTypeName: "Compositing", ForumChannelID: "f-comp"}},
		Tags: []model.ProductionForumTag{
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleWFA, TagID: "t1"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleRetake, TagID: "t2"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleDone, TagID: "t3"},
		},
		StatusRoles: []model.ProductionForumStatusRole{
			{StatusID: "s-wfa", StatusName: "Waiting For Approval", Role: model.ForumRoleWFA},
			{StatusID: "s-retake", StatusName: "Retake", Role: model.ForumRoleRetake},
			{StatusID: "s-done", StatusName: "Done", Role: model.ForumRoleDone},
		},
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	for _, tc := range []struct {
		lang       string
		title      string
		goneStatus []string
	}{
		{"ja", "タスクステータス", []string{"<h2>タスクステータス</h2>", "role-summary-line", "3つの役割が揃っています", "未設定の役割は通知されません"}},
		{"en", "Task statuses", []string{"<h2>Task statuses</h2>", "role-summary-line", "all three roles are assigned", "a role with no status is never notified"}},
	} {
		w := httptest.NewRecorder()
		BotHandler(db, nil)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/bot/statuses?lang="+tc.lang, nil))
		body := w.Body.String()
		if !strings.Contains(body, "<h1>"+tc.title+"</h1>") {
			t.Fatalf("statuses (%s) missing page title %q", tc.lang, tc.title)
		}
		if !strings.Contains(body, `class="role-box"`) || !strings.Contains(body, `data-role-count`) {
			t.Fatalf("statuses (%s) missing role boxes", tc.lang)
		}
		if strings.Count(body, `<select data-role-add-select`) != 3 || strings.Count(body, `data-role-add>`) != 3 {
			t.Fatalf("statuses (%s) must have one add control per role box", tc.lang)
		}
		if strings.Contains(body, `<select name="srole_role">`) {
			t.Fatalf("statuses (%s) must not render per-status role selects", tc.lang)
		}
		for _, gone := range tc.goneStatus {
			if strings.Contains(body, gone) {
				t.Fatalf("statuses (%s) must omit %q", tc.lang, gone)
			}
		}
	}

	for _, tc := range []struct {
		lang  string
		title string
	}{
		{"ja", "受け渡しの配線"},
		{"en", "Handoff wiring"},
	} {
		w := httptest.NewRecorder()
		BotHandler(db, nil)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/bot/wiring?lang="+tc.lang, nil))
		body := w.Body.String()
		if !strings.Contains(body, "<h1>"+tc.title+"</h1>") {
			t.Fatalf("wiring (%s) missing page title %q", tc.lang, tc.title)
		}
		if strings.Contains(body, "<h2>"+tc.title+"</h2>") {
			t.Fatalf("wiring (%s) must omit duplicate inner heading", tc.lang)
		}
	}
}

package setup

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newIAViewDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionChannelMapping{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}, &model.AuditLog{}, &model.UserMap{}, &model.ProjectUserMap{}, &model.ProjectCheckerMap{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestProductionCenteredViewsExposeApprovedSections(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "synthetic-production", Name: "Synthetic Production", DiscordGuildID: "synthetic-server", DiscordCategoryID: "synthetic-category"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=synthetic-production&lang=en", nil)
	w := httptest.NewRecorder()
	renderIAProductionList(w, r, db, "")
	body := w.Body.String()
	if !strings.Contains(body, `role="tablist"`) || !strings.Contains(body, `aria-selected="true"`) || !strings.Contains(body, `aria-labelledby="tab-overview"`) {
		t.Fatal("selected Production overview tab is not accessible")
	}
	for _, tab := range []string{"overview", "notifications", "user-settings", "storage-settings", "activity", "troubleshooting", "advanced", "danger-zone"} {
		r := httptest.NewRequest("GET", "/bot/admin/projects?project=synthetic-production&tab="+tab+"&lang=en", nil)
		w := httptest.NewRecorder()
		renderIAProductionList(w, r, db, "")
		if !strings.Contains(w.Body.String(), `aria-labelledby="tab-`+tab+`"`) {
			t.Fatalf("selected Production tab missing %q", tab)
		}
	}
	if strings.Contains(body, `id="panel-notifications"`) || strings.Contains(body, `id="panel-danger-zone"`) {
		t.Fatal("inactive Production panels remain rendered")
	}
	if strings.Contains(body, "<details class=\"accordion\"") {
		t.Fatal("selected Production view still uses the legacy accordion")
	}
	if strings.Contains(body, "name=\"guild_id\"") {
		t.Fatal("selected Production view exposes manual server ID editing")
	}
}

func TestLiveProductionPreviewsKeepTaskTypesIsolated(t *testing.T) {
	globalTaskTypeCalls := 0
	personCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/data/projects/":
			_, _ = w.Write([]byte(`[{"id":"production-a","name":"Production A"},{"id":"production-b","name":"Production B"}]`))
		case "/api/data/projects/production-a/task-types":
			_, _ = w.Write([]byte(`[{"id":"a-concept","name":"Concept","active":true},{"id":"a-archived","name":"Archived A","archived":true}]`))
		case "/api/data/projects/production-b/task-types":
			_, _ = w.Write([]byte(`[{"id":"b-concept","name":"Concept","active":true}]`))
		case "/api/data/task-types/":
			globalTaskTypeCalls++
			_, _ = w.Write([]byte(`[{"id":"global-only","name":"Global only"}]`))
		case "/api/data/persons/":
			personCalls++
			_, _ = w.Write([]byte(`[{"id":"global-person","full_name":"Global Person"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("KITSU_HOSTNAME", server.URL+"/")
	t.Setenv("KitsuJWTToken", "test-token")

	db := newIAViewDB(t)
	projects := availableProjects(db)
	previews := map[string]model.ValidationKitsuData{}
	for _, project := range projects {
		if !project.ReadOnlyPreview {
			continue
		}
		previews[project.KitsuProjectID] = project.ValidationData()
	}
	if len(previews) != 2 {
		t.Fatalf("got %d live previews, want 2", len(previews))
	}
	if got := previews["production-a"].TaskTypes; len(got) != 1 || got[0].ID != "a-concept" {
		t.Fatalf("Production A preview leaked or omitted Task Types: %+v", got)
	}
	if got := previews["production-b"].TaskTypes; len(got) != 1 || got[0].ID != "b-concept" {
		t.Fatalf("Production B preview leaked or omitted Task Types: %+v", got)
	}
	if len(previews["production-a"].Participants) != 0 || len(previews["production-b"].Participants) != 0 {
		t.Fatal("live previews loaded global participants that are not needed by the preview")
	}
	if globalTaskTypeCalls != 0 {
		t.Fatalf("live preview used the global Task Type endpoint %d time(s)", globalTaskTypeCalls)
	}
	if personCalls != 0 {
		t.Fatalf("live preview used the global persons endpoint %d time(s)", personCalls)
	}
}

func TestGlobalUserMappingHasNoProductionOrRoleControls(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.UserMap{KitsuName: "Synthetic User", DiscordID: "123456789012345678", DiscordDisplayName: "Synthetic Discord User"})
	r := httptest.NewRequest("GET", "/bot/admin/users?lang=en", nil)
	w := httptest.NewRecorder()
	renderGlobalUserMapping(w, r, db)
	body := w.Body.String()
	for _, forbidden := range []string{"Editing production", "Reviewer / Checker task types", "Production selector", "Discord ID"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("global mapping leaked production-scoped control %q", forbidden)
		}
	}
	if !strings.Contains(body, "Synthetic User") || !strings.Contains(body, "Synthetic Discord User") {
		t.Fatal("global user mapping did not show user-facing identities")
	}
}

func TestPrimaryNavigationAndNewConnectionFlow(t *testing.T) {
	r := httptest.NewRequest("GET", "/bot/admin?lang=en", nil)
	body := adminPage("en", "Dashboard", r, "")
	for _, want := range []string{"Dashboard", "Productions", "User Linking", "Connections", "System Status", "Audit Log"} {
		if !strings.Contains(body, want) {
			t.Fatalf("primary navigation missing %q", want)
		}
	}
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIANewConnection(w, r, db)
	setup := w.Body.String()
	for _, want := range []string{"Prerequisites", "Production", "Discord server", "Channel plan", "Review", "Execute", "Complete", "Configure the Kitsu connection"} {
		if !strings.Contains(setup, want) {
			t.Fatalf("new connection flow missing %q", want)
		}
	}
	if strings.Contains(setup, `name="new-connection-production"`) || strings.Contains(setup, `name="plan_guild"`) {
		t.Fatal("blocked wizard exposed later-step selectors")
	}
	if strings.Contains(setup, "Guild ID") || strings.Contains(setup, "Project Routing") {
		t.Fatal("new connection flow exposes implementation terms")
	}
	forced := httptest.NewRequest("GET", "/bot/setup?lang=en&wizard_step=2", nil)
	forcedWriter := httptest.NewRecorder()
	renderIANewConnection(forcedWriter, forced, db)
	if strings.Contains(forcedWriter.Body.String(), `id="wizard-production"`) {
		t.Fatal("later wizard step was reachable while prerequisites were blocked")
	}
}

func TestNewConnectionWizardUsesSharedJapaneseCatalog(t *testing.T) {
	db := newIAViewDB(t)
	r := httptest.NewRequest("GET", "/bot/setup?lang=ja", nil)
	w := httptest.NewRecorder()
	renderIANewConnection(w, r, db)
	body := w.Body.String()
	for _, want := range []string{"接続の前提条件", "Kitsu接続を設定", "新しいプロダクションを接続", "Discordサーバー"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Japanese wizard missing %q", want)
		}
	}
	/*
		for _, want := range []string{"接続の準備状況", "Kitsu接続を設定", "新しいProductionを接続", "Discordサーバー"} {
			if !strings.Contains(body, want) {
				t.Fatalf("Japanese wizard missing %q", want)
			}
		}
		for _, forbidden := range []string{"Guild", "routing", "runtime", "readiness", "dry-run", "stale"} {
			visibleMarker := ">" + strings.ToLower(forbidden) + "<"
			if strings.Contains(strings.ToLower(body), visibleMarker) {
				t.Fatalf("Japanese wizard leaked internal term %q", forbidden)
			}
		}
	*/
}

func TestSharedStatusSummaryRowHasSemanticStructureAndVariants(t *testing.T) {
	body := statusSummaryRow("Discord Bot", "blocked", "Not configured", "Bot Connection is required.", `<a class="btn">Set up</a>`)
	for _, want := range []string{`class="status-row"`, `class="status-row-label"`, `class="status-badge status-badge-blocked"`, `class="status-row-explanation"`, `class="status-row-action"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shared status row missing %q", want)
		}
	}
	if strings.Contains(body, `status-pill`) {
		t.Fatal("shared status row fell back to the legacy indistinguishable pill")
	}
	if strings.Contains(body, "\u7ab6") || strings.Contains(body, "\ufffd") {
		t.Fatal("shared status row contains corrupted decorative text")
	}
}

func TestSystemStatusUsesOneAlignedReadinessGrid(t *testing.T) {
	t.Setenv("KITSU_HOSTNAME", "")
	t.Setenv("KITSUSYNC_LOCAL_PROFILE", "")
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIAHealth(w, httptest.NewRequest("GET", "/bot/admin/health?lang=en", nil), db)
	body := w.Body.String()
	for _, label := range []string{"Kitsu API", "Event monitoring", "Connection / routing integrity", "Notification processing", "Internal data", "Discord API", "Recent system issues"} {
		if !strings.Contains(body, ">"+label+"<") {
			t.Fatalf("System Status missing row %q", label)
		}
	}
	if got := strings.Count(body, `class="pipeline-health-item"`); got != 4 {
		t.Fatalf("System Status rendered %d internal pipeline items, want 4", got)
	}
	if !strings.Contains(body, "Metrics that are not available are shown as unconfirmed.") {
		t.Fatal("System Status did not disclose unavailable metrics")
	}
	if strings.Contains(body, "Overall problem:") || strings.Contains(body, "Next required action:") {
		t.Fatal("System Status rendered the legacy duplicated guidance")
	}
	if !strings.Contains(body, `href="/bot/admin/bot?lang=en"`) {
		t.Fatal("System Status did not guide incomplete setup to Connections")
	}
}

func TestSystemStatusPipelineHealthUsesSafeUnavailableMetrics(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIAHealth(w, httptest.NewRequest("GET", "/bot/admin/health?lang=ja", nil), db)
	body := w.Body.String()
	for _, want := range []string{"Kitsu API", "イベント監視", "接続・ルーティング整合性", "Discord API", "通知処理", "内部データ", "最近のシステム問題", "未確認"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Japanese System Status missing %q", want)
		}
	}
	for _, forbidden := range []string{"polling", "runtime", "readiness", "webhook count", "Next required action:"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("System Status leaked internal term %q", forbidden)
		}
	}
}

func TestNormalViewsKeepTechnicalDetailsCollapsed(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "technical-details-p", Name: "Technical Details P", DiscordGuildID: "g", DiscordCategoryID: "c"})
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=technical-details-p&lang=ja", nil)
	w := httptest.NewRecorder()
	renderIAProductionList(w, r, db, "")
	body := w.Body.String()
	if strings.Contains(body, "<details") || strings.Contains(body, ">g<") || strings.Contains(body, ">c<") || strings.Contains(body, "Production ID") {
		t.Fatal("default Overview exposed advanced or destructive details")
	}
	for _, tab := range []string{"advanced", "danger-zone"} {
		r := httptest.NewRequest("GET", "/bot/admin/projects?project=technical-details-p&tab="+tab+"&lang=ja", nil)
		w := httptest.NewRecorder()
		renderIAProductionList(w, r, db, "")
		body := w.Body.String()
		if !strings.Contains(body, "<details") && tab == "danger-zone" {
			t.Fatal("Danger Zone is not a disclosure section")
		}
	}
}

func TestSelectedProductionTabsHaveSingleAccessiblePanel(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "tab-semantics-p", Name: "Tab Semantics P"})
	for _, tab := range []string{"", "notifications", "user-settings", "storage-settings", "activity", "troubleshooting", "advanced", "danger-zone", "invalid"} {
		path := "/bot/admin/projects?project=tab-semantics-p&lang=en"
		if tab != "" {
			path += "&tab=" + tab
		}
		w := httptest.NewRecorder()
		renderIAProductionList(w, httptest.NewRequest("GET", path, nil), db, "")
		body := w.Body.String()
		if strings.Count(body, `role="tabpanel"`) != 1 || strings.Count(body, `role="tablist"`) < 1 {
			t.Fatalf("tab %q did not render one tab panel", tab)
		}
		if tab == "invalid" && !strings.Contains(body, `id="panel-overview"`) {
			t.Fatal("invalid tab did not fall back to Overview")
		}
	}
}

func TestSelectedProductionPolishAndDangerConfirmation(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "polish-p", Name: "Polish Production", DiscordGuildID: "synthetic-guild"})
	w := httptest.NewRecorder()
	renderIAProductionList(w, httptest.NewRequest("GET", "/bot/admin/projects?project=polish-p&lang=en", nil), db, "")
	body := w.Body.String()
	if strings.Count(body, "Polish Production") != 1 {
		t.Fatalf("selected Production title rendered %d times", strings.Count(body, "Polish Production"))
	}
	if !strings.Contains(body, `class="section-nav production-tabs`) || !strings.Contains(body, `aria-selected="true"`) {
		t.Fatal("selected tab does not have the shared visual and ARIA state")
	}
	danger := httptest.NewRecorder()
	renderIAProductionList(danger, httptest.NewRequest("GET", "/bot/admin/projects?project=polish-p&tab=danger-zone&lang=en", nil), db, "")
	dangerBody := danger.Body.String()
	for _, want := range []string{`data-require-text="DISCONNECT"`, `data-require-text="DELETE"`, `action" value="execute_current_ia_discord_delete"`, "Discord resources remain.", "separate from disconnecting"} {
		if !strings.Contains(dangerBody, want) {
			t.Fatalf("Danger Zone missing confirmation safeguard %q", want)
		}
	}
	if strings.Contains(dangerBody, `action" value="preview_remove_connection_with_discord"`) || strings.Contains(dangerBody, "danger_preview=1") || strings.Contains(dangerBody, "validated_channels=1") {
		t.Fatal("current IA Danger Zone still links to the legacy preview flow")
	}
	if strings.Contains(dangerBody, `data-require-text="delete"`) {
		t.Fatal("Danger Zone confirmation is not exact and case-sensitive")
	}
}

func TestSelectedProductionJapaneseDangerConfirmation(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "polish-ja-p", Name: "日本語Production"})
	w := httptest.NewRecorder()
	renderIAProductionList(w, httptest.NewRequest("GET", "/bot/admin/projects?project=polish-ja-p&tab=danger-zone&lang=ja", nil), db, "")
	body := w.Body.String()
	for _, want := range []string{`data-require-text="連携解除"`, `data-require-text="削除"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("Japanese confirmation phrase missing %q", want)
		}
	}
}

func TestCurrentIADiscordDeleteRequiresConfirmationAndRemovesProductionOnSuccess(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "current-delete-p", Name: "Current Delete Production"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?lang=en", strings.NewReader("action=execute_current_ia_discord_delete&project_id=current-delete-p&confirm_text=wrong"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	AdminProjectsHandler(db, "", "")(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("invalid confirmation status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if model.FindProjectByKitsuID(db, "current-delete-p") == nil {
		t.Fatal("invalid confirmation changed local Production state")
	}
	if strings.Contains(response.Header().Get("Location"), "danger_preview") || strings.Contains(response.Header().Get("Location"), "validated_channels") {
		t.Fatal("current IA confirmation fell back to a legacy delete route")
	}

	request = httptest.NewRequest(http.MethodPost, "/bot/admin/projects?lang=en", strings.NewReader("action=execute_current_ia_discord_delete&project_id=current-delete-p&confirm_text=DELETE"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	AdminProjectsHandler(db, "", "")(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Deletion complete") {
		t.Fatalf("successful current IA delete did not render current result: status=%d body=%s", response.Code, response.Body.String())
	}
	if model.FindProjectByKitsuID(db, "current-delete-p") != nil {
		t.Fatal("successful current IA delete left the Production connected")
	}
	if strings.Contains(response.Body.String(), "danger_preview=1") || strings.Contains(response.Body.String(), "Assigned task types") || strings.Contains(response.Body.String(), "CONNECTED PRODUCTION") {
		t.Fatal("current IA delete result rendered legacy management UI")
	}
}

func TestCurrentIADeleteResultSuccessIsCompactAndDoesNotRenderFollowUp(t *testing.T) {
	project := model.Project{KitsuProjectID: "delete-result-p", Name: "Delete Result Production"}
	result := connectedProductionChannelDeleteExecution{
		Deleted:             make([]connectedProductionChannelValidationResult, 3),
		DeletedWebhookCount: 3,
		CategoryDeleted:     true,
		ConnectionDeleted:   true,
	}

	for _, lang := range []string{"ja", "en"} {
		body := renderIAConnectedProductionDeleteResultRefined(lang, httptest.NewRequest("GET", "/bot/admin/projects?lang="+lang, nil), project, result)
		if !strings.Contains(body, `<h1>`+map[string]string{"ja": "削除完了", "en": "Deletion complete"}[lang]+`</h1>`) || strings.Count(body, "<h1>") != 1 {
			t.Fatalf("%s result missing single visible completion title", lang)
		}
		if strings.Contains(body, "削除結果") || strings.Contains(body, "Delete result") || strings.Contains(body, "確認事項") || strings.Contains(body, "Follow-up items") || strings.Contains(body, "残りの確認事項はありません") || strings.Contains(body, "No follow-up items remain") {
			t.Fatalf("%s success result rendered redundant follow-up content", lang)
		}
		for _, want := range []string{">1</dd>", ">3</dd>", `href="/bot/admin/projects?lang=` + lang + `"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s success result missing summary or return link %q", lang, want)
			}
		}
	}
}

func TestCurrentIADeleteResultPartialShowsOnlyRemainingIssues(t *testing.T) {
	project := model.Project{KitsuProjectID: "delete-result-partial", Name: "Partial Delete Production"}
	result := connectedProductionChannelDeleteExecution{
		Deleted:           make([]connectedProductionChannelValidationResult, 2),
		Failed:            []connectedProductionChannelValidationResult{{Reason: "channel deletion failed"}},
		CategoryDeleted:   true,
		ConnectionDeleted: true,
	}
	body := renderIAConnectedProductionDeleteResultRefined("en", httptest.NewRequest("GET", "/bot/admin/projects?lang=en", nil), project, result)
	if !strings.Contains(body, `class="status-pill warn"`) || !strings.Contains(body, "Follow-up items") || !strings.Contains(body, "channel deletion failed") {
		t.Fatal("partial delete result did not show actionable warning state")
	}
	if strings.Contains(body, `<h1>Deletion complete</h1>`) {
		t.Fatal("partial delete result used the full-success title")
	}
}

func TestDashboardUsesSharedReadinessAndShowsNextAction(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/bot/admin?lang=en", nil)
	renderIADashboard(w, r, db)
	body := w.Body.String()
	if mainStart := strings.Index(body, `<main id="main-content">`); mainStart >= 0 {
		body = body[mainStart:]
	}
	for _, want := range []string{"Setup required", "Configure the Kitsu connection before continuing.", "Needs attention", "Productions needing attention", "dashboard-menu-status", "dashboard-menu-card"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing readiness copy %q", want)
		}
	}
	if strings.Contains(body, "Polling") || strings.Contains(body, "Runtime") {
		t.Fatal("dashboard exposes implementation status")
	}
	if strings.Contains(body, "Productions with notifications paused") || strings.Contains(body, "Paused") {
		t.Fatal("dashboard exposes the retired pause state")
	}
	if strings.Count(body, "<h1") != 1 {
		t.Fatalf("dashboard should have exactly one h1, got %d", strings.Count(body, "<h1"))
	}
	if strings.Contains(body, "chart") || strings.Contains(body, "sparkline") || strings.Contains(body, "waveform") {
		t.Fatal("dashboard contains decorative chart markup outside the approved operations layout")
	}
}

func TestDashboardUsesShortJapaneseSetupStatus(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIADashboard(w, httptest.NewRequest("GET", "/bot/admin?lang=ja", nil), db)
	body := w.Body.String()
	if !strings.Contains(body, `class="status-pill danger">未設定</span>`) {
		t.Fatal("current Dashboard did not use the short Japanese setup status")
	}
	if strings.Contains(body, "初期設定が必要です") {
		t.Fatal("current Dashboard retained the long Japanese setup status")
	}
}

func TestDashboardRemovesSubtitleAndUsesSemanticMetricStates(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIADashboard(w, httptest.NewRequest("GET", "/bot/admin?lang=en", nil), db)
	body := w.Body.String()
	if strings.Contains(body, "Review KitsuSync connection state and items that need attention.") {
		t.Fatal("dashboard subtitle is still rendered")
	}
	if strings.Count(body, `class="metric-card `) != 4 {
		t.Fatalf("dashboard summary cards are missing semantic classes: %d", strings.Count(body, `class="metric-card `))
	}
	if !strings.Contains(body, "semantic-neutral") || !strings.Contains(body, "semantic-good") || !strings.Contains(body, "semantic-warning") {
		t.Fatal("dashboard semantic metric states are incomplete")
	}
}

func TestDashboardDoesNotRenderDuplicateTopProductionSummary(t *testing.T) {
	db := newIAViewDB(t)
	for _, lang := range []string{"ja", "en"} {
		w := httptest.NewRecorder()
		renderIADashboard(w, httptest.NewRequest("GET", "/bot/admin?lang="+lang, nil), db)
		body := w.Body.String()
		if mainStart := strings.Index(body, `<main id="main-content">`); mainStart >= 0 {
			body = body[mainStart:]
		}
		if strings.Contains(body, "Production availability") || strings.Contains(body, "Kitsu Productions available") {
			t.Fatalf("duplicate top Production summary rendered in %s", lang)
		}
		if !strings.Contains(body, "dashboard-summary-grid") || !strings.Contains(body, "dashboard-queue") {
			t.Fatalf("existing Dashboard structure missing in %s", lang)
		}
		if !strings.Contains(body, "dashboard-cta") || strings.Count(body, `class="dashboard-menu-card`) != 5 {
			t.Fatalf("reference dashboard management cards are incomplete in %s", lang)
		}
		if !strings.Contains(body, `href="/bot/admin?lang=`) {
			t.Fatalf("dashboard refresh action does not stay on the dashboard in %s", lang)
		}
		if strings.Contains(body, "dashboard-activity") || strings.Contains(body, "dashboard-system") || strings.Contains(body, "通知システム") || strings.Contains(body, "Notification system") {
			t.Fatalf("retired lower dashboard panels remain in %s", lang)
		}
		if strings.Count(body, `class="dashboard-status-chip `) < 5 {
			t.Fatalf("dashboard management cards are missing status chips in %s", lang)
		}
		if strings.Contains(body, "dashboard-quick") && !strings.Contains(body, "display:none") {
			t.Fatal("legacy quick actions remain visible")
		}
	}
}

func TestLegacyPausedProductionIsReviewRequiredAndNotPaused(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "legacy-paused-dashboard-p", Name: "Legacy State Production"}
	db.Create(&p)
	db.Create(&model.ProductionNotificationConfig{ProductionID: p.KitsuProjectID, Enabled: false})
	class, label, hint := iaStatus(db, p, "en")
	if class != "bad" || label != "Needs review" || strings.Contains(strings.ToLower(hint), "paused") {
		t.Fatalf("legacy paused state was not converted to review-required state: %q %q %q", class, label, hint)
	}
	w := httptest.NewRecorder()
	renderIADashboard(w, httptest.NewRequest("GET", "/bot/admin?lang=en", nil), db)
	if strings.Contains(w.Body.String(), "Productions with notifications paused") || strings.Contains(w.Body.String(), "Paused") {
		t.Fatal("legacy paused state leaked as an ordinary Dashboard state")
	}
}

func TestNotificationsHasNoNormalPauseResumeControls(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "notification-controls-p", Name: "Notification Controls Production"}
	db.Create(&p)
	db.Create(&model.ProductionNotificationConfig{ProductionID: p.KitsuProjectID, Enabled: false})
	body := renderSelectedProductionNotifications(db, httptest.NewRequest("GET", "/bot/admin/projects?lang=en", nil), p, "en", "bad", "Needs review", "A legacy stopped state is saved.")
	for _, forbidden := range []string{"Pause notifications", "Resume notifications", "name=\"action\" value=\"pause\"", "name=\"action\" value=\"resume\""} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("normal Notifications UI exposes retired control %q", forbidden)
		}
	}
	if !strings.Contains(body, "Check without sending") || !strings.Contains(body, "Review notification settings") {
		t.Fatal("Notifications UI lost safe review controls")
	}
}

func TestDashboardProblemActionTargetsDirectDestination(t *testing.T) {
	p := model.Project{KitsuProjectID: "dashboard-action-p", Name: "Dashboard Action Production"}
	r := httptest.NewRequest("GET", "/bot/admin?lang=en", nil)
	notificationURL, notificationLabel := dashboardProblemAction(r, p, "en", "Notification destination needs attention")
	if !strings.Contains(notificationURL, "tab=notifications") || notificationLabel != "Review notification settings" {
		t.Fatalf("notification issue did not target notification settings: %q %q", notificationURL, notificationLabel)
	}
	userURL, userLabel := dashboardProblemAction(r, p, "en", "Reviewer participant is not mapped")
	if !strings.Contains(userURL, "tab=users") || userLabel != "Review user settings" {
		t.Fatalf("participant issue did not target user settings: %q %q", userURL, userLabel)
	}
}

func TestDashboardIncludesRecentActivityAndBotNextAction(t *testing.T) {
	db := newIAViewDB(t)
	db.Save(&model.Setting{Key: "kitsu.hostname", Value: "http://synthetic-kitsu.invalid"})
	db.Save(&model.Setting{Key: RuntimeKitsuEmailSettingKey, Value: "operator@synthetic.invalid"})
	db.Save(&model.Setting{Key: RuntimeKitsuTokenSettingKey, Value: "synthetic-runtime-credential"})
	model.WriteAuditLog(db, model.AuditLog{ProjectID: "dashboard-p", ProjectName: "Dashboard Production", EntityName: "configuration", Success: true})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/bot/admin?lang=en", nil)
	renderIADashboard(w, r, db)
	body := w.Body.String()
	if mainStart := strings.Index(body, `<main id="main-content">`); mainStart >= 0 {
		body = body[mainStart:]
	}
	for _, want := range []string{"Connections", "dashboard-menu-status"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
	if strings.Contains(body, "dashboard-activity-row") || strings.Contains(body, "Notification system") {
		t.Fatal("retired dashboard lower panels remain visible")
	}
	if strings.Contains(body, "Dashboard Production") {
		t.Fatal("dashboard activity data remains visible after removing the lower panels")
	}
}

func TestSelectedProductionKeepsIdentifiersAdvancedAndUsesUserCopy(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "selected-advanced-p", Name: "Selected Advanced P", DiscordGuildID: "synthetic-guild", DiscordCategoryID: "synthetic-category"}
	db.Create(&p)
	db.Create(&model.NotificationRoutingDiagnosis{ProductionID: "selected-advanced-p", Detail: "Synthetic stale destination"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=selected-advanced-p&lang=en", nil)
	renderIAProductionList(w, r, db, "")
	body := w.Body.String()
	if strings.Contains(body, "synthetic-guild") {
		t.Fatal("raw Discord identifier leaked into the normal Overview tab")
	}
	advancedRequest := httptest.NewRequest("GET", "/bot/admin/projects?project=selected-advanced-p&tab=advanced&lang=en", nil)
	advancedWriter := httptest.NewRecorder()
	renderIAProductionList(advancedWriter, advancedRequest, db, "")
	if !strings.Contains(advancedWriter.Body.String(), "synthetic-guild") {
		t.Fatal("Advanced settings did not expose the technical identifier")
	}
	troubleshootingRequest := httptest.NewRequest("GET", "/bot/admin/projects?project=selected-advanced-p&tab=troubleshooting&lang=en", nil)
	troubleshootingWriter := httptest.NewRecorder()
	renderIAProductionList(troubleshootingWriter, troubleshootingRequest, db, "")
	troubleshootingBody := troubleshootingWriter.Body.String()
	for _, want := range []string{"Current problem", "Next action", "Diagnostic details"} {
		if !strings.Contains(troubleshootingBody, want) {
			t.Fatalf("troubleshooting missing %q", want)
		}
	}
}

func TestSelectedProductionOffersReviewedServerChangeEntryPoint(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "server-change-unique-2026", Name: "Server Change Production", DiscordGuildID: "synthetic-guild"})
	w := httptest.NewRecorder()
	renderIAProductionList(w, httptest.NewRequest("GET", "/bot/admin/projects?project=server-change-unique-2026&lang=en", nil), db, "")
	body := w.Body.String()
	if !strings.Contains(body, "Review Discord server change") || !strings.Contains(body, "/bot/setup?lang=en&amp;project=server-change-unique-2026&amp;wizard_step=3") {
		t.Fatal("selected Production does not expose the reviewed server-change flow")
	}
	if strings.Contains(body, `name="guild_id"`) {
		t.Fatal("selected Production exposes a raw server ID editor")
	}
}

func TestGlobalUserMappingUsesSafeDiscordDisplayName(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.UserMap{KitsuName: "Synthetic Kitsu User", DiscordID: "123456789012345678", DiscordDisplayName: "Synthetic Discord Reviewer"})
	w := httptest.NewRecorder()
	renderGlobalUserMapping(w, httptest.NewRequest("GET", "/bot/admin/users?lang=en", nil), db)
	body := w.Body.String()
	if !strings.Contains(body, "Synthetic Discord Reviewer") {
		t.Fatal("global mapping does not show the safe Discord display name")
	}
	if strings.Contains(body, "synthetic-discord-id") {
		t.Fatal("global mapping leaked the raw Discord identifier")
	}
	if strings.Contains(body, "Discord user mapped") {
		t.Fatal("global mapping used generic identity text instead of the stored display name")
	}
	if strings.Count(body, "<h1") != 1 {
		t.Fatalf("global mapping should have exactly one h1, got %d", strings.Count(body, "<h1"))
	}
}

func TestGlobalUserMappingJapaneseHasNoMojibakeOrDecorativeStatusGlyph(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.UserMap{KitsuName: "Synthetic Kitsu User", DiscordID: "123456789012345678", DiscordDisplayName: "安全なDiscord表示名"})
	w := httptest.NewRecorder()
	renderGlobalUserMapping(w, httptest.NewRequest("GET", "/bot/admin/users?lang=ja", nil), db)
	body := w.Body.String()
	for _, marker := range []string{"\u7ab6", "\u8b41", "\u7e67", "\ufffd", "aria-hidden=\"true\""} {
		if strings.Contains(body, marker) {
			t.Fatalf("Japanese Global User Linking HTML contains mojibake or decorative glyph marker %q", marker)
		}
	}
	if !strings.Contains(body, "安全なDiscord表示名") || !strings.Contains(body, "紐づけ済み") {
		t.Fatal("Japanese Global User Linking did not render the safe display name and plain status")
	}
}

func TestSyntheticFixtureUserLinkingIsUnavailableAndDisabled(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "fixture-production", Name: "Fixture Production", DiscordGuildID: "qa-guild-active"})
	user := &model.UserMap{KitsuName: "Fixture User"}
	db.Create(user)
	w := httptest.NewRecorder()
	renderGlobalUserLinkForm(w, httptest.NewRequest("GET", "/bot/admin/users?edit=1&lang=ja", nil), db, user)
	body := w.Body.String()
	if !strings.Contains(body, "この検証用Productionでは実際のDiscordユーザーを取得できません。") {
		t.Fatal("fixture explanation was not shown")
	}
	if !strings.Contains(body, "name=\"discord_user_id\"") || !strings.Contains(body, "disabled") {
		t.Fatal("fixture linking selector or disabled Save state is missing")
	}
	if strings.Contains(body, "Bot接続を確認") {
		t.Fatal("fixture state incorrectly presented a Bot permission action")
	}
}

func TestDiscordMemberListRejectsSyntheticIDsBeforeNetwork(t *testing.T) {
	_, err := ListGuildMembers("qa-guild-active", "synthetic-token")
	if err == nil {
		t.Fatal("expected synthetic fixture ID to fail closed")
	}
	var failure *discordMemberListFailure
	if !errors.As(err, &failure) || failure.Kind != discordMemberFailureFixture {
		t.Fatalf("expected fixture classification, got %v", err)
	}
}

func TestDiscordMemberListAcceptsOnlySnowflakeIDs(t *testing.T) {
	for _, tc := range []struct {
		id    string
		valid bool
	}{{"12345678901234567", true}, {"12345678901234567890", true}, {"guild-123", false}, {"123", false}} {
		if got := isDiscordSnowflake(tc.id); got != tc.valid {
			t.Fatalf("snowflake validation for %q = %v, want %v", tc.id, got, tc.valid)
		}
	}
}

func TestDiscordMemberListEndpointUsesSafeGETShape(t *testing.T) {
	first, err := discordGuildMembersEndpoint("123456789012345678", "")
	if err != nil || !strings.Contains(first, "/guilds/123456789012345678/members?limit=1000") || strings.Contains(first, "after=") {
		t.Fatalf("unexpected first member-list endpoint: %q, %v", first, err)
	}
	next, err := discordGuildMembersEndpoint("123456789012345678", "123456789012345679")
	if err != nil || !strings.Contains(next, "limit=1000&after=123456789012345679") {
		t.Fatalf("unexpected paginated member-list endpoint: %q, %v", next, err)
	}
}

func TestDiscordMemberListClassifiesSafeFailures(t *testing.T) {
	malformed := classifyDiscordMemberListFailure(400, []byte(`{"message":"Invalid Form Body","code":50035}`))
	if malformed.Kind != discordMemberFailureMalformed || malformed.Code != 50035 {
		t.Fatalf("unexpected malformed classification: %+v", malformed)
	}
	intent := classifyDiscordMemberListFailure(403, []byte(`{"message":"Privileged intent is required","code":0}`))
	if intent.Kind != discordMemberFailureIntent {
		t.Fatalf("unexpected intent classification: %+v", intent)
	}
	message := globalDiscordMemberLoadMessage("en", malformed)
	if strings.Contains(message, "Invalid Form Body") || !strings.Contains(message, "Discord users could not be loaded") || !strings.Contains(message, "Diagnostic details") {
		t.Fatal("member-list error exposed raw API text or omitted safe diagnostic details")
	}
}

func TestGlobalUserLinkFormUsesSafeDiscordSelection(t *testing.T) {
	db := newIAViewDB(t)
	user := &model.UserMap{KitsuName: "Synthetic Kitsu User", DiscordID: "synthetic-discord-id", DiscordDisplayName: "Synthetic Discord Reviewer"}
	db.Create(user)
	w := httptest.NewRecorder()
	renderGlobalUserLinkForm(w, httptest.NewRequest("GET", "/bot/admin/users?edit=1&lang=en", nil), db, user)
	body := w.Body.String()
	for _, forbidden := range []string{`name="discord_id"`, `name="discord_display_name"`, "Production selector", "Reviewer / Checker task types"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("global link form exposed legacy control %q", forbidden)
		}
	}
	for _, want := range []string{`name="discord_user_id"`, `id="global-discord-user"`, "Discord users could not be loaded"} {
		if !strings.Contains(body, want) {
			t.Fatalf("global link form missing safe selection affordance %q", want)
		}
	}
	if strings.Contains(body, "synthetic-discord-id") {
		t.Fatal("global link form leaked the raw Discord identifier")
	}
}

func TestGlobalUserLinkingModelStoresStableKitsuAndDiscordServerIdentity(t *testing.T) {
	db := newIAViewDB(t)
	user := model.UpsertUserMapWithIdentity(db, "kitsu-user-1", "kitsu bot", "bot@example.invalid", "123456789012345678", "123456789012345679", "Discord Bot")
	if user == nil || user.KitsuID != "kitsu-user-1" || user.DiscordGuildID != "123456789012345678" || user.DiscordID != "123456789012345679" {
		t.Fatalf("stable identities were not persisted: %+v", user)
	}
	model.UpsertUserMapWithIdentity(db, "kitsu-user-1", "kitsu bot", "bot@example.invalid", "123456789012345678", "123456789012345680", "Discord Bot 2")
	if got := len(model.ListUserMap(db)); got != 1 {
		t.Fatalf("identity upsert created duplicate rows: %d", got)
	}
}

func TestGlobalUserLinkingDoesNotRequireConnectedProduction(t *testing.T) {
	db := newIAViewDB(t)
	if len(model.ListProjects(db)) != 0 {
		t.Fatal("test must start with zero connected Productions")
	}
	directory := globalDiscordDirectory{Guilds: []DiscordGuild{{ID: "123456789012345678", Name: "Test server"}}, SelectedGuild: DiscordGuild{ID: "123456789012345678", Name: "Test server"}, Options: []globalDiscordUserOption{{ID: "123456789012345679", Name: "Discord User"}}}
	if len(directory.Options) != 1 || directory.SelectedGuild.Name != "Test server" {
		t.Fatal("global directory should be independent from Production rows")
	}
}

func TestGlobalUserLinkingUsesTruthfulUnavailableState(t *testing.T) {
	db := newIAViewDB(t)
	t.Setenv("KITSUJWTToken", "")
	t.Setenv("KitsuJWTToken", "")
	w := httptest.NewRecorder()
	renderGlobalUserLinking(w, httptest.NewRequest("GET", "/bot/admin/users?lang=ja", nil), db)
	body := w.Body.String()
	if strings.Contains(body, "not_set") || strings.Contains(body, "KitsuSync Bot") {
		t.Fatal("global User Linking exposed a placeholder or synthetic user")
	}
	if !strings.Contains(body, "Discord Bot token is not configured") && !strings.Contains(body, "Discord") {
		t.Fatal("global User Linking did not expose a truthful Discord blocker")
	}
}

func TestBotConnectionNormalSurfaceUsesSeparatedSafeLabels(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderBotSettingsPage(w, httptest.NewRequest("GET", "/bot/admin/bot?lang=ja", nil), db)
	body := w.Body.String()
	for _, forbidden := range []string{"Bot設定", "Discord設定", "Kitsu Runtime", "Runtimeメール", "Runtimeパスワード", "preview_not_connected", "not_set"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("normal Japanese Bot Connection surface contains legacy/internal text %q", forbidden)
		}
	}
	for _, required := range []string{"Bot接続", "Kitsu接続", "Discord Bot接続", "未設定"} {
		if !strings.Contains(body, required) {
			t.Fatalf("normal Japanese Bot Connection surface is missing %q", required)
		}
	}
	w = httptest.NewRecorder()
	renderBotSettingsPage(w, httptest.NewRequest("GET", "/bot/admin/bot?lang=en", nil), db)
	body = w.Body.String()
	for _, required := range []string{"Connections", "Kitsu connection", "Discord Bot connection", "Not set"} {
		if !strings.Contains(body, required) {
			t.Fatalf("normal English Bot Connection surface is missing %q", required)
		}
	}
}

func checkConnectionsPageUsesSafeNameAndUnescapedStatusMarkup(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderConnectionsPage(w, httptest.NewRequest("GET", "/bot/admin/bot?lang=ja", nil), db)
	body := w.Body.String()
	for _, want := range []string{"接続設定", "Kitsu接続", "Discord Bot接続", "未設定"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Connections page missing %q", want)
		}
	}
	if strings.Contains(body, "&lt;span") || strings.Contains(body, "Bot設定") || strings.Contains(body, "Runtime") {
		t.Fatal("Connections page contains escaped markup or internal legacy terminology")
	}
	if got := strings.Count(body, "<h1"); got != 1 {
		t.Fatalf("Connections page has %d h1 elements, want 1", got)
	}
}

func TestConnectionsPageUsesCatalogLabelsAndUnescapedStatusMarkup(t *testing.T) {
	t.Setenv("KITSUSYNC_LOCAL_PROFILE", "1")
	t.Setenv("KITSU_HOSTNAME", "")
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	BotHandler(db, nil)(w, httptest.NewRequest("GET", "/bot/admin/bot?lang=ja", nil))
	body := w.Body.String()
	for _, want := range []string{"\u63a5\u7d9a\u8a2d\u5b9a", "Kitsu\u63a5\u7d9a", "Discord Bot\u63a5\u7d9a", "\u672a\u8a2d\u5b9a"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Connections page missing %q", want)
		}
	}
	if strings.Contains(body, `&lt;span class="status-pill`) || !strings.Contains(body, `<span class="status-pill `) || !strings.Contains(body, `role="status">`) {
		t.Fatal("Connections page contains escaped status markup")
	}
	if strings.Contains(body, `&amp;lt;span`) {
		t.Fatal("Connections page contains visible literal status markup")
	}
	if !strings.Contains(body, "127.0.0.1:8080") {
		t.Fatal("Connections page did not show a meaningful configured host")
	}
	if got := strings.Count(body, "<h1"); got != 1 {
		t.Fatalf("Connections page has %d h1 elements, want 1", got)
	}
	pageCard := strings.Index(body, `<div class="page-card glass">`)
	connectionsCard := strings.Index(body, `<section class="section-card glass connections-card">`)
	h1 := strings.Index(body, "<h1")
	if pageCard < 0 || connectionsCard < 0 || h1 < pageCard || h1 > connectionsCard {
		t.Fatal("Connections page does not use the shared page heading above its content card")
	}
	if strings.Count(body, `class="section-card glass connections-card"`) != 2 || !strings.Contains(body, `class="connections-summary-grid"`) {
		t.Fatal("Connections page does not use two peer summary cards")
	}
	if !strings.Contains(body, `class="button-row connections-actions"`) {
		t.Fatal("Connections page action row is missing from the content card")
	}

	w = httptest.NewRecorder()
	BotHandler(db, nil)(w, httptest.NewRequest("GET", "/bot/admin/bot?lang=en", nil))
	enBody := w.Body.String()
	if strings.Count(enBody, "<h1") != 1 || !strings.Contains(enBody, `<section class="section-card glass connections-card">`) {
		t.Fatal("English Connections page does not preserve the shared page-shell structure")
	}
}

func TestConnectionsSummaryUsesTwoExplicitPeerCards(t *testing.T) {
	db := newSetupStateTestDB(t)
	body := renderConnectionsDisplayBodyWithHealth("en", httptest.NewRequest("GET", "/bot/admin/bot?lang=en", nil), db, "", "ok", "Connected", "https://kitsu.example.test", "configured-token", true, true)
	if strings.Count(body, `class="section-card glass connections-card"`) != 2 {
		t.Fatalf("expected two summary cards, got %d", strings.Count(body, `class="section-card glass connections-card"`))
	}
	if !strings.Contains(body, `class="connections-summary-grid"`) || !strings.Contains(body, "Kitsu connection") || !strings.Contains(body, "Discord Bot connection") {
		t.Fatal("summary does not render explicit peer service cards")
	}
	if strings.Contains(body, "Authentication:") || strings.Contains(body, "KitsuSync") || strings.Contains(body, "Hidden") {
		t.Fatal("summary exposes redundant identity or secret metadata")
	}
	if strings.Contains(body, "configured-token") || strings.Contains(body, "Saved token is not displayed.") || !strings.Contains(body, "••••••••••••") {
		t.Fatal("summary secret handling is incorrect")
	}
	if strings.Contains(body, `connections-card-header"><div>`) || strings.Contains(body, `role="status">Connected</span></div><section`) {
		t.Fatal("summary retained the redundant combined status header")
	}
	body = renderConnectionsDisplayBodyWithHealth("ja", httptest.NewRequest("GET", "/bot/admin/bot?lang=ja", nil), db, "", "ok", "接続済", "https://kitsu.example.test", "configured-token", true, true)
	if !strings.Contains(body, "Kitsu接続") || !strings.Contains(body, "Discord Bot接続") || strings.Contains(body, "configured-token") {
		t.Fatal("Japanese summary did not preserve explicit service labels or secret safety")
	}
}

func TestWizardLiveProductionSelectionTargetsServerStep(t *testing.T) {
	db := newIAViewDB(t)
	production := KitsuProject{ID: "live-production-id", Name: "Live Production"}
	r := httptest.NewRequest("GET", "/bot/setup?lang=en&wizard_step=2", nil)
	body := renderWizardProductionLocalized("en", r, db, []KitsuProject{production})
	if strings.Contains(body, "disabled") {
		t.Fatal("unconnected live Production was disabled")
	}
	if !strings.Contains(body, `value="live-production-id"`) {
		t.Fatal("live Production stable ID was not rendered")
	}
	if !strings.Contains(body, `name="wizard_step" value="3"`) {
		t.Fatal("Production selection form does not target Step 3")
	}
}

func TestUserLinkingSaveStartsDisabledAndTracksChangedSelection(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.UserMap{KitsuID: "user-1", KitsuName: "User One", DiscordID: "123456789012345678", DiscordDisplayName: "Discord One"})
	w := httptest.NewRecorder()
	renderGlobalUserLinking(w, httptest.NewRequest("GET", "/bot/admin/users?lang=en", nil), db)
	body := w.Body.String()
	if !strings.Contains(body, `type="submit" disabled`) {
		t.Fatal("User Linking Save was not disabled initially")
	}
	if !strings.Contains(body, "data-initial-index") || !strings.Contains(body, "selectedIndex === Number(this.dataset.initialIndex)") {
		t.Fatal("User Linking did not include unchanged-selection dirty-state handling")
	}
	if !strings.Contains(body, `class="user-link-grid-row"`) || !strings.Contains(body, "data-label=") || !strings.Contains(body, "user-link-actions") {
		t.Fatal("User Linking did not render the shared responsive grid structure")
	}
	if strings.Contains(body, "123456789012345678") {
		t.Fatal("User Linking rendered a raw Discord ID")
	}
}

func TestDuplicateTaskTypePlanRendersDistinctLabelsAndGuidance(t *testing.T) {
	project := model.Project{KitsuProjectID: "production-1", DiscordGuildID: "guild-1", Name: "Live Production"}
	types := []kitsu.TaskType{{ID: "tt-2", Name: "Concept"}, {ID: "tt-1", Name: "Concept"}}
	jp := renderTaskTypeChannelPlanCard(project, nil, types, "ja")
	en := renderTaskTypeChannelPlanCard(project, nil, types, "en")
	for _, body := range []string{jp, en} {
		if !strings.Contains(body, "Concept (1)") || !strings.Contains(body, "Concept (2)") {
			t.Fatalf("duplicate Task Types were not distinguishable: %s", body)
		}
		if strings.Contains(body, "tt-1") || strings.Contains(body, "tt-2") {
			t.Fatal("duplicate Task Type guidance exposed raw IDs")
		}
	}
	if !strings.Contains(tr("ja", "channel_plan.duplicate_name"), "Discord") || !strings.Contains(tr("en", "channel_plan.duplicate_name"), "Multiple Task Types have the same name") {
		t.Fatal("duplicate Task Type explanation is not localized")
	}
}

func TestReadModelProvenanceUsesProductionAndUserLinkingSources(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "local-production-id", Name: "Local Production"})
	db.Create(&model.UserMap{KitsuID: "local-user-id", KitsuName: "Local User", DiscordID: "123456789012345678"})
	snapshot := ReadModelProvenanceSnapshot(db)
	if len(snapshot.Productions) != 1 || snapshot.Productions[0].Source != "local_sqlite" || !snapshot.Productions[0].ConnectedLocally {
		t.Fatalf("unexpected Production provenance: %+v", snapshot.Productions)
	}
	if len(snapshot.Users) != 1 || snapshot.Users[0].Source != "local_user_map" || !snapshot.Users[0].LocalMapping {
		t.Fatalf("unexpected user provenance: %+v", snapshot.Users)
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"email", "password", "token", "discord_id", "authorization"} {
		if strings.Contains(strings.ToLower(string(body)), forbidden) {
			t.Fatalf("provenance exposed forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestReadOnlyPreviewStatusDoesNotRenderInternalKey(t *testing.T) {
	db := newIAViewDB(t)
	class, label, hint := iaStatus(db, model.Project{ReadOnlyPreview: true}, "en")
	if class != "warning" || label != "Disconnected" || strings.Contains(label, "preview_not_connected") || !strings.Contains(hint, "Notifications are unavailable") {
		t.Fatalf("unexpected preview status: %q %q %q", class, label, hint)
	}
}

func TestReadOnlyProductionUsesDedicatedUnconnectedView(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "live-unconnected", Name: "KitsuSync Local Test", ReadOnlyPreview: true}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=live-unconnected&lang=en", nil)
	renderIASelectedProduction(w, r, db, p, "")
	body := w.Body.String()
	for _, expected := range []string{"KitsuSync Local Test", "Loaded from Kitsu", "Disconnected", "Configure connection"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("unconnected Production view missing %q: %s", expected, body)
		}
	}
	for _, forbidden := range []string{"production-tabs", "Danger Zone", "Notification state", "User settings", "Task Type"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("unconnected Production view exposed connected-only content %q", forbidden)
		}
	}
	var count int64
	if err := db.Model(&model.Project{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rendering unconnected Production changed local project rows: %d", count)
	}
}

func TestProductionUserSettingsShowsParticipantDisplayName(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "participant-display-p", Name: "Participant Display Production"}
	db.Create(&p)
	db.Create(&model.UserMap{KitsuName: "Synthetic Participant", DiscordID: "123456789012345678", DiscordDisplayName: "Synthetic Discord Name"})
	db.Create(&model.ProjectUserMap{ProjectID: p.ID, KitsuName: "Synthetic Participant"})
	body := renderSelectedProductionUserSettings(db, httptest.NewRequest("GET", "/bot/admin/projects?project=participant-display-p&tab=users&lang=en", nil), p, "en")
	if !strings.Contains(body, "Synthetic Discord Name") {
		t.Fatal("Production User Settings did not show the linked Discord display name")
	}
	if strings.Contains(body, "synthetic-discord-id") {
		t.Fatal("Production User Settings leaked the raw Discord identifier")
	}
}

func TestBotAndSystemStatusUseActualPrerequisiteValues(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/bot/admin/health?lang=en", nil)
	renderIAHealth(w, r, db)
	body := w.Body.String()
	for _, want := range []string{"Not configured", "Discord API", "Notification processing", "Internal data", "Recent system issues"} {
		if !strings.Contains(body, want) {
			t.Fatalf("system status missing %q", want)
		}
	}
	w = httptest.NewRecorder()
	renderIABot(w, r, db)
	if !strings.Contains(w.Body.String(), "Action required") || !strings.Contains(w.Body.String(), "View channels") {
		t.Fatal("Bot Connection does not expose state and permission list")
	}
}

func TestGlobalUserLinkingUsesApprovedJapaneseTerm(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderGlobalUserMapping(w, httptest.NewRequest("GET", "/bot/admin/users?lang=ja", nil), db)
	body := w.Body.String()
	if !strings.Contains(body, "\u30e6\u30fc\u30b6\u30fc\u7d10\u3065\u3051") {
		t.Fatal("global user linking did not use the approved Japanese term")
	}
	if strings.Contains(body, "\u30e6\u30fc\u30b6\u30fc\u5bfe\u5fdc\u4ed8\u3051") {
		t.Fatal("legacy Japanese user-management term remains in normal UI")
	}
	if !strings.Contains(body, "Kitsu\u30e6\u30fc\u30b6\u30fc\u3068Discord\u30e6\u30fc\u30b6\u30fc\u3092\u7d10\u3065\u3051\u307e\u3059\u3002") {
		t.Fatal("global user linking description is missing")
	}
}

func TestFixtureUserLinkingDoesNotDuplicateStatusText(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.UserMap{KitsuName: "Fixture User", DiscordID: "synthetic-discord-id", DiscordDisplayName: "Fixture Discord User"})
	w := httptest.NewRecorder()
	renderGlobalUserMapping(w, httptest.NewRequest("GET", "/bot/admin/users?lang=ja", nil), db)
	body := w.Body.String()
	start := strings.LastIndex(body, "Fixture User")
	end := strings.Index(body[start:], "</tr>")
	if start < 0 || end < 0 {
		t.Fatal("fixture user row is missing")
	}
	row := body[start : start+end]
	if !strings.Contains(row, "Fixture data") && !strings.Contains(row, "\u691c\u8a3c\u7528\u30c7\u30fc\u30bf") {
		t.Fatal("fixture status is missing")
	}
	if strings.Contains(row, "Fixture Discord User") || strings.Contains(row, "synthetic-discord-id") {
		t.Fatal("fixture row claims a real Discord identity or exposes its raw ID")
	}
}

func TestProductionUserSettingsEmptyStatesHaveNoDecorativeBullets(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "empty-user-settings", Name: "Empty User Settings"}
	db.Create(&p)
	body := renderSelectedProductionUserSettings(db, httptest.NewRequest("GET", "/bot/admin/projects?tab=users&lang=ja", nil), p, "ja")
	if strings.Contains(body, "empty-state-mark") || strings.Contains(body, "aria-hidden=\"true\"") || strings.Contains(body, "•") {
		t.Fatal("Production User Settings empty state contains a decorative bullet")
	}
}

func TestActiveUserLinkingRemovesRedundantDescription(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderGlobalUserLinking(w, httptest.NewRequest("GET", "/bot/admin/users?lang=ja", nil), db)
	body := w.Body.String()
	if strings.Contains(body, "KitsuユーザーとDiscordユーザーを紐づけます。") {
		t.Fatal("active User Linking still renders the redundant description")
	}
}

func TestNewConnectionHasOnePageHeading(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIANewConnection(w, httptest.NewRequest("GET", "/bot/setup?lang=ja", nil), db)
	body := w.Body.String()
	if got := strings.Count(body, "<h1"); got != 1 {
		t.Fatalf("New Production Connection rendered %d h1 elements, want 1", got)
	}
	if strings.Contains(body, `<section class="section-card glass"><h1`) {
		t.Fatal("New Production Connection page title must sit outside the content card")
	}
}

func TestSystemStatusRoutingDistinguishesWaitingFromRoutingFailure(t *testing.T) {
	cases := []struct {
		name, lang, want, forbidden string
	}{
		{"waiting-jp", "ja", "\u63a5\u7d9a\u5f85\u3061", "要確認"},
		{"waiting-en", "en", "Waiting", "Needs review"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pipelineRoutingValue(tc.lang, SharedBotRuntimeReadiness{PrerequisitesReady: true})
			if got != tc.want {
				t.Fatalf("zero connected Productions routing label = %q, want %q", got, tc.want)
			}
			if got == tc.forbidden {
				t.Fatalf("zero connected Productions must not be a routing failure: %q", got)
			}
		})
	}

	if got := pipelineRoutingValue("en", SharedBotRuntimeReadiness{ProductionConnected: true}); got != "Needs review" {
		t.Fatalf("connected Production without valid routing = %q, want Needs review", got)
	}
	if got := pipelineRoutingValue("en", SharedBotRuntimeReadiness{ProductionConnected: true, RoutingReady: true}); got != "Configured" {
		t.Fatalf("connected Production with valid routing = %q, want Configured", got)
	}
}

func TestDashboardRefinedMenuOrderHasNoNumericIndicators(t *testing.T) {
	db := newIAViewDB(t)
	readiness := SharedBotRuntimeReadiness{}
	body := renderDashboardMenuRefined("en", httptest.NewRequest("GET", "/bot/admin?lang=en", nil), db, nil, 0, readiness)
	order := []string{"Connections", "Production list", "User Linking", "System Status", "Audit Log"}
	last := -1
	for _, label := range order {
		pos := strings.Index(body, ">"+label+"<")
		if pos <= last {
			t.Fatalf("dashboard card order is incorrect for %q", label)
		}
		last = pos
	}
	if strings.Contains(body, `dashboard-menu-icon`) || strings.Contains(body, `>1<`) || strings.Contains(body, `>2<`) {
		t.Fatal("dashboard management cards contain numeric indicators")
	}
	if !strings.Contains(body, "Connect a Kitsu Production to a Discord server.") {
		t.Fatal("dashboard CTA is missing its concise supporting description")
	}
	if strings.Count(body, `class="dashboard-status-chip warning">Unavailable<`) != 1 {
		t.Fatal("system status card should expose one distinct notification state")
	}
	if strings.Contains(body, `class="dashboard-status-chip warning">Waiting<`) {
		t.Fatal("system status card should not duplicate its overall state as a second badge")
	}
}

func TestDashboardRefinedMenuUsesConfiguredStateAndConnectedCount(t *testing.T) {
	db := newIAViewDB(t)
	readiness := SharedBotRuntimeReadiness{KitsuConfigured: true, DiscordConfigured: true, OverallReady: true}
	body := renderDashboardMenuRefined("en", httptest.NewRequest("GET", "/bot/admin?lang=en", nil), db, []model.Project{{Name: "Connected", KitsuProjectID: "connected"}}, 0, readiness)
	if !strings.Contains(body, `class="dashboard-status-chip ok">Configured<`) {
		t.Fatal("configured User Linking state should use the positive status class")
	}
	if !strings.Contains(body, `Kitsu Connected`) || !strings.Contains(body, `Discord Connected`) {
		t.Fatal("Connections card should label Kitsu and Discord statuses independently")
	}
}

func TestStatusPolishUsesFixedSecretMaskAndRealSparkline(t *testing.T) {
	if got := secretMaskDisplay("en", true); got != "\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022" {
		t.Fatalf("unexpected secret mask %q", got)
	}
	if got := secretMaskDisplay("en", false); got != "Not configured" {
		t.Fatalf("unexpected unconfigured secret label %q", got)
	}
	items := []APIObservation{{Duration: 10 * time.Millisecond, Success: true}, {Duration: 20 * time.Millisecond, Success: false}}
	graph := apiObservationGraph(items)
	if strings.Count(graph, "<rect") != 2 || !strings.Contains(graph, "bar-success") || !strings.Contains(graph, "bar-failure") || strings.Contains(graph, "<polyline") {
		t.Fatal("bar graph did not reflect the recorded observations")
	}
	if strings.Contains(apiObservationGraph(nil), "polyline") {
		t.Fatal("empty telemetry should not render a fake graph")
	}
	if strings.Count(apiObservationGraph([]APIObservation{{Duration: 10 * time.Millisecond}}), "<rect") != 1 {
		t.Fatal("a single telemetry sample should render one bar")
	}
}

func TestDashboardRendersPrimaryContentBeforeManagementMenu(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIADashboard(w, httptest.NewRequest("GET", "/bot/admin?lang=en", nil), db)
	body := w.Body.String()
	positions := []string{`<section class="dashboard-intro">`, `class="dashboard-summary-grid"`, `dashboard-queue"`, `dashboard-cta"`, `dashboard-menu"`}
	last := -1
	for _, marker := range positions {
		pos := strings.Index(body[last+1:], marker)
		if pos >= 0 {
			pos += last + 1
		}
		if pos <= last {
			t.Fatalf("dashboard marker %q is out of order", marker)
		}
		last = pos
	}
}

func TestDashboardConnectionsCardStaysContainedAndCSSDoesNotReorderDOM(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIADashboard(w, httptest.NewRequest("GET", "/bot/admin?lang=en", nil), db)
	body := w.Body.String()
	start := strings.Index(body, `class="dashboard-menu-card"`)
	if start < 0 {
		t.Fatal("Dashboard management cards are missing")
	}
	end := strings.Index(body[start:], `</a>`)
	if end < 0 {
		t.Fatal("Connections card is incomplete")
	}
	card := body[start : start+end]
	if !strings.Contains(body, `class="dashboard-service-status"`) || strings.Contains(card, `Connected 1`) {
		t.Fatal("Production count is intruding into the Connections card")
	}
	if strings.Contains(body, `dashboard-menu-card-connections`) || !strings.Contains(adminThemeCSS, `.dashboard-menu-grid{display:grid;grid-template-columns:repeat(5,minmax(0,1fr))`) {
		t.Fatal("Dashboard management cards do not use the shared equal-width grid")
	}
	if !strings.Contains(adminThemeCSS, `.dashboard-service-status{display:grid`) || !strings.Contains(adminThemeCSS, `.dashboard-service-status>span{display:grid;grid-template-columns:minmax(0,1fr) auto`) {
		t.Fatal("Connections statuses are not using the shared vertical row layout")
	}
	if !strings.Contains(adminThemeCSS, `.section-stack>.dashboard-intro,.section-stack>.dashboard-summary-grid`) || !strings.Contains(adminThemeCSS, `{order:initial}`) {
		t.Fatal("Dashboard source order is not protected from obsolete CSS reordering")
	}
}

func TestConnectionsUseSharedActionSpacingToken(t *testing.T) {
	if !strings.Contains(adminThemeCSS, `--space-action-section:24px`) || !strings.Contains(adminThemeCSS, `.button-row.connections-actions,.button-row.connections-navigation{gap:12px;margin-top:var(--space-action-section,24px)}`) {
		t.Fatal("normal and edit Connections actions do not share the canonical spacing rule")
	}
}

func TestSystemStatusUsesExpandableSafeDetailsAndRefreshSnapshot(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIAHealth(w, httptest.NewRequest("GET", "/bot/admin/health?lang=en", nil), db)
	body := w.Body.String()
	if strings.Count(body, `<details class="pipeline-health-details">`) < 4 {
		t.Fatalf("system status details are not expandable: %d", strings.Count(body, `<details class="pipeline-health-details">`))
	}
	if !strings.Contains(body, `data-system-status-refresh`) {
		t.Fatal("system status does not include the bounded snapshot refresh marker")
	}
	if !strings.Contains(body, `window.setInterval(refresh,interval)`) {
		t.Fatal("system status does not include the bounded snapshot interval")
	}
	if !strings.Contains(body, `new Date(item.at).getTime()`) || !strings.Contains(body, `windowMs=select.value`) {
		t.Fatal("system status graph does not use timestamp-based positions")
	}
	if !strings.Contains(body, `function serviceScale(name,items)`) || !strings.Contains(body, `updateCard("kitsu",observations.kitsu||[],serviceScale("kitsu",observations.kitsu||[]))`) {
		t.Fatal("system status refresh does not apply independent service Y scales")
	}
	if !strings.Contains(body, `30s`) {
		t.Fatal("system status refresh is missing compact 30s label")
	}
	if !strings.Contains(body, `2m30s`) {
		t.Fatal("system status refresh is missing compact 2m30s label")
	}
	if !strings.Contains(body, `chart-tick`) || !strings.Contains(body, `chart-guide`) {
		t.Fatal("system status refresh is missing readable shared chart ticks or guide")
	}
	if !strings.Contains(body, `.system-status-sections .api-observation-meta{font-size:14px}`) || !strings.Contains(body, `.system-status-sections .api-sparkline .chart-axis-label,.system-status-sections .api-sparkline .chart-tick,.system-status-sections .api-sparkline .chart-time-label{font-size:12px}`) {
		t.Fatal("system status text sizing rules are missing")
	}
	if strings.Contains(body, `x1='2' y1='8' x2='28' y2='82`) || !strings.Contains(body, `x1='54' y1='8' x2='54' y2='82`) {
		t.Fatal("system status refresh contains a diagonal or missing vertical axis")
	}
	if !strings.Contains(body, `viewBox='0 0 466 104'`) || !strings.Contains(body, `x1='54' y1='82' x2='464' y2='82'`) {
		t.Fatal("system status refresh does not use the full-width chart viewBox")
	}
	if !strings.Contains(body, `class="api-observation-details"`) {
		t.Fatal("system status cards do not reserve shared detail geometry")
	}
	if strings.Contains(body, "telemetry-history") {
		t.Fatal("ambiguous telemetry dot history remains")
	}
	if strings.Contains(body, " / 20 observations") || strings.Contains(body, "Last 5 minutes · Last updated") {
		t.Fatal("normal API card still exposes sample count or duplicate window metadata")
	}
	if !strings.Contains(body, `data-telemetry-meta`) || !strings.Contains(body, `Last updated`) {
		t.Fatal("normal API card is missing the last-updated metadata")
	}
}

func TestProductionListRowsRemoveRedundantStatusCopy(t *testing.T) {
	rows := `<article class="section-card glass production-list-item"><div><h2>Test</h2><p class="field-help">Current state</p></div><div class="production-list-state"><span class="status-pill warning">Disconnected</span><span class="field-help">No route</span></div><a class="btn" href="#">Open Production</a></article>`
	clean := simplifyProductionListRows(rows)
	if strings.Contains(clean, "Current state") || strings.Contains(clean, "No route") {
		t.Fatal("redundant Production list copy remains")
	}
	if !strings.Contains(clean, "Disconnected") || !strings.Contains(clean, "Open Production") {
		t.Fatal("Production list lost its semantic status or action")
	}
}

func TestDisconnectedProductionCardIsCompactAndLocalized(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "disconnected-live", Name: "Live Production", ReadOnlyPreview: true}
	db.Create(&p)
	for _, lang := range []string{"ja", "en"} {
		body := renderDisconnectedProductionCard(lang, httptest.NewRequest("GET", "/bot/admin/projects?lang="+lang, nil), p)
		if !strings.Contains(body, "Live Production") || !strings.Contains(body, map[string]string{"ja": "未接続", "en": "Disconnected"}[lang]) {
			t.Fatalf("%s disconnected Production card is missing expected content", lang)
		}
		if strings.Contains(body, "Current state") || strings.Contains(body, "This Production is not connected to KitsuSync yet.") {
			t.Fatalf("%s disconnected Production card still contains redundant explanation", lang)
		}
	}
}

func TestProductionConnectionStateIsSharedByDashboardAndList(t *testing.T) {
	db := newIAViewDB(t)
	preview := model.Project{KitsuProjectID: "live-preview", Name: "Live Preview", ReadOnlyPreview: true}
	connected := model.Project{KitsuProjectID: "connected-local", Name: "Connected Local"}
	db.Create(&connected)
	projects := []model.Project{preview, connected}
	if got := connectedProductionCount(projects); got != 1 {
		t.Fatalf("connected Production count = %d, want 1", got)
	}
	if class, label := productionConnectionStatus(preview, "en"); class != "warning" || label != "Disconnected" {
		t.Fatalf("live-only Production status = %q/%q, want warning/Disconnected", class, label)
	}
	if class, label := productionConnectionStatus(connected, "en"); class != "ok" || label != "Connected" {
		t.Fatalf("local Production status = %q/%q, want ok/Connected", class, label)
	}
	w := httptest.NewRecorder()
	renderIAProductionList(w, httptest.NewRequest("GET", "/bot/admin/projects?lang=en", nil), db, "")
	body := w.Body.String()
	if !strings.Contains(body, `class="status-pill ok">Connected</span>`) {
		t.Fatal("Production list does not render the connected semantic state")
	}
	if strings.Contains(body, `min-width:170px`) {
		t.Fatal("Production status layout still reserves the old fixed-width column")
	}
	if got := replaceDashboardConnectedCount(`<div class="metric-value">2</div>`, 2, connectedProductionCount(projects)); got != `<div class="metric-value">1</div>` {
		t.Fatalf("Dashboard connected count rendering = %q, want 1", got)
	}
}

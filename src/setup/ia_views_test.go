package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/request"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newIAViewDB(t *testing.T) *gorm.DB {
	t.Helper()
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ia-views.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProjectSetting{}, &model.ProductionChannelMapping{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}, &model.AuditLog{}, &model.UserMap{}, &model.ProjectUserMap{}, &model.ProjectCheckerMap{}, &model.ProjectReviewerTarget{}, &model.ProductionNotificationWriteLock{}, &model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{}, &model.CheckerMap{}, &model.Setting{}, &model.ProductionForumConfig{}, &model.ProductionForumRoute{}, &model.ProductionForumStatusRole{}, &model.ProductionForumTag{}, &model.ProductionHandoffConfig{}, &model.ProductionHandoffProductType{}, &model.ProductionHandoffRoute{}, &model.ForumBaseline{}, &model.ForumTask{}, &model.StudioStatusPolicy{}, &model.StudioTaskRoute{}, &model.ProductionTaskRoute{}, &model.AdminException{}, &model.NotificationDecision{}); err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		// Windows cannot remove the temp SQLite file while its handle is open.
		t.Cleanup(func() { sqlDB.Close() })
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
	for _, tab := range []string{"overview", "notifications", "team", "settings"} {
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

func TestPipelineNotificationDiagnosticsExposeSafeDeliveryDisposition(t *testing.T) {
	db := newIAViewDB(t)
	event := model.NotificationDeliveryEvent{
		EventID: "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		TaskID:  "task-synthetic", ProductionID: "production-synthetic", TaskTypeID: "tt-synthetic", TaskStatus: "WFA",
		PayloadJSON: `{"content":"private synthetic content"}`, PayloadSHA256: strings.Repeat("a", 64),
		WebhookRowID: 1, DestinationGuildID: "guild-synthetic", DestinationChannelID: "channel-synthetic", DestinationWebhookID: "webhook-synthetic", DestinationFingerprint: strings.Repeat("b", 64),
		Disposition: model.DeliveryUnknown, FailureClass: "discord_server_error",
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	conflictAt := time.Now().UTC()
	conflict := model.NotificationDeliveryEvent{
		EventID: "identity-conflict-event", TaskID: "conflict-task", ProductionID: "production-synthetic", TaskTypeID: "tt-synthetic", TaskStatus: "WFA",
		PayloadJSON: "", PayloadSHA256: strings.Repeat("c", 64), PayloadRedactedAt: &conflictAt,
		WebhookRowID: 1, DestinationGuildID: "guild-synthetic", DestinationChannelID: "channel-synthetic", DestinationWebhookID: "webhook-synthetic", DestinationFingerprint: strings.Repeat("d", 64),
		Disposition: model.DeliveryDelivered, IdentityConflictCount: 3, IdentityConflictAt: &conflictAt,
	}
	if err := db.Create(&conflict).Error; err != nil {
		t.Fatal(err)
	}
	body := pipelineNotificationDetails("en", RuntimeSnapshot{}, db)
	for _, want := range []string{"1234567890ab", "unknown", "discord_server_error", "identity-conflicts", "3 conflicts", "event_identity_conflict"} {
		if !strings.Contains(body, want) {
			t.Errorf("safe delivery diagnostics missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"private synthetic content", "webhook-synthetic", "token"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("diagnostic exposed sensitive value %q", forbidden)
		}
	}
}

func TestPipelineNotificationDiagnosticsDoNotTreatReadFailureAsEmpty(t *testing.T) {
	body := pipelineNotificationDetails("en", RuntimeSnapshot{}, nil)
	if !strings.Contains(body, "Unavailable") {
		t.Fatalf("journal read failure appeared as an empty healthy journal: %s", body)
	}
}

func TestSystemStatusUsesCompactHealthySummaryAndSelectiveDiagnostics(t *testing.T) {
	item := pipelineHealthItem{label: "Event monitoring", value: "Running", class: "success", details: "<dl></dl>", detailsLabel: "Observation diagnostics"}
	rendered := renderPipelineHealthItem("en", item, 0)
	if strings.Contains(rendered, `class="field-help"`) {
		t.Fatal("healthy operational row should not repeat a redundant explanation")
	}
	if !strings.Contains(rendered, `class="pipeline-health-item"`) || !strings.Contains(rendered, `class="status-badge`) {
		t.Fatal("operational row lost its compact structure")
	}
	for _, want := range []string{`<details class="pipeline-health-diagnostic">`, `<summary>Observation diagnostics</summary>`, `class="pipeline-health-diagnostic-content"`} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("operational row missing geometry/detail contract %q", want)
		}
	}
	if strings.Contains(rendered, `>Details<`) || strings.Contains(rendered, `pipeline-health-details-toggle`) || strings.Contains(rendered, `aria-expanded`) {
		t.Fatal("operational row should not expose a generic Details control")
	}
	if noDetails := renderPipelineHealthItem("en", pipelineHealthItem{label: "Healthy", value: "Ready", class: "success"}, 0); strings.Contains(noDetails, "pipeline-health-details") {
		t.Fatal("operational row without useful diagnostics exposed a disclosure")
	}

	readiness := SharedBotRuntimeReadiness{KitsuConfigured: true, DiscordConfigured: true}
	body := renderRuntimeObservabilitySummary("en", RuntimeSnapshot{APIObservations: map[string][]APIObservation{"kitsu": {{At: time.Now(), Duration: 10 * time.Millisecond, Success: true}}, "discord": {{At: time.Now(), Duration: 12 * time.Millisecond, Success: true}}}}, readiness, telemetryWindow60Seconds, "<section class=\"system-observability\"></section>")
	if strings.Contains(body, `class="system-overall-summary"`) || strings.Contains(body, `system-overall-title`) {
		t.Fatal("System Status must not render a redundant page-level overall summary")
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
	t.Setenv("KITSU_API_BASE_URL", "")
	t.Setenv("KitsuJWTToken", "test-token")
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}

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
	for _, want := range []string{"Prerequisites", "Production", "Discord server", "Forum", "Review", "Execute", "Complete", "Configure the Kitsu connection"} {
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
	renderIAHealth(w, httptest.NewRequest("GET", "/bot/admin/health?lang=en", nil), db, time.Minute)
	body := w.Body.String()
	for _, label := range []string{"Kitsu API", "Event monitoring", "Connection / routing integrity", "Notification processing", "Internal data", "Discord API"} {
		if !strings.Contains(body, ">"+label+"<") {
			t.Fatalf("System Status missing row %q", label)
		}
	}
	if strings.Contains(body, "Recent system issues") || strings.Contains(body, "No recent issues.") {
		t.Fatal("empty Recent system issues section must be omitted")
	}
	if got := strings.Count(body, `class="pipeline-health-item"`); got != 4 {
		t.Fatalf("System Status rendered %d internal pipeline items, want 4", got)
	}
	for _, redundant := range []string{
		"Shows real response times in chronological order.",
		"Review each notification stage. Metrics that are not available are shown as unconfirmed.",
		"Recent failure and recovery records.",
	} {
		if strings.Contains(body, redundant) {
			t.Fatalf("System Status retained redundant helper copy %q", redundant)
		}
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
	renderIAHealth(w, httptest.NewRequest("GET", "/bot/admin/health?lang=ja", nil), db, time.Minute)
	body := w.Body.String()
	for _, want := range []string{"Kitsu API", "イベント監視", "接続・ルーティング整合性", "Discord API", "通知処理", "内部データ", "未確認"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Japanese System Status missing %q", want)
		}
	}
	if strings.Contains(body, "最近のシステム問題") {
		t.Fatal("empty Japanese Recent system issues section must be omitted")
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
	visibleBody := body
	if start := strings.Index(visibleBody, "<main"); start >= 0 {
		visibleBody = visibleBody[start:]
	}
	for {
		styleStart := strings.Index(visibleBody, "<style")
		scriptStart := strings.Index(visibleBody, "<script")
		start := styleStart
		if start < 0 || (scriptStart >= 0 && scriptStart < start) {
			start = scriptStart
		}
		if start < 0 {
			break
		}
		end := strings.Index(visibleBody[start:], "</style>")
		if scriptStart == start {
			end = strings.Index(visibleBody[start:], "</script>")
		}
		if end < 0 {
			break
		}
		visibleBody = visibleBody[:start] + visibleBody[start+end+len("</style>"):]
	}
	if strings.Contains(visibleBody, "<details") || strings.Contains(visibleBody, ">g<") || strings.Contains(visibleBody, ">c<") || strings.Contains(visibleBody, "Production ID") {
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
	for _, tc := range []struct{ requested, selected string }{{"", "overview"}, {"notifications", "notifications"}, {"team", "team"}, {"users", "team"}, {"user-settings", "team"}, {"reviewers", "notifications"}, {"storage-settings", "settings"}, {"activity", "overview"}, {"troubleshooting", "settings"}, {"advanced", "settings"}, {"danger-zone", "settings"}, {"invalid", "overview"}} {
		path := "/bot/admin/projects?project=tab-semantics-p&lang=en"
		if tc.requested != "" {
			path += "&tab=" + tc.requested
		}
		w := httptest.NewRecorder()
		renderIAProductionList(w, httptest.NewRequest("GET", path, nil), db, "")
		body := w.Body.String()
		if strings.Count(body, `role="tabpanel"`) != 1 || strings.Count(body, `role="tablist"`) < 1 {
			t.Fatalf("tab %q did not render one tab panel", tc.requested)
		}
		if !strings.Contains(body, `id="panel-`+tc.selected+`"`) {
			t.Fatalf("tab %q did not select %q", tc.requested, tc.selected)
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
		if strings.Count(body, `class="status-list delete-result-summary"`) != 1 || !strings.Contains(body, `class="status-list delete-result-summary"><div><dt>Production</dt><dd>`) {
			t.Fatalf("%s delete summary does not use aligned row structure", lang)
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
	for _, want := range []string{"Kitsu Task Type", "Forum", "Edit"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Notifications UI missing %q", want)
		}
	}
	if strings.Contains(body, "Notification routing") {
		t.Fatal("Notifications read mode should not repeat the tab's routing heading")
	}
	if strings.Contains(body, "Notification preview") || strings.Contains(body, "Example task") || strings.Contains(body, "discord-message-preview") {
		t.Fatal("Notifications retained the removed synthetic preview")
	}
	if !strings.Contains(body, "No notification routing is configured.") {
		t.Fatal("empty Notifications state should explain that no routed Task Types exist")
	}
	if strings.Contains(body, "Automatic recipients") || strings.Contains(body, "Additional recipients") {
		t.Fatal("empty routing state should not show recipient summaries without a routed Task Type")
	}
	if strings.Contains(body, `name="action" value="save"`) {
		t.Fatal("normal Notifications UI exposes routing editor controls")
	}
	for _, forbidden := range []string{"Check without sending", "Review notification settings", `name="action" value="dry_run"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("retired Notifications UI remains visible: %q", forbidden)
		}
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
	if !strings.Contains(userURL, "tab=notifications#wfa-recipients") || userLabel != "Review WFA recipients" {
		t.Fatalf("participant issue did not target WFA recipient settings: %q %q", userURL, userLabel)
	}
}

func TestDashboardAuditSummaryUsesCanonicalPersistedCount(t *testing.T) {
	for _, tc := range []struct {
		lang, countLabel, healthLabel, emptyLabel, action string
	}{
		{"ja", "3件の記録", "正常", "記録なし", "通知送信"},
		{"en", "3 records", "Normal", "No records", "Notification sent"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			db := newIAViewDB(t)
			r := httptest.NewRequest("GET", "/bot/admin/audit?lang="+tc.lang, nil)
			empty := renderDashboardMenuRefined(tc.lang, r, db, nil, 0, SharedBotRuntimeReadiness{}, nil)
			if !strings.Contains(empty, tc.emptyLabel) {
				t.Fatalf("dashboard omitted empty audit summary: %q", empty)
			}
			for i := 0; i < 3; i++ {
				model.WriteAuditLog(db, model.AuditLog{ProjectID: "audit-p", ProjectName: "Audit Production", DiscordMsgID: fmt.Sprintf("message-%d", i), Success: true})
			}
			if got := model.CountAuditLogs(db); got != 3 {
				t.Fatalf("canonical audit count = %d, want 3", got)
			}
			auditPage := httptest.NewRecorder()
			renderIAAudit(auditPage, r, db)
			auditBody := auditPage.Body.String()
			if !strings.Contains(auditBody, tc.action) {
				t.Fatalf("audit page omitted notification-send entry: %q", auditPage.Body.String())
			}
			if !strings.Contains(auditBody, `class="audit-log-content"`) || strings.Contains(auditBody, `<section class="section-card glass"><p class="hint">`) {
				t.Fatal("audit log retained a redundant inner card")
			}
			dashboard := renderDashboardMenuRefined(tc.lang, r, db, nil, 0, SharedBotRuntimeReadiness{}, nil)
			if !strings.Contains(dashboard, tc.countLabel) || !strings.Contains(dashboard, tc.healthLabel) {
				t.Fatalf("dashboard omitted canonical audit summary: %q", dashboard)
			}
			if strings.Contains(dashboard, "最新の記録") || strings.Contains(dashboard, "Latest record") || strings.Contains(dashboard, "最近の記録") || strings.Contains(dashboard, "Recent entries") {
				t.Fatalf("dashboard retained vague audit badge: %q", dashboard)
			}
			if strings.Contains(dashboard, tc.emptyLabel) {
				t.Fatalf("dashboard reported empty audit log despite entries: %q", dashboard)
			}
		})
	}
}

func TestAuditLogRendersPersistedActorInJapaneseAndEnglish(t *testing.T) {
	db := newIAViewDB(t)
	for _, actor := range []model.AuditLog{
		{ProjectName: "Human", ActorKind: model.AuditActorHuman, ActorID: "person-1", ActorName: "Ukyo Matsuo", Success: true},
		{ProjectName: "System", ActorKind: model.AuditActorSystem, ActorID: "bot-1", Success: true},
		{ProjectName: "Unknown", ActorKind: model.AuditActorUnknown, Success: true},
	} {
		model.WriteAuditLog(db, actor)
	}
	for _, tc := range []struct{ lang, human, system, unknown string }{
		{"ja", "Ukyo Matsuo", "システム", "不明"},
		{"en", "Ukyo Matsuo", "System", "Unknown"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/bot/admin/audit?lang="+tc.lang, nil)
			renderIAAudit(w, r, db)
			body := w.Body.String()
			for _, want := range []string{tc.human, tc.system, tc.unknown} {
				if !strings.Contains(body, want) {
					t.Fatalf("audit page omitted actor %q: %s", want, body)
				}
			}
			if strings.Contains(body, "記録なし") || strings.Contains(body, "Not recorded") {
				t.Fatal("audit page still renders the old missing-actor label")
			}
		})
	}
}

// The audit log can be narrowed to one production; the filter is a view
// option only and an unknown value falls back to the full list.
func TestAuditLogProductionFilterNarrowsRows(t *testing.T) {
	db := newIAViewDB(t)
	model.WriteAuditLog(db, model.AuditLog{ProjectID: "alpha", ProjectName: "Alpha Production", Success: true, ActorKind: model.AuditActorSystem})
	model.WriteAuditLog(db, model.AuditLog{ProjectID: "beta", ProjectName: "Beta Production", Success: false, ActorKind: model.AuditActorSystem})
	render := func(target string) string {
		w := httptest.NewRecorder()
		renderIAAudit(w, httptest.NewRequest("GET", target, nil), db)
		return w.Body.String()
	}
	all := render("/bot/admin/audit?lang=en")
	for _, want := range []string{`class="audit-filter-form"`, `<option value="">All</option>`, `<option value="Alpha Production">Alpha Production</option>`, `<option value="Beta Production">Beta Production</option>`, `data-label="Production">Alpha Production`, `data-label="Production">Beta Production`} {
		if !strings.Contains(all, want) {
			t.Fatalf("unfiltered audit page missing %q", want)
		}
	}
	filtered := render("/bot/admin/audit?lang=en&production=Beta+Production")
	if !strings.Contains(filtered, `data-label="Production">Beta Production`) {
		t.Fatal("filtered audit page lost the selected production's rows")
	}
	if strings.Contains(filtered, `data-label="Production">Alpha Production`) {
		t.Fatal("filtered audit page kept the other production's rows")
	}
	if !strings.Contains(filtered, `<option value="Alpha Production">Alpha Production</option>`) {
		t.Fatal("filtered audit page lost the filter choices")
	}
	unknown := render("/bot/admin/audit?lang=en&production=Missing")
	if !strings.Contains(unknown, `data-label="Production">Alpha Production`) || !strings.Contains(unknown, `data-label="Production">Beta Production`) {
		t.Fatal("an unknown production filter must fall back to all rows")
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
	advancedBody := advancedWriter.Body.String()
	if !strings.Contains(advancedBody, "synthetic-guild") {
		t.Fatal("Advanced settings did not expose the technical identifier")
	}
	for _, want := range []string{
		`data-copy-value="selected-advanced-p"`,
		`data-copy-value="synthetic-guild"`,
		`data-copy-value="synthetic-category"`,
		`aria-label="Copy Production ID"`,
		`aria-label="Copy Discord server ID"`,
		`aria-label="Copy Category ID"`,
		`navigator.clipboard.writeText(value)`,
	} {
		if !strings.Contains(advancedBody, want) {
			t.Fatalf("Technical details copy action missing %q", want)
		}
	}
	troubleshootingRequest := httptest.NewRequest("GET", "/bot/admin/projects?project=selected-advanced-p&tab=troubleshooting&lang=en", nil)
	troubleshootingWriter := httptest.NewRecorder()
	renderIAProductionList(troubleshootingWriter, troubleshootingRequest, db, "")
	troubleshootingBody := troubleshootingWriter.Body.String()
	for _, want := range []string{"Diagnostics", "Current issues", "Kitsu connection"} {
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
	if strings.Contains(body, "Review Discord server change") || strings.Contains(body, "/bot/setup?lang=en&amp;project=server-change-unique-2026&amp;wizard_step=3") {
		t.Fatal("selected Production exposes the misleading new-Production server-change flow")
	}
	if strings.Contains(body, `name="guild_id"`) {
		t.Fatal("selected Production exposes a raw server ID editor")
	}
}

func TestSelectedProductionOverviewUsesCanonicalConnectionStatuses(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "canonical-status-p", Name: "Canonical Status Production", DiscordGuildID: "guild"}
	db.Create(&p)
	w := httptest.NewRecorder()
	renderIAProductionList(w, httptest.NewRequest("GET", "/bot/admin/projects?project=canonical-status-p&lang=ja", nil), db, "")
	body := w.Body.String()
	for _, want := range []string{"接続済み", "未設定", "プロダクション接続", "Discordリソース", "通知ルーティング"} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview missing canonical status content %q", want)
		}
	}
	for _, forbidden := range []string{"次の操作", "通常どおり利用できます", "wizard_step=3"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("overview retained obsolete content %q", forbidden)
		}
	}
}

func TestSelectedProductionOverviewUsesCompactSummaryAndSeparatesIssues(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "compact-overview-p", Name: "Compact Overview Production", DiscordGuildID: "guild"}
	db.Create(&p)
	db.Create(&model.ProductionNotificationConfig{ProductionID: p.KitsuProjectID, Enabled: true})
	w := httptest.NewRecorder()
	renderIAProductionList(w, httptest.NewRequest("GET", "/bot/admin/projects?project=compact-overview-p&lang=en", nil), db, "")
	body := w.Body.String()
	if strings.Count(body, `class="status-row"`) != 3 || strings.Contains(body, "production-summary-card") || !strings.Contains(body, "production-current-issues") {
		t.Fatalf("overview should use a compact three-row status list and separate issues: %q", body)
	}
	if strings.Contains(body, "Notification destinations are active.") {
		t.Fatal("overview exposes redundant notification explanation text")
	}
}

func TestGlobalUserForKitsuPersonDoesNotCrossStablePersonIDs(t *testing.T) {
	users := []model.UserMap{{KitsuID: "old-person", KitsuName: "Shared Name", KitsuEmail: "shared@example.invalid", DiscordID: "123456789012345678"}}
	person := kitsu.Person{ID: "new-person", FullName: "Shared Name", Email: "shared@example.invalid"}
	if got := globalUserForKitsuPerson(users, person); got != nil {
		t.Fatalf("conflicting stable Person ID must not inherit mapping: %+v", got)
	}
}

func TestGlobalUserForKitsuPersonAllowsUnambiguousIdentitylessLegacyFallback(t *testing.T) {
	users := []model.UserMap{{KitsuName: "Legacy Person", KitsuEmail: "legacy@example.invalid", DiscordID: "123456789012345678"}}
	person := kitsu.Person{ID: "current-person", FullName: "Legacy Person", Email: "legacy@example.invalid"}
	if got := globalUserForKitsuPerson(users, person); got == nil || got.DiscordID != users[0].DiscordID {
		t.Fatalf("identityless legacy mapping should remain available, got %+v", got)
	}
}

func TestGlobalUserForKitsuPersonRejectsLegacyFallbackWhenIdentityConflicts(t *testing.T) {
	users := []model.UserMap{
		{KitsuID: "different-person", KitsuEmail: "shared@example.invalid", DiscordID: "123456789012345678"},
		{KitsuEmail: "shared@example.invalid", DiscordID: "123456789012345679"},
	}
	person := kitsu.Person{ID: "current-person", Email: "shared@example.invalid"}
	if got := globalUserForKitsuPerson(users, person); got != nil {
		t.Fatalf("identity conflict must block otherwise matching legacy fallback: %+v", got)
	}
}

func TestGlobalUserForKitsuPersonRejectsAmbiguousIdentitylessFallback(t *testing.T) {
	users := []model.UserMap{
		{KitsuName: "Same Name", DiscordID: "123456789012345678"},
		{KitsuName: "Same Name", DiscordID: "123456789012345679"},
	}
	person := kitsu.Person{FullName: "Same Name"}
	if got := globalUserForKitsuPerson(users, person); got != nil {
		t.Fatalf("ambiguous legacy fallback must fail closed, got %+v", got)
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

func TestConnectionsPageUsesCatalogLabelsAndUnescapedStatusMarkup(t *testing.T) {
	t.Setenv("KITSUSYNC_LOCAL_PROFILE", "1")
	t.Setenv("KITSU_HOSTNAME", "")
	// This test exercises the local-profile fallback after other tests may have
	// populated the process-wide discovery cache with a different candidate set.
	discoveryMu.Lock()
	discoveryAt = time.Time{}
	discoveryResult = KitsuHostDiscoveryResult{}
	discoveryCacheKey = ""
	discoveryMu.Unlock()
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
	if got := strings.Count(body, "<h1"); got != 1 {
		t.Fatalf("Connections page has %d h1 elements, want 1", got)
	}
	pageCard := strings.Index(body, `<div class="page-card glass editorial-workbench">`)
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
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	if err := setRuntimeKitsuToken(db, "configured-kitsu-token"); err != nil {
		t.Fatal(err)
	}
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
	if strings.Contains(body, "configured-token") || strings.Contains(body, "configured-kitsu-token") || strings.Contains(body, "Saved token is not displayed.") || !strings.Contains(body, connectionSecretMask) {
		t.Fatal("summary secret handling is incorrect")
	}
	if !strings.Contains(body, "Kitsu Bot API token") || !strings.Contains(body, "Discord Bot Token") {
		t.Fatal("summary is missing the canonical masked token rows")
	}
	if strings.Contains(body, `connections-card-header"><div>`) || strings.Contains(body, `role="status">Connected</span></div><section`) {
		t.Fatal("summary retained the redundant combined status header")
	}
	body = renderConnectionsDisplayBodyWithHealth("ja", httptest.NewRequest("GET", "/bot/admin/bot?lang=ja", nil), db, "", "ok", "接続済み", "https://kitsu.example.test", "configured-token", true, true)
	if !strings.Contains(body, "Kitsu接続") || !strings.Contains(body, "Discord Bot接続") || strings.Contains(body, "configured-token") || !strings.Contains(body, connectionSecretMask) {
		t.Fatal("Japanese summary did not preserve explicit service labels or secret safety")
	}
}

func TestSystemStatusRendersRecentIssuesOnlyWhenPresent(t *testing.T) {
	db := newIAViewDB(t)
	model.WriteAuditLog(db, model.AuditLog{EntityName: "notification", Success: false, ErrorMessage: "safe failure summary"})
	w := httptest.NewRecorder()
	renderIAHealth(w, httptest.NewRequest("GET", "/bot/admin/health?lang=en", nil), db, time.Minute)
	body := w.Body.String()
	if !strings.Contains(body, "Recent system issues") || !strings.Contains(body, "safe failure summary") {
		t.Fatal("non-empty Recent system issues section was not rendered")
	}
}

func TestWizardLiveProductionSelectionTargetsServerStep(t *testing.T) {
	db := newIAViewDB(t)
	production := KitsuProject{ID: "live-production-id", Name: "Live Production"}
	r := httptest.NewRequest("GET", "/bot/setup?lang=en&wizard_step=2", nil)
	body := renderWizardProductionLocalized("en", r, db, []KitsuProject{production}, nil)
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

// F1: GET wizard forms must carry lang, otherwise the browser drops the query
// and shellHeadExtras() re-adds it with a full page reload.
func TestWizardGetFormsCarryLanguage(t *testing.T) {
	db := newIAViewDB(t)
	r := httptest.NewRequest("GET", "/bot/setup?lang=en&wizard_step=4&project=live-production-id&plan_guild=123456789012345678", nil)
	production := KitsuProject{ID: "live-production-id", Name: "Live Production"}
	forms := map[string]string{
		"production": renderWizardProductionLocalized("en", r, db, []KitsuProject{production}, nil),
		"server":     renderWizardServer("en", r, "", production.ID),
	}
	for name, body := range forms {
		if !strings.Contains(body, `<input type="hidden" name="lang" value="en">`) {
			t.Fatalf("%s form did not carry the current language", name)
		}
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}

// F2: the repair link only sets repair on its first GET, so every wizard form
// and the wizard links must carry it forward or the repair falls back to step 1.
func TestWizardRepairModeCarriesThroughFormsAndLinks(t *testing.T) {
	db := newIAViewDB(t)
	r := httptest.NewRequest("GET", "/bot/setup?lang=en&wizard_step=4&repair=1&project=live-production-id&plan_guild=123456789012345678", nil)
	production := KitsuProject{ID: "live-production-id", Name: "Live Production"}
	forms := map[string]string{
		"production": renderWizardProductionLocalized("en", r, db, []KitsuProject{production}, nil),
		"server":     renderWizardServer("en", r, "", production.ID),
	}
	for name, body := range forms {
		if !strings.Contains(body, `<input type="hidden" name="repair" value="1">`) {
			t.Fatalf("%s form did not carry repair mode", name)
		}
	}
	if link := setupWizardURL(r, 3, production.ID, "", false); !strings.Contains(link, "repair=1") {
		t.Fatalf("wizard link dropped repair mode: %s", link)
	}
	plain := httptest.NewRequest("GET", "/bot/setup?lang=en&wizard_step=4&project=live-production-id", nil)
	if strings.Contains(renderWizardServer("en", plain, "", production.ID), `name="repair"`) {
		t.Fatal("repair input leaked into a normal wizard run")
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}

func TestWizardProductionLookupStateDistinguishesEmptyAndFailure(t *testing.T) {
	db := newIAViewDB(t)
	r := httptest.NewRequest("GET", "/bot/setup?lang=en&wizard_step=2", nil)
	empty := renderWizardProductionLocalized("en", r, db, nil, nil)
	failure := renderWizardProductionLocalized("en", r, db, nil, errors.New("synthetic lookup failure"))
	if !strings.Contains(empty, `role="status"`) || !strings.Contains(empty, "Kitsu returned no Productions") {
		t.Fatal("successful empty Production response was not shown as an empty state")
	}
	if !strings.Contains(failure, `role="alert"`) || !strings.Contains(failure, "Production list could not be loaded from Kitsu") {
		t.Fatal("Production request failure was not shown as an error state")
	}
	if strings.Contains(empty, "Production list could not be loaded from Kitsu") || strings.Contains(failure, "Kitsu returned no Productions") {
		t.Fatal("empty and failure states were conflated")
	}
}

func TestProductionNotificationLanguageSaveIsProductionScoped(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "language-production", Name: "Language Production", Language: "ja"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?lang=en", strings.NewReader("action=save_notification_language&project_id=language-production&notification_language=en"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	AdminProjectsHandler(db, "", "")(response, request)
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "tab=notifications&msg=saved") {
		t.Fatalf("language save did not redirect as saved: status=%d location=%s", response.Code, response.Header().Get("Location"))
	}
	updated := model.FindProjectByKitsuID(db, project.KitsuProjectID)
	if updated == nil || updated.Language != "en" {
		t.Fatalf("language save did not persist on the selected Production: %#v", updated)
	}
}

func TestVisibilityAndAuditTablesCollapseToLabeledCardsOnMobile(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "visibility-p", Name: "VeryLongProductionNameForMobile"})
	model.WriteAuditLog(db, model.AuditLog{ProjectName: "VeryLongProductionNameForMobile", EntityName: "Storyboard", Success: true})
	visibility := renderProductionVisibilitySection("en", httptest.NewRequest("GET", "/bot/admin/health?lang=en", nil), db)
	auditPage := httptest.NewRecorder()
	renderIAAudit(auditPage, httptest.NewRequest("GET", "/bot/admin/audit?lang=en", nil), db)
	for name, body := range map[string]string{
		"visibility": visibility,
		"audit":      auditPage.Body.String(),
	} {
		if !strings.Contains(body, `<table class="mobile-card-table">`) && !strings.Contains(body, `class="data-table mobile-card-table"`) {
			t.Fatalf("%s table is missing the mobile card class", name)
		}
		var cells []string
		if name == "visibility" {
			cells = []string{`class="mobile-card-lead"><span class="mobile-card-lead-label" data-label="Production">`, `data-label="Visible to bot"`, `data-label="Task count"`, `data-label="Last polling success"`}
		} else {
			cells = []string{`data-label="Date and time"`, `data-label="Production"`, `data-label="Action"`, `data-label="Acting user"`, `data-label="Result"`}
		}
		for _, cell := range cells {
			if !strings.Contains(body, cell) {
				t.Fatalf("%s table cell lost its mobile label %s", name, cell)
			}
		}
	}
	for _, rule := range []string{
		`th[scope=row]{color:var(--text);text-transform:none;letter-spacing:normal;font-family:inherit;font-size:inherit;}`,
		`.mobile-card-table thead{position:absolute;width:1px;height:1px;`,
		`.mobile-card-table tr>[data-label]::before,.mobile-card-table tr>.mobile-card-lead-label::before{content:attr(data-label);`,
		`.mobile-card-table tr>.mobile-card-lead:has(.status-pill){`,
	} {
		if !strings.Contains(adminThemeCSS, rule) {
			t.Errorf("mobile card table is missing style %q", rule)
		}
	}
}

func TestUserLinkingSaveStartsDisabledAndTracksChangedSelection(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.UserMap{KitsuID: "user-1", KitsuName: "User One", DiscordID: "123456789012345678", DiscordDisplayName: "Discord One"})
	directory := globalDiscordDirectory{
		Guilds:        []DiscordGuild{{ID: "123456789012345677", Name: "Studio"}},
		SelectedGuild: DiscordGuild{ID: "123456789012345677", Name: "Studio"},
		Options:       []globalDiscordUserOption{{ID: "123456789012345678", Name: "Discord One"}},
	}
	body := renderGlobalUserLinkingTable(db, "en", directory, []KitsuPerson{{ID: "user-1", FullName: "User One", Active: true}})
	if !strings.Contains(body, `id="user-linking-save" disabled`) {
		t.Fatal("User Linking Save was not disabled initially")
	}
	if !strings.Contains(body, "data-initial-index") || !strings.Contains(body, "selectedIndex!==Number(select.dataset.initialIndex)") {
		t.Fatal("User Linking did not include unchanged-selection dirty-state handling")
	}
	if !strings.Contains(body, `data-bulk-link-form`) || !strings.Contains(body, `name="action" value="save_global_links"`) {
		t.Fatal("User Linking did not render the single bulk save form")
	}
	if !strings.Contains(body, `class="user-link-grid-row"`) || !strings.Contains(body, "data-label=") || !strings.Contains(body, "user-link-actions") {
		t.Fatal("User Linking did not render the shared responsive grid structure")
	}
	if strings.Contains(body, ">123456789012345678<") {
		t.Fatal("User Linking rendered a raw Discord ID as visible identity text")
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
	if class != "neutral" || label != "Disconnected" || strings.Contains(label, "preview_not_connected") || !strings.Contains(hint, "Notifications are unavailable") {
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
	content := body
	if styleEnd := strings.Index(content, "</style>"); styleEnd >= 0 {
		content = content[styleEnd+len("</style>"):]
	}
	for _, forbidden := range []string{`role="tablist"`, "Danger Zone", "Notification state", "User settings", "Task Type"} {
		if strings.Contains(content, forbidden) {
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

func TestProductionTeamShowsLinkedDiscordDisplayName(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "participant-display-p", Name: "Participant Display Production", DiscordGuildID: "123456789012345678"}
	db.Create(&p)
	db.Create(&model.UserMap{KitsuID: "synthetic-participant", KitsuName: "Synthetic Participant", DiscordID: "123456789012345678", DiscordDisplayName: "Synthetic Discord Name"})
	oldReader := reviewerProductionTeamReader
	oldMembers := reviewerGuildMembersForGuild
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) {
		return []kitsu.Person{{ID: "synthetic-participant", FullName: "Synthetic Participant"}}, nil
	}
	reviewerGuildMembersForGuild = func(_, token string) ([]DiscordGuildMember, error) {
		if token != "synthetic-token" {
			t.Fatalf("unexpected Discord token %q", token)
		}
		return []DiscordGuildMember{reviewerTestGuildMember("123456789012345678", "ukyo", "Synthetic Discord Name", "")}, nil
	}
	t.Cleanup(func() { reviewerProductionTeamReader, reviewerGuildMembersForGuild = oldReader, oldMembers })
	body := renderCurrentProductionTeam(db, httptest.NewRequest("GET", "/bot/admin/projects?project=participant-display-p&tab=users&lang=en", nil), p, "en", "synthetic-token")
	if !strings.Contains(body, "Synthetic Discord Name") {
		t.Fatal("Production User Settings did not show the linked Discord display name")
	}
	if strings.Contains(body, "synthetic-discord-id") {
		t.Fatal("Production User Settings leaked the raw Discord identifier")
	}
}

func TestProductionNotificationsUseStagedSetupStyleRouting(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "routing-production", Name: "Routing Production", Language: "en", DiscordGuildID: "123456789012345678"}
	db.Create(&p)
	if err := model.SaveProductionForumSettings(db, reviewerBrowserForumSettings(p.KitsuProjectID, p.DiscordGuildID,
		[]model.ProductionForumRoute{{ProductionID: p.KitsuProjectID, TaskTypeID: "compositing", TaskTypeName: "Compositing", ForumChannelID: "forum-comp", ForumChannelName: "compositing"}})); err != nil {
		t.Fatal(err)
	}
	body := renderSelectedProductionNotifications(db, httptest.NewRequest("GET", "/bot/admin/projects?project=routing-production&tab=notifications&lang=en", nil), p, "en", "ok", "Active", "Ready")
	for _, expected := range []string{"Kitsu Task Type", "Forum", "Edit"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("notification IA missing %q: %s", expected, body)
		}
	}
	if strings.Contains(body, "Notification routing") {
		t.Fatal("Notifications read mode should not repeat the tab's routing heading")
	}
	for _, expected := range []string{"WFA recipients", "Task Type", `data-wfa-recipients`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("Notifications missing %q", expected)
		}
	}
	visibleBody := body
	if sourceStart := strings.Index(visibleBody, `<div hidden data-wfa-pending-source>`); sourceStart >= 0 {
		visibleBody = visibleBody[:sourceStart]
	}
	if strings.Contains(visibleBody, `data-wfa-group`) || strings.Contains(visibleBody, `class="production-wfa-kind"`) {
		t.Fatal("read mode must show effective recipients without Automatic/Additional provenance")
	}
	for _, forbidden := range []string{"Notification preview", "Task Type to preview", "Example task rendered", "Please review this task."} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("removed notification preview content remains: %q", forbidden)
		}
	}
	if strings.Contains(body, `name="action" value="save_current_production_routing"`) || strings.Contains(body, "<select name=\"task_type_id\">") {
		t.Fatal("default notification routing should be read-only")
	}
	editBody := renderSelectedProductionNotifications(db, httptest.NewRequest("GET", "/bot/admin/projects?project=routing-production&tab=notifications&edit_routing=1&lang=en", nil), p, "en", "ok", "Active", "Ready")
	if !strings.Contains(editBody, `data-async-notification-apply`) || !strings.Contains(editBody, `data-wfa-detail-panel`) {
		t.Fatal("explicit routing edit mode did not expose one staged Apply flow with linked WFA panel")
	}
	body = editBody
	for _, expected := range []string{"Apply", "Cancel", "data-routing-row", "data-wfa-add-target"} {
		if !strings.Contains(editBody, expected) {
			t.Fatalf("staged routing editor missing %q", expected)
		}
	}
	for _, expected := range []string{"class=\"routing-task-type-name\">Compositing", `data-task-type="compositing"`} {
		if !strings.Contains(editBody, expected) {
			t.Fatalf("compact routing row missing %q: %s", expected, editBody)
		}
	}
	if strings.Contains(editBody, "class=\"wizard-exclude routing-remove\"") || strings.Contains(editBody, "class=\"btn-danger-ghost routing-delete-open\"") {
		t.Fatal("routing row still exposes the old always-visible action controls")
	}
	if strings.Contains(body, "変更者:") {
		t.Fatalf("English notification UI leaked the Japanese author label: %s", body)
	}
}

func TestProductionTroubleshootingExposesProcessingDiagnostics(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "diagnostic-production", Name: "Diagnostic Production"}
	db.Create(&p)
	body := renderCurrentProductionTroubleshooting(db, p, "en", false)
	for _, want := range []string{"Diagnostics", "Kitsu connection", "Participant retrieval", "User linking", "Recent notification processing"} {
		if !strings.Contains(body, want) {
			t.Fatalf("troubleshooting diagnostics missing %q: %s", want, body)
		}
	}
	db.Create(&model.NotificationRoutingDiagnosis{ProductionID: p.KitsuProjectID, Reason: "route missing", Detail: "A route needs review."})
	body = renderCurrentProductionTroubleshooting(db, p, "en", false)
	if !strings.Contains(body, "A route needs review.") || !strings.Contains(body, "Current issue details") {
		t.Fatalf("troubleshooting does not expose the current issue detail: %s", body)
	}
}

func TestProductionDetailsUsesDetailsLabel(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "details-production", Name: "Details Production"}
	db.Create(&p)
	body := renderSelectedProductionPanel(db, httptest.NewRequest("GET", "/bot/admin/projects?project=details-production&tab=advanced&lang=en", nil), p, "en", "settings", "Connected", "Ready", "")
	if !strings.Contains(body, "Technical details") || strings.Contains(body, "Advanced settings") || !strings.Contains(body, `id="technical-details"`) || !strings.Contains(body, ` open>`) {
		t.Fatalf("legacy Details state did not map to expanded Technical details: %s", body)
	}
}

func TestProductionUserSettingsEmptyStatesHaveNoDecorativeBullets(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "empty-user-settings", Name: "Empty User Settings"}
	db.Create(&p)
	oldReader := reviewerProductionTeamReader
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) { return nil, nil }
	t.Cleanup(func() { reviewerProductionTeamReader = oldReader })
	body := renderCurrentProductionTeam(db, httptest.NewRequest("GET", "/bot/admin/projects?tab=users&lang=ja", nil), p, "ja")
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
	body := renderDashboardMenuRefined("en", httptest.NewRequest("GET", "/bot/admin?lang=en", nil), db, nil, 0, readiness, nil)
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
	if strings.Contains(body, "Connect a Kitsu Production to a Discord server.") || strings.Contains(body, "Use Setup to start a new Production connection.") {
		t.Fatal("dashboard CTA contains redundant supporting copy")
	}
	if strings.Contains(body, `class="dashboard-status-chip warning">Waiting<`) {
		t.Fatal("system status card should not duplicate its overall state as a second badge")
	}
}

func TestDashboardRefinedMenuUsesConfiguredStateAndConnectedCount(t *testing.T) {
	db := newIAViewDB(t)
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	model.SetSetting(db, "kitsu.hostname", "https://kitsu.example.test")
	if err := setRuntimeKitsuToken(db, "configured-kitsu-token"); err != nil {
		t.Fatalf("store Kitsu token: %v", err)
	}
	model.SetSetting(db, "discord.guild_id", "guild-1")
	setRuntimeDiscordBotToken(db, "configured-discord-token")
	installDiscordAPIStub(t, "guild-1")
	readiness := SharedBotRuntimeReadiness{KitsuConfigured: true, DiscordConfigured: true, OverallReady: true}
	body := renderDashboardMenuRefined("en", httptest.NewRequest("GET", "/bot/admin?lang=en", nil), db, []model.Project{{Name: "Connected", KitsuProjectID: "connected"}}, 0, readiness, func() bool { return true })
	if !strings.Contains(body, `class="dashboard-status-chip ok">Configured<`) {
		t.Fatal("configured User Linking state should use the positive status class")
	}
	if !strings.Contains(body, `Kitsu Connected`) || !strings.Contains(body, `Discord Connected`) {
		t.Fatal("Connections card should label Kitsu and Discord statuses independently")
	}
}

func TestDashboardConnectionStatusesMatchConnectionsPage(t *testing.T) {
	db := newSetupStateTestDB(t)
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	model.SetSetting(db, "kitsu.hostname", "https://kitsu.example.test")
	if err := setRuntimeKitsuToken(db, "configured-kitsu-token"); err != nil {
		t.Fatalf("store Kitsu token: %v", err)
	}
	model.SetSetting(db, "discord.guild_id", "guild-1")
	setRuntimeDiscordBotToken(db, "configured-discord-token")
	installDiscordAPIStub(t, "guild-1")
	readiness := SharedBotRuntimeReadiness{KitsuConfigured: true, DiscordConfigured: true}
	dashboard := renderDashboardMenuRefined("en", httptest.NewRequest("GET", "/bot/admin?lang=en", nil), db, nil, 0, readiness, func() bool { return false })
	connections := renderConnectionsDisplayBodyWithHealthRaw("en", httptest.NewRequest("GET", "/bot/admin/bot?lang=en", nil), db, "", "warn", "Needs review", "https://kitsu.example.test", "configured-discord-token", false, true)
	if !strings.Contains(dashboard, `aria-label="Kitsu Needs review"`) || !strings.Contains(connections, `class="status-pill warn" role="status">Needs review</span>`) {
		t.Fatal("Dashboard and Connections do not expose the same Kitsu review status")
	}
	if !strings.Contains(dashboard, `class="dashboard-status-chip warn">Needs review</span>`) {
		t.Fatal("Dashboard Kitsu Needs review badge did not use the canonical warning tone")
	}
	if !strings.Contains(dashboard, `aria-label="Discord Connected"`) || !strings.Contains(connections, `class="status-pill ok" role="status">Connected</span>`) {
		t.Fatal("Dashboard and Connections do not expose the same Discord connected status")
	}
}

func TestDashboardAndSystemNeedsReviewUseCanonicalWarningTone(t *testing.T) {
	readiness := SharedBotRuntimeReadiness{KitsuConfigured: true, DiscordConfigured: true}
	_, class, _ := overallRuntimeStatus("en", readiness, RuntimeSnapshot{LastPollErr: "transient"})
	if got := canonicalDashboardBadgeClass(class); got != "warn" {
		t.Fatalf("System Status Needs review class = %q, want warn", got)
	}
	_, auditClass := dashboardAuditHealthLabel("en", model.AuditHealthNeedsReview)
	if auditClass != "warn" {
		t.Fatalf("Audit Needs review class = %q, want warn", auditClass)
	}
}

func TestDashboardNotificationStatusUsesUnavailableCopyWithoutProductions(t *testing.T) {
	db := newIAViewDB(t)
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	model.SetSetting(db, "kitsu.hostname", "https://kitsu.example.test")
	if err := setRuntimeKitsuToken(db, "configured-kitsu-token"); err != nil {
		t.Fatalf("store Kitsu token: %v", err)
	}
	model.SetSetting(db, "discord.guild_id", "guild-1")
	setRuntimeDiscordBotToken(db, "configured-discord-token")
	installDiscordAPIStub(t, "guild-1")
	for _, tc := range []struct {
		lang, label, hint string
	}{
		{"ja", "利用不可", "通知可能なプロダクションがありません。"},
		{"en", "Unavailable", "No Production is currently available for notifications."},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			w := httptest.NewRecorder()
			renderIADashboardWithRuntime(w, httptest.NewRequest("GET", "/bot/admin?lang="+tc.lang, nil), db, func() bool { return false })
			body := w.Body.String()
			if !strings.Contains(body, `Notification status`) && tc.lang == "en" {
				t.Fatal("English dashboard is missing Notification status")
			}
			if !strings.Contains(body, `通知状態`) && tc.lang == "ja" {
				t.Fatal("Japanese dashboard is missing 通知状態")
			}
			if !strings.Contains(body, tc.label) || !strings.Contains(body, tc.hint) {
				t.Fatalf("dashboard missing unavailable notification copy: %q", body)
			}
			if strings.Contains(body, "Waiting for connection") || strings.Contains(body, "Waiting") || strings.Contains(body, "接続待ち") {
				t.Fatal("dashboard retained ambiguous waiting language")
			}
		})
	}
}

func TestConnectionsUseSharedActionSpacingToken(t *testing.T) {
	if !strings.Contains(adminThemeCSS, `--space-action-section:24px`) || !strings.Contains(adminThemeCSS, `.button-row.connections-actions,.button-row.connections-navigation{gap:12px;margin-top:var(--space-action-section,24px)}`) {
		t.Fatal("normal and edit Connections actions do not share the canonical spacing rule")
	}
}

func TestEditorialWorkbenchUsesRestrainedSurfaceContract(t *testing.T) {
	body := adminPage("en", "Dashboard", httptest.NewRequest(http.MethodGet, "/bot/admin?lang=en", nil), `<div class="dashboard-page"></div>`)
	for _, want := range []string{`class="page-card glass editorial-workbench"`, `.editorial-workbench{max-width:1088px;border-radius:12px;background:var(--panel);box-shadow:none}`, `.editorial-workbench>.page-heading h1{font-size:28px;line-height:1.15;font-weight:600}`, `.editorial-workbench .page-heading>div>h2,.editorial-workbench .dashboard-menu>h2,.editorial-workbench .dashboard-cta>div>h2{font-size:20px;line-height:1.2;font-weight:600}`, `.editorial-workbench .connections-card-header h2,.editorial-workbench .section-card>h2,.editorial-workbench .section-card>h3,.editorial-workbench .api-observation-card h3,.editorial-workbench .pipeline-health-item h3{font-size:16px;line-height:1.25;font-weight:600}`, `.editorial-workbench .field-help{font-size:13px;font-weight:400}`, `.editorial-workbench .status-pill,.editorial-workbench .status-badge,.editorial-workbench .dashboard-status-chip{font-size:12px;font-weight:600}`, `.editorial-workbench .btn,.editorial-workbench .btn-sm,.editorial-workbench .btn-ghost,.editorial-workbench .btn-danger{min-height:40px;border-radius:8px}`, `.editorial-workbench .production-tabpanel>.section-card{border:0;border-top:1px solid var(--divider-color);border-radius:0;background:transparent;box-shadow:none;padding:20px 0 0}`, `.editorial-advanced-settings{display:grid;grid-template-columns:minmax(220px,.7fr) minmax(0,1.3fr);align-items:start;gap:16px 32px;border-top:1px solid var(--divider-color);padding-top:24px}`, `.editorial-advanced-settings .editorial-advanced-heading{grid-column:1 / -1;grid-row:1}`, `.editorial-advanced-settings .editorial-advanced-explanation{grid-column:1;grid-row:2}`, `.editorial-advanced-settings>.connection-external-url-field{grid-column:2;grid-row:2}`, `.editorial-advanced-settings>.connection-expert-network{grid-column:1 / -1;grid-row:3}`, `.editorial-workbench .system-status-sections .system-observability-grid>article{display:grid;grid-template-rows:auto 1fr;min-height:224px;padding:16px;border:1px solid var(--border-default);border-radius:8px;background:var(--surface-subtle);box-shadow:none}`, `.system-status-sections .api-sparkline .chart-axis-label,.system-status-sections .api-sparkline .chart-tick,.system-status-sections .api-sparkline .chart-time-label{fill:var(--muted)}`, `.dashboard-service-status{display:grid;gap:8px;width:100%;min-width:0}`, `.editorial-workbench .connections-summary-grid>.connections-card{border-radius:8px}`} {
		if !strings.Contains(body, want) {
			t.Fatalf("editorial surface contract missing %q", want)
		}
	}
	if strings.Contains(adminThemeCSS, `.editorial-workbench h2{font-size:24px`) || strings.Contains(adminThemeCSS, `.editorial-workbench h3{font-size:18px`) || strings.Contains(adminThemeCSS, `.system-status-sections h2{font-size:24px`) {
		t.Fatal("editorial typography still applies blanket heading sizes")
	}
	if !strings.Contains(adminThemeCSS, `.editorial-advanced-settings .editorial-advanced-heading,.editorial-advanced-settings .editorial-advanced-explanation,.editorial-advanced-settings>.connection-external-url-field,.editorial-advanced-settings>.connection-expert-network{grid-column:1;grid-row:auto}`) || strings.Contains(adminThemeCSS, `.editorial-advanced-settings .connection-form-field:last-child`) {
		t.Fatal("Advanced settings mobile layout does not stack its semantic rows")
	}
}

func TestPrimaryNavigationHasNoOuterCardSurface(t *testing.T) {
	body := adminPage("en", "Dashboard", httptest.NewRequest(http.MethodGet, "/bot/admin?lang=en", nil), `<div></div>`)
	if !strings.Contains(body, `<div class="nav-card">`) || strings.Contains(body, `<div class="nav-card glass">`) {
		t.Fatal("primary navigation retained an outer glass/card surface")
	}
	for _, item := range []string{"Productions", "User Linking", "Connections", "System Status", "Audit Log"} {
		if !strings.Contains(body, item) {
			t.Fatalf("primary navigation missing %q", item)
		}
	}
}

func TestAdminFormSizingLeavesCheckboxesAndRadiosCompact(t *testing.T) {
	if !strings.Contains(adminThemeCSS, `input:not([type="checkbox"]):not([type="radio"]),select{min-height:var(--control-height-standard);}`) {
		t.Fatal("text input sizing does not exclude checkbox and radio controls")
	}
	if strings.Contains(adminThemeCSS, `input,select{min-height:44px;}`) {
		t.Fatal("checkboxes and radios still inherit the tall generic input rule")
	}
}

func TestMobileNavigationUsesOnePanelAndRestrainedRows(t *testing.T) {
	for _, fragment := range []string{
		`@media(max-width:760px){`,
		`.mobile-nav{border-radius:var(--radius-md);overflow:visible}`,
		`.mobile-nav[open]>.nav-card{margin-top:0;padding:4px 6px 8px;border:0;border-radius:0;background:transparent}`,
		`.mobile-nav .nav-chip{border-radius:var(--radius-sm)}`,
		`.mobile-nav .nav-chip.active,.mobile-nav .nav-chip[aria-current="page"]{border-radius:var(--radius-sm)}`,
	} {
		if !strings.Contains(adminThemeCSS, fragment) {
			t.Fatalf("mobile navigation radius contract is missing %q", fragment)
		}
	}
}

func TestSystemStatusUsesSelectiveSafeDetailsAndRefreshSnapshot(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIAHealth(w, httptest.NewRequest("GET", "/bot/admin/health?lang=en", nil), db, time.Minute)
	body := w.Body.String()
	if strings.Count(body, `<details class="pipeline-health-diagnostic">`) < 3 || strings.Contains(body, `class="pipeline-health-details-toggle"`) || strings.Contains(body, `>Details<`) {
		t.Fatalf("system status operational details are not selectively disclosed")
	}
	if strings.Contains(body, "Internal data diagnostics") || strings.Contains(body, "内部データ診断") {
		t.Fatal("system status exposed a datastore-only diagnostic disclosure")
	}
	if !strings.Contains(body, `data-system-status-refresh`) {
		t.Fatal("system status does not include the bounded snapshot refresh marker")
	}
	if !strings.Contains(body, `window.setInterval(refresh,interval)`) {
		t.Fatal("system status does not include the bounded snapshot interval")
	}
	if !strings.Contains(body, `Date.parse(item.at)`) || !strings.Contains(body, `telemetry-line`) || strings.Contains(body, `telemetry-bar`) {
		t.Fatal("system status graph does not use timestamp-positioned lines")
	}
	if !strings.Contains(body, `function scale(items)`) || !strings.Contains(body, `upper=scale(items)`) {
		t.Fatal("system status refresh does not apply independent zero-based Y scales")
	}
	if !strings.Contains(body, `class=\"chart-time-label\"`) || !strings.Contains(body, `30s`) || !strings.Contains(body, `payload.generated_at`) {
		t.Fatal("system status refresh is missing canonical time-axis labels")
	}
	if strings.Contains(body, `data-system-status-window`) || !strings.Contains(body, `window=60s`) || !strings.Contains(body, `>45000`) || !strings.Contains(body, `>30000`) {
		t.Fatal("system status must keep the fixed rolling timeline and stale/gap policy")
	}
	if !strings.Contains(body, `chart-tick`) || !strings.Contains(body, `chart-guide`) {
		t.Fatal("system status refresh is missing readable shared chart ticks or guide")
	}
	if !strings.Contains(body, `.system-status-sections .api-observation-meta{font-size:13px}`) || !strings.Contains(body, `.system-status-sections .api-sparkline .chart-tick,.system-status-sections .api-sparkline .chart-time-label{font-size:12px}`) {
		t.Fatal("system status text sizing rules are missing")
	}
	refreshStart := strings.Index(body, `<script data-system-status-refresh>`)
	refreshScript := body
	if refreshStart >= 0 {
		if refreshEnd := strings.Index(body[refreshStart:], `</script>`); refreshEnd >= 0 {
			refreshScript = body[refreshStart : refreshStart+refreshEnd]
		}
	}
	if strings.Contains(refreshScript, `class='chart-axis'`) || strings.Contains(refreshScript, `class="chart-axis"`) || !strings.Contains(refreshScript, `telemetry-line`) || strings.Contains(refreshScript, `telemetry-bar`) {
		t.Fatal("system status refresh does not use the canonical line graph")
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

func TestSystemStatusUsesContextualDiagnosticsLabels(t *testing.T) {
	rendered := renderPipelineHealthItem("en", pipelineHealthItem{label: "Routing", value: "Configured", class: "success", details: "<dl></dl>", detailsLabel: "Connection and routing diagnostics"}, 0)
	if strings.Contains(rendered, "pipeline-health-details-toggle") || strings.Contains(rendered, "aria-controls") || strings.Contains(rendered, ">Details<") {
		t.Fatal("system status should not use a generic Details control")
	}
	if !strings.Contains(rendered, "Connection and routing diagnostics") {
		t.Fatal("system status omitted the contextual diagnostics label")
	}
}

func TestProductionListRowsRemoveRedundantStatusCopy(t *testing.T) {
	rows := `<article class="section-card glass production-list-item"><div><h2>Test</h2><p class="field-help">Current state</p></div><div class="production-list-state"><span class="status-pill neutral">Disconnected</span><span class="field-help">No route</span></div><a class="btn" href="#">Open Production</a></article>`
	clean := simplifyProductionListRows(rows)
	if strings.Contains(clean, "Current state") || strings.Contains(clean, "No route") {
		t.Fatal("redundant Production list copy remains")
	}
	if !strings.Contains(clean, "Disconnected") || !strings.Contains(clean, "Open Production") {
		t.Fatal("Production list lost its semantic status or action")
	}
}

func TestProductionConnectionCountsTwoVisibleOneConnectedOneDisconnected(t *testing.T) {
	projects := []model.Project{
		{KitsuProjectID: "connected", Name: "Connected"},
		{KitsuProjectID: "live", Name: "Live", ReadOnlyPreview: true},
	}
	if got := productionConnectionCounts(projects); got != (productionCounts{Total: 2, Connected: 1, Disconnected: 1}) {
		t.Fatalf("two visible Production counts = %+v, want total=2 connected=1 disconnected=1", got)
	}
}

func TestProductionConnectionCountsExcludeValidationOnlyFromNormalSummary(t *testing.T) {
	projects := []model.Project{
		{KitsuProjectID: "connected", Name: "Connected"},
		{KitsuProjectID: "live", Name: "Live", ReadOnlyPreview: true},
		{KitsuProjectID: "validation", Name: "Validation", ValidationOnly: true},
	}
	if got := productionConnectionCounts(projects); got != (productionCounts{Total: 2, Connected: 1, Disconnected: 1}) {
		t.Fatalf("ValidationOnly record leaked into normal Production counts: %+v", got)
	}
}

func TestProductionConnectionCountsAllConnectedAndNoDisconnected(t *testing.T) {
	projects := []model.Project{{KitsuProjectID: "one"}, {KitsuProjectID: "two"}}
	if got := productionConnectionCounts(projects); got != (productionCounts{Total: 2, Connected: 2}) {
		t.Fatalf("all-connected Production counts = %+v", got)
	}
	if got := productionConnectionCounts(nil); got != (productionCounts{}) {
		t.Fatalf("empty Production counts = %+v", got)
	}
}

func TestProductionConnectionSummaryLabelsAreLocalized(t *testing.T) {
	counts := productionCounts{Total: 2, Connected: 1, Disconnected: 1}
	for _, tc := range []struct{ lang, total, connected, disconnected string }{
		{"en", "Production", "Connected 1", "Disconnected 1"},
		{"ja", "プロダクション", "接続済み 1", "未接続 1"},
	} {
		dashboard := dashboardProductionMetric(tc.lang, counts)
		list := productionListConnectionSummary(tc.lang, counts)
		for _, rendered := range []string{dashboard, list} {
			if !strings.Contains(rendered, strings.Split(tc.connected, " ")[0]+" <strong>1</strong>") || !strings.Contains(rendered, strings.Split(tc.disconnected, " ")[0]+" <strong>1</strong>") {
				t.Fatalf("%s summary labels are incomplete: %s", tc.lang, rendered)
			}
		}
		if !strings.Contains(dashboard, tc.total) {
			t.Fatalf("%s Dashboard is missing total Production label: %s", tc.lang, dashboard)
		}
	}
}

func TestCurrentNotificationRoutingIsReadOnlyUntilExplicitEdit(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "summary-routing", Name: "Summary Routing"}
	db.Create(&p)
	body := renderSelectedProductionNotifications(db, httptest.NewRequest("GET", "/bot/admin/projects?project=summary-routing&tab=notifications&lang=en", nil), p, "en", "ok", "Healthy", "")
	if strings.Contains(body, `name="action" value="save"`) {
		t.Fatal("default routing view exposed an edit form")
	}
	if !strings.Contains(body, "Kitsu Task Type") || !strings.Contains(body, "Edit") || strings.Contains(body, "Notification routing") {
		t.Fatal("read-only routing summary missing")
	}
}

func TestCurrentProductionUsersScaleWithoutSearchOrDetails(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "scale-production", Name: "Scale Production"}
	db.Create(&p)
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("User-%02d", i)
		email := fmt.Sprintf("user-%02d@example.com", i)
		db.Create(&model.UserMap{KitsuName: name, KitsuEmail: email, DiscordID: fmt.Sprintf("discord-%02d", i), DiscordDisplayName: "Discord " + name})
		model.UpsertProjectUserMap(db, p.ID, name, email, fmt.Sprintf("discord-%02d", i))
	}
	oldReader := reviewerProductionTeamReader
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) { return []kitsu.Person{}, nil }
	t.Cleanup(func() { reviewerProductionTeamReader = oldReader })
	body := renderCurrentProductionTeam(db, httptest.NewRequest("GET", "/bot/admin/projects?project=scale-production&tab=users&lang=en", nil), p, "en")
	if strings.Contains(body, "User-49") || strings.Contains(body, "User-00") {
		t.Fatalf("legacy ProjectUserMap rows were rendered as current Kitsu membership: %s", body)
	}
	if strings.Contains(body, "user_search") || strings.Contains(body, "user_status") || strings.Contains(body, "production-user-details") || strings.Contains(body, "production-participant-details") {
		t.Fatal("simple user view still contains scalable UI controls")
	}
}

func TestCurrentProductionTeamRendersSimpleReadOnlyFlow(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "simple-flow-production", Name: "Simple Flow Production"}
	db.Create(&p)
	db.Create(&model.UserMap{KitsuName: "Linked Human", KitsuEmail: "human@example.com", DiscordID: "discord-human", DiscordDisplayName: "Discord Human"})
	oldReader := reviewerProductionTeamReader
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) {
		return []kitsu.Person{{ID: "team-person", FullName: "Linked Human", Role: "artist"}}, nil
	}
	oldTasks := reviewerTaskTypesForProduction
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "task-animation", Name: "Animation"}}
	}
	t.Cleanup(func() { reviewerProductionTeamReader, reviewerTaskTypesForProduction = oldReader, oldTasks })
	body := renderCurrentProductionTeam(db, httptest.NewRequest("GET", "/bot/admin/projects?project=simple-flow-production&tab=team&lang=en", nil), p, "en")
	for _, want := range []string{"Team", "Kitsu role", "Linked Human", "Artist", "Department"} {
		if !strings.Contains(body, want) {
			t.Fatalf("simple flow missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `class="production-team-supervisor-scope"`) {
		t.Fatalf("ordinary Artist was given a Supervisor scope: %s", body)
	}
	for _, forbidden := range []string{"Add a user", "add_production_user", "Assigned", "remove_production_user"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("normal Production Users view still exposes manual membership %q: %s", forbidden, body)
		}
	}
	if strings.Contains(body, "user_search") || strings.Contains(body, "user_status") || strings.Contains(body, "production-user-details") || strings.Contains(body, "production-participant-details") {
		t.Fatalf("simple flow contains removed UI: %s", body)
	}
}

func TestCurrentProductionUsersUseKitsuTeamInsteadOfManualAssociations(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "kitsu-team-production", Name: "Kitsu Team Production", DiscordGuildID: "123456789012345678"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-linked", KitsuName: "Linked Person", DiscordID: "123456789012345678", DiscordDisplayName: "ukyo"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-unlinked", KitsuName: "Unlinked Person", DiscordID: "", DiscordDisplayName: ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-not-on-team", KitsuName: "Non Team Person", DiscordID: "123456789012345679", DiscordDisplayName: "outsider"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-bot", KitsuName: "Kitsu Bot", DiscordID: "123456789012345681", DiscordDisplayName: "bot"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProjectUserMap{ProjectID: project.ID, KitsuName: "Legacy Person", DiscordUserID: "123456789012345680"}).Error; err != nil {
		t.Fatal(err)
	}
	oldReader, oldMembers := reviewerProductionTeamReader, reviewerGuildMembersForGuild
	reviewerProductionTeamReader = func(_ *gorm.DB, projectID string) ([]kitsu.Person, error) {
		if projectID != project.KitsuProjectID {
			t.Fatalf("Kitsu team read project ID = %q, want %q", projectID, project.KitsuProjectID)
		}
		return []kitsu.Person{
			{ID: "person-linked", FullName: "Linked Person", Email: "linked@example.test"},
			{ID: "person-unlinked", FullName: "Unlinked Person", Email: "unlinked@example.test"},
			{ID: "person-bot", FullName: "Kitsu Bot", IsBot: true},
		}, nil
	}
	reviewerGuildMembersForGuild = func(_, _ string) ([]DiscordGuildMember, error) {
		return []DiscordGuildMember{reviewerTestGuildMember("123456789012345678", "ukyo", "ukyo", "")}, nil
	}
	t.Cleanup(func() { reviewerProductionTeamReader, reviewerGuildMembersForGuild = oldReader, oldMembers })

	body := renderCurrentProductionTeam(db, httptest.NewRequest("GET", "/bot/admin/projects?project=kitsu-team-production&tab=users&lang=en", nil), project, "en", "synthetic-token")
	for _, want := range []string{"Team", "Linked Person", "@ukyo", "Linked", "Unlinked Person", "Needs review", "Kitsu role"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Kitsu-driven Production Users view missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"add_production_user", "remove_production_user", "Add a user", "Assigned", "Legacy Person", "Non Team Person", "Kitsu Bot"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Production Users view still exposes or lists %q: %s", forbidden, body)
		}
	}
	if got := len(model.ListProjectUserMaps(db, project.ID)); got != 1 {
		t.Fatalf("rendering the Production Users page changed ProjectUserMap rows: got %d, want 1", got)
	}
}

func TestCurrentProductionUsersDistinguishKitsuEmptyAndReadFailure(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "team-state-production"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	oldReader := reviewerProductionTeamReader
	t.Cleanup(func() { reviewerProductionTeamReader = oldReader })
	render := func() string {
		return renderCurrentProductionTeam(db, httptest.NewRequest("GET", "/bot/admin/projects?project=team-state-production&tab=users&lang=en", nil), project, "en")
	}
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) { return []kitsu.Person{}, nil }
	empty := render()
	if !strings.Contains(empty, "The Kitsu Production Team is empty") || strings.Contains(empty, "could not load") {
		t.Fatalf("successful empty Production Team was not distinguished: %s", empty)
	}
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) { return nil, errors.New("fixture read failure") }
	failed := render()
	if !strings.Contains(failed, "Could not load the Kitsu Production Team") || strings.Contains(failed, "The Kitsu Production Team is empty") {
		t.Fatalf("Production Team read failure was collapsed into empty: %s", failed)
	}
	if strings.Contains(failed, `name="target_id"`) {
		t.Fatalf("Reviewer user target remained selectable after a team read failure: %s", failed)
	}
}

func TestProductionTeamViewUsesFreshKitsuMembershipOnEveryRender(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "live-team-production"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	oldReader := reviewerProductionTeamReader
	reads := 0
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) {
		reads++
		if reads == 1 {
			return []kitsu.Person{{ID: "removed-person", FullName: "Former Team Member"}}, nil
		}
		return []kitsu.Person{}, nil
	}
	t.Cleanup(func() { reviewerProductionTeamReader = oldReader })
	request := httptest.NewRequest("GET", "/bot/admin/projects?project=live-team-production&tab=users&lang=en", nil)
	first := renderCurrentProductionTeam(db, request, project, "en")
	second := renderCurrentProductionTeam(db, request, project, "en")
	if !strings.Contains(first, "Former Team Member") || strings.Contains(second, "Former Team Member") || reads != 2 {
		t.Fatalf("Production Team did not reflect the next live read: reads=%d first=%s second=%s", reads, first, second)
	}
	if got := len(model.ListProjectUserMaps(db, project.ID)); got != 0 {
		t.Fatalf("live team rendering persisted %d ProjectUserMap rows", got)
	}
}

func TestProductionSupervisorTaskTypeSummariesUseOnlyKnownMatchingMetadata(t *testing.T) {
	team := []kitsu.Person{
		{ID: "supervisor", Active: true, Role: "supervisor", Departments: []string{"dept-comp", "dept-anim"}},
		{ID: "artist", Active: true, Role: "artist", Departments: []string{"dept-comp"}},
		{ID: "unknown", Active: true, Role: "supervisor", Departments: []string{"dept-missing"}},
	}
	taskTypes := []kitsu.TaskType{
		{ID: "task-paint", Name: "Paint", DepartmentID: "dept-comp", DepartmentName: "Comp"},
		{ID: "task-roto", Name: "Roto", DepartmentID: "dept-comp", DepartmentName: "Comp"},
		{ID: "task-anim", Name: "Animation", DepartmentID: "dept-anim", DepartmentName: "Animation"},
		{ID: "task-no-name", Name: "No Department Name", DepartmentID: "dept-hidden"},
	}
	got := productionSupervisorTaskTypeSummaries(team, taskTypes)
	if want := "Animation: Animation / Comp: Paint, Roto"; got["supervisor"] != want {
		t.Fatalf("Supervisor summary = %q, want %q", got["supervisor"], want)
	}
	if _, ok := got["artist"]; ok {
		t.Fatal("non-Supervisor received a supervision summary")
	}
	if _, ok := got["unknown"]; ok {
		t.Fatal("Supervisor received a summary without matching Production Task Type metadata")
	}
}

func TestDiscordGuildMemberDisplayNamePriority(t *testing.T) {
	tests := []struct {
		name   string
		member DiscordGuildMember
		want   string
	}{
		{name: "nickname", member: reviewerTestGuildMember("", "username", "Global", "Guild Nick"), want: "@Guild Nick"},
		{name: "global display name", member: reviewerTestGuildMember("", "username", "Global", ""), want: "@Global"},
		{name: "username", member: reviewerTestGuildMember("", "username", "", ""), want: "@username"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := discordGuildMemberDisplayName(tc.member); got != tc.want {
				t.Fatalf("display name=%q want %q", got, tc.want)
			}
		})
	}
}

func TestProductionReviewerTargetMutationsValidateAndManageExplicitTargets(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "reviewer-mutations", DiscordGuildID: "123456789012345678"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProjectUserMap{ProjectID: project.ID, KitsuName: "Linked", DiscordUserID: "123456789012345679"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-linked", KitsuName: "Linked", KitsuEmail: "linked@example.com", DiscordID: "123456789012345679", DiscordDisplayName: "Linked Discord User"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-outsider", KitsuName: "Outsider", KitsuEmail: "outsider@example.com", DiscordID: "123456789012345683", DiscordDisplayName: "Outsider Discord User"}).Error; err != nil {
		t.Fatal(err)
	}
	legacy := model.ProjectCheckerMap{ProjectID: project.ID, TaskTypeID: "task-comp", TaskType: "Compositing", OverrideDiscordID: "123456789012345682"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	oldTasks, oldRoles, oldGuildRoles, oldTeam, oldGuildMembers := reviewerTaskTypesForProduction, reviewerDiscordRolesForGuild, reviewerGuildRolesReader, reviewerProductionTeamReader, reviewerGuildMembersForGuild
	reviewerTaskTypesForProduction = func(_ *gorm.DB, _ string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "task-comp", Name: "Compositing"}}
	}
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) {
		return []kitsu.Person{{ID: "person-linked", FullName: "Linked", Email: "linked@example.com", Active: true}}, nil
	}
	reviewerGuildMembersForGuild = func(_, _ string) ([]DiscordGuildMember, error) {
		return []DiscordGuildMember{reviewerTestGuildMember("123456789012345679", "linked", "", "")}, nil
	}
	reviewerDiscordRolesForGuild = func(guildID, _ string) ([]DiscordGuildRole, error) {
		return []DiscordGuildRole{{ID: guildID, Name: "@everyone", Mentionable: true}, {ID: "123456789012345680", Name: "Reviewers", Mentionable: true}, {ID: "123456789012345681", Name: "Private", Mentionable: false}}, nil
	}
	reviewerGuildRolesReader = reviewerDiscordRolesForGuild
	if err := model.SaveProductionForumSettings(db, reviewerBrowserForumSettings(project.KitsuProjectID, project.DiscordGuildID,
		[]model.ProductionForumRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", ForumChannelID: "forum-comp", ForumChannelName: "Compositing"}})); err != nil {
		t.Fatal(err)
	}
	forumReviewerRoleEvidenceReader = func(guildID, channelID, _ string) (reviewerRoleSendEvidence, error) {
		return reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, BotID: "123456789012345685", PermissionsKnown: true}, nil
	}
	t.Cleanup(func() {
		reviewerTaskTypesForProduction, reviewerDiscordRolesForGuild, reviewerGuildRolesReader, reviewerProductionTeamReader, reviewerGuildMembersForGuild = oldTasks, oldRoles, oldGuildRoles, oldTeam, oldGuildMembers
	})
	post := func(values string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/bot/admin/projects", strings.NewReader(values+"&project_id=reviewer-mutations"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		if !handleCurrentProductionUserMutation(w, r, db, "bot-token") {
			t.Fatal("Reviewer mutation was not handled")
		}
		return w
	}
	if w := post("action=add_production_reviewer_target&task_type_id=task-comp&target_kind=user&target_id=123456789012345679"); w.Code != http.StatusSeeOther {
		t.Fatalf("user add status=%d body=%s", w.Code, w.Body.String())
	} else if location := w.Header().Get("Location"); !strings.Contains(location, "tab=notifications") || !strings.Contains(location, "#wfa-recipients") {
		t.Fatalf("Reviewer mutation did not return to Notifications WFA recipients: %q", location)
	}
	if w := post("action=add_production_reviewer_target&task_type_id=task-comp&target_kind=role&target_id=123456789012345680"); w.Code != http.StatusSeeOther {
		t.Fatalf("role add status=%d body=%s", w.Code, w.Body.String())
	}
	if w := post("action=add_production_reviewer_target&task_type_id=task-comp&target_kind=user&target_id=123456789012345679"); w.Code != http.StatusSeeOther {
		t.Fatalf("duplicate add status=%d", w.Code)
	}
	targets := model.ListProjectReviewerTargets(db, project.ID)
	if len(targets) != 2 {
		t.Fatalf("target set=%+v, want one user and one role", targets)
	}
	if w := post("action=add_production_reviewer_target&task_type_id=task-comp&target_kind=role&target_id=123456789012345681"); w.Code != http.StatusBadRequest {
		t.Fatalf("non-mentionable role status=%d, want 400", w.Code)
	}
	if w := post("action=add_production_reviewer_target&task_type_id=task-comp&target_kind=user&target_id=123456789012345683"); w.Code != http.StatusBadRequest {
		t.Fatalf("globally linked non-Team user status=%d, want 400", w.Code)
	}
	if w := post("action=add_production_reviewer_target&task_type_id=unknown&target_kind=user&target_id=123456789012345679"); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown Task Type status=%d, want 400", w.Code)
	}
	if w := post("action=remove_production_reviewer_target&task_type_id=task-comp&target_id=" + strconv.FormatUint(uint64(targets[0].ID), 10)); w.Code != http.StatusSeeOther {
		t.Fatalf("remove status=%d body=%s", w.Code, w.Body.String())
	}
	if len(model.ListProjectReviewerTargets(db, project.ID)) != 1 {
		t.Fatal("removing one target removed more than the selected row")
	}
	if w := post("action=reset_production_reviewers&task_type_id=task-comp"); w.Code != http.StatusSeeOther {
		t.Fatalf("reset status=%d body=%s", w.Code, w.Body.String())
	}
	if len(model.ListProjectReviewerTargets(db, project.ID)) != 0 || len(model.ListProjectCheckerMaps(db, project.ID)) != 1 {
		t.Fatal("reset removed legacy ProjectCheckerMap rows or left explicit targets")
	}
}

func TestProductionReviewerMutationWaitsForNotificationApplyLock(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "reviewer-mutation-lock", DiscordGuildID: "123456789012345678"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	oldTaskTypes := reviewerTaskTypesForProduction
	calledTaskTypes := make(chan struct{}, 1)
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType {
		calledTaskTypes <- struct{}{}
		return nil
	}
	t.Cleanup(func() { reviewerTaskTypesForProduction = oldTaskTypes })

	unlock := lockProductionNotificationApply(project.KitsuProjectID)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader("action=reset_production_reviewers&project_id=reviewer-mutation-lock&task_type_id=task-comp"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handleCurrentProductionUserMutation(httptest.NewRecorder(), req, db)
	}()
	select {
	case <-calledTaskTypes:
		unlock()
		t.Fatal("Reviewer mutation began validation while Apply owned the Production lock")
	case <-time.After(30 * time.Millisecond):
	}
	unlock()
	select {
	case <-calledTaskTypes:
	case <-time.After(time.Second):
		t.Fatal("Reviewer mutation did not resume after Apply released the lock")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Reviewer mutation did not finish after lock release")
	}
}

func TestCurrentProductionUserMutationLeavesLegacyMembershipRowsUntouched(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "simple-mutation-production", Name: "Simple Mutation Production"}
	db.Create(&p)
	model.UpsertProjectUserMap(db, p.ID, "Legacy Person", "legacy@example.com", "123456789012345678")
	model.UpsertProjectCheckerMapWithUserAndTaskTypeID(db, p.ID, "task-animation", "Animation", "Legacy Person", "legacy@example.com", "123456789012345678", "")
	post := func(values string) bool {
		req := httptest.NewRequest("POST", "/bot/admin/projects", strings.NewReader(values))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return handleCurrentProductionUserMutation(httptest.NewRecorder(), req, db)
	}
	for _, action := range []string{
		"action=add_production_user&project_id=simple-mutation-production&user_id=1",
		"action=remove_production_user&project_id=simple-mutation-production&user_id=1",
		"action=save_production_checker&project_id=simple-mutation-production&user_id=1&task_type=Animation",
		"action=remove_production_checker&project_id=simple-mutation-production&task_type=Animation",
	} {
		if post(action) {
			t.Fatalf("obsolete manual Production membership action was still handled: %s", action)
		}
	}
	if len(model.ListProjectUserMaps(db, p.ID)) != 1 || len(model.ListProjectCheckerMaps(db, p.ID)) != 1 {
		t.Fatal("removing the manual workflow changed preserved legacy membership or Reviewer rows")
	}
}

func TestCurrentProductionUsersHideEmptyCheckerAssignmentList(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "empty-checker-production", Name: "Empty Checker Production"}
	db.Create(&p)
	model.UpsertProjectUserMap(db, p.ID, "Linked Human", "human@example.com", "discord-human")
	body := renderCurrentProductionTeam(db, httptest.NewRequest("GET", "/bot/admin/projects?project=empty-checker-production&tab=users&lang=en", nil), p, "en")
	if strings.Contains(body, `<h4>Assigned</h4>`) || strings.Contains(body, "No Reviewer / Checker assignments yet.") {
		t.Fatalf("empty Reviewer / Checker assignment list was rendered: %s", body)
	}
}

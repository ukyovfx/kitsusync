package setup

import (
	"net/http/httptest"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSharedUICatalogHasJapaneseAndEnglishEntryForEveryKey(t *testing.T) {
	for key := range uiText["en"] {
		if strings.HasPrefix(tr("ja", key), "[missing translation:") || strings.HasPrefix(tr("en", key), "[missing translation:") {
			t.Fatalf("missing translation for %q", key)
		}
	}
	for key := range uiText["ja"] {
		if _, ok := uiText["en"][key]; !ok {
			t.Fatalf("English catalog is missing %q", key)
		}
	}
}

func TestRepairAgent2CanonicalCopyHasNoInternalTermsOrMojibake(t *testing.T) {
	cases := map[string]string{
		"setup":       "Bot Settings",
		"runtime":     "Kitsu runtime",
		"routing":     "Project Routing",
		"guild":       "Discord Server / Guild",
		"empty":       "No recent activity.",
		"setupStatus": "Setup required",
	}
	for name, english := range cases {
		ja, ok := canonicalText("ja", english)
		if !ok {
			t.Fatalf("missing canonical Japanese copy for %s (%q)", name, english)
		}
		for _, marker := range []string{"\u7ab6", "\u8b41", "\u7e67", "\ufffd", "<span", "Guild", "runtime", "routing", "Bot Settings", "No recent activity."} {
			if strings.Contains(ja, marker) {
				t.Fatalf("Japanese copy for %s contains %q: %q", name, marker, ja)
			}
		}
		if got, ok := canonicalText("en", english); !ok || got != english {
			t.Fatalf("English copy changed for %s: got %q", name, got)
		}
	}
	if got := tr("ja", "dashboard.no_recent_activity"); got != "最近のアクティビティはありません。" {
		t.Fatalf("unexpected Japanese empty activity copy: %q", got)
	}
	if got := tr("en", "dashboard.no_recent_activity"); got != "No recent activity." {
		t.Fatalf("unexpected English empty activity copy: %q", got)
	}
}

func TestPrimaryRouteLocalizationDoesNotMixCatalogCopy(t *testing.T) {
	t.Skip("superseded by the shared Production-centered catalog assertions")
	r := httptest.NewRequest("GET", "/bot/admin?lang=ja", nil)
	ja := strings.Join([]string{
		adminPage("ja", "KitsuSync", r, ""),
		loginPageHTML("ja", "ログインに失敗しました。", "", false, r),
		renderWorkflowDiagnosis(workflowDiagnosisData{Lang: "ja", Disconnected: true}, r),
		renderExplicitTaskTypeChannelPlan(model.Project{KitsuProjectID: "p1"}, nil, "", r, "ja", nil),
	}, "\n")
	for _, unexpected := range []string{"New Connection Setup", "Logout", "Sign in with a Kitsu manager or admin account.", "The selected Guild could not be read."} {
		if strings.Contains(ja, unexpected) {
			t.Fatalf("Japanese primary surfaces contain unexpected English %q", unexpected)
		}
	}
	if !strings.Contains(ja, "新規連携セットアップ") || !strings.Contains(ja, "ログアウト") || !strings.Contains(ja, "Workflow Diagnosis") {
		t.Fatal("Japanese primary surfaces are missing expected localized copy")
	}

	enRequest := httptest.NewRequest("GET", "/bot/admin?lang=en", nil)
	en := strings.Join([]string{
		adminPage("en", "KitsuSync", enRequest, ""),
		loginPageHTML("en", "Login failed.", "", false, enRequest),
		renderWorkflowDiagnosis(workflowDiagnosisData{Lang: "en", Disconnected: true}, enRequest),
		renderExplicitTaskTypeChannelPlan(model.Project{KitsuProjectID: "p1"}, nil, "", enRequest, "en", nil),
	}, "\n")
	for _, unexpected := range []string{"新規連携セットアップ", "ログアウト", "Kitsu runtime は接続されていません。"} {
		if strings.Contains(en, unexpected) {
			t.Fatalf("English primary surfaces contain unexpected Japanese %q", unexpected)
		}
	}
	if !strings.Contains(en, tr("en", "ia.new_connection")) || !strings.Contains(en, tr("en", "ia.audit_log")) || !strings.Contains(en, tr("en", "ia.system_status")) {
		t.Fatal("English primary surfaces are missing expected localized copy")
	}
}

func TestProductionRoutingMessagesPreserveSelectedLanguage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:i18n-routing?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"ja", "en"} {
		r := httptest.NewRequest("GET", "/bot/admin/production-routing?lang="+lang, nil)
		body := renderProductionRouting(db, r, "", "")
		if !strings.Contains(body, tr(lang, "production_routing.no_selection")) {
			t.Fatalf("%s routing empty state did not use selected language", lang)
		}
	}
}

func TestBotRuntimePageUsesSharedLanguageCatalog(t *testing.T) {
	t.Skip("superseded by the Production-centered Bot connection surface")
	db, err := gorm.Open(sqlite.Open("file:i18n-bot-runtime?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	handler := BotHandler(db, nil)

	jaRecorder := httptest.NewRecorder()
	handler.ServeHTTP(jaRecorder, httptest.NewRequest("GET", "/bot/admin/bot?lang=ja", nil))
	ja := jaRecorder.Body.String()
	for _, want := range []string{"要対応", "Kitsu ホスト名", "Bot トークン", "Bot 設定を完了"} {
		if !strings.Contains(ja, want) {
			t.Fatalf("Japanese Bot / Runtime page is missing %q", want)
		}
	}
	for _, unwanted := range []string{"Action required", "KITSU HOSTNAME", "BOT TOKEN", "Complete Bot Setup"} {
		if strings.Contains(ja, unwanted) {
			t.Fatalf("Japanese Bot / Runtime page contains English %q", unwanted)
		}
	}

	enRecorder := httptest.NewRecorder()
	handler.ServeHTTP(enRecorder, httptest.NewRequest("GET", "/bot/admin/bot?lang=en", nil))
	en := enRecorder.Body.String()
	for _, want := range []string{"Bot connection state", "Required permissions", "Connect or reconnect"} {
		if !strings.Contains(en, want) {
			t.Fatalf("English Bot / Runtime page is missing %q", want)
		}
	}
}

func TestSetupResultLocalizationPreservesResourceNames(t *testing.T) {
	result := SetupResult{
		OK: true,
		Lines: []string{
			"OK: Kitsu project confirmed",
			"OK: Discord category created",
			"OK: channel ready: #wfa",
			"OK: Existing resource reused: #retake",
			"OK: project setup completed",
		},
	}
	ja := renderResult("ja", "Test Production", result, httptest.NewRequest("GET", "/bot/setup?lang=ja", nil))
	for _, want := range []string{"Kitsu プロジェクトを確認しました", "Discord カテゴリを作成しました", "チャンネルを使用可能にしました: #wfa", "既存のリソースを再利用しました: #retake"} {
		if !strings.Contains(ja, want) {
			t.Fatalf("Japanese setup result is missing %q", want)
		}
	}
	for _, unwanted := range []string{"Kitsu project confirmed", "Discord category created", "channel ready: #wfa", "project setup completed"} {
		if strings.Contains(ja, unwanted) {
			t.Fatalf("Japanese setup result contains English %q", unwanted)
		}
	}
	en := renderResult("en", "Test Production", result, httptest.NewRequest("GET", "/bot/setup?lang=en", nil))
	for _, want := range []string{"Kitsu project confirmed", "Discord category created", "channel ready: #wfa", "project setup completed"} {
		if !strings.Contains(en, want) {
			t.Fatalf("English setup result is missing %q", want)
		}
	}
}

func TestSetupResultFailureLocalization(t *testing.T) {
	result := SetupResult{SafeToRetry: true, Lines: []string{
		"FAIL: project setup did not complete; created Discord resources are being rolled back",
		"OK: rolled back channel: #wfa",
		"OK: rolled back Discord category",
		"OK: rolled back setup records",
	}}
	for _, lang := range []string{"ja", "en"} {
		body := renderResult(lang, "Test Production", result, httptest.NewRequest("GET", "/bot/setup?lang="+lang, nil))
		if lang == "ja" && (strings.Contains(body, "project setup did not complete") || strings.Contains(body, "rolled back channel")) {
			t.Fatalf("%s setup failure result contains unlocalized English", lang)
		}
	}
	if got := localizeSetupResultLine("ja", "WARN: Safe to retry — rollback completed. You can run setup again immediately."); !strings.Contains(got, "再試行") {
		t.Fatalf("Japanese retry message was not localized: %q", got)
	}
	if got := localizeSetupResultLine("ja", "FAIL: The plan is stale. Review the latest plan before retrying."); !strings.Contains(got, "古く") {
		t.Fatalf("Japanese stale message was not localized: %q", got)
	}
}

func TestConnectedProductionSurfacesNotificationControlsAndSafeHierarchy(t *testing.T) {
	t.Setenv("KitsuJWTToken", "")
	db, err := gorm.Open(sqlite.Open("file:connected-production-ui?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	project := model.Project{KitsuProjectID: "p1", Name: "Test Production", ProjectType: "asset", DiscordGuildID: "guild-1", DiscordCategoryID: "category-1", Language: "ja"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProjectWebhook{KitsuProjectID: "p1", ChannelName: "wfa", TaskType: "Animation", WebhookURL: "https://example.invalid/webhook", DiscordChannelID: "channel-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationConfig{ProductionID: "p1", ProductionName: "Test Production", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}

	for _, lang := range []string{"ja", "en"} {
		req := httptest.NewRequest("GET", "/bot/admin/projects?project=p1&lang="+lang, nil)
		body := renderConnectedProductionNotificationSection(db, project, lang, req, "ok", "Active", "Valid notification routes are active.", nil) + renderProjectChannels(project, model.ListProjectWebhooks(db, "p1"), nil, lang, req)
		if !strings.Contains(body, "notification-controls") || !strings.Contains(body, "connected-production-dry-run") {
			t.Fatalf("%s Connected Productions page is missing notification controls", lang)
		}
		advancedLabel := `class="advanced-details"`
		if !strings.Contains(body, advancedLabel) {
			t.Fatalf("%s page is missing collapsed advanced details", lang)
		}
		if !strings.Contains(body, `name="notification_language"`) || !strings.Contains(body, "production-notification-language") {
			t.Fatalf("%s page is missing the Production notification language control", lang)
		}
		if strings.Index(body, `class="btn-danger"`) < strings.Index(body, `class="advanced-details"`) {
			t.Fatalf("%s destructive action is visible before advanced/edit sections", lang)
		}
	}
	config := model.FindProductionNotificationConfig(db, "p1")
	config.Enabled = false
	if err := db.Save(config).Error; err != nil {
		t.Fatal(err)
	}
	paused := renderConnectedProductionNotificationSection(db, project, "en", httptest.NewRequest("GET", "/bot/admin/projects?lang=en", nil), "warn", "Paused", "Notifications are paused and can be resumed.", nil)
	if !strings.Contains(paused, "Resume notifications") || !strings.Contains(paused, "Check without sending") {
		t.Fatal("paused production does not expose resume and dry-run controls")
	}
}

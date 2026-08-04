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

func TestPrimaryRouteLocalizationDoesNotMixCatalogCopy(t *testing.T) {
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
	if !strings.Contains(en, "New Connection Setup") || !strings.Contains(en, "Logout") || !strings.Contains(en, "Workflow Diagnosis") {
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

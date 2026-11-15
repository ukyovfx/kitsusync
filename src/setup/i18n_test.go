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

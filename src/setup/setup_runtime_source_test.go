package setup

import (
	"app/src/model"
	"app/src/utils/request"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupUsesPersistedKitsuRuntimeSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/data/projects/":
			_, _ = w.Write([]byte(`[{"id":"production-1","name":"Visible Production"}]`))
		case "/api/data/projects/production-1/task-types":
			_, _ = w.Write([]byte(`[{"id":"tt-1","name":"Compositing","active":true}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}

	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	t.Setenv("KitsuJWTToken", "")
	t.Setenv("DISCORD_BOT_TOKEN", "")
	db := newRuntimeCredentialTestDB(t)
	model.SetSetting(db, "kitsu.hostname", server.URL)
	model.SetSetting(db, KitsuAPIBaseURLSettingKey, server.URL)
	if err := setRuntimeKitsuToken(db, "persisted-kitsu-token"); err != nil {
		t.Fatal(err)
	}
	if err := SetRuntimeDiscordBotToken(db, "persisted-discord-token"); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	renderIANewConnection(w, httptest.NewRequest("GET", "/bot/setup?lang=en&wizard_step=2", nil), db)
	body := w.Body.String()
	if !strings.Contains(body, `value="production-1"`) || !strings.Contains(body, "Visible Production") {
		t.Fatalf("Setup did not render the persisted-runtime Production: %s", body)
	}
	taskTypes := setupKitsuTaskTypes(db, "production-1")
	if len(taskTypes) != 1 || taskTypes[0].ID != "tt-1" || taskTypes[0].Name != "Compositing" {
		t.Fatalf("Setup Task Types did not use the persisted runtime source: %+v", taskTypes)
	}
}

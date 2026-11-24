package setup

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPR199ConnectionsSavedTokenControlsAreMaskedAndReversible(t *testing.T) {
	db := newSetupStateTestDB(t)
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	if err := setRuntimeKitsuToken(db, "saved-kitsu-token"); err != nil {
		t.Fatal(err)
	}
	if err := setRuntimeDiscordBotToken(db, "saved-discord-token"); err != nil {
		t.Fatal(err)
	}

	body := renderConnectionsEditFormWithIdentityRows("en", httptest.NewRequest("GET", "/bot/admin/bot?edit=1&lang=en", nil), db, "", "ok", "Connected", "https://kitsu.example.test", true, true, "Test Bot")

	for _, want := range []string{
		`id="kitsu-bot-token"`,
		`id="discord-bot-token"`,
		`placeholder="` + connectionSecretMask + `"`,
		"Cancel",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("configured-token edit view omitted %q", want)
		}
	}
	for _, unwanted := range []string{`data-token-change`, `data-token-cancel`, "Change token", " disabled placeholder="} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("configured-token edit view must not contain %q", unwanted)
		}
	}

	for _, secret := range []string{"saved-kitsu-token", "saved-discord-token"} {
		if strings.Contains(body, secret) {
			t.Fatalf("stored secret %q was rendered", secret)
		}
	}

	copy := "Used for links in Discord notifications."
	if strings.Count(body, copy) != 1 {
		t.Fatalf("External Kitsu URL explanation count=%d, want 1", strings.Count(body, copy))
	}
}

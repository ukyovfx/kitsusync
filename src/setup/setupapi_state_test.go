package setup

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newSetupStateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("failed to migrate settings schema: %v", err)
	}
	return db
}

func installDiscordAPIStub(t *testing.T, guildID string) {
	t.Helper()
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "discord.com" {
			return nil, fmt.Errorf("unexpected host: %s", req.URL.Host)
		}
		switch req.URL.Path {
		case "/api/v10/users/@me":
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"bot-user-id","username":"TestBot"}`))}, nil
		case "/api/v10/guilds/" + guildID:
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"name":"TestGuild"}`))}, nil
		case "/api/v10/guilds/" + guildID + "/members/bot-user-id":
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"permissions":"0"}`))}, nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"not found"}`))}, nil
		}
	})
	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
	})
}

func TestTestDiscordHandler_DoesNotMutateRuntimeOrSettings(t *testing.T) {
	db := newSetupStateTestDB(t)
	const originalToken = "original-token"
	const originalGuild = "111111111111111111"
	const submittedToken = "submitted-token"
	const submittedGuild = "222222222222222222"

	t.Setenv("DISCORD_BOT_TOKEN", originalToken)
	t.Setenv("DISCORD_GUILD_ID", originalGuild)
	model.SetSetting(db, "discord.guildID", "stored-before")
	installDiscordAPIStub(t, submittedGuild)

	h := TestDiscordHandler(db)
	body := `{"bot_token":"` + submittedToken + `","guild_id":"` + submittedGuild + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/setup/test-discord", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp TestDiscordResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if !resp.BotValid {
		t.Fatalf("expected bot_valid=true, got %+v", resp)
	}

	if got := strings.TrimSpace(model.GetSetting(db, "discord.guildID")); got != "stored-before" {
		t.Fatalf("discord.guildID should stay unchanged, got %q", got)
	}
	if got := strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")); got != originalToken {
		t.Fatalf("DISCORD_BOT_TOKEN should stay unchanged, got %q", got)
	}
	if got := strings.TrimSpace(os.Getenv("DISCORD_GUILD_ID")); got != originalGuild {
		t.Fatalf("DISCORD_GUILD_ID should stay unchanged, got %q", got)
	}
}

func TestBotHandler_PostPersistsRuntimeTokenAndGuildID(t *testing.T) {
	db := newSetupStateTestDB(t)
	model.SetSetting(db, "kitsu.hostname", "http://kitsu.local/")
	t.Setenv("DISCORD_BOT_TOKEN", "before-token")

	h := BotHandler(db, nil)
	form := url.Values{}
	form.Set("bot_token", "rotated-token")
	form.Set("guild_id", "999999999999999999")
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect status, got %d", rr.Code)
	}
	if got := strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")); got != "rotated-token" {
		t.Fatalf("expected runtime token update, got %q", got)
	}
	if got := strings.TrimSpace(model.GetSetting(db, RuntimeDiscordBotTokenKey)); got != "rotated-token" {
		t.Fatalf("expected persisted runtime token, got %q", got)
	}
	if got := strings.TrimSpace(model.GetSetting(db, "discord.guildID")); got != "999999999999999999" {
		t.Fatalf("expected persisted guild ID, got %q", got)
	}
}

func TestBotHandler_EditFormShowsDurablePersistenceCopy(t *testing.T) {
	db := newSetupStateTestDB(t)
	model.SetSetting(db, "kitsu.hostname", "http://kitsu.local/")

	resetSessions()
	token := newSessionToken("manager@example.com", "jwt", "manager", "/bot/admin/bot?edit=1&lang=en")
	req := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?edit=1&lang=en", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()

	BotHandler(db, nil)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for edit form, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Token changes take effect immediately for the running process and are also saved in app settings.") {
		t.Fatalf("expected durable token persistence copy in bot settings form")
	}
	if !strings.Contains(body, "After restart, the saved token is used first.") {
		t.Fatalf("expected restart persistence copy in bot settings form")
	}
	if strings.Contains(body, "Compatibility fallback Guild setting") {
		t.Fatalf("did not expect guild fallback section in bot settings form")
	}
}

func TestHandler_GetSetupUsesProductionAndServerSelectionFlow(t *testing.T) {
	db := newSetupStateTestDB(t)
	model.SetSetting(db, "kitsu.hostname", "http://kitsu.local/")
	model.SetSetting(db, RuntimeKitsuEmailSettingKey, "kitsusync-bot@local.invalid")

	req := httptest.NewRequest(http.MethodGet, "/bot/setup?lang=en", nil)
	rr := httptest.NewRecorder()

	Handler("http://kitsu.local/", "111111111111111111", "runtime-bot-token", db, func() bool { return true }, nil)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for setup page, got %d", rr.Code)
	}

	body := rr.Body.String()
	for _, want := range []string{"Prerequisites", "Production", "Discord server", "Channel plan", "Review", "Execute", "Complete", "Bot Connection is not configured"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected approved connection flow copy %q", want)
		}
	}
	if strings.Contains(body, `name="guild_id"`) || strings.Contains(body, "Discord Server / Guild ID") {
		t.Fatalf("did not expect manual Guild ID entry in the normal setup flow")
	}
}

func TestHandler_PostSetupRequiresProjectLevelGuildID(t *testing.T) {
	db := newSetupStateTestDB(t)

	form := url.Values{}
	form.Set("kitsu_project_id", "proj-1")
	form.Set("project_name", "Project One")
	form.Set("project_type", "cg")
	form.Set("language", "en")

	req := httptest.NewRequest(http.MethodPost, "/bot/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	Handler("http://kitsu.local/", "111111111111111111", "runtime-bot-token", db, func() bool { return true }, nil)(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when guild_id is missing, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "project-level guild_id is required") {
		t.Fatalf("expected missing guild_id error, got %q", rr.Body.String())
	}
}

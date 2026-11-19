package setup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"app/src/api/discord"
	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testNotificationDestinationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.Setting{}, &model.ProductionForumConfig{}, &model.ProductionForumRoute{}, &model.ProductionForumStatusRole{}, &model.ProductionForumTag{}, &model.ForumBaseline{}, &model.ForumTask{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func saveTestForumSettings(t *testing.T, db *gorm.DB, productionID, guildID string, forums ...string) {
	t.Helper()
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: productionID, GuildID: guildID},
		StatusRoles: []model.ProductionForumStatusRole{
			{StatusID: "s-wfa", Role: model.ForumRoleWFA},
			{StatusID: "s-retake", Role: model.ForumRoleRetake},
			{StatusID: "s-done", Role: model.ForumRoleDone},
		},
	}
	for i, forumID := range forums {
		settings.Routes = append(settings.Routes, model.ProductionForumRoute{TaskTypeID: "tt-" + string(rune('a'+i)), ForumChannelID: forumID, ForumChannelName: forumID})
		for _, role := range []string{model.ForumRoleWFA, model.ForumRoleRetake, model.ForumRoleDone} {
			settings.Tags = append(settings.Tags, model.ProductionForumTag{ForumChannelID: forumID, Role: role, TagID: forumID + "-" + role})
		}
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatal(err)
	}
}

// stubTestNotificationDiscord replaces the channel list cache and captures the
// forum thread create so no request leaves the process.
func stubTestNotificationDiscord(t *testing.T, forumID string) *[]map[string]any {
	t.Helper()
	oldCache := guildChannelListCache
	t.Cleanup(func() { guildChannelListCache = oldCache })

	bodies := &[]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["path"] = r.URL.Path
		*bodies = append(*bodies, body)
		json.NewEncoder(w).Encode(map[string]any{"id": "thread-1"})
	}))
	t.Cleanup(server.Close)
	if previous := discord.SetDiscordBotAPIBase(server.URL); previous != "" {
		t.Cleanup(func() { discord.SetDiscordBotAPIBase(previous) })
	}

	channelBody := []byte(`[{"id":"` + forumID + `","name":"animation","type":15,"available_tags":[]}]`)
	guildChannelListCache = &guildChannelsCache{
		ttl:     guildChannelCacheTTL,
		entries: map[string]*guildChannelCacheEntry{"g1": {body: channelBody, fetchedAt: time.Now()}},
		loads:   map[string]*guildChannelLoad{},
		fetch: func(string, string) ([]byte, int, error) {
			return channelBody, http.StatusOK, nil
		},
		now: time.Now,
	}
	return bodies
}

func TestTestNotificationResolvesForumDestinationWithoutWebhookRows(t *testing.T) {
	db := testNotificationDestinationDB(t)
	if err := model.CreateProject(db, "project-1", "Local Test", "", "g1", "", "ja"); err != nil {
		t.Fatal(err)
	}
	saveTestForumSettings(t, db, "project-1", "g1", "forum-1", "forum-2")
	if rows := model.ListProjectWebhooks(db, "project-1"); len(rows) != 0 {
		t.Fatalf("fixture must have no webhook rows, got %d", len(rows))
	}

	settings, ok, err := model.LoadProductionForumSettings(db, "project-1")
	if err != nil || !ok {
		t.Fatalf("forum settings must load: ok=%t err=%v", ok, err)
	}
	if _, err := testNotificationForum(settings, ""); err == nil {
		t.Fatal("two routed forums must require an explicit selection")
	}
	forum, err := testNotificationForum(settings, "forum-2")
	if err != nil {
		t.Fatal(err)
	}
	if forum.ForumChannelID != "forum-2" {
		t.Fatalf("unexpected forum: %#v", forum)
	}
	if _, err := testNotificationForum(settings, "forum-unknown"); err == nil {
		t.Fatal("a forum that is not routed must be rejected")
	}
}

func TestTestNotificationSendsIntoTheForumThroughTheBot(t *testing.T) {
	db := testNotificationDestinationDB(t)
	if err := model.CreateProject(db, "project-1", "Local Test", "", "g1", "", "ja"); err != nil {
		t.Fatal(err)
	}
	saveTestForumSettings(t, db, "project-1", "g1", "forum-1")
	bodies := stubTestNotificationDiscord(t, "forum-1")

	handler := TestNotificationHandler(db, func() (string, string, string, string) { return "", "bot-token", "g1", "" })
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/api/setup/test-notification",
		strings.NewReader(`{"project_id":"project-1"}`)))

	var resp TestNotificationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	if !resp.OK || resp.Error != nil || resp.MessageID != "thread-1" {
		t.Fatalf("test notification did not succeed: %s", rec.Body.String())
	}
	if len(*bodies) != 1 || (*bodies)[0]["path"] != "/channels/forum-1/threads" {
		t.Fatalf("expected one forum thread create, got %#v", *bodies)
	}
	if !strings.EqualFold(model.GetSetting(db, setupTestNotificationVerifiedKey), "true") {
		t.Fatal("the test notification must be recorded as verified")
	}
}

func TestProjectDeliverySelectorListsRoutedForumsAndKeepsRoutingReadOnly(t *testing.T) {
	db := testNotificationDestinationDB(t)
	if err := model.CreateProject(db, "project-1", "Local Test", "", "g1", "", "ja"); err != nil {
		t.Fatal(err)
	}
	saveTestForumSettings(t, db, "project-1", "g1", "forum-1", "forum-2")
	before, _, err := model.LoadProductionForumSettings(db, "project-1")
	if err != nil {
		t.Fatal(err)
	}

	state := buildProjectDeliveryState("ja", db, "/api/setup/test-notification")
	if len(state.Projects) != 1 || len(state.Projects[0].Destinations) != 2 || state.SummaryStatus != "warn" {
		t.Fatalf("expected both routed forums, got %#v", state)
	}
	markup := renderProjectDeliverySelectorCard("ja", state)
	for _, want := range []string{"テスト送信先", "送信先を選択", "destination_forum", "通常の通知ルーティングは変更しません"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("selector missing %q", want)
		}
	}
	enMarkup := renderProjectDeliverySelectorCard("en", state)
	for _, want := range []string{"Test destination", "Select a destination", "Normal notification routing is not changed"} {
		if !strings.Contains(enMarkup, want) {
			t.Fatalf("English selector missing %q", want)
		}
	}

	after, _, err := model.LoadProductionForumSettings(db, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Routes) != len(before.Routes) || len(after.Tags) != len(before.Tags) {
		t.Fatalf("rendering the selector changed forum routing: before=%#v after=%#v", before, after)
	}
}

func TestProjectDeliveryStateReportsMissingForumSettings(t *testing.T) {
	db := testNotificationDestinationDB(t)
	if err := model.CreateProject(db, "project-1", "No Forum", "", "g1", "", "ja"); err != nil {
		t.Fatal(err)
	}
	state := buildProjectDeliveryState("ja", db, "/api/setup/test-notification")
	if state.SummaryStatus != "fail" || len(state.Projects) != 1 {
		t.Fatalf("missing forum settings must fail the delivery state: %#v", state)
	}
	if state.Projects[0].Summary != "フォーラム未設定" {
		t.Fatalf("unexpected summary: %#v", state.Projects[0])
	}
}

func TestTestNotificationPayloadUsesProductionLanguageAndNeverMentions(t *testing.T) {
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(workingDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()
	ja := testNotificationPayload(model.Project{Name: "Local", Language: "ja"}, "hello @everyone")
	if ja.AllowedMentions == nil || len(ja.AllowedMentions.Users) != 0 || len(ja.AllowedMentions.Roles) != 0 || len(ja.AllowedMentions.Parse) != 0 {
		t.Fatalf("Japanese test payload must disable mentions: %#v", ja.AllowedMentions)
	}
	if strings.Contains(ja.Content, "@") || strings.Contains(ja.Embeds[0].Author.Name, "👀") {
		t.Fatalf("test payload leaked a mention or Task Type decoration: %#v", ja)
	}
	en := testNotificationPayload(model.Project{Name: "Local", Language: "en"}, "")
	if !strings.Contains(en.Embeds[0].Description, "Please review this test notification") {
		t.Fatalf("English production language was not used: %#v", en.Embeds[0])
	}
}

var _ = url.Values{}

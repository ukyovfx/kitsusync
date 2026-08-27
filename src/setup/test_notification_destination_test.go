package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testNotificationDestinationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:test-notification-destination-%p?mode=memory&cache=shared", t)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestValidateTestNotificationDestinationRequiresExplicitOwnedDestination(t *testing.T) {
	db := testNotificationDestinationDB(t)
	if err := model.CreateProject(db, "project-1", "Local Test", "", "guild-1", "category-1", "ja"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "project-1", "animation", "Animation", "https://discord.invalid/one", "channel-1"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "project-1", "compositing", "Compositing", "https://discord.invalid/two", "channel-2"); err != nil {
		t.Fatal(err)
	}
	webhooks := model.ListProjectWebhooks(db, "project-1")
	if len(webhooks) != 2 {
		t.Fatalf("expected two destinations, got %d", len(webhooks))
	}
	channels := []DiscordGuildChannel{
		{ID: "channel-1", ParentID: "category-1"},
		{ID: "channel-2", ParentID: "category-1"},
	}
	if _, err := validateTestNotificationDestination(db, "project-1", 0, channels); err == nil {
		t.Fatal("missing destination selection must be rejected")
	}
	selected, err := validateTestNotificationDestination(db, "project-1", webhooks[1].ID, channels)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != webhooks[1].ID || selected.DiscordChannelID != "channel-2" {
		t.Fatalf("unexpected selected destination: %#v", selected)
	}
}

func TestValidateTestNotificationDestinationRejectsStaleCrossProjectAndCategory(t *testing.T) {
	db := testNotificationDestinationDB(t)
	for _, project := range []struct {
		id, guild, category string
	}{
		{"project-1", "guild-1", "category-1"},
		{"project-2", "guild-2", "category-2"},
	} {
		if err := model.CreateProject(db, project.id, project.id, "", project.guild, project.category, "en"); err != nil {
			t.Fatal(err)
		}
	}
	if err := model.CreateProjectWebhook(db, "project-2", "other", "Other", "https://discord.invalid/other", "channel-other"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "project-1", "stale", "Stale", "https://discord.invalid/stale", "channel-stale"); err != nil {
		t.Fatal(err)
	}
	other := model.ListProjectWebhooks(db, "project-2")[0]
	stale := model.ListProjectWebhooks(db, "project-1")[0]
	if _, err := validateTestNotificationDestination(db, "project-1", other.ID, []DiscordGuildChannel{{ID: "channel-other", ParentID: "category-1"}}); err == nil {
		t.Fatal("cross-project destination must be rejected")
	}
	if _, err := validateTestNotificationDestination(db, "project-1", stale.ID, nil); err == nil {
		t.Fatal("stale destination must be rejected")
	}
	if _, err := validateTestNotificationDestination(db, "project-1", stale.ID, []DiscordGuildChannel{{ID: "channel-stale", ParentID: "category-other"}}); err == nil {
		t.Fatal("cross-category destination must be rejected")
	}
}

func TestProjectDeliverySelectorListsAllDestinationsAndKeepsRoutingReadOnly(t *testing.T) {
	db := testNotificationDestinationDB(t)
	if err := model.CreateProject(db, "project-1", "Local Test", "", "guild-1", "category-1", "ja"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "project-1", "animation", "Animation", "https://discord.invalid/one", "channel-1"); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "project-1", "compositing", "Compositing", "https://discord.invalid/two", "channel-2"); err != nil {
		t.Fatal(err)
	}
	webhooks := model.ListProjectWebhooks(db, "project-1")
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "project-1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "animation", DestinationWebhookID: webhooks[0].ID}}); err != nil {
		t.Fatal(err)
	}
	beforeRoutes := len(model.ListProductionNotificationRoutes(db, "project-1"))
	state := buildProjectDeliveryState("ja", db, "/api/setup/test-notification")
	if len(state.Projects) != 1 || len(state.Projects[0].Destinations) != 2 {
		t.Fatalf("expected all configured destinations, got %#v", state.Projects)
	}
	markup := renderProjectDeliverySelectorCard("ja", state)
	for _, want := range []string{"テスト送信先", "送信先を選択", "destination_webhook_id", "通常の通知ルーティングは変更しません"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("selector missing %q", want)
		}
	}
	if afterRoutes := len(model.ListProductionNotificationRoutes(db, "project-1")); afterRoutes != beforeRoutes {
		t.Fatalf("test notification selector changed normal routing: before=%d after=%d", beforeRoutes, afterRoutes)
	}
	enMarkup := renderProjectDeliverySelectorCard("en", state)
	for _, want := range []string{"Test destination", "Select a destination", "Normal notification routing is not changed"} {
		if !strings.Contains(enMarkup, want) {
			t.Fatalf("English selector missing %q", want)
		}
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

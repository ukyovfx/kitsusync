package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type setupDiscordProvisioningStub struct {
	t                *testing.T
	categoryID       string
	createdChannelID []string
	deletedIDs       []string
	nextChannelNum   int
}

func newSetupHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}); err != nil {
		t.Fatalf("failed to migrate setup schema: %v", err)
	}
	return db
}

func installSetupDiscordProvisioningStub(t *testing.T) *setupDiscordProvisioningStub {
	t.Helper()
	stub := &setupDiscordProvisioningStub{
		t:          t,
		categoryID: "cat-setup-test",
	}

	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "discord.com" {
			return nil, fmt.Errorf("unexpected host: %s", req.URL.Host)
		}
		if !strings.HasPrefix(req.URL.Path, "/api/v10/") {
			return nil, fmt.Errorf("unexpected path: %s", req.URL.Path)
		}

		switch {
		case req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/api/v10/guilds/") && strings.HasSuffix(req.URL.Path, "/channels"):
			var payload struct {
				Type int `json:"type"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload.Type == 4 {
				return jsonResp(http.StatusOK, `{"id":"`+stub.categoryID+`"}`), nil
			}
			if payload.Type == 0 {
				stub.nextChannelNum++
				channelID := fmt.Sprintf("ch-%d", stub.nextChannelNum)
				stub.createdChannelID = append(stub.createdChannelID, channelID)
				return jsonResp(http.StatusOK, `{"id":"`+channelID+`"}`), nil
			}
			return jsonResp(http.StatusBadRequest, `{"message":"unexpected channel type"}`), nil

		case req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/api/v10/channels/") && strings.HasSuffix(req.URL.Path, "/webhooks"):
			parts := strings.Split(req.URL.Path, "/")
			channelID := parts[len(parts)-2]
			return jsonResp(http.StatusOK, `{"id":"wh-`+channelID+`","token":"tok-`+channelID+`"}`), nil

		case req.Method == http.MethodDelete && strings.HasPrefix(req.URL.Path, "/api/v10/channels/"):
			parts := strings.Split(req.URL.Path, "/")
			channelID := parts[len(parts)-1]
			stub.deletedIDs = append(stub.deletedIDs, channelID)
			return jsonResp(http.StatusOK, `{"id":"`+channelID+`"}`), nil
		}

		return jsonResp(http.StatusNotFound, `{"message":"not found"}`), nil
	})
	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
	})
	return stub
}

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func registerWebhookCreateFailure(t *testing.T, db *gorm.DB) {
	t.Helper()
	const callbackName = "test:fail_project_webhook_create"
	err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "project_webhooks" {
			tx.AddError(errors.New("forced webhook insert failure"))
		}
	})
	if err != nil {
		t.Fatalf("failed to register callback: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(callbackName)
	})
}

func TestRunProjectSetup_CleansUpDiscordResourcesWhenDBTransactionFails(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	stub := installSetupDiscordProvisioningStub(t)
	registerWebhookCreateFailure(t, db)

	res := RunProjectSetup(
		"kitsu-proj-db-fail",
		"Project DB Fail",
		"cg",
		"en",
		"http://kitsu.local/",
		"fallback-guild",
		"guild-123",
		"bot-token",
		db,
	)

	if res.OK {
		t.Fatalf("expected setup failure when DB transaction fails")
	}
	if !res.SafeToRetry {
		t.Fatalf("expected SafeToRetry=true when cleanup succeeds")
	}
	if len(stub.createdChannelID) == 0 {
		t.Fatalf("expected stub to create channels")
	}
	expectedDeleteCount := len(stub.createdChannelID) + 1
	if len(stub.deletedIDs) != expectedDeleteCount {
		t.Fatalf("expected %d cleanup deletes, got %d (%v)", expectedDeleteCount, len(stub.deletedIDs), stub.deletedIDs)
	}
	for _, channelID := range stub.createdChannelID {
		if !containsString(stub.deletedIDs, channelID) {
			t.Fatalf("expected cleanup delete for channel %s; got %v", channelID, stub.deletedIDs)
		}
	}
	if !containsString(stub.deletedIDs, stub.categoryID) {
		t.Fatalf("expected cleanup delete for category %s; got %v", stub.categoryID, stub.deletedIDs)
	}
	if got := model.FindProjectByKitsuID(db, "kitsu-proj-db-fail"); got != nil {
		t.Fatalf("project should not persist after tx rollback")
	}
	if got := model.ListProjectWebhooks(db, "kitsu-proj-db-fail"); len(got) != 0 {
		t.Fatalf("webhooks should not persist after tx rollback, got %d", len(got))
	}
}

func TestRunProjectSetup_DoesNotCleanupOnSuccess(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	stub := installSetupDiscordProvisioningStub(t)

	res := RunProjectSetup(
		"kitsu-proj-success",
		"Project Success",
		"cg",
		"en",
		"http://kitsu.local/",
		"fallback-guild",
		"guild-123",
		"bot-token",
		db,
	)

	if !res.OK {
		t.Fatalf("expected setup success, got lines: %v", res.Lines)
	}
	if len(stub.deletedIDs) != 0 {
		t.Fatalf("expected no cleanup deletes on success path, got %v", stub.deletedIDs)
	}
	if got := model.FindProjectByKitsuID(db, "kitsu-proj-success"); got == nil {
		t.Fatalf("project should persist on success")
	}
	if got := model.ListProjectWebhooks(db, "kitsu-proj-success"); len(got) == 0 {
		t.Fatalf("expected persisted webhooks on success")
	}
}

func TestRunProjectSetup_PersistsRequestedGuildIDOnProjectRecord(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	installSetupDiscordProvisioningStub(t)

	res := RunProjectSetup(
		"kitsu-proj-requested-guild",
		"Project Requested Guild",
		"cg",
		"en",
		"http://kitsu.local/",
		"fallback-guild",
		"requested-guild",
		"bot-token",
		db,
	)

	if !res.OK {
		t.Fatalf("expected setup success, got lines: %v", res.Lines)
	}
	project := model.FindProjectByKitsuID(db, "kitsu-proj-requested-guild")
	if project == nil {
		t.Fatalf("expected project to persist on success")
	}
	if got := strings.TrimSpace(project.DiscordGuildID); got != "requested-guild" {
		t.Fatalf("expected requested guild to persist, got %q", got)
	}
}

func TestRunProjectSetup_UsesFallbackGuildIDWhenProjectInputIsBlank(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	installSetupDiscordProvisioningStub(t)

	res := RunProjectSetup(
		"kitsu-proj-fallback-guild",
		"Project Fallback Guild",
		"cg",
		"en",
		"http://kitsu.local/",
		"fallback-guild",
		"",
		"bot-token",
		db,
	)

	if !res.OK {
		t.Fatalf("expected setup success, got lines: %v", res.Lines)
	}
	project := model.FindProjectByKitsuID(db, "kitsu-proj-fallback-guild")
	if project == nil {
		t.Fatalf("expected project to persist on success")
	}
	if got := strings.TrimSpace(project.DiscordGuildID); got != "fallback-guild" {
		t.Fatalf("expected fallback guild to persist, got %q", got)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

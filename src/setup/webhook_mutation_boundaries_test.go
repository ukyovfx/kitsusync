package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"app/src/model"
)

func TestCompleteConnectionCleanupRechecksWebhookStateAfterWaitingForApply(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "cleanup-race", Name: "Cleanup Race"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	unlockApply := lockProductionNotificationApply(project.KitsuProjectID)
	requestStarted := make(chan struct{})
	requestDone := make(chan struct{})
	response := httptest.NewRecorder()
	go func() {
		defer close(requestDone)
		form := url.Values{"action": {"complete_connection_cleanup"}, "project_id": {project.KitsuProjectID}, "confirm_text": {"delete"}}
		request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?project=cleanup-race", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		close(requestStarted)
		AdminProjectsHandler(db, "", "")(response, request)
	}()
	<-requestStarted

	// Let the handler observe the initially empty connection state before the
	// simulated Apply commits its first webhook while holding the shared lock.
	time.Sleep(100 * time.Millisecond)
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "new-route", "", "https://example.invalid/new-route", "channel-new"); err != nil {
		unlockApply()
		t.Fatal(err)
	}
	unlockApply()
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish after Apply released the Production lock")
	}
	if model.FindProjectByKitsuID(db, project.KitsuProjectID) == nil {
		t.Fatal("cleanup removed the Production after a webhook appeared while it waited")
	}
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	if len(webhooks) != 1 || webhooks[0].DiscordChannelID != "channel-new" {
		t.Fatalf("cleanup did not preserve the new routing webhook: %#v", webhooks)
	}
}

func TestHealthReconnectWebhookFailsClosedOutsideProductionApply(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "health-reconnect", Name: "Health Reconnect"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "comp", "Compositing", "https://example.invalid/original", "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	form := url.Values{"action": {"reconnect_webhook"}, "webhook_id": {strconv.FormatUint(uint64(webhook.ID), 10)}}
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/health?legacy=1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	HealthHandler(db)(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("legacy webhook repair should direct the operator to Apply, status=%d body=%q", response.Code, response.Body.String())
	}
	if got := model.FindProjectWebhookByID(db, webhook.ID); got == nil || got.WebhookURL != "https://example.invalid/original" {
		t.Fatal("legacy reconnect changed the persisted webhook URL outside Apply")
	}
}

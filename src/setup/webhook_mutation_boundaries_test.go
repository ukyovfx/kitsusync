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

	// Give the cleanup request time to reach the shared lock before the
	// simulated Apply commits its first webhook while holding that lock.
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

func TestLegacyWebhookDeletionWaitsForApplyWithoutProjectRow(t *testing.T) {
	db := newIAViewDB(t)
	const productionID = "orphan-webhook-delete-lock"
	if err := model.CreateProjectWebhook(db, productionID, "orphan", "", "https://example.invalid/orphan", "channel-orphan"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, productionID)[0]
	unlockApply := lockProductionNotificationApply(productionID)
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- deleteProjectWebhookSerialized(db, webhook.ID) }()

	select {
	case err := <-deleteDone:
		unlockApply()
		t.Fatalf("orphan webhook deletion bypassed the Production lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if model.FindProjectWebhookByID(db, webhook.ID) == nil {
		unlockApply()
		t.Fatal("orphan webhook was deleted while the Production Apply lock was held")
	}
	unlockApply()
	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatalf("orphan webhook deletion failed after Apply released the lock: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("orphan webhook deletion did not resume after Apply released the lock")
	}
	if model.FindProjectWebhookByID(db, webhook.ID) != nil {
		t.Fatal("orphan webhook remained after the serialized deletion")
	}
}

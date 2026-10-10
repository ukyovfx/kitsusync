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
	"gorm.io/gorm"
)

func TestLegacyTaskTypeDeleteRejectsOverflowingWebhookID(t *testing.T) {
	form := url.Values{
		"action":       {"task_type_delete"},
		"webhook_id":   {"18446744073709551616"},
		"task_type":    {"Compositing"},
		"confirm_text": {"delete"},
	}
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	if !handleProjectRoutingMutation(response, request, "en", "", "", nil) {
		t.Fatal("legacy task type delete request was not handled")
	}
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Invalid webhook ID") {
		t.Fatalf("overflowing webhook ID was not rejected explicitly: status=%d has_invalid_id=%t", response.Code, strings.Contains(response.Body.String(), "Invalid webhook ID"))
	}
}

func TestLegacyTaskTypeDeleteEscapesRequestPathInErrorLink(t *testing.T) {
	form := url.Values{
		"action":       {"task_type_delete"},
		"webhook_id":   {"1"},
		"task_type":    {"<script>taskTypeXSS</script>"},
		"confirm_text": {"delete"},
	}
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
	request.URL.Path = `/bot/admin/projects/%"><script>pathXSS</script>`
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	if !handleProjectRoutingMutation(response, request, "en", "", "", nil) {
		t.Fatal("legacy task type delete request was not handled")
	}
	body := response.Body.String()
	if strings.Contains(body, `%"><script>pathXSS</script>`) || strings.Contains(body, "<script>taskTypeXSS</script>") {
		t.Fatal("untrusted request data was rendered as HTML")
	}
	if !strings.Contains(body, "&lt;script&gt;taskTypeXSS&lt;/script&gt;") {
		t.Fatal("task type label was not retained as escaped text")
	}
	if !strings.Contains(body, `pathXSS&lt;/script&gt;?lang=ja`) {
		t.Fatal("escaped return path was not retained in the error link")
	}
}

func TestParseWebhookIDUsesPlatformWidthAndRejectsInvalidValues(t *testing.T) {
	valid, err := parseWebhookID("42")
	if err != nil || valid != 42 {
		t.Fatalf("parseWebhookID(42) = %d, %v", valid, err)
	}
	maxUint := uint64(^uint(0))
	maxValue, err := parseWebhookID(strconv.FormatUint(maxUint, 10))
	if err != nil || uint64(maxValue) != maxUint {
		t.Fatalf("parseWebhookID(platform max) = %d, %v", maxValue, err)
	}
	for _, input := range []string{"", "0", "not-a-number", "18446744073709551616"} {
		if _, err := parseWebhookID(input); err == nil {
			t.Errorf("parseWebhookID(%q) unexpectedly succeeded", input)
		}
	}
	if strconv.IntSize != 32 && strconv.IntSize != 64 {
		t.Fatalf("unexpected platform integer width: %d", strconv.IntSize)
	}
}

func TestLegacyTaskAssignWaitsForProductionApply(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*gorm.DB, http.ResponseWriter, *http.Request)
	}{
		{name: "setup-handler", call: invokeLegacySetupHandler},
		{name: "admin-handler", call: invokeLegacyRoutingHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newSetupHandlerTestDB(t)
			project := model.Project{KitsuProjectID: "legacy-assign-lock-" + tc.name, Name: "Legacy Assign Lock"}
			if err := db.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "comp", "", "https://example.invalid/comp", "channel-1"); err != nil {
				t.Fatal(err)
			}
			webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
			unlockApply := lockProductionNotificationApply(project.KitsuProjectID)
			started, done := make(chan struct{}), make(chan struct{})
			response := httptest.NewRecorder()
			form := url.Values{"action": {"task_assign"}, "kitsu_project_id": {project.KitsuProjectID}, "task_type": {"Compositing"}, "channel_name": {"comp"}}
			request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			go func() {
				defer close(done)
				close(started)
				tc.call(db, response, request)
			}()
			<-started
			time.Sleep(100 * time.Millisecond)
			pending := model.FindProjectWebhookByID(db, webhook.ID)
			if pending == nil || pending.TaskType != "" {
				unlockApply()
				t.Fatalf("legacy task assignment changed webhook while Apply held lock: %#v", pending)
			}
			unlockApply()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("legacy task assignment did not resume after Apply released the lock")
			}
			if got := model.FindProjectWebhookByID(db, webhook.ID); got == nil || got.TaskType != "Compositing" {
				t.Fatalf("legacy assignment did not complete after lock release: %#v", got)
			}
		})
	}
}

func TestLegacyCreateChannelWaitsForProductionApply(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*gorm.DB, http.ResponseWriter, *http.Request)
	}{
		{name: "setup-handler", call: invokeLegacySetupHandler},
		{name: "admin-handler", call: invokeLegacyRoutingHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newSetupHandlerTestDB(t)
			project := model.Project{KitsuProjectID: "legacy-create-lock-" + tc.name, Name: "Legacy Create Lock", DiscordGuildID: "guild-1", DiscordCategoryID: "cat-1"}
			if err := db.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			stub := installSetupDiscordProvisioningStub(t)
			stub.channelStarted = make(chan struct{}, 1)
			unlockApply := lockProductionNotificationApply(project.KitsuProjectID)
			started, done := make(chan struct{}), make(chan struct{})
			response := httptest.NewRecorder()
			form := url.Values{"action": {"create_channel"}, "kitsu_project_id": {project.KitsuProjectID}, "channel_name_custom": {"new-channel"}}
			request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			go func() {
				defer close(done)
				close(started)
				tc.call(db, response, request)
			}()
			<-started
			earlyProvision := false
			select {
			case <-stub.channelStarted:
				earlyProvision = true
			case <-time.After(100 * time.Millisecond):
			}
			unlockApply()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("legacy channel creation did not resume after Apply released the lock")
			}
			if earlyProvision {
				t.Fatal("legacy channel was provisioned while Production Apply held the lock")
			}
			if got := model.ListProjectWebhooks(db, project.KitsuProjectID); len(got) != 1 || got[0].DiscordChannelID == "" {
				t.Fatalf("legacy channel and webhook were not persisted after lock release: %#v", got)
			}
		})
	}
}

func invokeLegacySetupHandler(db *gorm.DB, w http.ResponseWriter, r *http.Request) {
	Handler("", "guild-1", "test-token", db, func() bool { return true }, nil)(w, r)
}

func invokeLegacyRoutingHandler(db *gorm.DB, w http.ResponseWriter, r *http.Request) {
	handleProjectRoutingMutation(w, r, "en", "guild-1", "test-token", db)
}

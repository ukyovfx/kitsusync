package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"app/src/model"
	"gorm.io/gorm"
)

func TestNotificationRecoveryPageShowsOnlyUnknownJournalMetadata(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	secretURL := "https://discord.com/api/webhooks/123456789012345678/secret-token"
	if err := db.Create(&model.NotificationDeliveryEvent{
		EventID: "unknown-event-full-id", SourceEventID: "plugin-source-event-full-id", AttemptID: "0123456789abcdef0123456789abcdef", DeliverySource: "vfx_files",
		Disposition: model.DeliveryUnknown, FailureClass: "process_restart_during_delivery", ProductionID: "production-a",
		TaskID: "task-a", TaskTypeID: "task-type-a", DestinationChannelID: "678901234567890123", PayloadSHA256: strings.Repeat("a", 64),
		PayloadJSON: `{"content":"sensitive message body"}`, WebhookRowID: 1, DestinationGuildID: "456789012345678901", DestinationWebhookID: "123456789012345678",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NotificationDeliveryEvent{
		EventID: "delivered-event", AttemptID: "fedcba9876543210fedcba9876543210", DeliverySource: "vfx_files",
		Disposition: model.DeliveryDelivered, ProductionID: "production-a", PayloadJSON: `{"content":"sent"}`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProjectWebhook{ID: 1, KitsuProjectID: "production-a", WebhookURL: secretURL}).Error; err != nil {
		t.Fatal(err)
	}
	resetSessions()
	t.Cleanup(resetSessions)
	token := newSessionToken("manager@example.test", "", "manager", "/bot/admin")
	req := httptest.NewRequest(http.MethodGet, "/bot/admin/notifications/recovery", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	NotificationRecoveryHandler(db, nil)(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{"unknown-event-full-id", "plugin-source-event-full-id", "0123456789abcdef0123456789abcdef", "process_restart_during_delivery", "DeliverySource"} {
		if strings.Contains(body, want) == (want == "DeliverySource") {
			t.Fatalf("page sensitivity/visibility check for %q failed", want)
		}
	}
	for _, secret := range []string{secretURL, "secret-token", "sensitive message body", "delivered-event"} {
		if strings.Contains(body, secret) {
			t.Fatalf("recovery page exposed non-recovery data %q", secret)
		}
	}
}

func TestNotificationRecoveryPageRequiresAdministratorRole(t *testing.T) {
	db := newSetupHandlerTestDB(t)
	resetSessions()
	t.Cleanup(resetSessions)
	token := newSessionToken("artist@example.test", "", "artist", "/bot/admin")
	req := httptest.NewRequest(http.MethodGet, "/bot/admin/notifications/recovery", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	NotificationRecoveryHandler(db, nil)(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin recovery listing status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestNotificationRecoveryPostRequiresRecentAdminAndSameOriginCSRF(t *testing.T) {
	for _, tc := range []struct {
		name       string
		role       string
		recent     bool
		origin     string
		verifyErr  error
		wantStatus int
		wantCalled bool
	}{
		{name: "verified admin", role: "manager", recent: true, origin: "http://admin.example", wantStatus: http.StatusSeeOther, wantCalled: true},
		{name: "artist denied", role: "artist", recent: true, origin: "http://admin.example", wantStatus: http.StatusForbidden},
		{name: "stale authentication denied", role: "manager", recent: false, origin: "http://admin.example", wantStatus: http.StatusForbidden},
		{name: "cross origin denied", role: "manager", recent: true, origin: "http://evil.example", wantStatus: http.StatusForbidden},
		{name: "provider proof unavailable", role: "manager", recent: true, origin: "http://admin.example", verifyErr: errors.New("synthetic readback unavailable"), wantStatus: http.StatusConflict, wantCalled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newSetupHandlerTestDB(t)
			var token string
			var err error
			if tc.recent {
				token, err = newRecentlyAuthenticatedSessionTokenChecked("manager@example.test", "", tc.role)
			} else {
				token = newSessionToken("manager@example.test", "", tc.role, "/bot/admin")
			}
			if err != nil {
				t.Fatal(err)
			}
			form := url.Values{"event_id": {"event-1"}, "attempt_id": {"0123456789abcdef0123456789abcdef"}, "message_id": {"1234567890123456789"}, "thread_id": {"987654321098765432"}}
			called := false
			verify := func(_ *gorm.DB, eventID, attemptID, messageID, threadID, actor string, _ time.Time) error {
				called = true
				if eventID != "event-1" || attemptID != "0123456789abcdef0123456789abcdef" || messageID != "1234567890123456789" || threadID != "987654321098765432" || actor != "manager@example.test" {
					t.Errorf("unexpected verifier input: %q %q %q %q %q", eventID, attemptID, messageID, threadID, actor)
				}
				return tc.verifyErr
			}
			req := httptest.NewRequest(http.MethodPost, "http://admin.example/bot/admin/notifications/recovery", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", tc.origin)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
			response := httptest.NewRecorder()
			CSRFProtection(RequireSession(NotificationRecoveryHandler(db, verify))).ServeHTTP(response, req)
			if response.Code != tc.wantStatus || called != tc.wantCalled {
				t.Fatalf("status=%d verifier_called=%v body=%s", response.Code, called, response.Body.String())
			}
		})
	}
}

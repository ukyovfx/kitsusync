package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/model"
)

func TestDeleteFinalRateLimitedAfterProbeBudget(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "rate-limit-delete", Name: "Rate Limit Delete"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	const peerAddr = "203.0.113.10:12345"
	setupProbeLimiter.Lock()
	delete(setupProbeLimiter.started, "203.0.113.10")
	setupProbeLimiter.Unlock()
	t.Cleanup(func() {
		setupProbeLimiter.Lock()
		delete(setupProbeLimiter.started, "203.0.113.10")
		setupProbeLimiter.Unlock()
	})
	form := url.Values{
		"action":           {"delete_final"},
		"kitsu_project_id": {project.KitsuProjectID},
		"project_name":     {project.Name},
		"admin_email":      {"admin@example.test"},
		"admin_password":   {"wrong-password"},
	}

	// Exhaust the 6-attempt per-minute budget on a bad-endpoint request (fails auth without needing a live Kitsu server).
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = peerAddr
		rr := httptest.NewRecorder()
		Handler("http://127.0.0.1:1", "", "", db, func() bool { return false }, nil)(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d hit rate limit prematurely", i+1)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = peerAddr
	rr := httptest.NewRecorder()
	Handler("http://127.0.0.1:1", "", "", db, func() bool { return false }, nil)(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("7th delete_final attempt status = %d, want %d", rr.Code, http.StatusTooManyRequests)
	}
}

func TestLegacyBotSaveRateLimitedAfterProbeBudget(t *testing.T) {
	db := newIAViewDB(t)
	t.Setenv("KITSU_HOSTNAME", "")
	model.SetSetting(db, "kitsu.hostname", "http://127.0.0.1:1")

	tok, err := newRecentlyAuthenticatedSessionTokenChecked("admin@example.test", "kitsu-token", "admin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = destroySession(tok) })

	const peerAddr = "203.0.113.11:12345"
	setupProbeLimiter.Lock()
	delete(setupProbeLimiter.started, "203.0.113.11")
	setupProbeLimiter.Unlock()
	t.Cleanup(func() {
		setupProbeLimiter.Lock()
		delete(setupProbeLimiter.started, "203.0.113.11")
		setupProbeLimiter.Unlock()
	})
	form := url.Values{
		"kitsu_hostname":         {"http://127.0.0.1:1"},
		"kitsu_runtime_email":    {"bot@example.test"},
		"kitsu_runtime_password": {"wrong-password"},
	}

	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?legacy=1&lang=en", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = peerAddr
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
		rr := httptest.NewRecorder()
		BotHandler(db, nil)(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d hit rate limit prematurely", i+1)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?legacy=1&lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = peerAddr
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	rr := httptest.NewRecorder()
	BotHandler(db, nil)(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("7th legacy save attempt status = %d, want %d", rr.Code, http.StatusTooManyRequests)
	}
}

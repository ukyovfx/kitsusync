package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeManagerSetupRequiredDoesNotPoll(t *testing.T) {
	runtime := newRuntimeManager()
	called := false
	if runtime.authenticate("", "", "") {
		t.Fatal("missing credentials must not authenticate")
	}
	if runtime.runWhenReady(func() { called = true }) || called {
		t.Fatal("polling must remain disabled in setup-required mode")
	}
	snapshot := runtime.snapshot()
	if snapshot.Mode != runtimeSetupRequired || snapshot.Notifications != "paused" {
		t.Fatalf("unexpected setup-required snapshot: %+v", snapshot)
	}
}

func TestRuntimeManagerTransitionsAndKeepsPreviousTokenOnOutage(t *testing.T) {
	runtime := newRuntimeManager()
	responses := []string{"runtime-jwt", ""}
	runtime.auth = func(_, _, _ string) string {
		response := responses[0]
		responses = responses[1:]
		return response
	}
	if !runtime.authenticate("http://kitsu.local", "runtime@example.com", "password") {
		t.Fatal("valid runtime credentials should configure runtime")
	}
	if runtime.authenticate("http://kitsu.local", "runtime@example.com", "password") {
		t.Fatal("refresh outage should report authentication failure")
	}
	snapshot := runtime.snapshot()
	if snapshot.Mode != runtimeDegraded || !runtime.ready() {
		t.Fatalf("previous usable token should remain available in degraded mode: %+v", snapshot)
	}
}

func TestHealthHandlerReportsSetupRequiredWithoutSecrets(t *testing.T) {
	runtime := newRuntimeManager()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	healthHandler(runtime)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("health status = %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"mode":"setup_required"`) || !strings.Contains(body, `"notifications":"paused"`) {
		t.Fatalf("unexpected health body: %s", body)
	}
}

func TestOverallNotificationReadinessUsesDiscordValidation(t *testing.T) {
	if got := overallNotificationReadiness(true, true, true, true); got != "ready" {
		t.Fatalf("fully validated runtime = %q, want ready", got)
	}
	if got := overallNotificationReadiness(true, true, false, true); got != "ready_pending_discord_validation" {
		t.Fatalf("unvalidated Discord runtime = %q, want pending", got)
	}
	if got := overallNotificationReadiness(true, true, true, false); got != "blocked" {
		t.Fatalf("unconfigured routing = %q, want blocked", got)
	}
}

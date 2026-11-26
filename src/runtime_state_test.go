package main

import (
	"app/src/setup"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeManagerSetupRequiredDoesNotPoll(t *testing.T) {
	runtime := newRuntimeManager()
	called := false
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
	runtime.mu.Lock()
	runtime.mode = runtimeConfigured
	runtime.canPoll = true
	runtime.hadToken = true
	runtime.mu.Unlock()
	if runtime.authenticateToken(setup.KitsuURLModel{RuntimeBaseURL: "http://kitsu.invalid", ResolvedAPIBaseURL: "http://kitsu.invalid/api"}, "runtime-token") {
		t.Fatal("refresh outage should report authentication failure")
	}
	snapshot := runtime.snapshot()
	if snapshot.Mode != runtimeDegraded || !runtime.ready() {
		t.Fatalf("previous usable token should remain available in degraded mode: %+v", snapshot)
	}
}

func TestReadinessHandlerRejectsSetupAndDegradedModes(t *testing.T) {
	for _, mode := range []runtimeMode{runtimeSetupRequired, runtimeDegraded} {
		t.Run(string(mode), func(t *testing.T) {
			runtime := newRuntimeManager()
			runtime.mu.Lock()
			runtime.mode = mode
			runtime.canPoll = mode == runtimeDegraded
			runtime.mu.Unlock()
			rr := httptest.NewRecorder()
			readinessHandler(runtime)(rr, httptest.NewRequest(http.MethodGet, "/ready", nil))
			if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), `"status":"`+string(mode)+`"`) {
				t.Fatalf("%s readiness = %d %s", mode, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestReadinessHandlerAcceptsOnlyConfiguredAuthenticatedRuntime(t *testing.T) {
	runtime := newRuntimeManager()
	runtime.mu.Lock()
	runtime.mode = runtimeConfigured
	runtime.canPoll = true
	runtime.hadToken = true
	runtime.mu.Unlock()
	rr := httptest.NewRecorder()
	readinessHandler(runtime)(rr, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"status":"ready"`) {
		t.Fatalf("configured readiness = %d %s", rr.Code, rr.Body.String())
	}
	for _, forbidden := range []string{"KitsuJWTToken", "password", "Bearer"} {
		if strings.Contains(rr.Body.String(), forbidden) {
			t.Fatalf("readiness exposed forbidden value %q: %s", forbidden, rr.Body.String())
		}
	}
}

func TestHealthHandlerReportsBareStatusWithoutSecretsOrBuildInfo(t *testing.T) {
	runtime := newRuntimeManager()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	healthHandler(runtime)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("health status = %d", rr.Code)
	}
	body := strings.TrimSpace(rr.Body.String())
	if body != `{"status":"ok"}` {
		t.Fatalf("unexpected health body: %s", body)
	}
}

func TestHealthHandlerHealthyWhenExternalDependenciesUnavailable(t *testing.T) {
	runtime := newRuntimeManager()
	for _, dependency := range []string{"discord", "kitsu"} {
		t.Run(dependency, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rr := httptest.NewRecorder()
			// External dependency probes are intentionally absent from process health.
			healthHandler(runtime)(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("health status with unavailable %s = %d, want 200", dependency, rr.Code)
			}
		})
	}
}

func TestHealthHandlerReadyConfiguredRuntimeIsHealthy(t *testing.T) {
	runtime := newRuntimeManager()
	runtime.mu.Lock()
	runtime.mode = runtimeConfigured
	runtime.canPoll = true
	runtime.mu.Unlock()
	rr := httptest.NewRecorder()
	healthHandler(runtime)(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("configured runtime health = %d %s", rr.Code, rr.Body.String())
	}
}

func TestHealthHandlerFailsOnLocalRuntimeFailure(t *testing.T) {
	runtime := newRuntimeManager()
	rr := httptest.NewRecorder()
	healthHandler(runtime, func(context.Context) error { return errors.New("local database unavailable") })(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status = %d, want 503", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"status":"unhealthy"`) {
		t.Fatalf("unexpected unhealthy response: %s", rr.Body.String())
	}
}

func TestHealthHandlerDoesNotWaitForSlowExternalDependency(t *testing.T) {
	runtime := newRuntimeManager()
	called := false
	block := make(chan struct{})
	slowExternalProbe := func() {
		called = true
		<-block
	}
	_ = slowExternalProbe
	rr := httptest.NewRecorder()
	healthHandler(runtime)(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK || called {
		t.Fatalf("health must not invoke external dependency probes: %d %s", rr.Code, rr.Body.String())
	}
}

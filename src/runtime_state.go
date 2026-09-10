package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"

	"app/src/setup"
	"app/src/utils/request"
)

type runtimeMode string

const (
	runtimeSetupRequired runtimeMode = "setup_required"
	runtimeConfigured    runtimeMode = "configured"
	runtimeDegraded      runtimeMode = "degraded"
)

type runtimeSnapshot struct {
	Mode                 runtimeMode `json:"mode"`
	Kitsu                string      `json:"kitsu"`
	Notifications        string      `json:"notifications"`
	RuntimeAuthenticated bool        `json:"runtime_authenticated"`
}

type runtimeManager struct {
	authMu   sync.Mutex
	mu       sync.RWMutex
	mode     runtimeMode
	canPoll  bool
	hadToken bool
}

func newRuntimeManager() *runtimeManager {
	return &runtimeManager{mode: runtimeSetupRequired}
}

func (m *runtimeManager) authenticateToken(connection setup.KitsuURLModel, token string) bool {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	hostname := strings.TrimSpace(connection.RuntimeBaseURL)
	token = strings.TrimSpace(token)
	if hostname == "" || token == "" {
		m.mu.Lock()
		m.mode = runtimeSetupRequired
		m.canPoll = false
		m.mu.Unlock()
		return false
	}
	if err := setup.VerifyKitsuToken(context.Background(), connection, token); err != nil {
		m.mu.Lock()
		if m.hadToken {
			m.mode = runtimeDegraded
			m.canPoll = true
		} else {
			m.mode = runtimeSetupRequired
			m.canPoll = false
		}
		m.mu.Unlock()
		return false
	}
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: connection.ResolvedAPIBaseURL, PinnedIPs: connection.VerifiedIPs}); err != nil {
		m.mu.Lock()
		m.mode = runtimeSetupRequired
		m.canPoll = false
		m.mu.Unlock()
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	os.Setenv("KITSU_HOSTNAME", hostname)
	os.Setenv("KitsuJWTToken", token)
	m.mode = runtimeConfigured
	m.canPoll = true
	m.hadToken = true
	return true
}

func (m *runtimeManager) ready() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.canPoll
}

func (m *runtimeManager) snapshot() runtimeSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot := runtimeSnapshot{Mode: m.mode, Kitsu: "disconnected", Notifications: "paused", RuntimeAuthenticated: m.canPoll}
	if m.mode == runtimeConfigured {
		snapshot.Kitsu = "connected"
		snapshot.Notifications = "ready"
	}
	if m.mode == runtimeDegraded {
		snapshot.Kitsu = "degraded"
		snapshot.Notifications = "degraded"
	}
	return snapshot
}

func (m *runtimeManager) runWhenReady(fn func()) bool {
	if !m.ready() {
		return false
	}
	fn()
	return true
}

// healthHandler reports process and local-runtime health only. Dependency
// readiness belongs to the setup/admin status surfaces so a slow or
// unavailable Discord/Kitsu API cannot flap the container healthcheck.
func healthHandler(runtime *runtimeManager, localChecks ...func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		healthy := runtime != nil
		for _, check := range localChecks {
			if check != nil {
				if err := check(r.Context()); err != nil {
					healthy = false
					break
				}
			}
		}
		status := http.StatusOK
		statusText := "ok"
		if !healthy {
			status = http.StatusServiceUnavailable
			statusText = "unhealthy"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		response := struct {
			Status  string          `json:"status"`
			Build   buildInfo       `json:"build"`
			Runtime runtimeSnapshot `json:"runtime"`
		}{Status: statusText, Build: currentBuildInfo()}
		if runtime != nil {
			response.Runtime = runtime.snapshot()
		}
		_ = json.NewEncoder(w).Encode(response)
	}
}

// readinessHandler reports whether the configured runtime may perform its
// normal work. Unlike /health, setup-required and degraded modes are not ready.
// The response contains only the non-secret runtime snapshot and build identity.
func readinessHandler(runtime *runtimeManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusServiceUnavailable
		statusText := string(runtimeSetupRequired)
		var snapshot runtimeSnapshot
		if runtime != nil {
			snapshot = runtime.snapshot()
			statusText = string(snapshot.Mode)
			if snapshot.Mode == runtimeConfigured && snapshot.RuntimeAuthenticated {
				status = http.StatusOK
				statusText = "ready"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(struct {
			Status  string          `json:"status"`
			Build   buildInfo       `json:"build"`
			Runtime runtimeSnapshot `json:"runtime"`
		}{Status: statusText, Build: currentBuildInfo(), Runtime: snapshot})
	}
}

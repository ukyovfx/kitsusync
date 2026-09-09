package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"

	"app/src/setup"
	"app/src/utils/basicauth"
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
	auth     func(url, email, password string) string
}

func newRuntimeManager() *runtimeManager {
	return &runtimeManager{mode: runtimeSetupRequired, auth: basicauth.AuthForJWTToken}
}

func (m *runtimeManager) authenticate(hostname, email, password string) bool {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	hostname = strings.TrimSpace(hostname)
	email = strings.TrimSpace(email)
	if hostname == "" || email == "" || password == "" {
		m.mu.Lock()
		m.mode = runtimeSetupRequired
		m.canPoll = false
		m.mu.Unlock()
		return false
	}
	if !strings.HasSuffix(hostname, "/") {
		hostname += "/"
	}
	token := m.auth(hostname+"api/auth/login", email, password)
	m.mu.Lock()
	defer m.mu.Unlock()
	if token == "" {
		if m.hadToken {
			m.mode = runtimeDegraded
			m.canPoll = true
		} else {
			m.mode = runtimeSetupRequired
			m.canPoll = false
		}
		return false
	}
	os.Setenv("KITSU_HOSTNAME", hostname)
	os.Setenv("KitsuJWTToken", token)
	m.mode = runtimeConfigured
	m.canPoll = true
	m.hadToken = true
	return true
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

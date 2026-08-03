package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"

	"app/src/utils/basicauth"
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

func (m *runtimeManager) authenticateToken(hostname, token string) bool {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	hostname = strings.TrimSpace(hostname)
	token = strings.TrimSpace(token)
	if hostname == "" || token == "" {
		m.mu.Lock()
		m.mode = runtimeSetupRequired
		m.canPoll = false
		m.mu.Unlock()
		return false
	}
	if !strings.HasSuffix(hostname, "/") {
		hostname += "/"
	}
	if !basicauth.ValidateJWTToken(hostname+"api/auth/authenticated", token) {
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

func healthHandler(runtime *runtimeManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := struct {
			Status  string          `json:"status"`
			Runtime runtimeSnapshot `json:"runtime"`
		}{Status: "ok", Runtime: runtime.snapshot()}
		_ = json.NewEncoder(w).Encode(response)
	}
}

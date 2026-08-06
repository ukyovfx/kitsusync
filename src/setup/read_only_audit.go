package setup

import (
	"net/http"
	"os"
	"strings"
	"sync"
)

const readOnlyAuditEnv = "KITSUSYNC_READ_ONLY_AUDIT"

var readOnlyAuditEnvMu sync.Mutex

func ReadOnlyAuditModeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(readOnlyAuditEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func readOnlyAuditSessionHasRole(r *http.Request) bool {
	_, token, role, ok := CurrentSessionKitsuAuth(r)
	if !ok || strings.TrimSpace(token) == "" {
		return false
	}
	role = strings.ToLower(strings.TrimSpace(role))
	return role == "admin" || role == "manager"
}

// ReadOnlyAuditRoute permits authenticated GET/HEAD inspection before the
// background runtime is ready, but only when explicitly enabled locally.
func ReadOnlyAuditRoute(ready func() bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready == nil || ready() {
			next(w, r)
			return
		}
		if !ReadOnlyAuditModeEnabled() || !readOnlyAuditSessionHasRole(r) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			RuntimeReadyRequired(ready, next)(w, r)
			return
		}
		withReadOnlyAuditToken(r, next, w)
	}
}

func ReadOnlyAuditPreviewRoute(ready func() bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready == nil || ready() {
			next(w, r)
			return
		}
		if !ReadOnlyAuditModeEnabled() || !readOnlyAuditSessionHasRole(r) || r.Method != http.MethodPost {
			RuntimeReadyRequired(ready, next)(w, r)
			return
		}
		withReadOnlyAuditToken(r, next, w)
	}
}

func withReadOnlyAuditToken(r *http.Request, next http.HandlerFunc, w http.ResponseWriter) {
	_, token, _, _ := CurrentSessionKitsuAuth(r)
	readOnlyAuditEnvMu.Lock()
	defer readOnlyAuditEnvMu.Unlock()
	previous, wasSet := os.LookupEnv("KitsuJWTToken")
	_ = os.Setenv("KitsuJWTToken", strings.TrimSpace(token))
	defer func() {
		if wasSet {
			_ = os.Setenv("KitsuJWTToken", previous)
		} else {
			_ = os.Unsetenv("KitsuJWTToken")
		}
	}()
	next(w, r)
}

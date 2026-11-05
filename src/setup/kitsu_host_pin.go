package setup

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
)

// appManagedKitsuHostname remembers the last KITSU_HOSTNAME value that the
// application itself exported (a conf.toml fallback or a UI save). Only a value
// that differs from it counts as operator-managed, so an app-written value does
// not lock the UI after the first save.
var appManagedKitsuHostname struct {
	mu    sync.Mutex
	value string
}

// SetAppManagedKitsuHostname exports host as KITSU_HOSTNAME for the current
// process without marking it as operator-pinned.
func SetAppManagedKitsuHostname(host string) {
	appManagedKitsuHostname.mu.Lock()
	defer appManagedKitsuHostname.mu.Unlock()
	appManagedKitsuHostname.value = host
	os.Setenv("KITSU_HOSTNAME", host)
}

// ExportRuntimeKitsuHostname is used by runtime authentication to export the
// endpoint it connected to. Without an operator pin the value is app-managed;
// with a pin the export keeps its previous behavior but is not recorded, so it
// never turns an app value into a pin or hides a genuine one.
func ExportRuntimeKitsuHostname(host string) {
	if _, pinned := operatorPinnedKitsuHost(); pinned {
		os.Setenv("KITSU_HOSTNAME", host)
		return
	}
	SetAppManagedKitsuHostname(host)
}

// operatorPinnedKitsuHost returns the normalized endpoint when the operator set
// a valid, non-placeholder KITSU_HOSTNAME. An invalid value is not a pin.
func operatorPinnedKitsuHost() (string, bool) {
	configured := osKitsuHostname()
	appManagedKitsuHostname.mu.Lock()
	if configured == "" {
		appManagedKitsuHostname.value = ""
	}
	// Compare normalized forms: runtime authentication exports the same endpoint
	// with and without a trailing slash.
	appManaged := appManagedKitsuHostname.value != "" && sameKitsuEndpoint(normalizeKitsuHostname(configured), normalizeKitsuHostname(appManagedKitsuHostname.value))
	appManagedKitsuHostname.mu.Unlock()
	if configured == "" || appManaged {
		return "", false
	}
	normalized, err := validateKitsuEndpoint(configured)
	if err != nil || isPlaceholderKitsuEndpoint(normalized) {
		return "", false
	}
	return normalized, true
}

func sameKitsuEndpoint(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), "/"), strings.TrimRight(strings.TrimSpace(b), "/"))
}

// kitsuEndpointChangeBlocked reports whether saving requestedHost would
// override an operator-pinned endpoint. Re-submitting the pinned host itself is
// not a change.
func kitsuEndpointChangeBlocked(requestedHost string) bool {
	pinned, ok := operatorPinnedKitsuHost()
	if !ok {
		return false
	}
	if requested := normalizeKitsuHostname(requestedHost); requested != "" && sameKitsuEndpoint(requested, pinned) {
		return false
	}
	return true
}

// kitsuAPIOverrideBlocked reports whether an explicit API base URL would point
// the connection somewhere other than the operator-pinned endpoint.
func kitsuAPIOverrideBlocked(apiOverride string) bool {
	if strings.TrimSpace(apiOverride) == "" {
		return false
	}
	pinned, ok := operatorPinnedKitsuHost()
	if !ok {
		return false
	}
	target, err := NormalizeKitsuURL(apiOverride, APISourceExplicit)
	if err != nil {
		return true
	}
	return !sameKitsuEndpoint(strings.TrimRight(target.RuntimeBaseURL, "/")+"/", pinned)
}

// logSafe strips control characters so a request-derived value cannot forge log lines.
func logSafe(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
}

// rejectPinnedKitsuEndpointChange writes a bilingual explanation (never a blank
// page or a 500) and logs the rejection without any endpoint value.
func rejectPinnedKitsuEndpointChange(w http.ResponseWriter, lang, source string) {
	slog.Warn("Kitsu endpoint change rejected", "source", source, "reason", "operator_pinned_kitsu_hostname")
	w.WriteHeader(http.StatusConflict)
	fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang,
		"Kitsuの接続先は運用者が環境変数 KITSU_HOSTNAME で固定しているため、画面から変更できません。変更するには環境変数を更新して再起動してください。",
		"The Kitsu endpoint is pinned by the operator through the KITSU_HOSTNAME environment variable and cannot be changed from this screen. Update the environment variable and restart to change it.")))
}

// logKitsuEndpointChange records an applied endpoint change with credential-free
// display values only.
func logKitsuEndpointChange(source, previous, current string) {
	if sameKitsuEndpoint(previous, current) {
		return
	}
	slog.Info("Kitsu endpoint changed", "source", source, "previous", logSafe(safeKitsuHostDisplay(previous)), "current", logSafe(safeKitsuHostDisplay(current)))
}

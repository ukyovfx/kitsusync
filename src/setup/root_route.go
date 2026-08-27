package setup

import (
	"net/http"
	"net/url"
)

// BotRootHandler keeps /bot/ as a useful entry point for both new and
// configured installations. Authentication remains enforced by the existing
// login flow; the runtime readiness callback only chooses the authenticated
// destination.
func BotRootHandler(runtimeReady func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if _, ok := currentSessionData(r); !ok {
			login := "/bot/login?next=" + url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, appendLang(login, currentLang(r)), http.StatusSeeOther)
			return
		}

		target := "/bot/setup"
		if runtimeReady != nil && runtimeReady() {
			target = "/bot/admin"
		}
		http.Redirect(w, r, withLang(target, r), http.StatusSeeOther)
	}
}

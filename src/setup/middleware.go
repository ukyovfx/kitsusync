package setup

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type sessionData struct {
	Expiry       time.Time
	Email        string
	KitsuToken   string
	Role         string
	BotEditUntil time.Time
}

var (
	sessionMu  sync.Mutex
	sessions   = map[string]sessionData{}
	sessionTTL = 15 * time.Minute
)

const sessionCookieName = "kitsu_admin_session"

func isHTTPSRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	// For v0.1.0 self-hosted deployments, trust X-Forwarded-Proto=https by default.
	proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(proto, "https")
}

func sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isHTTPSRequest(r),
		// Lax is intentionally used to preserve login redirect flows.
		SameSite: http.SameSiteLaxMode,
	}
}

func newSessionToken(email, kitsuToken, role, next string) string {
	buffer := make([]byte, 16)
	_, _ = rand.Read(buffer)
	token := hex.EncodeToString(buffer)
	session := sessionData{
		Expiry:     time.Now().Add(sessionTTL),
		Email:      email,
		KitsuToken: kitsuToken,
		Role:       role,
	}
	if strings.Contains(next, "/bot/admin/bot") && strings.Contains(next, "edit=1") {
		session.BotEditUntil = time.Now().Add(10 * time.Minute)
	}
	sessionMu.Lock()
	sessions[token] = session
	sessionMu.Unlock()
	return token
}

func validSession(token string) bool {
	if token == "" {
		return false
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()
	session, ok := sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(session.Expiry) {
		delete(sessions, token)
		return false
	}
	return true
}

func destroySession(token string) {
	sessionMu.Lock()
	delete(sessions, token)
	sessionMu.Unlock()
}

func botEditAllowed(r *http.Request) bool {
	if r == nil {
		return false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()
	session, ok := sessions[cookie.Value]
	if !ok {
		return false
	}
	return time.Now().Before(session.BotEditUntil)
}

func RequireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || !validSession(cookie.Value) {
			http.Redirect(w, r, appendLang("/bot/login?next="+url.QueryEscape(r.URL.RequestURI()), currentLang(r)), http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func currentSessionData(r *http.Request) (sessionData, bool) {
	if r == nil {
		return sessionData{}, false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return sessionData{}, false
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()
	session, ok := sessions[cookie.Value]
	if !ok || time.Now().After(session.Expiry) {
		return sessionData{}, false
	}
	return session, true
}

func CurrentSessionKitsuAuth(r *http.Request) (email, token, role string, ok bool) {
	session, ok := currentSessionData(r)
	if !ok {
		return "", "", "", false
	}
	return session.Email, session.KitsuToken, session.Role, true
}

func LoginHandler(kitsuHostname string, onAdminAuthenticated func(hostname string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		configuredHostname := normalizeKitsuHostname(kitsuHostname)

		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			hostname := configuredHostname
			if hostname == "" {
				hostname = normalizeKitsuHostname(r.FormValue("hostname"))
			}
			email := strings.TrimSpace(r.FormValue("email"))
			password := r.FormValue("password")
			next := strings.TrimSpace(r.FormValue("next"))
			validNext := strings.HasPrefix(next, "/bot/")
			if next == "" || !validNext {
				if configuredHostname == "" {
					next = withLang("/bot/setup", r)
				} else {
					next = withLang("/bot/admin", r)
				}
			}

			if hostname == "" || (!strings.HasPrefix(hostname, "http://") && !strings.HasPrefix(hostname, "https://")) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, loginPageHTML(lang, t(lang, "Kitsu URL は http:// または https:// で入力してください。", "Enter a Kitsu URL beginning with http:// or https://."), next, configuredHostname == "", r))
				return
			}
			loginURL := strings.TrimRight(hostname, "/") + "/api/auth/login"
			role, kitsuToken, ok := kitsuLoginCheck(loginURL, email, password)
			if !ok || (role != "admin" && role != "manager") {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, loginPageHTML(lang, t(lang, "ログインに失敗しました。Kitsu のメール、パスワード、manager/admin 権限を確認してください。", "Login failed. Check the Kitsu email, password, and manager/admin permissions."), next, configuredHostname == "", r))
				return
			}
			if onAdminAuthenticated != nil {
				onAdminAuthenticated(hostname)
			}

			token := newSessionToken(email, kitsuToken, role, next)
			http.SetCookie(w, sessionCookie(r, token, int(sessionTTL.Seconds())))
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}

		next := r.URL.Query().Get("next")
		fmt.Fprint(w, loginPageHTML(lang, "", next, configuredHostname == "", r))
	}
}

func LogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			destroySession(cookie.Value)
		}
		http.SetCookie(w, sessionCookie(r, "", -1))
		http.Redirect(w, r, withLang("/bot/login", r), http.StatusSeeOther)
	}
}

func kitsuLoginCheck(loginURL, email, password string) (role, kitsuToken string, ok bool) {
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req, err := http.NewRequest(http.MethodPost, loginURL, bytes.NewReader(body))
	if err != nil {
		return "", "", false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		return "", "", false
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		User        struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", false
	}
	return result.User.Role, result.AccessToken, result.User.Role != "" && result.AccessToken != ""
}

func loginPageHTML(lang, errMsg, next string, showHostname bool, r *http.Request) string {
	errHTML := ""
	if errMsg != "" {
		errHTML = `<div class="toast glass" role="alert" aria-live="assertive" style="background:rgba(255,106,80,.12);border-color:rgba(255,106,80,.24);color:#ffd7cf">` + html.EscapeString(errMsg) + `</div>`
	}
	nextInput := ""
	if next != "" {
		nextInput = `<input type="hidden" name="next" value="` + html.EscapeString(next) + `">`
	}
	hostnameInput := ""
	if showHostname {
		hostnameInput = `<label for="login-hostname">` + t(lang, "Kitsu URL", "Kitsu URL") + `</label><input id="login-hostname" type="url" name="hostname" placeholder="http://127.0.0.1:8080" required>`
	}

	body := fmt.Sprintf(`
<div class="page-card glass" style="width:100%%;max-width:520px;margin:6vh auto 0">
  <div class="page-heading">
    <div>
      <div class="eyebrow">%s</div>
      <h1>%s</h1>
      <p>%s</p>
    </div>
  </div>
  %s
  <form method="POST" class="section-stack">
    %s
    <div class="section-card glass">
      %s
      <label for="login-email">%s</label>
      <input id="login-email" type="email" name="email" autocomplete="email" required autofocus>
      <label for="login-password">%s</label>
      <input id="login-password" type="password" name="password" autocomplete="current-password" required>
      <div class="button-row">
        <button type="submit" class="btn">%s</button>
      </div>
    </div>
  </form>
</div>`,
		t(lang, "管理画面ログイン", "Admin Access"),
		"KitsuSync",
		t(lang, "Kitsu の manager / admin アカウントでログインしてください。", "Sign in with a Kitsu manager or admin account."),
		errHTML,
		nextInput,
		hostnameInput,
		t(lang, "メール", "Email"),
		t(lang, "パスワード", "Password"),
		t(lang, "ログイン", "Login"),
	)

	return appShell("KitsuSync", "", lang, r, "", body)
}

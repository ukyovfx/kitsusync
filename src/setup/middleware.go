package setup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"app/src/model"
	"github.com/gookit/slog"
	"gorm.io/gorm"
)

type sessionData struct {
	Expiry       time.Time
	Email        string
	KitsuToken   string
	Role         string
	BotEditUntil time.Time
	Wizard       wizardState
}

type wizardState struct {
	ProductionID    string
	GuildID         string
	PlanFingerprint string
	Confirmed       bool
}

var (
	sessionMu      sync.Mutex
	sessions       = map[string]sessionData{}
	sessionStoreDB *gorm.DB
	sessionTTL     = 15 * time.Minute
)

const sessionCookieName = "kitsu_admin_session"

// ConfigureSessionStore enables SQLite-backed session validation. A nil DB
// retains the in-memory mode used by isolated unit tests.
func ConfigureSessionStore(db *gorm.DB) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	sessionStoreDB = db
}

func sessionTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

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
	token, _ := newSessionTokenChecked(email, kitsuToken, role, next)
	return token
}

func newSessionTokenChecked(email, kitsuToken, role, next string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
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
	db := sessionStoreDB
	sessionMu.Unlock()
	if db != nil {
		if err := db.Create(&model.AdminSession{
			TokenHash:    sessionTokenHash(token),
			Email:        session.Email,
			Role:         session.Role,
			Expiry:       session.Expiry,
			BotEditUntil: session.BotEditUntil,
		}).Error; err != nil {
			sessionMu.Lock()
			delete(sessions, token)
			sessionMu.Unlock()
			return "", fmt.Errorf("persist admin session: %w", err)
		}
	}
	return token, nil
}

// classifySessionPersistenceError intentionally returns only a stable,
// non-sensitive category suitable for logs and diagnostics. SQLite errors can
// include SQL text, paths, or values, so callers must not log the raw error.
func classifySessionPersistenceError(err error) string {
	return classifySQLitePersistenceError(err)
}

func classifySQLitePersistenceError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "no such table"):
		return "schema_table_missing"
	case strings.Contains(message, "no such column"), strings.Contains(message, "has no column"), strings.Contains(message, "database schema"):
		return "schema_incompatible"
	case strings.Contains(message, "readonly"):
		return "database_readonly"
	case strings.Contains(message, "database is locked"), strings.Contains(message, "database is busy"), strings.Contains(message, "sqlite_busy"):
		return "database_busy"
	case strings.Contains(message, "constraint failed"), strings.Contains(message, "unique constraint"):
		return "constraint_failed"
	default:
		return "persistence_failed"
	}
}

func validSession(token string) bool {
	if token == "" {
		return false
	}
	sessionMu.Lock()
	session, ok := sessions[token]
	db := sessionStoreDB
	sessionMu.Unlock()
	if ok {
		if time.Now().After(session.Expiry) {
			destroySession(token)
			return false
		}
		return true
	}
	if db == nil {
		return false
	}
	var persisted model.AdminSession
	if err := db.Where("token_hash = ?", sessionTokenHash(token)).First(&persisted).Error; err != nil {
		return false
	}
	if time.Now().After(persisted.Expiry) {
		_ = db.Delete(&persisted).Error
		return false
	}
	sessionMu.Lock()
	sessions[token] = sessionData{Expiry: persisted.Expiry, Email: persisted.Email, Role: persisted.Role, BotEditUntil: persisted.BotEditUntil}
	sessionMu.Unlock()
	return true
}

func destroySession(token string) {
	sessionMu.Lock()
	delete(sessions, token)
	db := sessionStoreDB
	sessionMu.Unlock()
	if db != nil {
		_ = db.Where("token_hash = ?", sessionTokenHash(token)).Delete(&model.AdminSession{}).Error
	}
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
	session, ok := sessions[cookie.Value]
	db := sessionStoreDB
	sessionMu.Unlock()
	if !ok && db != nil {
		var persisted model.AdminSession
		if db.Where("token_hash = ?", sessionTokenHash(cookie.Value)).First(&persisted).Error == nil {
			session = sessionData{Expiry: persisted.Expiry, Email: persisted.Email, Role: persisted.Role, BotEditUntil: persisted.BotEditUntil}
			ok = true
		}
	}
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

// CSRFProtection rejects cross-site browser mutations for authenticated
// administration and setup routes. SameSite=Lax remains an additional
// defense, while Origin/Fetch Metadata checks protect cookie-authenticated
// POST forms without adding a token to every existing form.
func CSRFProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil || !csrfProtectedPath(r.URL.Path) || !csrfUnsafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := currentSessionData(r); !ok {
			// Preserve the existing unauthenticated behavior; RequireSession
			// remains responsible for redirecting or rejecting the request.
			next.ServeHTTP(w, r)
			return
		}
		if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
			http.Error(w, "cross-site request rejected", http.StatusForbidden)
			return
		}
		effective := requestOrigin(r)
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			if !sameOrigin(origin, effective) {
				http.Error(w, "origin mismatch", http.StatusForbidden)
				return
			}
		} else if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
			if !sameOriginReferer(referer, effective) {
				http.Error(w, "referer mismatch", http.StatusForbidden)
				return
			}
		} else {
			http.Error(w, "origin or referer required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func csrfUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func csrfProtectedPath(path string) bool {
	return path == "/setup" || path == "/bot/setup" ||
		path == "/admin" || path == "/bot/admin" ||
		strings.HasPrefix(path, "/admin/") || strings.HasPrefix(path, "/bot/admin/") ||
		strings.HasPrefix(path, "/api/setup/") || strings.HasPrefix(path, "/bot/api/setup/") ||
		path == "/logout" || path == "/bot/logout"
}

func requestOrigin(r *http.Request) string {
	if r == nil || strings.TrimSpace(r.Host) == "" {
		return ""
	}
	scheme := "http"
	if isHTTPSRequest(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func sameOrigin(candidate, expected string) bool {
	candidateURL, err := url.Parse(candidate)
	if err != nil || candidateURL.Scheme == "" || candidateURL.Host == "" ||
		candidateURL.User != nil || candidateURL.Path != "" ||
		candidateURL.RawQuery != "" || candidateURL.Fragment != "" {
		return false
	}
	return sameOriginURL(candidateURL, expected)
}

func sameOriginReferer(candidate, expected string) bool {
	candidateURL, err := url.Parse(candidate)
	if err != nil || candidateURL.Scheme == "" || candidateURL.Host == "" || candidateURL.User != nil || candidateURL.RawQuery != "" || candidateURL.Fragment != "" {
		return false
	}
	return sameOriginURL(candidateURL, expected)
}

func sameOriginURL(candidateURL *url.URL, expected string) bool {
	expectedURL, err := url.Parse(expected)
	if err != nil || expectedURL.Scheme == "" || expectedURL.Host == "" {
		return false
	}
	return strings.EqualFold(candidateURL.Scheme, expectedURL.Scheme) &&
		strings.EqualFold(candidateURL.Host, expectedURL.Host)
}

// RequestTrace wraps the current admin/setup routes with secret-safe request
// diagnostics. It deliberately records only routing metadata and the final
// HTTP status; request bodies and credential-bearing fields are never logged.
func RequestTrace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil || !tracePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		action := traceAction(r)
		authenticated := false
		if _, ok := currentSessionData(r); ok {
			authenticated = true
		}
		statusWriter := &traceResponseWriter{ResponseWriter: w}
		next.ServeHTTP(statusWriter, r)
		status := statusWriter.status
		if status == 0 {
			status = http.StatusOK
		}
		slog.Info("admin/setup request", "method", r.Method, "path", r.URL.Path, "status", status, "action", action, "session_authenticated", authenticated)
	})
}

type traceResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *traceResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *traceResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func tracePath(path string) bool {
	return path == "/setup" || path == "/bot/setup" ||
		path == "/admin" || path == "/bot/admin" ||
		strings.HasPrefix(path, "/admin/") || strings.HasPrefix(path, "/bot/admin/")
}

func traceAction(r *http.Request) string {
	if r == nil {
		return "missing"
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	if action == "" && r.Method == http.MethodPost && strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") && r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			if values, parseErr := url.ParseQuery(string(body)); parseErr == nil {
				action = strings.TrimSpace(values.Get("action"))
			}
		}
	}
	if action == "" {
		return "missing"
	}
	for _, ch := range action {
		if !(ch == '_' || ch == '-' || ch == ':' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9') {
			return "other"
		}
	}
	if len(action) > 64 {
		return "other"
	}
	return action
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
	session, ok := sessions[cookie.Value]
	db := sessionStoreDB
	sessionMu.Unlock()
	if !ok && db != nil {
		var persisted model.AdminSession
		if db.Where("token_hash = ?", sessionTokenHash(cookie.Value)).First(&persisted).Error == nil {
			session = sessionData{Expiry: persisted.Expiry, Email: persisted.Email, Role: persisted.Role, BotEditUntil: persisted.BotEditUntil}
			ok = true
		}
	}
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

func wizardStateForRequest(r *http.Request) wizardState {
	session, ok := currentSessionData(r)
	if !ok {
		return wizardState{}
	}
	return session.Wizard
}

func updateWizardState(r *http.Request, update func(*wizardState)) {
	if r == nil {
		return
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return
	}
	// Hydrate a persisted session into the process cache before applying the
	// update, so wizard state remains available after a container replacement.
	if _, ok := currentSessionData(r); !ok {
		return
	}
	sessionMu.Lock()
	session, ok := sessions[cookie.Value]
	db := sessionStoreDB
	if !ok || time.Now().After(session.Expiry) {
		sessionMu.Unlock()
		return
	}
	update(&session.Wizard)
	sessions[cookie.Value] = session
	sessionMu.Unlock()
	if db != nil {
		_ = db.Model(&model.AdminSession{}).Where("token_hash = ?", sessionTokenHash(cookie.Value)).Updates(map[string]interface{}{"updated_at": time.Now()}).Error
	}
}

func LoginHandler(kitsuHostname string) http.HandlerFunc {
	return loginHandlerWithPersist(kitsuHostname, nil, nil)
}

// LoginHandlerWithDiscovery persists a host selected by a successful,
// authenticated login. Discovery itself remains read-only.
func LoginHandlerWithDiscovery(resolve func() (string, string), persist func(string)) http.HandlerFunc {
	return LoginHandlerWithDiscoveryAndConnection(resolve, func(host, _ string) {
		if persist != nil {
			persist(host)
		}
	})
}

// LoginHandlerWithDiscoveryAndConnection persists the display/runtime host and
// optional independently verified API base after a successful sign-in.
func LoginHandlerWithDiscoveryAndConnection(resolve func() (string, string), persist func(string, string)) http.HandlerFunc {
	return loginHandlerWithPersist("", resolve, persist)
}

func loginHandlerWithPersist(kitsuHostname string, resolve func() (string, string), persist func(string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		configuredHostname := normalizeKitsuHostname(kitsuHostname)

		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			hostname := configuredHostname
			source := ""
			if hostname == "" && resolve != nil {
				hostname, source = resolve()
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
			if internalHostname := strings.TrimSpace(r.FormValue("internal_hostname")); internalHostname != "" {
				hostname = internalHostname
			}

			if hostname == "" || (!strings.HasPrefix(hostname, "http://") && !strings.HasPrefix(hostname, "https://")) {
				manualHostname := strings.TrimSpace(r.FormValue("hostname"))
				if manualHostname != "" {
					if normalized, err := validateKitsuEndpoint(manualHostname); err == nil && !isPlaceholderKitsuEndpoint(normalized) {
						hostname = normalized
						source = "operator-supplied"
					}
				}
			}
			if hostname == "" || (!strings.HasPrefix(hostname, "http://") && !strings.HasPrefix(hostname, "https://")) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, loginPageHTMLWithHostname(lang, t(lang, "Kitsu URLを確認できませんでした。KitsuのベースURLを確認してください。", "Kitsu could not be detected. Check the Kitsu base URL."), next, configuredHostname == "", r, r.FormValue("hostname")))
				return
			}
			apiOverride := strings.TrimSpace(r.FormValue("api_base_url"))
			connection, connectionErr := ResolveKitsuConnection(context.Background(), hostname, apiOverride)
			if connectionErr != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, loginPageHTMLWithHostname(lang, t(lang, "Kitsu 接続先を確認できませんでした。", "Kitsu could not be verified before sign-in.")+" ("+connectionErrorClass(connectionErr)+")", next, configuredHostname == "", r, r.FormValue("hostname")))
				return
			}
			hostname = connection.RuntimeBaseURL
			kitsuToken, role, authErr := AuthenticateKitsuCredentials(context.Background(), connection, email, password)
			if authErr != nil || !isStudioManagerOrHigher(role) {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, loginPageHTMLWithHostname(lang, t(lang, "ログインに失敗しました。Kitsu のメール、パスワード、manager/admin 権限を確認してください。", "Login failed. Check the Kitsu email, password, and manager/admin permissions."), next, configuredHostname == "", r, r.FormValue("hostname")))
				return
			}
			token, sessionErr := newSessionTokenChecked(email, kitsuToken, role, next)
			if sessionErr != nil {
				slog.Error("admin session persistence failed", "error_class", classifySessionPersistenceError(sessionErr))
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, loginPageHTMLWithHostname(lang, t(lang, "Kitsu認証は成功しましたが、KitsuSyncは管理者ログイン状態を保存できませんでした。管理者に確認してください。", "Kitsu authentication succeeded, but KitsuSync could not save the admin session. Contact the administrator."), next, configuredHostname == "", r, r.FormValue("hostname")))
				return
			}
			if persist != nil && (source == "local-discovered" || source == "operator-supplied" || apiOverride != "") {
				persist(hostname, apiOverride)
			}
			http.SetCookie(w, sessionCookie(r, token, int(sessionTTL.Seconds())))
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}

		next := r.URL.Query().Get("next")
		showHostname := configuredHostname == ""
		if showHostname && resolve != nil {
			resolved, _ := resolve()
			showHostname = strings.TrimSpace(resolved) == ""
		}
		fmt.Fprint(w, loginPageHTML(lang, "", next, showHostname, r))
	}
}

func LogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "logout requires POST", http.StatusMethodNotAllowed)
			return
		}
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			destroySession(cookie.Value)
		}
		http.SetCookie(w, sessionCookie(r, "", -1))
		http.Redirect(w, r, withLang("/bot/login", r), http.StatusSeeOther)
	}
}

func isStudioManagerOrHigher(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "manager", "admin":
		return true
	default:
		return false
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

	resp, err := (&http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
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

func legacyLoginPageHTML(lang, errMsg, next string, showHostname bool, r *http.Request) string {
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

func loginPageHTML(lang, errMsg, next string, showHostname bool, r *http.Request) string {
	return loginPageHTMLWithHostname(lang, errMsg, next, showHostname, r, "")
}

func loginPageHTMLWithHostname(lang, errMsg, next string, showHostname bool, r *http.Request, hostnameValue string) string {
	errHTML := ""
	if errMsg != "" {
		errHTML = `<div class="toast glass" role="alert" aria-live="assertive">` + html.EscapeString(errMsg) + `</div>`
	}
	nextInput := ""
	if next != "" {
		nextInput = `<input type="hidden" name="next" value="` + html.EscapeString(next) + `">`
	}
	hostnameInput := ""
	if showHostname {
		hostnameInput = `<label for="login-hostname">` + esc(t(lang, "KitsuベースURL", "Kitsu base URL")) + `</label><input id="login-hostname" type="url" name="hostname" value="` + esc(hostnameValue) + `" placeholder="https://kitsu.example.com" autocomplete="url" required><p class="field-help">` + esc(t(lang, "Kitsuを自動検出できない場合に入力します。検証に成功したURLだけを保存します。", "Use this only when Kitsu cannot be detected automatically. Only a successfully validated URL is saved.")) + `</p>`
	}
	advanced := `<details class="connection-advanced"><summary>` + esc(t(lang, "詳細設定（任意）", "Advanced (optional)")) + `</summary><label for="login-internal-hostname">` + esc(t(lang, "内部 Kitsu URL", "Internal Kitsu URL")) + `</label><input id="login-internal-hostname" type="url" name="internal_hostname" autocomplete="url"><p class="field-help">` + esc(t(lang, "KitsuSyncが別の内部経路を使う場合だけ必要です。", "Only needed when KitsuSync must use a different internal route to reach Kitsu.")) + `</p><details class="connection-expert"><summary>` + esc(t(lang, "Expert: API URL", "Expert: API URL")) + `</summary><label for="login-api-base-url">` + esc(t(lang, "API Base URL", "API Base URL")) + `</label><input id="login-api-base-url" type="url" name="api_base_url" autocomplete="url"><p class="field-help">` + esc(t(lang, "Kitsu URLとAPIの起点が異なる特殊なリバースプロキシ用です。", "Only needed for unusual reverse proxy setups where the API is not under the Kitsu URL.")) + `</p></details></details>`
	body := `<div class="page-card glass" style="width:100%;max-width:520px;margin:6vh auto 0"><div class="page-heading"><div><div class="eyebrow">` + esc(tr(lang, "login.admin_access")) + `</div><h1>KitsuSync</h1><p>` + esc(tr(lang, "login.description")) + `</p></div></div>` + errHTML + `<form method="POST" class="section-stack">` + nextInput + `<div class="section-card glass">` + hostnameInput + advanced + `<label for="login-email">` + esc(tr(lang, "login.email")) + `</label><input id="login-email" type="email" name="email" autocomplete="email" required autofocus><label for="login-password">` + esc(tr(lang, "login.password")) + `</label><input id="login-password" type="password" name="password" autocomplete="current-password" required><div class="button-row"><button type="submit" class="btn">` + esc(tr(lang, "login.submit")) + `</button></div></div></form></div>`
	return appShell("KitsuSync", "", lang, r, "", body)
}

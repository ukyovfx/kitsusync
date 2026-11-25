package setup

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"app/src/model"
)

func TestDiscordButtonReceiverSaveRoundTripAndViewEditModes(t *testing.T) {
	db := newIAViewDB(t)
	oldValidate := validateDiscordBotTokenForSave
	validateDiscordBotTokenForSave = func(string) error { return nil }
	t.Cleanup(func() { validateDiscordBotTokenForSave = oldValidate })
	installDiscordAPIStub(t, "")

	// Save bot token via save_discord, then save application public key via save_discord_receiver.
	form := url.Values{
		"action":    {"save_discord"},
		"bot_token": {"valid-discord-token"},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=ja", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addRecentBotEditSession(t, postReq)
	postRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusSeeOther || !strings.Contains(postRec.Header().Get("Location"), "discord_saved") {
		t.Fatalf("expected 303 redirect to discord_saved on save_discord, got %d location=%q (%s)", postRec.Code, postRec.Header().Get("Location"), postRec.Body.String())
	}

	recvForm := url.Values{
		"action":                 {"save_discord_receiver"},
		"application_public_key": {"0123456789abcdef0123456789abcdef"},
	}
	recvReq := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=ja", strings.NewReader(recvForm.Encode()))
	recvReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recvRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(recvRec, recvReq)
	if recvRec.Code != http.StatusSeeOther || !strings.Contains(recvRec.Header().Get("Location"), "discord_receiver_saved") {
		t.Fatalf("expected 303 redirect to discord_receiver_saved, got %d location=%q (%s)", recvRec.Code, recvRec.Header().Get("Location"), recvRec.Body.String())
	}
	if got := model.GetSetting(db, DiscordApplicationPublicKeySettingKey); got != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("expected plain setting persisted, got %q", got)
	}

	// View mode: configured state in full-width section below summary grid, no key input in DOM, bot identity present.
	viewReq := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?lang=ja", nil)
	viewRec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(viewRec, viewReq)
	viewBody := viewRec.Body.String()
	if !strings.Contains(viewBody, "ボタンの受け口：設定済み ・ ポータルの確認：未確認") {
		t.Fatalf("view mode missing combined button receiver status line: %s", viewBody)
	}
	summaryGridIdx := strings.Index(viewBody, `class="connections-summary-grid"`)
	viewRecvIdx := strings.Index(viewBody, `id="button-receiver-section"`)
	viewActionsIdx := strings.Index(viewBody, `class="button-row connections-actions"`)
	if summaryGridIdx < 0 || viewRecvIdx < 0 || viewActionsIdx < 0 || viewRecvIdx < summaryGridIdx || viewRecvIdx > viewActionsIdx {
		t.Fatalf("expected button-receiver-section below summary grid in view mode (grid=%d, recv=%d, actions=%d)", summaryGridIdx, viewRecvIdx, viewActionsIdx)
	}
	if strings.Contains(viewBody[:viewRecvIdx], "ボタンの受け口：設定済み") {
		t.Fatal("button receiver status must not be inside Discord column in view mode")
	}
	if strings.Contains(viewBody, `name="application_public_key"`) {
		t.Fatal("key input must not be present in view-mode HTML")
	}
	if strings.Contains(viewBody, `name="app_external_url"`) {
		t.Fatal("app_external_url input must not be present in view-mode HTML")
	}
	if !strings.Contains(viewBody, "Bot：TestBot（bot-user-id）") {
		t.Fatalf("view mode missing bot identity line: %s", viewBody)
	}

	// Record PING timestamp and verify confirmed datetime renders in both ja and en.
	pingTime := time.Date(2026, 11, 25, 14, 30, 0, 0, time.Local)
	if err := model.SetSettingWithError(db, DiscordInteractionsLastPingAtSettingKey, strconv.FormatInt(pingTime.Unix(), 10)); err != nil {
		t.Fatal(err)
	}
	viewEnReq := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?lang=en", nil)
	viewEnRec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(viewEnRec, viewEnReq)
	viewEnBody := viewEnRec.Body.String()
	wantTime := pingTime.Format("2006-01-02 15:04")
	if !strings.Contains(viewEnBody, "Button receiver: configured · Portal check: confirmed ("+wantTime+")") {
		t.Fatalf("en view mode missing combined button receiver status line: %s", viewEnBody)
	}
	if !strings.Contains(viewEnBody, "Bot: TestBot (bot-user-id)") {
		t.Fatalf("en view mode missing bot identity line: %s", viewEnBody)
	}

	// Edit mode: receiver section below edit grid with its own form, key input pre-filled, endpoint URL from forwarded/observed host, single guidance line.
	editReq := httptest.NewRequest(http.MethodGet, "http://internal:8090/bot/admin/bot?edit=1&lang=en", nil)
	editReq.Header.Set("X-Forwarded-Proto", "https")
	editReq.Header.Set("X-Forwarded-Host", "kitsu.example.com")
	editRec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(editRec, editReq)
	editBody := editRec.Body.String()
	editGridIdx := strings.Index(editBody, `class="connections-edit-grid"`)
	editRecvIdx := strings.Index(editBody, `id="button-receiver-section"`)
	if editGridIdx < 0 || editRecvIdx < 0 || editRecvIdx < editGridIdx {
		t.Fatalf("expected button-receiver-section below connections-edit-grid (grid=%d, recv=%d)", editGridIdx, editRecvIdx)
	}
	if strings.Contains(editBody[:editRecvIdx], `name="application_public_key"`) || strings.Contains(editBody[:editRecvIdx], `name="app_external_url"`) {
		t.Fatal("receiver fields must not be inside Discord column markup in edit mode")
	}
	if !strings.Contains(editBody[editRecvIdx:], `id="discord-receiver-form"`) || !strings.Contains(editBody[editRecvIdx:], `name="action" value="save_discord_receiver"`) || !strings.Contains(editBody[editRecvIdx:], "Save receiver") {
		t.Fatalf("edit mode receiver section missing dedicated form/save button: %s", editBody[editRecvIdx:])
	}
	if !strings.Contains(editBody, `name="application_public_key" value="0123456789abcdef0123456789abcdef"`) {
		t.Fatalf("edit mode missing pre-filled public key input: %s", editBody)
	}
	if !strings.Contains(editBody, "https://kitsu.example.com/interactions") {
		t.Fatalf("edit mode missing forwarded-header interactions endpoint URL: %s", editBody)
	}
	if !strings.Contains(editBody, "Button receiver: configured · Portal check: confirmed ("+wantTime+")") {
		t.Fatalf("edit mode missing combined receiver status line: %s", editBody)
	}
	if !strings.Contains(editBody, "The Public Key is in Discord Developer Portal → your app → General Information. Save this URL as the portal&#39;s Interactions Endpoint URL and the check above turns confirmed.") {
		t.Fatalf("edit mode missing compressed guidance line: %s", editBody)
	}

	// Clearing application_public_key via save_discord_receiver removes the setting and leaves bot token intact.
	clearForm := url.Values{
		"action":                 {"save_discord_receiver"},
		"application_public_key": {""},
	}
	clearReq := httptest.NewRequest(http.MethodPost, "http://localhost:8090/bot/admin/bot?lang=en", strings.NewReader(clearForm.Encode()))
	clearReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	clearRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on clear, got %d (%s)", clearRec.Code, clearRec.Body.String())
	}
	if got := DiscordApplicationPublicKey(db); got != "" {
		t.Fatalf("expected empty public key after clearing, got %q", got)
	}
	if got := storedRuntimeDiscordBotToken(db); got != "valid-discord-token" {
		t.Fatalf("expected discord bot token untouched by receiver clear, got %q", got)
	}
}

func TestDiscordInteractionsEndpointURLFallbackWithoutForwardedHeaders(t *testing.T) {
	plainReq := httptest.NewRequest(http.MethodGet, "http://localhost:8090/bot/admin/bot?edit=1", nil)
	if got := discordInteractionsEndpointURL(nil, plainReq); got != "http://localhost:8090/interactions" {
		t.Fatalf("expected http://localhost:8090/interactions, got %q", got)
	}

	tlsReq := httptest.NewRequest(http.MethodGet, "https://studio.local/bot/admin/bot?edit=1", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	if got := discordInteractionsEndpointURL(nil, tlsReq); got != "https://studio.local/interactions" {
		t.Fatalf("expected https://studio.local/interactions, got %q", got)
	}
}

func TestDiscordAppExternalURLPinsEndpointAndRoundTrips(t *testing.T) {
	db := newIAViewDB(t)
	oldValidate := validateDiscordBotTokenForSave
	validateDiscordBotTokenForSave = func(string) error { return nil }
	t.Cleanup(func() { validateDiscordBotTokenForSave = oldValidate })
	installDiscordAPIStub(t, "")

	// Save app_external_url (with trailing slashes) via save_discord_receiver.
	saveForm := url.Values{
		"action":           {"save_discord_receiver"},
		"app_external_url": {"https://bot.example.com///"},
	}
	saveReq := httptest.NewRequest(http.MethodPost, "http://localhost:8090/bot/admin/bot?lang=ja", strings.NewReader(saveForm.Encode()))
	saveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(saveRec, saveReq)
	if saveRec.Code != http.StatusSeeOther || !strings.Contains(saveRec.Header().Get("Location"), "discord_receiver_saved") {
		t.Fatalf("expected 303 redirect on save_discord_receiver, got %d (%s)", saveRec.Code, saveRec.Body.String())
	}
	if got := AppExternalURL(db); got != "https://bot.example.com///" {
		t.Fatalf("expected stored app.external_url, got %q", got)
	}

	// Endpoint URL uses app.external_url (trailing slashes trimmed) even when request host differs.
	tailscaleReq := httptest.NewRequest(http.MethodGet, "http://100.64.0.10:8090/bot/admin/bot?edit=1&lang=ja", nil)
	tailscaleReq.Header.Set("X-Forwarded-Proto", "https")
	tailscaleReq.Header.Set("X-Forwarded-Host", "tailscale.internal.ts.net")
	if got := discordInteractionsEndpointURL(db, tailscaleReq); got != "https://bot.example.com/interactions" {
		t.Fatalf("expected pinned external endpoint URL, got %q", got)
	}

	// Edit mode shows External URL field above Interactions Endpoint URL, prefilled, and displays pinned URL.
	editRec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(editRec, tailscaleReq)
	editBody := editRec.Body.String()
	extIdx := strings.Index(editBody, `name="app_external_url" value="https://bot.example.com///"`)
	endIdx := strings.Index(editBody, "Interactions Endpoint URL")
	if extIdx < 0 {
		t.Fatalf("edit mode missing prefilled app_external_url input: %s", editBody)
	}
	if endIdx < 0 || extIdx > endIdx {
		t.Fatalf("expected app_external_url input above Interactions Endpoint URL (extIdx=%d, endIdx=%d)", extIdx, endIdx)
	}
	if !strings.Contains(editBody, "kitsusync の公開URL") {
		t.Fatalf("ja edit mode missing kitsusync の公開URL label: %s", editBody)
	}
	if !strings.Contains(editBody, "https://bot.example.com/interactions") {
		t.Fatalf("edit mode missing pinned interactions endpoint URL: %s", editBody)
	}
	if strings.Contains(editBody, "https://tailscale.internal.ts.net/interactions") {
		t.Fatalf("edit mode must not use request-derived URL when app.external_url is set: %s", editBody)
	}

	// View mode must not contain app_external_url input.
	viewReq := httptest.NewRequest(http.MethodGet, "http://localhost:8090/bot/admin/bot?lang=en", nil)
	viewRec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(viewRec, viewReq)
	if strings.Contains(viewRec.Body.String(), `name="app_external_url"`) {
		t.Fatal("app_external_url input must not be present in view-mode HTML")
	}

	// Omitting app_external_url in save_discord_receiver leaves the setting untouched.
	omitForm := url.Values{
		"action":                 {"save_discord_receiver"},
		"application_public_key": {"abcdef0123456789"},
	}
	omitReq := httptest.NewRequest(http.MethodPost, "http://localhost:8090/bot/admin/bot?lang=en", strings.NewReader(omitForm.Encode()))
	omitReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	omitRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(omitRec, omitReq)
	if omitRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect when omitting app_external_url, got %d", omitRec.Code)
	}
	if got := AppExternalURL(db); got != "https://bot.example.com///" {
		t.Fatalf("expected app.external_url preserved when omitted, got %q", got)
	}

	// Clearing app_external_url removes the setting and restores request-derived endpoint URL.
	clearForm := url.Values{
		"action":           {"save_discord_receiver"},
		"app_external_url": {""},
	}
	clearReq := httptest.NewRequest(http.MethodPost, "http://localhost:8090/bot/admin/bot?lang=en", strings.NewReader(clearForm.Encode()))
	clearReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	clearRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on clear, got %d (%s)", clearRec.Code, clearRec.Body.String())
	}
	if got := AppExternalURL(db); got != "" {
		t.Fatalf("expected empty app.external_url after clearing, got %q", got)
	}
	if got := discordInteractionsEndpointURL(db, tailscaleReq); got != "https://tailscale.internal.ts.net/interactions" {
		t.Fatalf("expected request-derived URL after clearing app.external_url, got %q", got)
	}
}

func TestDiscordBotIdentityAbsentWhenNotConnected(t *testing.T) {
	db := newIAViewDB(t)
	req := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?lang=en", nil)
	rec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return false }).ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "Bot: ") || strings.Contains(body, "Bot：") {
		t.Fatalf("bot identity line must not render when token is unset: %s", body)
	}
	if !strings.Contains(body, "Button receiver: not configured") {
		t.Fatalf("expected unconfigured button receiver state: %s", body)
	}
}

func TestHealthHandlerButtonReceiverProblemEntry(t *testing.T) {
	db := newIAViewDB(t)

	// Unset: problem entry appears with link to /bot/admin/bot.
	unsetReq := httptest.NewRequest(http.MethodGet, "/bot/admin/health?lang=ja", nil)
	unsetRec := httptest.NewRecorder()
	HealthHandler(db, time.Minute).ServeHTTP(unsetRec, unsetReq)
	unsetBody := unsetRec.Body.String()
	if !strings.Contains(unsetBody, "Discord ボタンの受け口が未設定です") {
		t.Fatalf("expected Japanese button receiver problem entry when unset: %s", unsetBody)
	}
	if !strings.Contains(unsetBody, `href="/bot/admin/bot?lang=ja"`) {
		t.Fatalf("expected link to /bot/admin/bot in problem entry: %s", unsetBody)
	}

	unsetEnReq := httptest.NewRequest(http.MethodGet, "/bot/admin/health?lang=en", nil)
	unsetEnRec := httptest.NewRecorder()
	HealthHandler(db, time.Minute).ServeHTTP(unsetEnRec, unsetEnReq)
	if !strings.Contains(unsetEnRec.Body.String(), "The Discord button receiver is not configured") {
		t.Fatalf("expected English button receiver problem entry when unset")
	}

	// Set: problem entry disappears.
	if err := model.SetSettingWithError(db, DiscordApplicationPublicKeySettingKey, "0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	setReq := httptest.NewRequest(http.MethodGet, "/bot/admin/health?lang=ja", nil)
	setRec := httptest.NewRecorder()
	HealthHandler(db, time.Minute).ServeHTTP(setRec, setReq)
	setBody := setRec.Body.String()
	if strings.Contains(setBody, "Discord ボタンの受け口が未設定です") {
		t.Fatalf("problem entry must disappear when public key is set: %s", setBody)
	}
}

func TestDiscordOperationPatternRoundTripAndInternalVisibility(t *testing.T) {
	db := newIAViewDB(t)
	oldValidate := validateDiscordBotTokenForSave
	validateDiscordBotTokenForSave = func(string) error { return nil }
	t.Cleanup(func() { validateDiscordBotTokenForSave = oldValidate })
	installDiscordAPIStub(t, "")

	// Invalid stored value is treated as undeclared.
	if err := model.SetSettingWithError(db, AppOperationPatternSettingKey, "bogus"); err != nil {
		t.Fatal(err)
	}
	if got := AppOperationPattern(db); got != "" {
		t.Fatalf("expected invalid operation pattern to normalize to empty, got %q", got)
	}
	if err := model.DeleteSettingWithError(db, AppOperationPatternSettingKey); err != nil {
		t.Fatal(err)
	}

	// Seed bot token, public key, and external URL so we can verify preservation when saving internal.
	seedForm := url.Values{
		"action":    {"save_discord"},
		"bot_token": {"valid-discord-token"},
	}
	seedReq := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=ja", strings.NewReader(seedForm.Encode()))
	seedReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addRecentBotEditSession(t, seedReq)
	seedRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(seedRec, seedReq)
	if seedRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on seed save, got %d (%s)", seedRec.Code, seedRec.Body.String())
	}
	seedRecvForm := url.Values{
		"action":                 {"save_discord_receiver"},
		"application_public_key": {"0123456789abcdef0123456789abcdef"},
		"app_external_url":       {"https://bot.example.com"},
	}
	seedRecvReq := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=ja", strings.NewReader(seedRecvForm.Encode()))
	seedRecvReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	seedRecvRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(seedRecvRec, seedRecvReq)
	if seedRecvRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on seed receiver save, got %d (%s)", seedRecvRec.Code, seedRecvRec.Body.String())
	}

	// Undeclared view mode (ja & en): state line at page top.
	for _, tc := range []struct {
		lang string
		want string
	}{
		{lang: "ja", want: "運用：未宣言"},
		{lang: "en", want: "Operation: Undeclared"},
	} {
		vReq := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?lang="+tc.lang, nil)
		vRec := httptest.NewRecorder()
		BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(vRec, vReq)
		if !strings.Contains(vRec.Body.String(), tc.want) {
			t.Fatalf("undeclared view mode (%s) missing %q: %s", tc.lang, tc.want, vRec.Body.String())
		}
	}

	// Undeclared edit mode: page-top form has 未宣言 selected above both cards, plus key/URL/endpoint/guidance in Discord card.
	undeclaredReq := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?edit=1&lang=ja", nil)
	undeclaredRec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(undeclaredRec, undeclaredReq)
	undeclaredBody := undeclaredRec.Body.String()
	for _, want := range []string{
		`id="operation-pattern-form"`,
		`name="action" value="save_operation_pattern"`,
		`name="app_operation_pattern"`,
		`<option value="" selected>未宣言</option>`,
		`<option value="internal">内部運用</option>`,
		`<option value="external">外部運用</option>`,
		`name="application_public_key"`,
		`name="app_external_url"`,
		"Interactions Endpoint URL",
		"ボタンの受け口：設定済み",
	} {
		if !strings.Contains(undeclaredBody, want) {
			t.Fatalf("undeclared edit mode missing %q: %s", want, undeclaredBody)
		}
	}
	opFormIdx := strings.Index(undeclaredBody, `id="operation-pattern-form"`)
	gridIdx := strings.Index(undeclaredBody, `class="connections-edit-grid"`)
	if opFormIdx < 0 || gridIdx < 0 || opFormIdx > gridIdx {
		t.Fatalf("expected operation-pattern-form above connections-edit-grid (opFormIdx=%d, gridIdx=%d)", opFormIdx, gridIdx)
	}
	discordFormIdx := strings.Index(undeclaredBody, `name="action" value="save_discord"`)
	if discordFormIdx < 0 || strings.Contains(undeclaredBody[discordFormIdx:], `name="app_operation_pattern"`) {
		t.Fatalf("Discord card form must not contain app_operation_pattern: %s", undeclaredBody)
	}

	// Save pattern=internal via dedicated operation-pattern form; stored key and external URL must remain untouched.
	internalForm := url.Values{
		"action":                {"save_operation_pattern"},
		"app_operation_pattern": {"internal"},
	}
	internalReq := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=ja", strings.NewReader(internalForm.Encode()))
	internalReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	internalRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(internalRec, internalReq)
	if internalRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on internal save, got %d (%s)", internalRec.Code, internalRec.Body.String())
	}
	if loc := internalRec.Header().Get("Location"); !strings.Contains(loc, "edit=1") {
		t.Fatalf("expected redirect back to edit mode so cards re-render, got %q", loc)
	}
	if got := AppOperationPattern(db); got != OperationPatternInternal {
		t.Fatalf("expected internal operation pattern, got %q", got)
	}
	if got := DiscordApplicationPublicKey(db); got != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("saving internal must not clear stored public key, got %q", got)
	}
	if got := AppExternalURL(db); got != "https://bot.example.com" {
		t.Fatalf("saving internal must not clear stored external URL, got %q", got)
	}

	// Internal edit mode: select stays rendered (without 未宣言), state row is internal line, key/URL/endpoint/guidance omitted.
	editInternalReq := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?edit=1&lang=ja", nil)
	editInternalRec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(editInternalRec, editInternalReq)
	editInternalBody := editInternalRec.Body.String()
	for _, want := range []string{
		`name="app_operation_pattern"`,
		`<option value="internal" selected>内部運用</option>`,
		`<option value="external">外部運用</option>`,
		"ボタンの受け口：内部運用のため無効",
	} {
		if !strings.Contains(editInternalBody, want) {
			t.Fatalf("internal edit mode missing %q: %s", want, editInternalBody)
		}
	}
	for _, gone := range []string{
		`<option value=""`,
		"未宣言",
		`id="discord-receiver-form"`,
		`name="application_public_key"`,
		`name="app_external_url"`,
		"Interactions Endpoint URL",
		"Discord Developer Portal",
	} {
		if strings.Contains(editInternalBody, gone) {
			t.Fatalf("internal edit mode must omit %q: %s", gone, editInternalBody)
		}
	}

	// Internal view mode (ja & en): page-top state line + internal receiver line, no select, no key/URL/endpoint/guidance.
	for _, tc := range []struct {
		lang string
		want []string
	}{
		{lang: "ja", want: []string{"運用：内部運用", "ボタンの受け口：内部運用のため無効"}},
		{lang: "en", want: []string{"Operation: Internal operation", "Button receiver: disabled (internal operation)"}},
	} {
		vReq := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?lang="+tc.lang, nil)
		vRec := httptest.NewRecorder()
		BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(vRec, vReq)
		vBody := vRec.Body.String()
		for _, w := range tc.want {
			if !strings.Contains(vBody, w) {
				t.Fatalf("internal view mode (%s) missing %q: %s", tc.lang, w, vBody)
			}
		}
		for _, gone := range []string{`name="app_operation_pattern"`, `name="application_public_key"`, `name="app_external_url"`, "Interactions Endpoint URL"} {
			if strings.Contains(vBody, gone) {
				t.Fatalf("internal view mode (%s) must omit %q: %s", tc.lang, gone, vBody)
			}
		}
	}

	// Health page: with key unset and pattern=internal, problem entry must NOT appear.
	if err := model.DeleteSettingWithError(db, DiscordApplicationPublicKeySettingKey); err != nil {
		t.Fatal(err)
	}
	healthIntReq := httptest.NewRequest(http.MethodGet, "/bot/admin/health?lang=ja", nil)
	healthIntRec := httptest.NewRecorder()
	HealthHandler(db, time.Minute).ServeHTTP(healthIntRec, healthIntReq)
	if strings.Contains(healthIntRec.Body.String(), "Discord ボタンの受け口が未設定です") {
		t.Fatalf("health problem entry must not appear when pattern=internal: %s", healthIntRec.Body.String())
	}

	// Save pattern=external: receiver fields return, health problem entry appears when key is unset.
	extForm := url.Values{
		"action":                {"save_operation_pattern"},
		"app_operation_pattern": {"external"},
	}
	extReq := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=ja", strings.NewReader(extForm.Encode()))
	extReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	extRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(extRec, extReq)
	if extRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on external save, got %d (%s)", extRec.Code, extRec.Body.String())
	}
	if got := AppOperationPattern(db); got != OperationPatternExternal {
		t.Fatalf("expected external operation pattern, got %q", got)
	}
	editExtReq := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?edit=1&lang=en", nil)
	editExtRec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(editExtRec, editExtReq)
	editExtBody := editExtRec.Body.String()
	for _, want := range []string{
		`<option value="external" selected>External operation</option>`,
		`name="application_public_key"`,
		`name="app_external_url"`,
		"Interactions Endpoint URL",
		"Button receiver: not configured",
	} {
		if !strings.Contains(editExtBody, want) {
			t.Fatalf("external edit mode missing %q: %s", want, editExtBody)
		}
	}
	if strings.Contains(editExtBody, "Undeclared") {
		t.Fatalf("external edit mode must not show Undeclared option: %s", editExtBody)
	}
	healthExtReq := httptest.NewRequest(http.MethodGet, "/bot/admin/health?lang=ja", nil)
	healthExtRec := httptest.NewRecorder()
	HealthHandler(db, time.Minute).ServeHTTP(healthExtRec, healthExtReq)
	if !strings.Contains(healthExtRec.Body.String(), "Discord ボタンの受け口が未設定です") {
		t.Fatalf("health problem entry must appear when pattern=external and key unset: %s", healthExtRec.Body.String())
	}

	// External view mode (ja & en): page-top state line.
	for _, tc := range []struct {
		lang string
		want string
	}{
		{lang: "ja", want: "運用：外部運用"},
		{lang: "en", want: "Operation: External operation"},
	} {
		vReq := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?lang="+tc.lang, nil)
		vRec := httptest.NewRecorder()
		BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(vRec, vReq)
		if !strings.Contains(vRec.Body.String(), tc.want) {
			t.Fatalf("external view mode (%s) missing %q: %s", tc.lang, tc.want, vRec.Body.String())
		}
	}

	// Save pattern="" (back to undeclared): setting is cleared.
	backForm := url.Values{
		"action":                {"save_operation_pattern"},
		"app_operation_pattern": {""},
	}
	backReq := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=ja", strings.NewReader(backForm.Encode()))
	backReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	backRec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(backRec, backReq)
	if backRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on clearing pattern, got %d (%s)", backRec.Code, backRec.Body.String())
	}
	if got := AppOperationPattern(db); got != "" {
		t.Fatalf("expected empty operation pattern after clearing, got %q", got)
	}
}

func TestDiscordObservedPublicURLAndPriority(t *testing.T) {
	db := newIAViewDB(t)
	installDiscordAPIStub(t, "")

	// Non-public hosts must NOT be recorded.
	nonPublicHosts := []struct {
		proto string
		host  string
	}{
		{"http", "localhost:8090"},
		{"http", "127.0.0.1:8090"},
		{"http", "127.0.1.1"},
		{"http", "[::1]:8090"},
		{"http", "10.0.0.5:8090"},
		{"http", "172.16.0.1:8090"},
		{"http", "192.168.1.10:8090"},
		{"http", "169.254.1.2:8090"},
		{"http", "[fc00::1]:8090"},
		{"http", "[fe80::1]:8090"},
		{"https", "kitsu.tail1234.ts.net"},
		{"https", "studio.local"},
	}
	for _, tc := range nonPublicHosts {
		req := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?edit=1&lang=ja", nil)
		req.Header.Set("X-Forwarded-Proto", tc.proto)
		req.Header.Set("X-Forwarded-Host", tc.host)
		rec := httptest.NewRecorder()
		BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(rec, req)
		if got := AppObservedPublicURL(db); got != "" {
			t.Fatalf("non-public host %q must not be recorded, got %q", tc.host, got)
		}
	}

	// Public host (with non-default port) IS recorded via X-Forwarded-* headers.
	pubReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8090/bot/admin/bot?edit=1&lang=ja", nil)
	pubReq.Header.Set("X-Forwarded-Proto", "https")
	pubReq.Header.Set("X-Forwarded-Host", "sync.studio.example:8443")
	pubRec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(pubRec, pubReq)
	if got := AppObservedPublicURL(db); got != "https://sync.studio.example:8443" {
		t.Fatalf("expected observed public URL recorded, got %q", got)
	}

	// Subsequent non-public request (e.g. Tailscale or localhost) must NOT overwrite stored observed URL.
	tsReq := httptest.NewRequest(http.MethodGet, "http://localhost:8090/bot/admin/bot?edit=1&lang=ja", nil)
	tsReq.Header.Set("X-Forwarded-Proto", "https")
	tsReq.Header.Set("X-Forwarded-Host", "kitsu.tail1234.ts.net")
	tsRec := httptest.NewRecorder()
	BotHandlerWithRuntime(db, nil, func() bool { return true }).ServeHTTP(tsRec, tsReq)
	if got := AppObservedPublicURL(db); got != "https://sync.studio.example:8443" {
		t.Fatalf("non-public request must not overwrite observed URL, got %q", got)
	}

	// Priority: no app.external_url -> endpoint uses app.observed_public_url even on a Tailscale request,
	// and kitsusync の公開URL input is prefilled with the observed value.
	tsBody := tsRec.Body.String()
	if !strings.Contains(tsBody, "https://sync.studio.example:8443/interactions") {
		t.Fatalf("expected endpoint URL to use observed public URL, got: %s", tsBody)
	}
	if !strings.Contains(tsBody, `name="app_external_url" value="https://sync.studio.example:8443"`) {
		t.Fatalf("expected app_external_url input prefilled with observed public URL, got: %s", tsBody)
	}

	// Priority: saved app.external_url overrides app.observed_public_url.
	if err := model.SetSettingWithError(db, AppExternalURLSettingKey, "https://pinned.example.com"); err != nil {
		t.Fatal(err)
	}
	if got := discordInteractionsEndpointURL(db, tsReq); got != "https://pinned.example.com/interactions" {
		t.Fatalf("expected saved app.external_url to take priority over observed URL, got %q", got)
	}
}

func TestDiscordReceiverSaveDoesNotTouchTokensOrKitsu(t *testing.T) {
	db := newIAViewDB(t)
	if err := setRuntimeDiscordBotToken(db, "existing-discord-token"); err != nil {
		t.Fatal(err)
	}
	if err := setRuntimeKitsuToken(db, "existing-kitsu-token"); err != nil {
		t.Fatal(err)
	}
	if err := model.SetSettingWithError(db, ExternalKitsuURLSettingKey, "https://kitsu.example.test"); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"action":                 {"save_discord_receiver"},
		"application_public_key": {"fedcba9876543210"},
		"app_external_url":       {"https://receiver.example.com"},
		"bot_token":              {"should-be-ignored"},
		"kitsu_bot_token":        {"should-be-ignored"},
		"kitsu_external_url":     {"https://should-be-ignored.test"},
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=ja", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	BotHandler(db, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "discord_receiver_saved") {
		t.Fatalf("expected 303 redirect to discord_receiver_saved, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := DiscordApplicationPublicKey(db); got != "fedcba9876543210" {
		t.Fatalf("expected public key saved, got %q", got)
	}
	if got := AppExternalURL(db); got != "https://receiver.example.com" {
		t.Fatalf("expected external URL saved, got %q", got)
	}
	if got := storedRuntimeDiscordBotToken(db); got != "existing-discord-token" {
		t.Fatalf("discord bot token must not be touched by save_discord_receiver, got %q", got)
	}
	if got := StoredRuntimeKitsuToken(db); got != "existing-kitsu-token" {
		t.Fatalf("kitsu token must not be touched by save_discord_receiver, got %q", got)
	}
	if got := ExternalKitsuURL(db); got != "https://kitsu.example.test" {
		t.Fatalf("kitsu external URL must not be touched by save_discord_receiver, got %q", got)
	}
}

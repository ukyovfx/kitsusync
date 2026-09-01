package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/basicauth"
	"app/src/utils/config"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gookit/slog"

	"gorm.io/gorm"
)

const (
	RuntimeKitsuEmailSettingKey = "kitsu.runtime_email"
	RuntimeKitsuEmailEnv        = "KITSU_RUNTIME_EMAIL"
	RuntimeKitsuPasswordEnv     = "KITSU_RUNTIME_PASSWORD"
	RuntimeDiscordBotTokenKey   = "discord.runtime_bot_token"
	runtimeBotEmail             = "kitsusync-bot@google.com"
	runtimeBotFirstName         = "KitsuSync"
	runtimeBotLastName          = "Bot"
)

type KitsuPerson struct {
	ID       string
	FullName string
	Email    string
	Active   bool
	Archived bool
	IsBot    bool
	Role     string
}

type KitsuProject struct {
	ID   string
	Name string
}

type TemplateChannel struct {
	NameJA   string
	NameEN   string
	TaskType string
}

func (c TemplateChannel) Name(lang string) string {
	if lang == "en" {
		if c.NameEN != "" {
			return c.NameEN
		}
		return strings.ToLower(strings.ReplaceAll(c.TaskType, " ", "-"))
	}
	if c.NameJA != "" {
		return c.NameJA
	}
	if c.NameEN != "" {
		return c.NameEN
	}
	return c.TaskType
}

type ProjectTemplate struct {
	Channels []TemplateChannel
}

var Templates = map[string]ProjectTemplate{
	"cg": {
		Channels: []TemplateChannel{
			{NameJA: "企画・構成", NameEN: "kitsu-concept", TaskType: "Concept"},
			{NameJA: "企画・構成", NameEN: "kitsu-concept", TaskType: "Storyboard"},
			{NameJA: "アセット制作", NameEN: "kitsu-assets", TaskType: "Modeling"},
			{NameJA: "アセット制作", NameEN: "kitsu-assets", TaskType: "Rigging"},
			{NameJA: "アセット制作", NameEN: "kitsu-assets", TaskType: "LookDev"},
			{NameJA: "ショット制作前半", NameEN: "kitsu-animation", TaskType: "Layout"},
			{NameJA: "ショット制作前半", NameEN: "kitsu-animation", TaskType: "Animation"},
			{NameJA: "FX・ライティング・合成", NameEN: "kitsu-fx-lighting-comp", TaskType: "FX"},
			{NameJA: "FX・ライティング・合成", NameEN: "kitsu-fx-lighting-comp", TaskType: "Lighting"},
			{NameJA: "FX・ライティング・合成", NameEN: "kitsu-fx-lighting-comp", TaskType: "Compositing"},
			{NameJA: "ポストプロダクション", NameEN: "kitsu-post", TaskType: "Color Grading"},
			{NameJA: "ポストプロダクション", NameEN: "kitsu-post", TaskType: "Sound"},
			{NameJA: "ポストプロダクション", NameEN: "kitsu-post", TaskType: "Edit"},
		},
	},
}

var AssetTypesByProjectType = map[string][]string{
	"cg": {"Character", "Environment", "Prop"},
}

const discordAPI = "https://discord.com/api/v10"

func normalizeKitsuHostname(raw string) string {
	host := strings.TrimSpace(raw)
	if host == "" {
		return ""
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	if !strings.HasSuffix(host, "/") {
		host += "/"
	}
	return host
}

// safeKitsuHostDisplay keeps container-only addressing out of normal UI while
// retaining a useful local-development summary. Credentials, paths, queries,
// and fragments are never displayed.
func safeKitsuHostDisplay(raw string) string {
	normalized := normalizeKitsuHostname(raw)
	if normalized == "" {
		return ""
	}
	u, err := url.Parse(normalized)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	if strings.EqualFold(u.Hostname(), "host.docker.internal") {
		return "http://127.0.0.1:8080"
	}
	if u.User != nil {
		u.User = nil
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	return strings.TrimRight(u.String(), "/")
}

// effectiveRuntimeKitsuEndpoint resolves the address used by server-side Kitsu
// requests. A saved setting wins; environment/profile defaults are only used
// when no saved setting exists. This value must never be rendered directly.
func effectiveRuntimeKitsuEndpoint(db *gorm.DB) string {
	if db != nil {
		if saved := strings.TrimSpace(model.GetSetting(db, "kitsu.hostname")); saved != "" {
			return normalizeKitsuHostname(saved)
		}
	}
	if configured := strings.TrimSpace(os.Getenv("KITSU_HOSTNAME")); configured != "" {
		return normalizeKitsuHostname(configured)
	}
	return normalizeKitsuHostname(LocalDevelopmentKitsuHostname())
}

func validateKitsuEndpoint(raw string) (string, error) {
	normalized := normalizeKitsuHostname(raw)
	if normalized == "" {
		return "", errors.New("kitsu endpoint is empty")
	}
	u, err := url.Parse(normalized)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", errors.New("kitsu endpoint is invalid")
	}
	return normalized, nil
}

// runtimeEndpointFromDisplay accepts the safe local display value without
// allowing it to replace the container-to-host runtime address.
func runtimeEndpointFromDisplay(db *gorm.DB, displayed string) (string, error) {
	normalized, err := validateKitsuEndpoint(displayed)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(strings.TrimRight(normalized, "/"), "http://127.0.0.1:8080") {
		// The normal UI intentionally displays a safe host summary. Resolve that
		// summary to the persistent local Kitsu address before making a request.
		// Do not reuse the saved endpoint here: it may be a stale disposable or
		// otherwise different local instance that the operator is replacing.
		return "http://host.docker.internal:8080/", nil
	}
	return normalized, nil
}

func publicKitsuHostnameFromRequest(r *http.Request, storedHost string) string {
	if r != nil {
		scheme := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
		if scheme == "" {
			if r.TLS != nil {
				scheme = "https"
			} else {
				scheme = "http"
			}
		}
		host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
		if host == "" {
			host = strings.TrimSpace(r.Host)
		}
		if host != "" && !strings.Contains(host, "localhost") && !strings.HasPrefix(host, "127.0.0.1") {
			return normalizeKitsuHostname(scheme + "://" + host)
		}
	}
	return normalizeKitsuHostname(storedHost)
}

func ListKitsuPersons(_ string) []KitsuPerson {
	persons := kitsu.GetPersons()
	out := make([]KitsuPerson, 0, len(persons.Each))
	for _, person := range persons.Each {
		fullName := strings.TrimSpace(person.FullName)
		if fullName == "" {
			fullName = strings.TrimSpace(strings.TrimSpace(person.FirstName) + " " + strings.TrimSpace(person.LastName))
		}
		out = append(out, KitsuPerson{
			ID:       person.ID,
			FullName: strings.TrimSpace(fullName),
			Email:    strings.TrimSpace(person.Email),
			Active:   person.Active,
			Archived: person.Archived,
			IsBot:    person.IsBot,
			Role:     strings.TrimSpace(person.Role),
		})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName) })
	return out
}

func ListKitsuProjectParticipants(projectID string) []KitsuPerson {
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) == "" || strings.TrimSpace(projectID) == "" {
		return nil
	}
	persons := kitsu.GetProjectTeam(strings.TrimSpace(projectID))
	out := make([]KitsuPerson, 0, len(persons))
	for _, person := range persons {
		fullName := strings.TrimSpace(person.FullName)
		if fullName == "" {
			fullName = strings.TrimSpace(strings.TrimSpace(person.FirstName) + " " + strings.TrimSpace(person.LastName))
		}
		out = append(out, KitsuPerson{ID: person.ID, FullName: fullName, Email: strings.TrimSpace(person.Email), Active: person.Active, Archived: person.Archived, IsBot: person.IsBot, Role: strings.TrimSpace(person.Role)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName) })
	return out
}

func ListKitsuProjects(_ string) []KitsuProject {
	projects := kitsu.GetProjects()
	out := make([]KitsuProject, 0, len(projects.Each))
	for _, project := range projects.Each {
		out = append(out, KitsuProject{ID: project.ID, Name: strings.TrimSpace(project.Name)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func GetKitsuProjectID(_ string, projectName string) string {
	target := strings.TrimSpace(projectName)
	if target == "" {
		return ""
	}
	for _, project := range ListKitsuProjects("") {
		if strings.EqualFold(strings.TrimSpace(project.Name), target) {
			return project.ID
		}
	}
	return ""
}

func AllTaskTypeNames() []string {
	taskTypes := kitsu.GetTaskTypes()
	seen := map[string]bool{}
	out := make([]string, 0, len(taskTypes.Each))
	for _, taskType := range taskTypes.Each {
		name := strings.TrimSpace(taskType.Name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		out = []string{"Animation", "Background Art", "Color Grading", "Compositing", "Concept", "Design", "Edit", "FX", "Layout", "Lighting", "Lookdev", "Modeling", "Rendering", "Rigging", "Script", "Shading", "Sound", "Storyboard", "Texturing"}
	}
	sort.Strings(out)
	return out
}

type kitsuBotPerson struct {
	ID        string `json:"id,omitempty"`
	Email     string `json:"email,omitempty"`
	FullName  string `json:"full_name,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	IsBot     bool   `json:"is_bot,omitempty"`
}

type kitsuAPIError struct {
	method   string
	path     string
	status   int
	category string
}

func (e *kitsuAPIError) Error() string {
	if e.status == 0 {
		return "Kitsu API request failed (network error)"
	}
	return fmt.Sprintf("Kitsu API request failed (HTTP %d: %s)", e.status, e.category)
}

func kitsuAPIErrorCategory(status int) string {
	switch {
	case status == http.StatusBadRequest:
		return "validation rejected"
	case status == http.StatusUnauthorized:
		return "authentication expired or rejected"
	case status == http.StatusForbidden:
		return "permission denied"
	case status == http.StatusNotFound:
		return "endpoint or account not found"
	case status == http.StatusConflict:
		return "request conflicts with existing data"
	case status >= 400 && status < 500:
		return "request rejected"
	case status >= 500:
		return "Kitsu service unavailable"
	default:
		return "unexpected response"
	}
}

func runtimeBotSetupError(err error) string {
	var apiErr *kitsuAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.category {
		case "authentication expired or rejected":
			return "Kitsuの管理者セッションが期限切れか拒否されました。再ログインしてからもう一度お試しください。"
		case "permission denied":
			return "Kitsuがアカウント作成を拒否しました。管理者権限を確認してください。"
		case "validation rejected":
			return "Kitsuがランタイムアカウント情報を検証できませんでした。Kitsuの設定を確認してください。"
		case "request conflicts with existing data":
			return "ランタイムアカウントと既存データが競合しています。管理者に確認してください。"
		}
	}
	return "Kitsuのランタイムアカウント作成に失敗しました。Kitsuの状態を確認してからもう一度お試しください。"
}

func storedRuntimeKitsuEmail(db *gorm.DB) string {
	if db != nil {
		if value := strings.TrimSpace(model.GetSetting(db, RuntimeKitsuEmailSettingKey)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(os.Getenv(RuntimeKitsuEmailEnv))
}

func setRuntimeKitsuEmail(db *gorm.DB, email string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	if db != nil {
		model.SetSetting(db, RuntimeKitsuEmailSettingKey, email)
	}
	os.Setenv(RuntimeKitsuEmailEnv, email)
	os.Unsetenv("KITSU_EMAIL")
}

func storedRuntimeDiscordBotToken(db *gorm.DB) string {
	if db != nil {
		if value := strings.TrimSpace(model.GetSetting(db, RuntimeDiscordBotTokenKey)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN"))
}

// DiscordBotTokenFingerprint returns a short, secret-safe diagnostic identity.
// It is intentionally one-way and must never be used as a credential.
func DiscordBotTokenFingerprint(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:12]
}

var validateDiscordBotTokenForSave = validateDiscordBotToken

func validateDiscordBotToken(token string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("Discord Bot Token is required")
	}
	body, status, err := botDo(http.MethodGet, discordAPI+"/users/@me", nil, token)
	if err != nil {
		return errors.New("Discord Bot could not be reached")
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		_ = body
		return discordBotAPIError("Discord Bot validation failed", status, nil)
	}
	return nil
}

func setRuntimeDiscordBotToken(db *gorm.DB, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	if db != nil {
		model.SetSecretSetting(db, RuntimeDiscordBotTokenKey, token)
	}
	os.Setenv("DISCORD_BOT_TOKEN", token)
}

func runtimeBotFullName() string {
	return runtimeBotFirstName + " " + runtimeBotLastName
}

func generateRuntimePassword() (string, error) {
	seed := make([]byte, 18)
	if _, err := rand.Read(seed); err != nil {
		return "", err
	}
	return "ksb-" + hex.EncodeToString(seed), nil
}

func kitsuJSON(token, method, requestURL string, payload, out interface{}) error {
	var body io.Reader
	if payload != nil {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		parsedURL, _ := url.Parse(requestURL)
		path := parsedURL.Path
		return &kitsuAPIError{method: method, path: path, status: resp.StatusCode, category: kitsuAPIErrorCategory(resp.StatusCode)}
	}
	if out != nil && len(bytes.TrimSpace(respBody)) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return err
		}
	}
	return nil
}

func findRuntimeBotPerson(kitsuHost, adminToken string) (*kitsuBotPerson, error) {
	var persons []kitsuBotPerson
	requestURL := normalizeKitsuHostname(kitsuHost) + "api/data/persons"
	if err := kitsuJSON(adminToken, http.MethodGet, requestURL, nil, &persons); err != nil {
		return nil, err
	}
	for _, person := range persons {
		if strings.EqualFold(strings.TrimSpace(person.Email), runtimeBotEmail) {
			personCopy := person
			return &personCopy, nil
		}
	}
	return nil, nil
}

func createRuntimeBotPerson(kitsuHost, adminToken, password string) (*kitsuBotPerson, error) {
	requestURL := normalizeKitsuHostname(kitsuHost) + "api/data/persons"
	payload := map[string]interface{}{
		"first_name": runtimeBotFirstName,
		"last_name":  runtimeBotLastName,
		"email":      runtimeBotEmail,
		"password":   password,
		"role":       "admin",
		"active":     true,
		"is_bot":     true,
	}
	var created kitsuBotPerson
	if err := kitsuJSON(adminToken, http.MethodPost, requestURL, payload, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

func CreateKitsuBotAccount(kitsuHost, adminEmail, adminPassword string) (string, string, error) {
	loginURL := normalizeKitsuHostname(kitsuHost) + "api/auth/login"
	adminToken := basicauth.AuthForJWTToken(loginURL, adminEmail, adminPassword)
	if adminToken == "" {
		return "", "", errors.New("admin authentication failed")
	}
	return CreateKitsuBotAccountWithToken(kitsuHost, adminToken)
}

func CreateKitsuBotAccountWithToken(kitsuHost, adminToken string) (string, string, error) {
	if strings.TrimSpace(adminToken) == "" {
		return "", "", errors.New("admin session is missing")
	}
	runtimePassword, err := generateRuntimePassword()
	if err != nil {
		return "", "", err
	}

	person, err := findRuntimeBotPerson(kitsuHost, adminToken)
	if err != nil {
		return "", "", err
	}
	if person == nil {
		person, err = createRuntimeBotPerson(kitsuHost, adminToken, runtimePassword)
		if err != nil {
			return "", "", err
		}
	} else {
		if !person.IsBot && !strings.EqualFold(strings.TrimSpace(person.FullName), runtimeBotFullName()) {
			return "", "", errors.New("runtime bot identity is already used by another account")
		}
		return "", "", errors.New("runtime bot account already exists and cannot be recovered automatically")
	}

	email := strings.TrimSpace(person.Email)
	if email == "" {
		email = runtimeBotEmail
	}
	return email, runtimePassword, nil
}

func ReuseRuntimeBotAccountWithToken(kitsuHost, adminToken, email, password string) (string, string, error) {
	if strings.TrimSpace(email) == "" || strings.TrimSpace(password) == "" {
		return "", "", errors.New("saved runtime bot credentials are incomplete")
	}
	person, err := findRuntimeBotPerson(kitsuHost, adminToken)
	if err != nil {
		return "", "", err
	}
	if person == nil || !person.IsBot || !strings.EqualFold(strings.TrimSpace(person.Email), strings.TrimSpace(email)) {
		return "", "", errors.New("saved runtime bot account could not be verified")
	}
	return strings.TrimSpace(email), password, nil
}

func SeedFromConfig(db *gorm.DB, conf config.Config) {
	if db == nil {
		return
	}
	for _, user := range conf.Mention.UserMap {
		if strings.TrimSpace(user.KitsuName) == "" || strings.TrimSpace(user.DiscordID) == "" {
			continue
		}
		model.UpsertUserMap(db, strings.TrimSpace(user.KitsuName), strings.TrimSpace(user.DiscordID))
	}
	for _, checker := range conf.Mention.Checkers {
		if strings.TrimSpace(checker.TaskType) == "" || strings.TrimSpace(checker.DiscordID) == "" {
			continue
		}
		model.AddCheckerMap(db, strings.TrimSpace(checker.TaskType), strings.TrimSpace(checker.DiscordID))
	}
}

func selectedAttr(selected bool) string {
	if selected {
		return "selected"
	}
	return ""
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// botDo executes a Discord Bot API request and retries once on HTTP 429 (rate limit).
func botDo(method, endpoint string, payload any, botToken string) ([]byte, int, error) {
	trimmedToken := strings.TrimSpace(botToken)
	if trimmedToken == "" {
		return nil, 0, errors.New("discord bot token is missing")
	}

	var rawPayload []byte
	if payload != nil {
		var err error
		rawPayload, err = json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		var body io.Reader
		if rawPayload != nil {
			body = bytes.NewReader(rawPayload)
		}
		req, err := http.NewRequest(method, endpoint, body)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bot "+trimmedToken)
		if rawPayload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, readErr
		}

		// On rate limit, respect Retry-After and retry
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := 1.0
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if v, parseErr := strconv.ParseFloat(ra, 64); parseErr == nil && v > 0 {
					retryAfter = v
				}
			} else {
				// Parse retry_after from JSON body
				var rateBody struct {
					RetryAfter float64 `json:"retry_after"`
				}
				if jsonErr := json.Unmarshal(respBody, &rateBody); jsonErr == nil && rateBody.RetryAfter > 0 {
					retryAfter = rateBody.RetryAfter
				}
			}
			wait := time.Duration(retryAfter*1000) * time.Millisecond
			if wait > 15*time.Second {
				wait = 15 * time.Second
			}
			slog.Warn("Discord rate limited; retrying", "endpoint", endpoint, "retry_after_ms", wait.Milliseconds(), "attempt", attempt+1)
			time.Sleep(wait)
			continue
		}

		return respBody, resp.StatusCode, nil
	}
	return nil, http.StatusTooManyRequests, fmt.Errorf("discord API rate limited after %d retries: %s", maxRetries, endpoint)
}

func discordBotAPIError(action string, status int, respBody []byte) error {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: Discord bot token is missing or invalid (HTTP 401 Unauthorized)", action)
	case http.StatusForbidden:
		return fmt.Errorf("%s: Discord bot is authenticated but lacks permission for this request (HTTP 403 Forbidden)", action)
	case http.StatusNotFound:
		return fmt.Errorf("%s: Discord resource was not found for this request (HTTP 404 Not Found)", action)
	case http.StatusBadRequest:
		return fmt.Errorf("%s: Discord rejected the request payload (HTTP 400 Bad Request)", action)
	default:
		return fmt.Errorf("%s: Discord API returned HTTP %d", action, status)
	}
}

func CreateCategory(guildID, name, botToken string) (string, error) {
	respBody, status, err := botDo(http.MethodPost, fmt.Sprintf("%s/guilds/%s/channels", discordAPI, guildID), map[string]any{
		"name": strings.TrimSpace(name),
		"type": 4,
	}, botToken)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", discordBotAPIError("discord category create failed", status, respBody)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if result.ID == "" {
		return "", fmt.Errorf("discord category id was empty")
	}
	return result.ID, nil
}

func CreateTextChannel(guildID, categoryID, name, botToken string) (string, error) {
	respBody, status, err := botDo(http.MethodPost, fmt.Sprintf("%s/guilds/%s/channels", discordAPI, guildID), map[string]any{
		"name":      strings.TrimSpace(name),
		"type":      0,
		"parent_id": categoryID,
	}, botToken)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", discordBotAPIError("discord channel create failed", status, respBody)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if result.ID == "" {
		return "", fmt.Errorf("discord channel id was empty")
	}
	return result.ID, nil
}

func SetGuildChannelPosition(channelID string, position int, botToken string) error {
	respBody, status, err := botDo(http.MethodPatch, fmt.Sprintf("%s/channels/%s", discordAPI, strings.TrimSpace(channelID)), map[string]any{
		"position": position,
	}, botToken)
	if err != nil {
		return err
	}
	if status >= 400 {
		return discordBotAPIError("discord channel reorder failed", status, respBody)
	}
	return nil
}

type DiscordChannelPosition struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
}

func SetGuildChannelPositions(guildID string, positions []DiscordChannelPosition, botToken string) error {
	guildID = strings.TrimSpace(guildID)
	if guildID == "" || len(positions) == 0 {
		return fmt.Errorf("discord channel reorder requires a guild and at least one channel")
	}
	respBody, status, err := botDo(http.MethodPatch, fmt.Sprintf("%s/guilds/%s/channels", discordAPI, guildID), positions, botToken)
	if err != nil {
		return err
	}
	if status >= 400 {
		return discordBotAPIError("discord channel reorder failed", status, respBody)
	}
	return nil
}

type DiscordGuildChannel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     int    `json:"type"`
	ParentID string `json:"parent_id"`
	Position int    `json:"position"`
}

type DiscordGuild struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DiscordGuildMember struct {
	User struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		GlobalName  string `json:"global_name"`
		DisplayName string `json:"display_name"`
	} `json:"user"`
	Nick string `json:"nick"`
}

type discordMemberListFailure struct {
	Kind      string
	Status    int
	Code      int
	Technical string
}

func (e *discordMemberListFailure) Error() string {
	return e.Technical
}

const (
	discordMemberFailureFixture     = "fixture"
	discordMemberFailureMalformed   = "malformed"
	discordMemberFailureMismatch    = "mismatch"
	discordMemberFailureAccess      = "access"
	discordMemberFailureIntent      = "intent"
	discordMemberFailureUnavailable = "unavailable"
)

func ListBotGuilds(botToken string) ([]DiscordGuild, error) {
	body, status, err := botDo(http.MethodGet, discordAPI+"/users/@me/guilds", nil, botToken)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, discordBotAPIError("discord guild list failed", status, body)
	}
	var guilds []DiscordGuild
	if err := json.Unmarshal(body, &guilds); err != nil {
		return nil, fmt.Errorf("discord guild list response was invalid")
	}
	return guilds, nil
}

func ListGuildChannels(guildID, botToken string) ([]DiscordGuildChannel, error) {
	body, status, err := botDo(http.MethodGet, fmt.Sprintf("%s/guilds/%s/channels", discordAPI, strings.TrimSpace(guildID)), nil, botToken)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, discordBotAPIError("discord guild channel list failed", status, body)
	}
	var channels []DiscordGuildChannel
	if err := json.Unmarshal(body, &channels); err != nil {
		return nil, fmt.Errorf("discord guild channel list response was invalid")
	}
	return channels, nil
}

func ListGuildMembers(guildID, botToken string) ([]DiscordGuildMember, error) {
	guildID = strings.TrimSpace(guildID)
	if !isDiscordSnowflake(guildID) {
		kind := discordMemberFailureMalformed
		if isSyntheticDiscordID(guildID) {
			kind = discordMemberFailureFixture
		}
		return nil, &discordMemberListFailure{Kind: kind, Technical: "Discord guild member list was not requested because the server ID is not a valid Discord snowflake"}
	}
	if strings.TrimSpace(botToken) == "" {
		return nil, &discordMemberListFailure{Kind: discordMemberFailureUnavailable, Technical: "Discord Bot token is not configured"}
	}
	const pageLimit = 1000
	const maxPages = 100
	var all []DiscordGuildMember
	after := ""
	for page := 0; page < maxPages; page++ {
		endpoint, endpointErr := discordGuildMembersEndpoint(guildID, after)
		if endpointErr != nil {
			return nil, &discordMemberListFailure{Kind: discordMemberFailureMalformed, Technical: endpointErr.Error()}
		}
		body, status, err := botDo(http.MethodGet, endpoint, nil, botToken)
		if err != nil {
			return nil, &discordMemberListFailure{Kind: discordMemberFailureUnavailable, Technical: "Discord guild member list request failed"}
		}
		if status >= 400 {
			return nil, classifyDiscordMemberListFailure(status, body)
		}
		var members []DiscordGuildMember
		if err := json.Unmarshal(body, &members); err != nil {
			return nil, &discordMemberListFailure{Kind: discordMemberFailureMalformed, Status: status, Technical: "Discord returned an invalid member list response"}
		}
		all = append(all, members...)
		if len(members) < pageLimit {
			return all, nil
		}
		lastID := strings.TrimSpace(members[len(members)-1].User.ID)
		if !isDiscordSnowflake(lastID) || lastID == after {
			return nil, &discordMemberListFailure{Kind: discordMemberFailureMalformed, Status: status, Technical: "Discord member list pagination returned an invalid cursor"}
		}
		after = lastID
	}
	return nil, &discordMemberListFailure{Kind: discordMemberFailureUnavailable, Technical: "Discord member list pagination exceeded the safe page limit"}
}

func isSyntheticDiscordID(id string) bool {
	lower := strings.ToLower(strings.TrimSpace(id))
	return strings.HasPrefix(lower, "qa-guild-") || strings.HasPrefix(lower, "synthetic-") || strings.HasPrefix(lower, "fixture-")
}

func isDiscordSnowflake(id string) bool {
	if len(id) < 17 || len(id) > 20 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func discordGuildMembersEndpoint(guildID, after string) (string, error) {
	guildID = strings.TrimSpace(guildID)
	if !isDiscordSnowflake(guildID) {
		return "", fmt.Errorf("Discord guild ID is not a valid snowflake")
	}
	endpoint := fmt.Sprintf("%s/guilds/%s/members?limit=1000", discordAPI, url.PathEscape(guildID))
	if strings.TrimSpace(after) != "" {
		if !isDiscordSnowflake(strings.TrimSpace(after)) {
			return "", fmt.Errorf("Discord member pagination cursor is not a valid snowflake")
		}
		endpoint += "&after=" + url.QueryEscape(strings.TrimSpace(after))
	}
	return endpoint, nil
}

func classifyDiscordMemberListFailure(status int, body []byte) *discordMemberListFailure {
	var parsed struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	}
	_ = json.Unmarshal(body, &parsed)
	lower := strings.ToLower(parsed.Message)
	kind := discordMemberFailureUnavailable
	switch status {
	case http.StatusBadRequest:
		kind = discordMemberFailureMalformed
	case http.StatusForbidden:
		kind = discordMemberFailureAccess
		if strings.Contains(lower, "intent") {
			kind = discordMemberFailureIntent
		}
	case http.StatusNotFound:
		kind = discordMemberFailureMismatch
	}
	return &discordMemberListFailure{Kind: kind, Status: status, Code: parsed.Code, Technical: fmt.Sprintf("Discord member list request failed with HTTP %d", status)}
}

func CreateWebhook(channelID, name, botToken string) (string, error) {
	respBody, status, err := botDo(http.MethodPost, fmt.Sprintf("%s/channels/%s/webhooks", discordAPI, channelID), map[string]any{
		"name": strings.TrimSpace(name),
	}, botToken)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", discordBotAPIError("discord webhook create failed", status, respBody)
	}
	var result struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if result.ID == "" || result.Token == "" {
		return "", fmt.Errorf("discord webhook response was incomplete")
	}
	return fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", result.ID, result.Token), nil
}

// DeleteWebhook removes a webhook by its Discord ID using the bot credential.
// It is intentionally separate from channel deletion so E2E cleanup cannot
// accidentally remove a channel while rotating a webhook.
func DeleteWebhook(webhookID, botToken string) error {
	webhookID = strings.TrimSpace(webhookID)
	if webhookID == "" {
		return fmt.Errorf("discord webhook ID is required")
	}
	respBody, status, err := botDo(http.MethodDelete, fmt.Sprintf("%s/webhooks/%s", discordAPI, webhookID), nil, botToken)
	if err != nil {
		return err
	}
	if status >= 400 {
		return discordBotAPIError("discord webhook delete failed", status, respBody)
	}
	return nil
}

func DeleteChannel(channelID, botToken string) error {
	respBody, status, err := botDo(http.MethodDelete, fmt.Sprintf("%s/channels/%s", discordAPI, channelID), nil, botToken)
	if err != nil {
		return err
	}
	if status >= 400 {
		return discordBotAPIError("discord channel delete failed", status, respBody)
	}
	return nil
}

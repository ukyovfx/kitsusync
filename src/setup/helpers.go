package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/basicauth"
	"app/src/utils/config"
	"bytes"
	"crypto/rand"
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
			Role:     strings.TrimSpace(person.Role),
		})
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
		req.Header.Set("Content-Type", "application/json")

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
	bodyText := strings.TrimSpace(string(respBody))
	message := bodyText
	if message == "" {
		message = http.StatusText(status)
	}

	var discordErr struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	}
	if len(respBody) > 0 && json.Unmarshal(respBody, &discordErr) == nil && strings.TrimSpace(discordErr.Message) != "" {
		message = strings.TrimSpace(discordErr.Message)
	}

	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: Discord bot token is missing or invalid (HTTP 401 Unauthorized)", action)
	case http.StatusForbidden:
		return fmt.Errorf("%s: Discord bot is authenticated but lacks permission for this request (HTTP 403 Forbidden)", action)
	case http.StatusNotFound:
		return fmt.Errorf("%s: Discord resource was not found for this request (HTTP 404 Not Found): %s", action, message)
	case http.StatusBadRequest:
		return fmt.Errorf("%s: Discord rejected the request payload (HTTP 400 Bad Request): %s", action, message)
	default:
		return fmt.Errorf("%s: Discord API returned HTTP %d: %s", action, status, message)
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

package discord

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/config"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"gorm.io/gorm"
)

type EmbedAuthor struct {
	Name string `json:"name"`
}
type EmbedFooter struct {
	Text string `json:"text"`
}
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}
type EmbedImage struct {
	URL string `json:"url"`
}
type Embed struct {
	Description string       `json:"description,omitempty"`
	Title       string       `json:"title,omitempty"`
	Color       int          `json:"color,omitempty"`
	Url         string       `json:"url,omitempty"`
	Author      EmbedAuthor  `json:"author,omitempty"`
	Footer      EmbedFooter  `json:"footer,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Image       *EmbedImage  `json:"image,omitempty"`
}

type Payload struct {
	Username        string           `json:"username,omitempty"`
	AvatarUrl       string           `json:"avatar_url,omitempty"`
	Content         string           `json:"content"`
	Embeds          []Embed          `json:"embeds,omitempty"`
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
}

// AllowedMentions keeps Kitsu-originated text from turning into an accidental
// Discord-wide or role mention. User mentions are opt-in and listed explicitly.
type AllowedMentions struct {
	Parse []string `json:"parse,omitempty"`
	Users []string `json:"users,omitempty"`
	Roles []string `json:"roles,omitempty"`
}
type Assignee struct {
	Fullname string
	Email    string
	Phone    string
}
type Template struct {
	ProjectName             string
	GroupName               string
	ParentName              string
	TaskName                string
	TaskType                string
	CurrentStatus           string
	PreviousStatus          string
	CommentContent          string
	CommentAuthor           string
	EntityType              string
	Assignees               []Assignee
	AssigneesStr            string
	TaskURL                 string
	MentionContent          string
	ProcessEmoji            string
	StatusMessage           string
	StatusUpper             string
	StatusEmoji             string
	GoogleDriveURL          string
	PreviewImageURL         string // Kitsu プレビュー画像 URL（空ならスキップ）
	IsCommentOnly           bool   // コメントのみの更新（ステータス変化なし）
	IsAssignNotification    bool   // 新規タスクアサイン（TODO ステータス、notifyOnAssign=true）
	StatusTransitionMessage string // ステータス遷移を説明するメッセージ
	CommentOnlyMessage      string
	CommentLabel            string
	CommentAuthorLabel      string
	AssigneeLabel           string
	LinksLabel              string
	ChannelName             string // 通知先の Discord チャンネル名（ルーティング確認用）
	NotificationLanguage    string
	AllowedUserIDs          []string
	Color                   int
}

const (
	neutralEmbedColor         = 0x64748B
	maxNotificationRecipients = 20
)

// Discord APIのメッセージ作成レスポンス
type DiscordMessage struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"` // スレッド作成時はここにスレッドIDが入る
}

// UserMapResolver は Kitsu 名 → Discord ID を解決するフック。
// main 起動時に DB バックエンドの実装を注入する。
// nil の場合は conf.Mention.UserMap にフォールバックする。
// projectID はプロジェクトスコープ検索に使用; kitsuEmail はリネーム時のフォールバック検索に使用（空でも可）。
var UserMapResolver func(projectID, kitsuName, kitsuEmail string) string

// CheckerResolver はタスクタイプ名 → チェッカーの Discord ID 一覧を解決するフック。
// nil の場合は conf.Mention.Checkers にフォールバックする。
// projectID はプロジェクトスコープ検索に使用する。
var CheckerResolver func(projectID, taskType string) []string

// GoogleDriveURLResolver はプロジェクト ID に対応するファイルストレージ URL を返すフック。
// プロジェクトごとの URL を DB から引く。nil または空文字の場合は conf.GoogleDrive.URL にフォールバック。
var GoogleDriveURLResolver func(projectID string) string

// SendResult は SendMessage / SendMessageBunch の戻り値
type SendResult struct {
	MessageID       string // Discord メッセージID
	ThreadID        string // Discord スレッドID（UseThreads=true 時のみ）
	FailureCategory string // safe category for a failed delivery
	Retryable       bool   // safe to retry automatically on the next poll
	Unknown         bool   // the request may have been accepted without a response
}

// containsIgnoreCase reports whether `target` (case-insensitive) is present in `list`.
func containsIgnoreCase(list []string, target string) bool {
	for _, s := range list {
		if strings.EqualFold(s, target) {
			return true
		}
	}
	return false
}

func normalizeNotificationLang(lang string) string {
	if strings.EqualFold(strings.TrimSpace(lang), "en") {
		return "en"
	}
	return "ja"
}

func sanitizeDiscordText(value string) string {
	value = strings.ReplaceAll(value, "<", "＜")
	value = strings.ReplaceAll(value, ">", "＞")
	value = strings.ReplaceAll(value, "@everyone", "@ everyone")
	value = strings.ReplaceAll(value, "@here", "@ here")
	return value
}

func localizedTemplatePreset(tplPreset, notifLang string) string {
	if strings.EqualFold(tplPreset, "rich") && normalizeNotificationLang(notifLang) == "en" {
		return "eng"
	}
	return tplPreset
}

func localizedStatusMessageInfo(status, lang string) (string, string) {
	switch strings.ToUpper(status) {
	case "WFA":
		if normalizeNotificationLang(lang) == "en" {
			return "Please review", "👀"
		}
		return "チェックをお願いします", "👀"
	case "RETAKE":
		if normalizeNotificationLang(lang) == "en" {
			return "A revision is needed", "🔄"
		}
		return "修正をお願いします", "🔄"
	case "DONE":
		if normalizeNotificationLang(lang) == "en" {
			return "Completed. Please check if needed.", "✅"
		}
		return "完了しました。必要に応じてご確認ください。", "✅"
	case "READY":
		if normalizeNotificationLang(lang) == "en" {
			return "Ready for the next step", "🟢"
		}
		return "次工程の作業準備ができました", "🟢"
	case "TODO":
		if normalizeNotificationLang(lang) == "en" {
			return "A task has been assigned", "📋"
		}
		return "タスクがアサインされました", "📋"
	case "WIP":
		if normalizeNotificationLang(lang) == "en" {
			return "Work is now in progress", "🚧"
		}
		return "作業を開始しました", "🚧"
	default:
		return "", "ℹ️"
	}
}

func localizedStatusTransitionMessage(prev, current, lang string) string {
	if prev == "" || strings.EqualFold(prev, current) {
		return ""
	}
	key := strings.ToUpper(prev) + "->" + strings.ToUpper(current)
	switch key {
	case "RETAKE->WFA":
		if normalizeNotificationLang(lang) == "en" {
			return "The revision is ready for review"
		}
		return "修正が完了しました。再チェックをお願いします"
	case "RETAKE->DONE":
		if normalizeNotificationLang(lang) == "en" {
			return "Revision completed"
		}
		return "修正対応が完了しました"
	case "WFA->RETAKE":
		if normalizeNotificationLang(lang) == "en" {
			return "A revision is required after review"
		}
		return "レビュー結果により修正が必要です"
	case "WFA->WIP":
		if normalizeNotificationLang(lang) == "en" {
			return "Returned to rework"
		}
		return "🛠 修正作業に戻りました"
	case "WFA->READY":
		if normalizeNotificationLang(lang) == "en" {
			return "Review is complete. Ready for the next step"
		}
		return "確認が完了しました。次工程に進めます"
	case "READY->WIP":
		if normalizeNotificationLang(lang) == "en" {
			return "Work has started"
		}
		return "作業を開始しました"
	case "READY->DONE":
		if normalizeNotificationLang(lang) == "en" {
			return "Completed without additional work"
		}
		return "✅ 作業完了・承認されました"
	case "WIP->WFA":
		if normalizeNotificationLang(lang) == "en" {
			return "Ready for review"
		}
		return "📩 チェック依頼が送られました"
	case "WFA->DONE":
		if normalizeNotificationLang(lang) == "en" {
			return "Review completed"
		}
		return "レビューが完了しました"
	case "RETAKE->WIP":
		if normalizeNotificationLang(lang) == "en" {
			return "Revision work has started"
		}
		return "🛠 リテイク対応を開始しました"
	case "WIP->RETAKE":
		if normalizeNotificationLang(lang) == "en" {
			return "A retake was requested"
		}
		return "🔁 リテイクが入りました"
	case "TODO->WIP":
		if normalizeNotificationLang(lang) == "en" {
			return "Work has started"
		}
		return "🚀 作業を開始しました"
	case "NONE->WIP":
		if normalizeNotificationLang(lang) == "en" {
			return "Work has started"
		}
		return "🚀 作業を開始しました"
	case "TODO->DONE", "NONE->DONE", "WIP->DONE":
		if normalizeNotificationLang(lang) == "en" {
			return "Work completed and approved"
		}
		return "✅ 作業完了・承認されました"
	default:
		return ""
	}
}

func localizedCommentOnlyMessage(status, lang string) string {
	if normalizeNotificationLang(lang) == "en" {
		return "Revision details were updated"
	}
	return "修正内容が更新されました"
}

func parseKitsuStatusColor(raw string) int {
	hex := strings.TrimSpace(raw)
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return neutralEmbedColor
	}
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return neutralEmbedColor
	}
	return int(value)
}

func isValidDiscordUserID(id string) bool {
	if len(id) < 2 || len(id) > 30 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func uniqueDiscordIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !isValidDiscordUserID(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
		if len(result) >= maxNotificationRecipients {
			break
		}
	}
	return result
}

func mentionContent(ids []string) string {
	ids = uniqueDiscordIDs(ids)
	mentions := make([]string, len(ids))
	for i, id := range ids {
		mentions[i] = "<@" + id + ">"
	}
	return strings.Join(mentions, " ")
}

func notificationRecipientCandidates(status string, assignNotification bool, assignees, checkers []string, conf config.Config) []string {
	if assignNotification {
		var candidates []string
		if containsIgnoreCase(conf.Mention.CheckerStatuses, status) {
			candidates = append(candidates, checkers...)
		}
		if containsIgnoreCase(conf.Mention.ArtistStatuses, status) {
			candidates = append(candidates, assignees...)
		}
		return uniqueDiscordIDs(candidates)
	}
	switch strings.ToUpper(status) {
	case "WFA":
		return uniqueDiscordIDs(checkers)
	case "RETAKE":
		return uniqueDiscordIDs(assignees)
	case "DONE":
		var candidates []string
		if containsIgnoreCase(conf.Mention.CheckerStatuses, status) {
			candidates = append(candidates, checkers...)
		}
		if containsIgnoreCase(conf.Mention.ArtistStatuses, status) {
			candidates = append(candidates, assignees...)
		}
		return uniqueDiscordIDs(candidates)
	default:
		return nil
	}
}

func safeNotificationURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "YOUR_FOLDER_ID") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return raw
}

func statusTransitionMessage(prev, current string) string {
	if prev == "" || strings.EqualFold(prev, current) {
		return ""
	}
	key := strings.ToUpper(prev) + "→" + strings.ToUpper(current)
	switch key {
	case "RETAKE→WFA":
		return "📤 修正版がアップされました"
	case "RETAKE→DONE":
		return "✅ 承認されました"
	case "WFA→RETAKE":
		return "🔃 リテイクお願いします。"
	case "WFA→READY":
		return "🟢 承認完了。次工程の作業待ちです"
	case "READY→WIP":
		return "🚀 作業を開始しました"
	case "READY→DONE":
		return "🎉 作業完了・承認されました"
	case "WIP→WFA":
		return "📤 チェック依頼が送られました"
	case "WFA→DONE":
		return "🎉 最終承認されました！お疲れ様でした"
	case "RETAKE→WIP":
		return "🔧 修正作業を開始しました"
	case "WIP→RETAKE":
		return "🔄 リテイクお願いします。"
	case "TODO→WIP":
		return "🚀 作業を開始しました"
	default:
		return ""
	}
}

// 工程ごとのアイコン
func getProcessEmoji(taskType string) string {
	switch strings.ToLower(strings.TrimSpace(taskType)) {
	case "animation", "anim":
		return "🏃"
	case "asset", "assets", "background art", "background", "bg", "environment", "env":
		return "🖼️"
	case "camera", "matchmove", "tracking":
		return "🎥"
	case "color grading", "grading", "color":
		return "🌈"
	case "compositing", "comp":
		return "🧩"
	case "concept", "concept art":
		return "💭"
	case "cleanup", "clean", "paint":
		return "🖌️"
	case "design":
		return "🎨"
	case "editing", "edit", "offline edit", "online edit":
		return "✂️"
	case "fx", "vfx", "simulation", "sim":
		return "✨"
	case "lighting":
		return "💡"
	case "layout":
		return "📐"
	case "lookdev", "look development":
		return "🔍"
	case "modeling", "model":
		return "🧊"
	case "previs", "previz":
		return "🎬"
	case "production", "pm", "coordination":
		return "📋"
	case "rendering", "render":
		return "💻"
	case "rigging", "rig":
		return "🦴"
	case "rotoscoping", "roto":
		return "✂️"
	case "script", "writing":
		return "📜"
	case "shading":
		return "🌓"
	case "sound", "audio", "music":
		return "🔊"
	case "storyboard", "board":
		return "📝"
	case "texture", "texturing":
		return "🧵"
	case "review", "approval":
		return "👀"
	case "shot":
		return "🎞️"
	case "sequence", "episode":
		return "📚"
	default:
		return "🏷️"
	}
}

// DeleteMessage は既存のDiscordメッセージを削除する。
// threadID が空でない場合はスレッド内メッセージの削除として処理する。
// 成功時は true、失敗時は false を返す（失敗してもプロセスは継続）
func DeleteMessage(webhookURL, messageID, threadID string) bool {
	if webhookURL == "" || messageID == "" {
		return false
	}
	deleteURL := fmt.Sprintf("%s/messages/%s", webhookURL, messageID)
	if threadID != "" {
		deleteURL += "?thread_id=" + url.QueryEscape(threadID)
	}
	req, err := http.NewRequest(http.MethodDelete, deleteURL, nil)
	if err != nil {
		slog.Warn("DeleteMessage: failed to build request", "err", err, "messageID", messageID)
		return false
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("DeleteMessage: request failed", "err", err, "messageID", messageID)
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	// 204 No Content = 削除成功 / 404 Not Found = すでに削除済み（成功扱い）
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return true
	}
	slog.Warn("DeleteMessage: unexpected status", "status", resp.StatusCode, "messageID", messageID)
	return false
}

// sendMessageOnce は HTTP POST を1回試みる内部関数。
// 戻り値: (result, retryable, err)
// retryable=true なら呼び出し側がリトライを判断する。
func sendMessageOnce(body []byte, reqURL string) (respBody []byte, statusCode int, retryAfterSec float64, err error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(reqURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()
	respBody, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, 0, err
	}
	// 429 Rate Limit: Retry-After ヘッダを秒数として返す
	if resp.StatusCode == 429 {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if v, e := strconv.ParseFloat(ra, 64); e == nil {
				retryAfterSec = v
			} else {
				retryAfterSec = 1.0
			}
		} else {
			retryAfterSec = 1.0
		}
	}
	return respBody, resp.StatusCode, retryAfterSec, nil
}

// SendMessage は1タスク分のメッセージを送信して SendResult を返す。
// threadID が空で threadName が非空の場合は新規スレッドを作成する。
// threadID が非空の場合は既存スレッドに返信する。
// 429 Rate Limit と 5xx エラーは自動リトライ（最大3回）する。
// 失敗時は空の SendResult を返す。
func SendMessage(payload Payload, webhookURL, threadID, threadName string) SendResult {
	// URL 組み立て
	reqURL := webhookURL + "?wait=true"
	if threadID != "" {
		reqURL += "&thread_id=" + url.QueryEscape(threadID)
	} else if threadName != "" {
		// スレッド名は最大 100 文字
		if len([]rune(threadName)) > 100 {
			runes := []rune(threadName)
			threadName = string(runes[:100])
		}
		reqURL += "&thread_name=" + url.QueryEscape(threadName)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("SendMessage: marshal failed", "err", err)
		return SendResult{FailureCategory: "invalid_payload"}
	}

	// リトライループ: 429 Rate Limit と 5xx エラーは最大3回まで再試行
	const maxRetries = 3
	var respBody []byte
	var statusCode int
	for attempt := 0; attempt <= maxRetries; attempt++ {
		var retryAfterSec float64
		respBody, statusCode, retryAfterSec, err = sendMessageOnce(body, reqURL)
		if err != nil {
			slog.Error("SendMessage: HTTP post failed", "category", "network_error", "attempt", attempt+1)
			if attempt < maxRetries {
				time.Sleep(time.Duration(1<<attempt) * time.Second) // 1s, 2s, 4s
				continue
			}
			return SendResult{FailureCategory: "network_error", Retryable: true, Unknown: true}
		}
		if statusCode == 429 {
			// Rate Limited: Retry-After ヘッダの秒数だけ待って再試行。
			// 上限30秒: Discordが極端に長いretry_afterを返した場合（例: 1600秒超）に
			// 起動・ポーリングが無期限ブロックされるのを防ぐ。
			// 上限を超えた場合はこの試行を断念し、次のポーリングサイクルで再試行する。
			const maxRateLimitWait = 30 * time.Second
			wait := time.Duration(retryAfterSec*1000) * time.Millisecond
			if wait < 100*time.Millisecond {
				wait = 100 * time.Millisecond
			}
			if wait > maxRateLimitWait {
				slog.Warn("SendMessage: rate limit wait exceeds cap; skipping this task until next poll cycle",
					"retryAfterSec", retryAfterSec, "capSec", maxRateLimitWait.Seconds(), "attempt", attempt+1)
				return SendResult{FailureCategory: "rate_limited", Retryable: true}
			}
			slog.Warn("SendMessage: rate limited, waiting",
				"retryAfterSec", retryAfterSec, "attempt", attempt+1)
			time.Sleep(wait)
			continue
		}
		if statusCode >= 500 && attempt < maxRetries {
			// サーバーエラー: 指数バックオフで再試行
			wait := time.Duration(1<<attempt) * time.Second
			slog.Warn("SendMessage: server error, retrying",
				"status", statusCode, "attempt", attempt+1, "waitSec", wait.Seconds())
			time.Sleep(wait)
			continue
		}
		break // 成功 or リトライ不要なエラー
	}

	if statusCode < 200 || statusCode >= 300 {
		slog.Error("SendMessage: non-2xx response",
			"status", statusCode,
			"category", discordHTTPErrorCategory(statusCode))
		return SendResult{FailureCategory: discordHTTPErrorCategory(statusCode), Retryable: statusCode == http.StatusTooManyRequests || statusCode >= 500, Unknown: statusCode >= 500}
	}

	var msg DiscordMessage
	if err := json.Unmarshal(respBody, &msg); err != nil {
		slog.Error("SendMessage: unmarshal failed", "err", err)
		return SendResult{FailureCategory: "unknown_response", Unknown: true}
	}

	result := SendResult{MessageID: msg.ID}
	// スレッド新規作成時: channel_id がスレッドID になる
	if threadName != "" && msg.ChannelID != "" {
		result.ThreadID = msg.ChannelID
	} else {
		result.ThreadID = threadID // 既存スレッドはそのまま引き継ぐ
	}
	return result
}

func discordHTTPErrorCategory(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_payload"
	case http.StatusUnauthorized:
		return "invalid_token"
	case http.StatusForbidden:
		return "missing_permission"
	case http.StatusNotFound:
		return "missing_destination"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		if statusCode >= 500 {
			return "discord_server_error"
		}
		return "discord_http_error"
	}
}

// SendMessageBunch は各タスクを処理して SendResult のマップを返す
// キーはタスクID
func SendMessageBunch(conf config.Config, data []kitsu.MessagePayload, webHookURL string, previousMessageIDs map[string]string, previousWebhookURLs map[string]string, previousThreadIDs map[string]string, projectNotificationLanguages map[string]string, db *gorm.DB) map[string]SendResult {
	result := make(map[string]SendResult)

	for _, elem := range data {
		var placeholders Template
		notifLang := normalizeNotificationLang(projectNotificationLanguages[elem.Project.ID])

		placeholders.ProjectName = sanitizeDiscordText(elem.Project.Name)
		placeholders.GroupName = sanitizeDiscordText(elem.EntityType.Name)
		placeholders.ParentName = sanitizeDiscordText(elem.Parent.Name)
		placeholders.TaskName = sanitizeDiscordText(elem.Entity.Name)
		placeholders.TaskType = sanitizeDiscordText(elem.TaskType.Name)
		placeholders.CurrentStatus = sanitizeDiscordText(elem.TaskStatus.ShortName)
		placeholders.StatusUpper = strings.ToUpper(elem.TaskStatus.ShortName)
		placeholders.PreviousStatus = elem.PreviousStatusName
		placeholders.CommentContent = sanitizeDiscordText(elem.LatestComment.Comment.Text)
		placeholders.CommentAuthor = sanitizeDiscordText(elem.LatestComment.Author.FullName)
		placeholders.EntityType = sanitizeDiscordText(elem.EntityType.EntityType.Name)
		placeholders.ProcessEmoji = getProcessEmoji(elem.TaskType.Name)
		if db != nil {
			placeholders.ChannelName = model.FindChannelNameByWebhookURL(db, webHookURL)
		}

		// Assignees を解決しつつ、アーティストの Discord ID も収集。
		// 同じ Kitsu 名が userMap に複数行で書かれていても 1 回しか mention しないよう、
		// 既に追加した DiscordID を seen で重複排除する。
		var assigneeNames []string
		var artistMentions []string
		placeholders.Assignees = make([]Assignee, len(elem.Assignees))
		for i := 0; i < len(elem.Assignees); i++ {
			if elem.Assignees[i].IsBot {
				slog.Info("Kitsu bot assignee excluded from Discord mention", "taskID", elem.Task.ID)
				continue
			}
			fullName := elem.Assignees[i].FullName
			placeholders.Assignees[i].Fullname = sanitizeDiscordText(fullName)
			placeholders.Assignees[i].Email = elem.Assignees[i].Email
			assigneeNames = append(assigneeNames, sanitizeDiscordText(fullName))

			matched := false
			// DB 優先: UserMapResolver が注入されていれば使う（プロジェクトスコープ → グローバル フォールバック対応）
			if UserMapResolver != nil {
				assigneeEmail := elem.Assignees[i].Email
				if did := UserMapResolver(elem.Project.ID, fullName, assigneeEmail); did != "" {
					artistMentions = append(artistMentions, did)
					matched = true
				}
			}
			// フォールバック: conf.toml の userMap
			if !matched {
				for _, u := range conf.Mention.UserMap {
					if u.KitsuName == fullName {
						artistMentions = append(artistMentions, u.DiscordID)
						matched = true
						break
					}
				}
			}
			if !matched {
				slog.Warn("Kitsu user not registered; will not be @-mentioned",
					"kitsuName", fullName,
					"taskID", elem.Task.ID,
					"hint", "add this person via /bot/admin/users")
			}
		}
		if len(assigneeNames) > 0 {
			placeholders.AssigneesStr = strings.Join(assigneeNames, ", ")
		} else if normalizeNotificationLang(notifLang) == "en" {
			placeholders.AssigneesStr = "Unassigned"
		} else {
			placeholders.AssigneesStr = "未割り当て"
		}

		// タスクタイプからチェッカーの Discord ID 一覧を検索（DB 優先、複数人対応）
		var checkerIDs []string
		if CheckerResolver != nil {
			if dids := CheckerResolver(elem.Project.ID, elem.TaskType.Name); len(dids) > 0 {
				for _, did := range dids {
					checkerIDs = append(checkerIDs, did)
				}
			}
		}
		if len(checkerIDs) == 0 {
			for _, c := range conf.Mention.Checkers {
				if strings.EqualFold(c.TaskType, elem.TaskType.Name) {
					checkerIDs = append(checkerIDs, c.DiscordID)
				}
			}
		}

		// ステータスごとのメンション対象を conf.Mention の設定から決める。
		// CheckerStatuses に含まれるステータスではチェッカーをメンション、
		// ArtistStatuses に含まれるステータスではアーティスト全員をメンションする。
		// HereStatuses に含まれるステータスでは @here を追加（緊急通知）。
		// 複数に含まれるステータスでは全てを併記する。
		currentStatus := strings.ToUpper(elem.TaskStatus.ShortName)
		recipientIDs := notificationRecipientCandidates(currentStatus, elem.IsAssignNotification, artistMentions, checkerIDs, conf)
		if len(recipientIDs) == 0 && containsIgnoreCase(conf.Mention.CheckerStatuses, currentStatus) && len(conf.Mention.Checkers) > 0 {
			slog.Warn("No checker configured for task type; checker will not be @-mentioned",
				"taskType", elem.TaskType.Name,
				"status", currentStatus,
				"taskID", elem.Task.ID,
				"hint", "add this task type to [[mention.checkers]] in conf.toml")
		}
		// 緊急ステータスは @here でチャンネル全員に通知
		// @here and @everyone are never emitted by KitsuSync.
		mentionContent := mentionContent(recipientIDs)
		statusMessage, statusEmoji := localizedStatusMessageInfo(currentStatus, notifLang)

		placeholders.MentionContent = mentionContent
		placeholders.StatusMessage = statusMessage
		placeholders.StatusEmoji = statusEmoji
		// ストレージ URL はプロジェクト別 DB 優先、なければ conf.toml のグローバル値
		if GoogleDriveURLResolver != nil {
			if u := GoogleDriveURLResolver(elem.Project.Project.ID); u != "" {
				placeholders.GoogleDriveURL = safeNotificationURL(u)
			} else {
				placeholders.GoogleDriveURL = safeNotificationURL(conf.GoogleDrive.URL)
			}
		} else {
			placeholders.GoogleDriveURL = safeNotificationURL(conf.GoogleDrive.URL)
		}
		placeholders.IsCommentOnly = elem.IsCommentOnly
		placeholders.IsAssignNotification = elem.IsAssignNotification
		placeholders.CommentOnlyMessage = localizedCommentOnlyMessage(currentStatus, notifLang)
		if normalizeNotificationLang(notifLang) == "en" {
			placeholders.CommentLabel = "Comment"
			placeholders.CommentAuthorLabel = "Comment by"
			placeholders.AssigneeLabel = "Assignee"
			placeholders.LinksLabel = "Links"
		} else {
			placeholders.CommentLabel = "コメント"
			placeholders.CommentAuthorLabel = "コメント投稿者"
			placeholders.AssigneeLabel = "担当"
			placeholders.LinksLabel = "リンク"
		}
		if elem.IsCommentOnly {
			placeholders.StatusTransitionMessage = ""
		} else {
			placeholders.StatusTransitionMessage = localizedStatusTransitionMessage(elem.PreviousStatusName, elem.TaskStatus.ShortName, notifLang)
		}

		// TaskURL を組み立て
		category := "assets"
		lowEntityType := strings.ToLower(placeholders.EntityType)
		if strings.Contains(lowEntityType, "shot") || strings.Contains(lowEntityType, "sequence") || strings.Contains(lowEntityType, "episode") {
			category = "shots"
		}
		host := safeNotificationURL(conf.Kitsu.Hostname)
		if host != "" {
			if !strings.HasSuffix(host, "/") {
				host += "/"
			}
			// ショット一覧 or アセット一覧に飛ぶ
			placeholders.TaskURL = fmt.Sprintf("%sproductions/%s/%s", host, url.PathEscape(elem.Project.ID), category)
		}

		// Kitsu プレビュー画像 URL を組み立て
		// Entity.PreviewFileID は interface{} なので string にキャストする
		// パス: /api/pictures/thumbnails/preview-files/{id}.png
		// nginx で認証バイパス設定が必要（README 参照）
		if id, ok := elem.Entity.Entity.PreviewFileID.(string); ok && id != "" {
			if host != "" {
				placeholders.PreviewImageURL = fmt.Sprintf("%sapi/pictures/thumbnails/preview-files/%s.png", host, url.PathEscape(id))
			}
		}

		// カラーコードを変換（ステータスカラー → フォールバック用）
		intColor := parseKitsuStatusColor(elem.TaskStatus.Color)

		// テンプレートを展開
		tplPreset := localizedTemplatePreset(conf.TplPreset, notifLang)

		placeholders.NotificationLanguage = notifLang
		placeholders.AllowedUserIDs = recipientIDs
		placeholders.Color = int(intColor)
		payload := RenderNotificationPayload(placeholders, tplPreset)

		// スレッドモード: 既存スレッドに返信 or 新規スレッドを作成
		prevThreadID := previousThreadIDs[elem.Task.ID]
		threadName := ""
		if conf.Discord.UseThreads && prevThreadID == "" {
			// 初回のみスレッド名を生成（スレッド作成）
			threadName = fmt.Sprintf("%s %s/%s - %s",
				placeholders.ProcessEmoji,
				placeholders.ParentName,
				placeholders.TaskName,
				placeholders.TaskType)
		}

		// まず新規メッセージを送信。送信成功時のみ旧メッセージを削除する
		// （順序が逆だと、新規送信失敗時に Discord 上の履歴が消失する）
		newResult := SendMessage(payload, webHookURL, prevThreadID, threadName)
		if newResult.MessageID == "" {
			slog.Warn("SendMessage failed; preserving prior delivery state",
				"taskID", elem.Task.ID,
				"category", newResult.FailureCategory,
				"retryable", newResult.Retryable,
				"unknown", newResult.Unknown)
			// Return failures to the orchestration layer so permanent and
			// unknown outcomes can be recorded without claiming delivery.
			result[elem.Task.ID] = newResult
			continue
		}

		// 送信成功 → 旧メッセージを削除
		// スレッドモード時はスレッド内の古いメッセージだけ消す（スレッド自体は残す）
		if prevMsgID, ok := previousMessageIDs[elem.Task.ID]; ok && prevMsgID != "" {
			prevWebhook := previousWebhookURLs[elem.Task.ID]
			if prevWebhook == "" {
				prevWebhook = webHookURL
			}
			DeleteMessage(prevWebhook, prevMsgID, prevThreadID)
		}

		result[elem.Task.ID] = newResult
	}

	return result
}

func parseTaskTemplate(tplFilePath string, data Template) string {
	tpl, err := os.ReadFile(tplFilePath)
	if err != nil {
		return ""
	}
	t := template.Must(template.New("template").Funcs(template.FuncMap{
		"json": func(value string) string {
			encoded, _ := json.Marshal(value)
			return strings.Trim(string(encoded), "\"")
		},
	}).Parse(string(tpl)))
	output := new(bytes.Buffer)
	t.Execute(output, data)
	return strings.TrimSpace(output.String())
}

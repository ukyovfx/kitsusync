package discord

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/config"
	"bytes"
	"encoding/json"
	"errors"
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
	ChannelName             string // 通知先の Discord チャンネル名（ルーティング確認用）
	NotificationLanguage    string
	CardContext             string // optional secondary context such as a test-notification marker
	AllowedUserIDs          []string
	AllowedRoleIDs          []string
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

// UserMapResolver は Kitsu Person identity → Discord ID を解決するフック。
// main 起動時に DB バックエンドの実装を注入する。
// nil の場合は conf.Mention.UserMap にフォールバックする。
// projectID はプロジェクトスコープ検索に使用; kitsuEmail はリネーム時のフォールバック検索に使用（空でも可）。
var UserMapResolver func(projectID, kitsuPersonID, kitsuName, kitsuEmail string) string

// CheckerResolver はタスクタイプ ID / 名前 → チェッカーの Discord ID 一覧を解決するフック。
// nil の場合は conf.Mention.Checkers にフォールバックする。
// projectID はプロジェクトスコープ検索に使用する。
var CheckerResolver func(projectID, taskTypeID, taskTypeName string) []string

// ReviewerResolver supplies the fully validated WFA Reviewer recipient union.
type ReviewerTarget struct {
	Kind string
	ID   string
}

var ReviewerResolver func(projectID, taskTypeID, taskTypeName, senderWebhookURL string) ([]ReviewerTarget, error)

// GoogleDriveURLResolver はプロジェクト ID に対応するファイルストレージ URL を返すフック。
// プロジェクトごとの URL を DB から引く。nil または空文字の場合は conf.GoogleDrive.URL にフォールバック。
var GoogleDriveURLResolver func(projectID string) string

// KitsuPublicURLResolver supplies the user-reachable Kitsu base URL for
// notification links. It is separate from the runtime endpoint, which may
// use container-only addressing.
var KitsuPublicURLResolver func() string

// SendResult は SendMessage / SendMessageBunch の戻り値
type SendResult struct {
	MessageID        string    // Discord メッセージID
	ThreadID         string    // Discord スレッドID（UseThreads=true 時のみ）
	FailureCategory  string    // safe category for a failed delivery
	RetryAfterAt     time.Time // persisted lower bound for a definitive Discord 429 retry
	Retryable        bool      // safe to retry automatically on the next poll
	Unknown          bool      // the request may have been accepted without a response
	Durable          bool      // Discord acceptance and delivery checkpoint committed atomically
	AlreadyDelivered bool      // deterministic event already has a committed delivery record
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
		return "修正作業に戻りました"
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
		return "作業完了・承認されました"
	case "WIP->WFA":
		if normalizeNotificationLang(lang) == "en" {
			return "Ready for review"
		}
		return "チェック依頼が送られました"
	case "WFA->DONE":
		if normalizeNotificationLang(lang) == "en" {
			return "Review completed"
		}
		return "レビューが完了しました"
	case "RETAKE->WIP":
		if normalizeNotificationLang(lang) == "en" {
			return "Revision work has started"
		}
		return "リテイク対応を開始しました"
	case "WIP->RETAKE":
		if normalizeNotificationLang(lang) == "en" {
			return "A retake was requested"
		}
		return "リテイクが入りました"
	case "TODO->WIP":
		if normalizeNotificationLang(lang) == "en" {
			return "Work has started"
		}
		return "作業を開始しました"
	case "NONE->WIP":
		if normalizeNotificationLang(lang) == "en" {
			return "Work has started"
		}
		return "作業を開始しました"
	case "TODO->DONE", "NONE->DONE", "WIP->DONE":
		if normalizeNotificationLang(lang) == "en" {
			return "Work completed and approved"
		}
		return "作業完了・承認されました"
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

// notificationStatusColor uses restrained semantic accents for the three
// operator-facing review states. Kitsu's raw status color remains available
// for diagnostics, but notification cards use a stable visual vocabulary.
func notificationStatusColor(status string) int {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "WFA":
		return 0xD4A72C
	case "RETAKE":
		return 0xC45656
	case "DONE":
		return 0x4FAF78
	default:
		return neutralEmbedColor
	}
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

func uniqueDiscordRoleIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if len(id) < 17 || len(id) > 20 || !isValidDiscordUserID(id) {
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

func uniqueReviewerTargets(targets []ReviewerTarget) []ReviewerTarget {
	seen := make(map[string]struct{}, len(targets))
	result := make([]ReviewerTarget, 0, len(targets))
	for _, target := range targets {
		id := strings.TrimSpace(target.ID)
		if target.Kind != "user" && target.Kind != "role" || len(id) < 17 || len(id) > 20 || !isValidDiscordUserID(id) {
			continue
		}
		key := target.Kind + ":" + id
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, ReviewerTarget{Kind: target.Kind, ID: id})
		if len(result) >= maxNotificationRecipients {
			break
		}
	}
	return result
}

func reviewerTargetMentions(targets []ReviewerTarget) string {
	mentions := make([]string, 0, len(targets))
	for _, target := range targets {
		switch target.Kind {
		case "user":
			mentions = append(mentions, "<@"+target.ID+">")
		case "role":
			mentions = append(mentions, "<@&"+target.ID+">")
		}
	}
	return strings.Join(mentions, " ")
}

func mentionContent(ids []string) string {
	ids = uniqueDiscordIDs(ids)
	mentions := make([]string, len(ids))
	for i, id := range ids {
		mentions[i] = "<@" + id + ">"
	}
	return strings.Join(mentions, " ")
}

func kitsuPersonDisplayName(person kitsu.Person, lang string) string {
	first := strings.TrimSpace(person.FirstName)
	last := strings.TrimSpace(person.LastName)
	if normalizeNotificationLang(lang) == "ja" && first != "" && last != "" {
		return sanitizeDiscordText(last + " " + first)
	}
	if normalizeNotificationLang(lang) == "en" && first != "" && last != "" && strings.TrimSpace(person.FullName) == "" {
		return sanitizeDiscordText(first + " " + last)
	}
	return sanitizeDiscordText(person.FullName)
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
		return uniqueDiscordIDs(assignees)
	default:
		return nil
	}
}

func resolveAssigneeDiscordID(projectID string, person kitsu.Person, displayName string, conf config.Config) string {
	if UserMapResolver != nil {
		if id := UserMapResolver(projectID, person.ID, person.FullName, person.Email); id != "" {
			return id
		}
	}
	for _, entry := range conf.Mention.UserMap {
		if strings.EqualFold(strings.TrimSpace(entry.KitsuName), strings.TrimSpace(person.FullName)) || strings.EqualFold(strings.TrimSpace(entry.KitsuName), strings.TrimSpace(displayName)) {
			return entry.DiscordID
		}
	}
	return ""
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

func safePublicNotificationURL(raw string) string {
	raw = safeNotificationURL(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	// Local/private addresses can be intentional human-facing Kitsu URLs in
	// OSS deployments. Only reject the container-only address synthesized by
	// KitsuSync's runtime normalization.
	host := strings.ToLower(parsed.Hostname())
	if host == "host.docker.internal" {
		return ""
	}
	return raw
}

// KitsuTaskURL builds the task route used by the Kitsu frontend. The route
// shape is intentionally limited to entity types whose task pages are part of
// the verified frontend router; unknown entity types fail closed.
func KitsuTaskURL(host, productionID, entityType, taskID string) string {
	base := safePublicNotificationURL(host)
	productionID = strings.TrimSpace(productionID)
	taskID = strings.TrimSpace(taskID)
	if base == "" || productionID == "" || taskID == "" {
		return ""
	}
	var routeType string
	switch strings.ToLower(strings.TrimSpace(entityType)) {
	case "shot":
		routeType = "shots"
	case "asset":
		routeType = "assets"
	default:
		return ""
	}
	joined, err := url.JoinPath(base, "productions", productionID, routeType, "tasks", taskID)
	if err != nil {
		return ""
	}
	return joined
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
		slog.Warn("DeleteMessage: failed to build request", "error_class", "request_invalid", "messageID", messageID)
		return false
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("DeleteMessage: request failed", "error_class", "network_error", "messageID", messageID)
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
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
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
// 429 Rate Limit のみ自動リトライ（最大3回）する。POST の transport
// error と 5xx は Discord が受理した可能性があるため再送しない。
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

	// リトライループ: 429 Rate Limit のみ最大3回まで再試行
	const maxRetries = 3
	var respBody []byte
	var statusCode int
	for attempt := 0; attempt <= maxRetries; attempt++ {
		var retryAfterSec float64
		respBody, statusCode, retryAfterSec, err = sendMessageOnce(body, reqURL)
		if err != nil {
			slog.Error("SendMessage: HTTP post failed", "category", "network_error", "attempt", attempt+1)
			return SendResult{FailureCategory: "network_error", Unknown: true}
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
			if wait > maxRateLimitWait || attempt == maxRetries {
				slog.Warn("SendMessage: persisting rate-limit retry deadline",
					"retryAfterSec", retryAfterSec, "capSec", maxRateLimitWait.Seconds(), "attempt", attempt+1)
				return SendResult{FailureCategory: "rate_limited", Retryable: true, RetryAfterAt: time.Now().UTC().Add(wait)}
			}
			slog.Warn("SendMessage: rate limited, waiting",
				"retryAfterSec", retryAfterSec, "attempt", attempt+1)
			time.Sleep(wait)
			continue
		}
		break // 成功 or リトライ不要なエラー
	}

	if statusCode < 200 || statusCode >= 300 {
		slog.Error("SendMessage: non-2xx response",
			"status", statusCode,
			"category", discordHTTPErrorCategory(statusCode))
		return SendResult{FailureCategory: discordHTTPErrorCategory(statusCode), Retryable: statusCode == http.StatusTooManyRequests, Unknown: statusCode >= 500 || statusCode >= 300 && statusCode < 400}
	}

	var msg DiscordMessage
	if err := json.Unmarshal(respBody, &msg); err != nil {
		slog.Error("SendMessage: unmarshal failed", "err", err)
		return SendResult{FailureCategory: "unknown_response", Unknown: true}
	}
	if strings.TrimSpace(msg.ID) == "" {
		slog.Error("SendMessage: response has no message ID")
		return SendResult{FailureCategory: "unknown_response", Unknown: true}
	}
	if threadName != "" && strings.TrimSpace(msg.ChannelID) == "" {
		slog.Error("SendMessage: thread response has no channel ID")
		return SendResult{FailureCategory: "unknown_response", Unknown: true}
	}

	result := SendResult{MessageID: msg.ID}
	// スレッド新規作成時: channel_id がスレッドID になる
	if threadName != "" {
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

// DiscordWebhookIDFromURL extracts only the non-secret webhook snowflake.
// Tokens are never returned, formatted in errors, or written to the journal.
func DiscordWebhookIDFromURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("managed webhook URL is invalid")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost")) {
		return "", fmt.Errorf("managed webhook URL scheme is not trusted")
	}
	if parsed.Scheme == "https" && !strings.EqualFold(parsed.Hostname(), "discord.com") {
		return "", fmt.Errorf("managed webhook host is not trusted")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "webhooks" && strings.TrimSpace(parts[i+1]) != "" && strings.TrimSpace(parts[i+2]) != "" {
			return parts[i+1], nil
		}
	}
	return "", fmt.Errorf("managed webhook identity is incomplete")
}

// VerifyWebhookIdentity performs a read-only credentialed GET to the stored
// webhook URL and checks exact webhook, Guild, and channel identity. Redirects
// are forbidden so an untrusted endpoint cannot receive the webhook token.
func VerifyWebhookIdentity(raw, expectedWebhookID, expectedGuildID, expectedChannelID string) error {
	webhookID, err := DiscordWebhookIDFromURL(raw)
	if err != nil || webhookID != strings.TrimSpace(expectedWebhookID) || expectedWebhookID == "" || expectedGuildID == "" || expectedChannelID == "" {
		return fmt.Errorf("managed webhook identity is not verifiable")
	}
	request, err := http.NewRequest(http.MethodGet, strings.TrimSpace(raw), nil)
	if err != nil {
		return fmt.Errorf("managed webhook identity read could not be prepared")
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("managed webhook identity read failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("managed webhook identity read returned HTTP %d", response.StatusCode)
	}
	var identity DiscordMessageWebhookIdentity
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&identity); err != nil {
		return fmt.Errorf("managed webhook identity response is invalid")
	}
	if identity.Type != 1 || strings.TrimSpace(identity.ID) != expectedWebhookID || strings.TrimSpace(identity.GuildID) != expectedGuildID || strings.TrimSpace(identity.ChannelID) != expectedChannelID {
		return fmt.Errorf("managed webhook identity does not match the saved destination")
	}
	return nil
}

func sendDurableNotification(db *gorm.DB, elem kitsu.MessagePayload, payload Payload, webhookURL, threadName, notificationRevision string) SendResult {
	webhookID, err := DiscordWebhookIDFromURL(webhookURL)
	if err != nil {
		return SendResult{FailureCategory: "unverified_destination"}
	}
	destination, err := model.ResolveNotificationDestination(db, elem.Project.ID, elem.TaskType.ID, webhookURL, webhookID)
	if err != nil {
		return SendResult{FailureCategory: "unverified_destination"}
	}
	previous, err := model.LatestDeliveredNotificationForDestination(db, elem.Task.ID, destination)
	if err != nil {
		return SendResult{FailureCategory: "delivery_state_read_failed"}
	}
	priorMessageID, priorThreadID := "", ""
	if previous != nil {
		priorMessageID, priorThreadID = previous.DiscordMessageID, previous.DiscordThreadID
	}
	if threadName != "" && priorThreadID != "" {
		threadName = ""
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return SendResult{FailureCategory: "invalid_payload"}
	}
	intent := model.NotificationDeliveryIntent{
		TaskID: elem.Task.ID, ProductionID: elem.Project.ID, TaskTypeID: elem.TaskType.ID,
		TaskStatus: strings.ToUpper(strings.TrimSpace(elem.TaskStatus.ShortName)), TaskUpdatedAt: elem.Task.UpdatedAt,
		CommentID: elem.LatestComment.Comment.ID, CommentUpdatedAt: elem.LatestComment.Comment.UpdatedAt,
		IsAssignNotification: elem.IsAssignNotification, Payload: payloadBytes, WebhookRowID: destination.WebhookRowID,
		GuildID: destination.GuildID, ChannelID: destination.ChannelID, DiscordWebhookID: destination.WebhookID,
		NotificationRevision: notificationRevision,
		ThreadName:           threadName, PriorMessageID: priorMessageID, PriorThreadID: priorThreadID,
	}
	event, err := model.PrepareNotificationDelivery(db, intent)
	if err != nil {
		return SendResult{FailureCategory: "delivery_intent_persist_failed", Unknown: errors.Is(err, model.ErrDeliveryIdentityConflict)}
	}
	return dispatchPreparedNotification(db, *event, webhookURL)
}

func dispatchPreparedNotification(db *gorm.DB, event model.NotificationDeliveryEvent, webhookURL string) SendResult {
	webhookID, err := DiscordWebhookIDFromURL(webhookURL)
	if err != nil || webhookID != event.DestinationWebhookID {
		if stateErr := model.SetUnstartedNotificationDeliveryDisposition(db, event.EventID, model.DeliveryTerminalFailure, "webhook_identity_changed"); stateErr != nil {
			return SendResult{FailureCategory: "delivery_state_persist_failed", Unknown: true}
		}
		return SendResult{FailureCategory: "unverified_destination"}
	}
	destination, err := model.ResolveNotificationDestination(db, event.ProductionID, event.TaskTypeID, webhookURL, webhookID)
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (destination.WebhookRowID != event.WebhookRowID || destination.GuildID != event.DestinationGuildID || destination.ChannelID != event.DestinationChannelID) {
		if stateErr := model.SetUnstartedNotificationDeliveryDisposition(db, event.EventID, model.DeliveryTerminalFailure, "destination_changed"); stateErr != nil {
			return SendResult{FailureCategory: "delivery_state_persist_failed", Unknown: true}
		}
		return SendResult{FailureCategory: "unverified_destination"}
	}
	if err != nil {
		return SendResult{FailureCategory: "unverified_destination"}
	}
	if err := VerifyWebhookIdentity(webhookURL, event.DestinationWebhookID, event.DestinationGuildID, event.DestinationChannelID); err != nil {
		return SendResult{FailureCategory: "unverified_destination", Retryable: true}
	}
	if event.Disposition == model.DeliveryDelivered {
		return SendResult{Durable: true, AlreadyDelivered: true, MessageID: event.DiscordMessageID, ThreadID: event.DiscordThreadID}
	}
	attempt, started, err := model.BeginNotificationDeliveryForEvent(db, event, time.Now().UTC())
	if err != nil || !started {
		return SendResult{FailureCategory: "delivery_not_dispatchable", Unknown: event.Disposition == model.DeliveryInFlight || event.Disposition == model.DeliveryUnknown}
	}
	var persisted Payload
	if err := json.Unmarshal([]byte(attempt.Event.PayloadJSON), &persisted); err != nil {
		if stateErr := model.SetNotificationDeliveryDisposition(db, *attempt, model.DeliveryTerminalFailure, "invalid_persisted_payload"); stateErr != nil {
			return SendResult{FailureCategory: "delivery_state_persist_failed", Unknown: true}
		}
		return SendResult{FailureCategory: "invalid_persisted_payload"}
	}
	result := SendMessage(persisted, webhookURL, attempt.Event.PriorThreadID, attempt.Event.ThreadName)
	if result.MessageID == "" {
		disposition := model.DeliveryTerminalFailure
		if result.Unknown {
			disposition = model.DeliveryUnknown
		} else if result.Retryable {
			disposition = model.DeliveryRetryable
		}
		var stateErr error
		if disposition == model.DeliveryRetryable && !result.RetryAfterAt.IsZero() {
			stateErr = model.SetNotificationDeliveryRetryAfter(db, *attempt, result.FailureCategory, result.RetryAfterAt)
		} else {
			stateErr = model.SetNotificationDeliveryDisposition(db, *attempt, disposition, result.FailureCategory)
		}
		if stateErr != nil {
			return SendResult{FailureCategory: "delivery_state_persist_failed", Unknown: true}
		}
		return result
	}
	if err := model.CompleteNotificationDelivery(db, *attempt, result.MessageID, result.ThreadID, time.Now().UTC()); err != nil {
		if quarantineErr := model.QuarantineAcceptedButUnpersisted(db, *attempt); quarantineErr != nil {
			slog.Error("Accepted Discord delivery requires reconciliation", "error_class", "accepted_delivery_checkpoint_uncommitted", "event", attempt.Event.EventID[:12], "quarantine_state", "durable_attempt_retained")
		}
		return SendResult{FailureCategory: "delivery_persistence_failed", Unknown: true}
	}
	result.Durable = true
	if attempt.Event.PriorMessageID != "" {
		cleanup, claimed, claimErr := model.BeginPriorMessageCleanup(db, attempt.Event.EventID)
		if claimErr != nil || !claimed || cleanup == nil {
			slog.Warn("Prior notification cleanup requires review", "error_class", "prior_message_cleanup_claim_failed", "event", attempt.Event.EventID[:12])
		} else {
			deleted := DeleteMessage(webhookURL, cleanup.PriorMessageID, cleanup.PriorThreadID)
			if err := model.RecordPriorMessageCleanup(db, attempt.Event.EventID, deleted, map[bool]string{true: "", false: "prior_message_cleanup_failed"}[deleted]); err != nil || !deleted {
				slog.Warn("Prior notification cleanup requires review", "error_class", "prior_message_cleanup_failed", "event", attempt.Event.EventID[:12])
			}
		}
	}
	return result
}

// RetryPriorMessageCleanup uses only the exact webhook identity recorded for
// the delivered event. It never follows a newly configured route to another
// channel or webhook.
func RetryPriorMessageCleanup(db *gorm.DB, limit int) {
	if db == nil {
		return
	}
	var events []model.NotificationDeliveryEvent
	query := db.Where("disposition = ? AND prior_cleanup_pending = ? AND prior_cleanup_in_flight = ?", model.DeliveryDelivered, true, false).Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&events).Error; err != nil {
		slog.Error("Notification cleanup journal read failed", "error_class", "delivery_cleanup_read_failed")
		return
	}
	for _, candidate := range events {
		event, claimed, claimErr := model.BeginPriorMessageCleanup(db, candidate.EventID)
		if claimErr != nil || !claimed || event == nil {
			continue
		}
		webhook := model.FindProjectWebhookByID(db, event.WebhookRowID)
		if webhook == nil || webhook.DiscordChannelID != event.DestinationChannelID {
			_ = model.RecordPriorMessageCleanup(db, event.EventID, false, "destination_identity_changed")
			continue
		}
		webhookID, err := DiscordWebhookIDFromURL(webhook.WebhookURL)
		if err != nil || webhookID != event.DestinationWebhookID || VerifyWebhookIdentity(webhook.WebhookURL, event.DestinationWebhookID, event.DestinationGuildID, event.DestinationChannelID) != nil {
			if stateErr := model.RecordPriorMessageCleanup(db, event.EventID, false, "prior_webhook_verification_failed"); stateErr != nil {
				slog.Warn("Prior notification cleanup claim could not be released", "error_class", "delivery_cleanup_state_write_failed", "event", event.EventID[:12])
			}
			continue
		}
		deleted := DeleteMessage(webhook.WebhookURL, event.PriorMessageID, event.PriorThreadID)
		failure := ""
		if !deleted {
			failure = "prior_message_cleanup_failed"
		}
		if err := model.RecordPriorMessageCleanup(db, event.EventID, deleted, failure); err != nil {
			slog.Warn("Prior notification cleanup state update failed", "error_class", "delivery_cleanup_state_write_failed", "event", event.EventID[:12])
		}
	}
}

type DiscordMessageWebhookIdentity struct {
	ID        string `json:"id"`
	Type      int    `json:"type"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
}

// SendMessageBunch は各タスクを処理して SendResult のマップを返す
// キーはタスクID
func SendMessageBunch(conf config.Config, data []kitsu.MessagePayload, webHookURL string, previousMessageIDs map[string]string, previousWebhookURLs map[string]string, previousThreadIDs map[string]string, projectNotificationLanguages map[string]string, db *gorm.DB) map[string]SendResult {
	result := make(map[string]SendResult)

	for _, elem := range data {
		var placeholders Template
		notifLang := normalizeNotificationLang(projectNotificationLanguages[elem.Project.ID])
		notificationRevision := ""
		if db == nil {
			result[elem.Task.ID] = SendResult{FailureCategory: "notification_state_unavailable"}
			continue
		}
		project := model.FindProjectByKitsuID(db, elem.Project.ID)
		if project == nil {
			result[elem.Task.ID] = SendResult{FailureCategory: "notification_state_unavailable"}
			continue
		}
		var revisionErr error
		notificationRevision, revisionErr = model.ProductionNotificationRevisionWithError(db, project.ID, elem.Project.ID)
		if revisionErr != nil {
			result[elem.Task.ID] = SendResult{FailureCategory: "notification_state_unavailable"}
			continue
		}

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
		var assigneeIDs []string
		placeholders.Assignees = make([]Assignee, len(elem.Assignees))
		for i := 0; i < len(elem.Assignees); i++ {
			if elem.Assignees[i].IsBot {
				slog.Info("Kitsu bot assignee excluded from Discord mention", "taskID", elem.Task.ID)
				continue
			}
			fullName := elem.Assignees[i].FullName
			displayPersonName := kitsuPersonDisplayName(elem.Assignees[i], notifLang)
			if displayPersonName == "" {
				displayPersonName = sanitizeDiscordText(fullName)
			}
			placeholders.Assignees[i].Fullname = displayPersonName
			placeholders.Assignees[i].Email = elem.Assignees[i].Email
			displayName := displayPersonName
			assigneeNames = append(assigneeNames, displayName)
			if id := resolveAssigneeDiscordID(elem.Project.ID, elem.Assignees[i], displayPersonName, conf); id != "" {
				assigneeIDs = append(assigneeIDs, id)
			}
		}
		if len(assigneeNames) > 0 {
			placeholders.AssigneesStr = strings.Join(assigneeNames, ", ")
		} else if normalizeNotificationLang(notifLang) == "en" {
			placeholders.AssigneesStr = "Unassigned"
		} else {
			placeholders.AssigneesStr = "未割り当て"
		}

		currentStatus := strings.ToUpper(elem.TaskStatus.ShortName)

		// WFA status notifications use the Reviewer precedence chain. Assignment
		// and other status notifications keep the existing Checker resolution.
		var checkerIDs []string
		var reviewerTargets []ReviewerTarget
		isWFAReviewer := !elem.IsAssignNotification && currentStatus == "WFA"
		if isWFAReviewer {
			if ReviewerResolver != nil {
				rawTargets, resolveErr := ReviewerResolver(elem.Project.ID, elem.TaskType.ID, elem.TaskType.Name, webHookURL)
				if resolveErr != nil {
					slog.Warn("Reviewer lookup failed; WFA will be posted without targeted mentions",
						"error_class", "reviewer_resolution_failed",
						"taskType", elem.TaskType.Name,
						"taskID", elem.Task.ID,
					)
				} else {
					reviewerTargets = uniqueReviewerTargets(rawTargets)
				}
			}
		} else if CheckerResolver != nil {
			if dids := CheckerResolver(elem.Project.ID, elem.TaskType.ID, elem.TaskType.Name); len(dids) > 0 {
				for _, did := range dids {
					checkerIDs = append(checkerIDs, did)
				}
			}
		}
		if !isWFAReviewer && len(checkerIDs) == 0 && len(reviewerTargets) == 0 {
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
		var recipientIDs []string
		var recipientRoleIDs []string
		if isWFAReviewer {
			for _, target := range reviewerTargets {
				if target.Kind == "user" {
					recipientIDs = append(recipientIDs, target.ID)
				} else if target.Kind == "role" {
					recipientRoleIDs = append(recipientRoleIDs, target.ID)
				}
			}
			recipientIDs = uniqueDiscordIDs(recipientIDs)
			recipientRoleIDs = uniqueDiscordRoleIDs(recipientRoleIDs)
		} else {
			recipientIDs = notificationRecipientCandidates(currentStatus, elem.IsAssignNotification, assigneeIDs, checkerIDs, conf)
		}
		if !isWFAReviewer && len(recipientIDs) == 0 && len(recipientRoleIDs) == 0 && containsIgnoreCase(conf.Mention.CheckerStatuses, currentStatus) && len(conf.Mention.Checkers) > 0 {
			slog.Warn("No checker configured for task type; checker will not be @-mentioned",
				"taskType", elem.TaskType.Name,
				"status", currentStatus,
				"taskID", elem.Task.ID,
				"hint", "add this task type to [[mention.checkers]] in conf.toml")
		}
		// 緊急ステータスは @here でチャンネル全員に通知
		// @here and @everyone are never emitted by KitsuSync.
		mentionText := mentionContent(recipientIDs)
		if isWFAReviewer {
			mentionText = reviewerTargetMentions(reviewerTargets)
		}
		statusMessage, statusEmoji := localizedStatusMessageInfo(currentStatus, notifLang)

		placeholders.MentionContent = mentionText
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
		} else {
			placeholders.CommentLabel = "コメント"
			placeholders.CommentAuthorLabel = "コメント投稿者"
			placeholders.AssigneeLabel = "担当"
		}
		if elem.IsCommentOnly {
			placeholders.StatusTransitionMessage = ""
		} else {
			placeholders.StatusTransitionMessage = localizedStatusTransitionMessage(elem.PreviousStatusName, elem.TaskStatus.ShortName, notifLang)
		}

		host := ""
		if KitsuPublicURLResolver != nil {
			host = safeNotificationURL(KitsuPublicURLResolver())
		}
		placeholders.TaskURL = KitsuTaskURL(host, elem.Project.ID, placeholders.EntityType, elem.Task.ID)

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
		intColor := notificationStatusColor(elem.TaskStatus.ShortName)

		// テンプレートを展開
		tplPreset := localizedTemplatePreset(conf.TplPreset, notifLang)

		placeholders.NotificationLanguage = notifLang
		placeholders.AllowedUserIDs = uniqueDiscordIDs(recipientIDs)
		placeholders.AllowedRoleIDs = uniqueDiscordRoleIDs(recipientRoleIDs)
		placeholders.Color = int(intColor)
		payload := RenderNotificationPayload(placeholders, tplPreset)

		threadName := ""
		if conf.Discord.UseThreads {
			// sendDurableNotification selects an existing verified thread if one
			// exists; otherwise this name creates the first thread.
			threadName = fmt.Sprintf("%s %s/%s - %s",
				placeholders.ProcessEmoji,
				placeholders.ParentName,
				placeholders.TaskName,
				placeholders.TaskType)
		}

		newResult := sendDurableNotification(db, elem, payload, webHookURL, threadName, notificationRevision)
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

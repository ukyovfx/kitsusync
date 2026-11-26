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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// Components holds the action rows of an interactive message. An empty
	// list clears the rows on edit, so it is not omitempty.
	Components []ActionRow `json:"components"`
}

// ActionRow is one row of message components; Discord allows five rows per
// message and five buttons per row.
type ActionRow struct {
	Type       int        `json:"type"` // 1 = action row
	Components []Button   `json:"components"`
}

// Button is an interaction button (type 2). A non-empty CustomID makes it
// usable while nothing listens for button presses yet.
type Button struct {
	Type     int    `json:"type"` // 2 = button
	Style    int    `json:"style"`
	Label    string `json:"label"`
	CustomID string `json:"custom_id,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

const (
	ButtonStylePrimary   = 1
	ButtonStyleSecondary = 2
	ButtonStyleSuccess   = 3
	ButtonStyleDanger    = 4
)

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
	IsAssignNotification    bool   // 新規タスクアサイン通知（前回ポール時はステータス none）
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
// projectID はプロジェクトスコープ検索に使用; kitsuEmail はリネーム時のフォールバック検索に使用（空でも可）。
var UserMapResolver func(projectID, kitsuPersonID, kitsuName, kitsuEmail string) string

// ReviewerTarget is one fully validated WFA Reviewer recipient (user or role).
type ReviewerTarget struct {
	Kind string
	ID   string
}

// GoogleDriveURLResolver はプロジェクト ID に対応するファイルストレージ URL を返すフック。
// プロジェクトごとの URL を DB から引く。nil または空文字の場合は conf.GoogleDrive.URL にフォールバック。
var GoogleDriveURLResolver func(projectID string) string

// DiscordBotTokenResolver lazily supplies the runtime bot token to card
// rendering helpers that need it without a db handle.
var DiscordBotTokenResolver func() string

// KitsuPublicURLResolver supplies the user-reachable Kitsu base URL for
// notification links. It is separate from the runtime endpoint, which may
// use container-only addressing.
var KitsuPublicURLResolver func() string

// SendResult は SendMessage / SendDirectMessage の戻り値
type SendResult struct {
	MessageID        string // Discord メッセージID
	ThreadID         string // Discord スレッドID（スレッド内送信時）
	FailureCategory  string // safe category for a failed delivery
	Retryable        bool   // safe to retry automatically on the next poll
	Unknown          bool   // the request may have been accepted without a response
	Durable          bool   // Discord acceptance and delivery checkpoint committed atomically
	AlreadyDelivered bool   // deterministic event already has a committed delivery record
}

func normalizeNotificationLang(lang string) string {
	if strings.EqualFold(strings.TrimSpace(lang), "en") {
		return "en"
	}
	return "ja"
}

// discordLinkPattern matches masked markdown links inside embed text. Their
// visible text can hide the real target (phishing), so the syntax is
// neutralized to plain "text (url)".
var discordLinkPattern = regexp.MustCompile(`\[(.*?)\]\((.*?)\)`)

// discordControlPattern strips control, bidirectional-override and zero-width
// characters that can visually disguise content (U+202E RTL override etc.).
var discordControlPattern = regexp.MustCompile("[\x00-\x08\x0b\x0c\x0e-\x1f\x7f\u0080-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069\ufeff]")

func sanitizeDiscordText(value string) string {
	value = discordControlPattern.ReplaceAllString(value, "")
	// Nested masked links need repeated passes: "[[safe](a)](b)" still hides
	// an outer masked link after one replacement. Every pass removes bracket
	// structure, so the loop reaches a fixed point.
	for {
		neutralized := discordLinkPattern.ReplaceAllString(value, "$1 ($2)")
		if neutralized == value {
			break
		}
		value = neutralized
	}
	value = strings.ReplaceAll(value, "<", "＜")
	value = strings.ReplaceAll(value, ">", "＞")
	value = strings.ReplaceAll(value, "@everyone", "@ everyone")
	value = strings.ReplaceAll(value, "@here", "@ here")
	return value
}

// SanitizeText neutralizes Kitsu-originated text for Discord bodies built
// outside this package (aggregated summaries, digest cards).
func SanitizeText(value string) string {
	return sanitizeDiscordText(value)
}

// discordRateLimitedUntil marks a hard rate-limit window reported by Discord.
// While it is in the future, further sends fail fast as retryable instead of
// sleeping inline, so one rate-limited destination cannot serially stall the
// whole delivery batch; the durable ledger resumes them on the next poll.
var discordRateLimitedUntil atomic.Int64

func discordRateLimitGateActive() bool {
	return discordRateLimitedUntil.Load() > time.Now().UnixNano()
}

func markDiscordRateLimited(wait time.Duration) {
	if wait <= 0 {
		return
	}
	const maxMark = 10 * time.Minute
	if wait > maxMark {
		wait = maxMark
	}
	until := time.Now().Add(wait).UnixNano()
	for {
		current := discordRateLimitedUntil.Load()
		if current >= until || discordRateLimitedUntil.CompareAndSwap(current, until) {
			return
		}
	}
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
			return "Completed.", "✅"
		}
		return "完了しました。", "✅"
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

func kitsuPersonDisplayName(person kitsu.Person, lang string) string {
	// Names keep the order registered in Kitsu; reordering for ja display was
	// dropped (fixed on the Kitsu input side). lang is kept for the signature.
	first := strings.TrimSpace(person.FirstName)
	last := strings.TrimSpace(person.LastName)
	if first != "" && last != "" {
		return sanitizeDiscordText(first + " " + last)
	}
	return sanitizeDiscordText(person.FullName)
}

// ResolveAssigneeDiscordIDs maps Kitsu person IDs to Discord user IDs through
// the project user map.
func ResolveAssigneeDiscordIDs(projectID string, kitsuPersonIDs []string) []string {
	return ResolveAssigneeDiscordIDsWithIdentity(projectID, kitsuPersonIDs, nil)
}

// ResolveAssigneeDiscordIDsWithIdentity is ResolveAssigneeDiscordIDs with the
// identity used for the fallback match. A person the project map cannot resolve
// is left out rather than guessed from the global name map: forum assignee
// signals must not mention whoever happens to share a display name.
func ResolveAssigneeDiscordIDsWithIdentity(projectID string, kitsuPersonIDs []string, identity func(kitsuPersonID string) (name, email string)) []string {
	ids := make([]string, 0, len(kitsuPersonIDs))
	for _, personID := range kitsuPersonIDs {
		if UserMapResolver == nil {
			return nil
		}
		var name, email string
		if identity != nil {
			name, email = identity(personID)
		}
		if id := UserMapResolver(projectID, personID, name, email); id != "" {
			ids = append(ids, id)
		}
	}
	return uniqueDiscordIDs(ids)
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
	if err := validateDiscordWebhookURL(webhookURL); err != nil {
		slog.Warn("DeleteMessage: rejected invalid webhook URL", "error_class", "invalid_webhook_url")
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

// EditMessage replaces a webhook message's content (PATCH). It is used by the
// aggregated-summary lifecycle: a summary whose WFA members resolve is edited
// down to the still-waiting tasks and deleted once none remain.
func EditMessage(webhookURL, messageID, threadID string, payload Payload) bool {
	if webhookURL == "" || messageID == "" {
		return false
	}
	if err := validateDiscordWebhookURL(webhookURL); err != nil {
		slog.Warn("EditMessage: rejected invalid webhook URL", "error_class", "invalid_webhook_url")
		return false
	}
	editURL := fmt.Sprintf("%s/messages/%s", webhookURL, messageID)
	if threadID != "" {
		editURL += "?thread_id=" + url.QueryEscape(threadID)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("EditMessage: failed to marshal payload", "error_class", "request_invalid", "messageID", messageID)
		return false
	}
	req, err := http.NewRequest(http.MethodPatch, editURL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("EditMessage: failed to build request", "error_class", "request_invalid", "messageID", messageID)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("EditMessage: request failed", "error_class", "network_error", "messageID", messageID)
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return true
	}
	slog.Warn("EditMessage: unexpected status", "status", resp.StatusCode, "messageID", messageID)
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
	if err := validateDiscordWebhookURL(webhookURL); err != nil {
		slog.Warn("SendMessage: rejected invalid webhook URL", "error_class", "invalid_webhook_url")
		return SendResult{FailureCategory: "invalid_webhook_url"}
	}
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
			if wait > maxRateLimitWait {
				slog.Warn("SendMessage: rate limit wait exceeds cap; skipping this task until next poll cycle",
					"retryAfterSec", retryAfterSec, "capSec", maxRateLimitWait.Seconds(), "attempt", attempt+1)
				markDiscordRateLimited(wait)
				return SendResult{FailureCategory: "rate_limited", Retryable: true}
			}
			if discordRateLimitGateActive() {
				// Another send already observed a hard rate limit: skip the
				// inline wait so the batch is not serially stalled.
				return SendResult{FailureCategory: "rate_limited", Retryable: true}
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
		if statusCode == http.StatusTooManyRequests {
			// Inline retries exhausted while still rate-limited: mark the
			// window so the rest of the batch fails fast instead of stalling.
			markDiscordRateLimited(30 * time.Second)
		}
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

// discordBotAPI is the Discord Bot API base used for DM delivery. Webhook
// delivery keeps using the stored webhook URL and never touches this API.
// It is a variable so tests can point it at an httptest server.
var discordBotAPI = "https://discord.com/api/v10"

// SetDiscordBotAPIBase overrides the Discord Bot API base and returns the
// previous value. Test hook only.
func SetDiscordBotAPIBase(base string) string {
	previous := discordBotAPI
	discordBotAPI = strings.TrimSpace(base)
	return previous
}

var discordWebhookTestBase string

// SetDiscordWebhookTestBase overrides the allowed webhook URL base (scheme+host)
// for tests pointing at an httptest server, and returns the previous value.
// Test hook only.
func SetDiscordWebhookTestBase(base string) string {
	previous := discordWebhookTestBase
	discordWebhookTestBase = strings.TrimSpace(base)
	return previous
}

var discordWebhookPathRegexp = regexp.MustCompile(`^/api/webhooks/[0-9]+/[^/]+$`)

func validateDiscordWebhookURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("webhook URL is empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}
	if discordWebhookTestBase != "" {
		testURL, testErr := url.Parse(discordWebhookTestBase)
		if testErr == nil && testURL.Scheme != "" && testURL.Host != "" &&
			strings.EqualFold(parsed.Scheme, testURL.Scheme) &&
			strings.EqualFold(parsed.Host, testURL.Host) {
			return nil
		}
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return errors.New("webhook URL must use https")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "discord.com" && host != "discordapp.com" {
		return fmt.Errorf("untrusted webhook host %q", host)
	}
	if parsed.Port() != "" {
		return errors.New("webhook URL must not specify a non-standard port")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("webhook URL must not contain userinfo, query, or fragment")
	}
	if !discordWebhookPathRegexp.MatchString(parsed.Path) {
		return errors.New("webhook URL path must match /api/webhooks/<id>/<token>")
	}
	return nil
}

// ResetDMChannelCache drops every cached DM channel. Test hook only.
func ResetDMChannelCache() {
	dmChannelCache.Range(func(key, _ any) bool {
		dmChannelCache.Delete(key)
		return true
	})
}

// dmChannelCache caches opened DM channel IDs per recipient user ID. Discord
// returns a stable channel ID for the same user pair, so one lookup per
// process lifetime is enough; a 404 on send clears the entry.
var dmChannelCache sync.Map

// OpenDMChannel opens (or reuses) the DM channel between the bot and a user.
// The bot token is never logged and never stored in the journal.
func OpenDMChannel(botToken, recipientID string) (string, error) {
	recipientID = strings.TrimSpace(recipientID)
	if strings.TrimSpace(botToken) == "" || recipientID == "" {
		return "", fmt.Errorf("dm channel request is incomplete")
	}
	if cached, ok := dmChannelCache.Load(recipientID); ok {
		if id, _ := cached.(string); id != "" {
			return id, nil
		}
	}
	body, err := json.Marshal(map[string]string{"recipient_id": recipientID})
	if err != nil {
		return "", fmt.Errorf("dm channel request could not be encoded")
	}
	req, err := http.NewRequest(http.MethodPost, discordBotAPI+"/users/@me/channels", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("dm channel request could not be prepared")
	}
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return "", fmt.Errorf("dm channel request failed")
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("dm channel open returned HTTP %d", resp.StatusCode)
	}
	var opened struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &opened); err != nil || strings.TrimSpace(opened.ID) == "" {
		return "", fmt.Errorf("dm channel response is invalid")
	}
	dmChannelCache.Store(recipientID, strings.TrimSpace(opened.ID))
	return strings.TrimSpace(opened.ID), nil
}

// SendDirectMessage posts one payload to an open DM channel and returns the
// message ID. Webhook-only fields (username/avatar) are stripped because the
// bot message endpoint rejects them. Retry and failure classification mirror
// SendMessage.
func SendDirectMessage(botToken, dmChannelID string, payload Payload) SendResult {
	if strings.TrimSpace(botToken) == "" {
		return SendResult{FailureCategory: "dm_bot_token_missing"}
	}
	if strings.TrimSpace(dmChannelID) == "" {
		return SendResult{FailureCategory: "dm_channel_unavailable"}
	}
	payload.Username, payload.AvatarUrl = "", ""
	body, err := json.Marshal(payload)
	if err != nil {
		return SendResult{FailureCategory: "invalid_payload"}
	}
	req, err := http.NewRequest(http.MethodPost, discordBotAPI+"/channels/"+url.PathEscape(strings.TrimSpace(dmChannelID))+"/messages", bytes.NewReader(body))
	if err != nil {
		return SendResult{FailureCategory: "invalid_payload"}
	}
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")
	const maxAttempts = 3
	var respBody []byte
	var statusCode int
	for attempt := 0; attempt <= maxAttempts; attempt++ {
		resp, err := (&http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			slog.Error("SendDirectMessage: request failed", "category", "network_error", "attempt", attempt+1)
			return SendResult{FailureCategory: "network_error", Unknown: true}
		}
		respBody, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		statusCode = resp.StatusCode
		resp.Body.Close()
		if err != nil {
			return SendResult{FailureCategory: "unknown_response", Unknown: true}
		}
		if statusCode != http.StatusTooManyRequests {
			break
		}
		wait := time.Second
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if v, e := strconv.ParseFloat(ra, 64); e == nil && v > 0 {
				wait = time.Duration(v * float64(time.Second))
			}
		}
		const maxRateLimitWait = 30 * time.Second
		if wait > maxRateLimitWait {
			markDiscordRateLimited(wait)
			return SendResult{FailureCategory: "rate_limited", Retryable: true}
		}
		if discordRateLimitGateActive() {
			// Another send already observed a hard rate limit: skip the
			// inline wait so the batch is not serially stalled.
			return SendResult{FailureCategory: "rate_limited", Retryable: true}
		}
		time.Sleep(wait)
		// Rebuild the reader consumed by the previous attempt.
		req, err = http.NewRequest(http.MethodPost, discordBotAPI+"/channels/"+url.PathEscape(strings.TrimSpace(dmChannelID))+"/messages", bytes.NewReader(body))
		if err != nil {
			return SendResult{FailureCategory: "invalid_payload"}
		}
		req.Header.Set("Authorization", "Bot "+botToken)
		req.Header.Set("Content-Type", "application/json")
	}
	if statusCode < 200 || statusCode >= 300 {
		slog.Error("SendDirectMessage: non-2xx response", "status", statusCode, "category", discordHTTPErrorCategory(statusCode))
		if statusCode == http.StatusNotFound {
			// The cached DM channel disappeared; drop it so the next attempt reopens.
			return SendResult{FailureCategory: "dm_channel_unavailable"}
		}
		if statusCode == http.StatusTooManyRequests {
			// Inline retries exhausted while still rate-limited: mark the
			// window so the rest of the batch fails fast instead of stalling.
			markDiscordRateLimited(30 * time.Second)
		}
		return SendResult{FailureCategory: discordHTTPErrorCategory(statusCode), Retryable: statusCode == http.StatusTooManyRequests, Unknown: statusCode >= 500}
	}
	var msg DiscordMessage
	if err := json.Unmarshal(respBody, &msg); err != nil || strings.TrimSpace(msg.ID) == "" {
		return SendResult{FailureCategory: "unknown_response", Unknown: true}
	}
	return SendResult{MessageID: msg.ID}
}

// EditBotMessage replaces the content and components of a message through the
// bot API. An edit always states embeds and components explicitly so the old
// card cannot survive next to the new one.
func EditBotMessage(botToken, channelID, messageID string, payload Payload) bool {
	if strings.TrimSpace(botToken) == "" || strings.TrimSpace(channelID) == "" || strings.TrimSpace(messageID) == "" {
		return false
	}
	payload.Username, payload.AvatarUrl = "", ""
	if payload.Embeds == nil {
		payload.Embeds = []Embed{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	endpoint := discordBotAPI + "/channels/" + url.PathEscape(strings.TrimSpace(channelID)) + "/messages/" + url.PathEscape(strings.TrimSpace(messageID))
	req, err := http.NewRequest(http.MethodPatch, endpoint, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("EditBotMessage: non-2xx response", "status", resp.StatusCode)
		return false
	}
	return true
}


// NotificationCardTemplate builds the card template for one task: assignee
// names, labels, links and colors. The forum first message shares it so the
// forum post renders the identical card.
func NotificationCardTemplate(conf config.Config, elem kitsu.MessagePayload, webHookURL, notifLang string, db *gorm.DB) (Template, error) {
	var placeholders Template
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

	var assigneeNames []string
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
		assigneeNames = append(assigneeNames, displayPersonName)
	}
	if len(assigneeNames) > 0 {
		placeholders.AssigneesStr = strings.Join(assigneeNames, ", ")
	} else if normalizeNotificationLang(notifLang) == "en" {
		placeholders.AssigneesStr = "Unassigned"
	} else {
		placeholders.AssigneesStr = "未割り当て"
	}

	currentStatus := strings.ToUpper(elem.TaskStatus.ShortName)

	statusMessage, statusEmoji := localizedStatusMessageInfo(currentStatus, notifLang)

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
	// Asset tasks carry a concrete asset-type name ("Character"), not "Asset",
	// so map the entity type onto the verified route kind before linking.
	routeKind := placeholders.EntityType
	if !strings.EqualFold(strings.TrimSpace(routeKind), "shot") {
		routeKind = "asset"
	}
	placeholders.TaskURL = KitsuTaskURL(host, elem.Project.ID, routeKind, elem.Task.ID)

	// Kitsu プレビュー画像 URL を組み立て
	// Entity.PreviewFileID は interface{} なので string にキャストする
	// パス: /api/pictures/thumbnails/preview-files/{id}.png
	if id, ok := elem.Entity.Entity.PreviewFileID.(string); ok && id != "" {
		if host != "" {
			placeholders.PreviewImageURL = fmt.Sprintf("%sapi/pictures/thumbnails/preview-files/%s.png", host, url.PathEscape(id))
		}
	}

	placeholders.NotificationLanguage = notifLang
	placeholders.Color = int(notificationStatusColor(elem.TaskStatus.ShortName))
	return placeholders, nil
}

func parseTaskTemplate(tplFilePath string, data Template) string {
	tpl, err := os.ReadFile(tplFilePath)
	if err != nil {
		return ""
	}
	t, err := template.New("template").Funcs(template.FuncMap{
		"json": func(value string) string {
			encoded, _ := json.Marshal(value)
			return strings.Trim(string(encoded), "\"")
		},
	}).Parse(string(tpl))
	if err != nil {
		return ""
	}
	output := new(bytes.Buffer)
	if err := t.Execute(output, data); err != nil {
		return ""
	}
	return strings.TrimSpace(output.String())
}

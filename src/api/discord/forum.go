package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Forum (thread) operations for the Bot API. Stage D only needs thread
// creation, thread state sync and message posting; every call is a separate
// request because combining them into one PATCH hits the rate limit.

// ForumThread is the subset of a thread channel stage D reads.
type ForumThread struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	ParentID       string   `json:"parent_id"`
	AppliedTags    []string `json:"applied_tags"`
	ThreadMetadata *struct {
		Archived            bool `json:"archived"`
		AutoArchiveDuration int  `json:"auto_archive_duration"`
	} `json:"thread_metadata"`
}

// ForumThreadCreate is the request body for POST /channels/{forum}/threads.
type ForumThreadCreate struct {
	Name                string             `json:"name"`
	AutoArchiveDuration int                `json:"auto_archive_duration"`
	AppliedTags         []string           `json:"applied_tags,omitempty"`
	Message             ForumThreadMessage `json:"message"`
}

// ForumThreadMessage is the starter message of a new thread.
type ForumThreadMessage struct {
	Content         string           `json:"content"`
	Embeds          []Embed          `json:"embeds,omitempty"`
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
}

// ForumThreadPatch is the request body for PATCH /channels/{thread}.
type ForumThreadPatch struct {
	Archived            *bool    `json:"archived,omitempty"`
	AutoArchiveDuration *int     `json:"auto_archive_duration,omitempty"`
	AppliedTags         []string `json:"applied_tags,omitempty"`
}

// ForumMessageCreate is the request body for POST /channels/{thread}/messages.
type ForumMessageCreate struct {
	Content         string           `json:"content"`
	Embeds          []Embed          `json:"embeds,omitempty"`
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
	Flags           int              `json:"flags,omitempty"`
	Nonce           string           `json:"nonce,omitempty"`
	EnforceNonce    bool             `json:"enforce_nonce,omitempty"`
}

// ForumAPIError carries the HTTP status and Discord error code of a failed call.
type ForumAPIError struct {
	Operation  string
	Status     int
	Code       int
	Message    string
	RetryAfter time.Duration
}

func (e *ForumAPIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: HTTP %d (%s)", e.Operation, e.Status, e.Message)
	}
	return fmt.Sprintf("%s: HTTP %d", e.Operation, e.Status)
}

// ErrorClass maps the failure to the delivery failure vocabulary.
func (e *ForumAPIError) ErrorClass() string {
	switch {
	case e.Status == http.StatusTooManyRequests:
		return "rate_limited"
	case e.Status == http.StatusNotFound:
		return "missing_destination"
	case e.Status == http.StatusForbidden || e.Code == 50001:
		return "missing_permission"
	case e.Status >= 500:
		return "discord_server_error"
	case e.Status == http.StatusBadRequest:
		return "invalid_request"
	default:
		return "discord_http_error"
	}
}

// RetryAfter returns the server-provided wait for a 429 response.
func (e *ForumAPIError) RetryAfterDuration() time.Duration {
	return e.RetryAfter
}

// forumHTTPClient is swapped in tests.
var forumHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// CreateForumThread creates a thread in a forum channel.
func CreateForumThread(botToken, forumID string, body ForumThreadCreate) (ForumThread, error) {
	var thread ForumThread
	err := forumRequest(http.MethodPost, fmt.Sprintf("/channels/%s/threads", forumID), botToken, body, &thread, "create forum thread")
	return thread, err
}

// GetForumThread reads the current thread state.
func GetForumThread(botToken, threadID string) (ForumThread, error) {
	var thread ForumThread
	err := forumRequest(http.MethodGet, fmt.Sprintf("/channels/%s", threadID), botToken, nil, &thread, "get forum thread")
	return thread, err
}

// PatchForumThread applies one thread change. Callers must send unarchive and
// tag changes as separate calls.
func PatchForumThread(botToken, threadID string, body ForumThreadPatch) error {
	return forumRequest(http.MethodPatch, fmt.Sprintf("/channels/%s", threadID), botToken, body, nil, "patch forum thread")
}

// PostForumMessage posts a message into a thread.
func PostForumMessage(botToken, threadID string, body ForumMessageCreate) (DiscordMessage, error) {
	var msg DiscordMessage
	err := forumRequest(http.MethodPost, fmt.Sprintf("/channels/%s/messages", threadID), botToken, body, &msg, "post forum message")
	return msg, err
}

// ListForumThreadMessages returns recent messages, used for crash recovery.
func ListForumThreadMessages(botToken, threadID string, limit int) ([]ForumMessage, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	var messages []ForumMessage
	err := forumRequest(http.MethodGet, fmt.Sprintf("/channels/%s/messages?limit=%d", threadID, limit), botToken, nil, &messages, "list forum messages")
	return messages, err
}

// ForumMessage is the subset of a message stage D reads.
type ForumMessage struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
	Author    struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	} `json:"author"`
}

// ListActiveGuildThreads returns the guild's active threads, used to reconcile
// a create_thread row that was in flight when the process stopped.
func ListActiveGuildThreads(botToken, guildID string) ([]ForumThread, error) {
	var resp struct {
		Threads []ForumThread `json:"threads"`
	}
	err := forumRequest(http.MethodGet, fmt.Sprintf("/guilds/%s/threads/active", guildID), botToken, nil, &resp, "list active threads")
	return resp.Threads, err
}

// ListArchivedForumThreads returns public archived threads of a forum channel.
func ListArchivedForumThreads(botToken, forumID string) ([]ForumThread, error) {
	var resp struct {
		Threads []ForumThread `json:"threads"`
	}
	err := forumRequest(http.MethodGet, fmt.Sprintf("/channels/%s/threads/archived/public?limit=50", forumID), botToken, nil, &resp, "list archived threads")
	return resp.Threads, err
}

// forumRequest performs one Bot API call and decodes the response body.
func forumRequest(method, path, botToken string, payload, out any, operation string) error {
	if strings.TrimSpace(botToken) == "" {
		return errors.New("discord bot token is missing")
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("%s: encode request: %w", operation, err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, discordBotAPI+path, body)
	if err != nil {
		return fmt.Errorf("%s: build request: %w", operation, err)
	}
	req.Header.Set("Authorization", "Bot "+botToken)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := forumHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return fmt.Errorf("%s: read response: %w", operation, readErr)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return &ForumAPIError{
			Operation:  operation,
			Status:     resp.StatusCode,
			Code:       forumErrorCode(raw),
			Message:    "rate limited",
			RetryAfter: forumRetryAfter(raw, resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode >= 400 {
		return &ForumAPIError{
			Operation: operation,
			Status:    resp.StatusCode,
			Code:      forumErrorCode(raw),
			Message:   forumErrorMessage(raw),
		}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: decode response: %w", operation, err)
	}
	return nil
}

func forumErrorCode(raw []byte) int {
	var payload struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}
	return payload.Code
}

func forumErrorMessage(raw []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return payload.Message
}

// forumRetryAfter reads the wait from the Retry-After header or the JSON body.
func forumRetryAfter(raw []byte, header string) time.Duration {
	if value := strings.TrimSpace(header); value != "" {
		if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
			return time.Duration(seconds * float64(time.Second))
		}
	}
	var payload struct {
		RetryAfter float64 `json:"retry_after"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.RetryAfter > 0 {
		return time.Duration(payload.RetryAfter * float64(time.Second))
	}
	return time.Second
}

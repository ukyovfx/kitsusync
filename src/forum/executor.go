package forum

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"app/src/api/discord"
	"app/src/model"
)

// Executor drains the forum outbox. It is single-threaded: one row at a time,
// oldest first, never two rows of the same task concurrently.

// Executor holds the dependencies of one drain pass.
type Executor struct {
	DB       *gorm.DB
	BotToken func() string
	GuildID  func() string
	// TaskURL resolves the Kitsu link for a task.
	TaskURL func(taskID string) string
	// Recipients resolves the mention targets for a signal.
	Recipients func(taskID string, payload model.ForumPostMessagePayload) (userIDs, roleIDs []string)
	Now        func() time.Time
	// Sleep is injectable so tests do not wait for real backoff.
	Sleep func(time.Duration)
}

// Drain processes due rows until none remain runnable or the budget is spent.
func (e Executor) Drain(maxRows int) (int, error) {
	if e.DB == nil {
		return 0, nil
	}
	if e.Now == nil {
		e.Now = func() time.Time { return time.Now().UTC() }
	}
	if e.Sleep == nil {
		e.Sleep = time.Sleep
	}
	processed := 0
	for processed < maxRows {
		row, err := model.ClaimNextForumOutboxRow(e.DB, e.Now())
		if err != nil {
			return processed, err
		}
		if row == nil {
			return processed, nil
		}
		if err := e.execute(*row); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (e Executor) execute(row model.ForumOutbox) error {
	token := ""
	if e.BotToken != nil {
		token = e.BotToken()
	}
	if strings.TrimSpace(token) == "" {
		return e.retryLater(row, "missing_bot_token", 10*time.Minute)
	}
	switch row.Kind {
	case model.ForumOutboxCreateThread:
		return e.executeCreateThread(row, token)
	case model.ForumOutboxSyncThread:
		return e.executeSyncThread(row, token)
	case model.ForumOutboxPostMessage:
		return e.executePostMessage(row, token)
	case model.ForumOutboxWarnMessage:
		return e.executeWarnMessage(row, token)
	default:
		return model.FailForumOutboxRow(e.DB, row.ID, "unsupported_kind", row.Kind)
	}
}

func (e Executor) executeCreateThread(row model.ForumOutbox, token string) error {
	var payload model.ForumCreateThreadPayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return model.FailForumOutboxRow(e.DB, row.ID, "invalid_payload", err.Error())
	}
	if payload.ForumID == "" {
		return model.FailForumOutboxRow(e.DB, row.ID, "invalid_payload", "missing forum id")
	}
	var card discord.Payload
	if len(payload.Card) > 0 {
		if err := json.Unmarshal(payload.Card, &card); err != nil {
			return model.FailForumOutboxRow(e.DB, row.ID, "invalid_payload", err.Error())
		}
	}
	body := discord.ForumThreadCreate{
		Name:                payload.Name,
		AutoArchiveDuration: payload.ArchiveMinutes,
		Message:             discord.ForumFirstMessage(card),
	}
	if tag := e.roleTag(payload.ForumID, payload.Role); tag != "" {
		body.AppliedTags = []string{tag}
	}
	thread, err := discord.CreateForumThread(token, payload.ForumID, body)
	if err != nil {
		return e.handleFailure(row, err)
	}
	return model.SetForumTaskThread(e.DB, row.TaskID, payload.ForumID, thread.ID, row.ID)
}

func (e Executor) executeSyncThread(row model.ForumOutbox, token string) error {
	task, err := model.FindForumTaskWithError(e.DB, row.TaskID)
	if err != nil {
		return model.FailForumOutboxRow(e.DB, row.ID, "missing_task", row.TaskID)
	}
	if task.ThreadID == "" {
		return model.FinishForumOutboxRow(e.DB, row.ID, "no_thread")
	}
	// Re-read the ledger so a status change that arrived while the row waited is
	// applied in one pass.
	role, forumID, err := e.currentRoleAndForum(task)
	if err != nil {
		return model.FailForumOutboxRow(e.DB, row.ID, "missing_settings", err.Error())
	}
	thread, err := discord.GetForumThread(token, task.ThreadID)
	if err != nil {
		return e.handleFailure(row, err)
	}
	plan := discord.PlanForumSync(thread, e.managedTags(forumID), e.roleTag(forumID, role), role, nil)
	if plan.NeedUnarchive {
		if err := discord.PatchForumThread(token, task.ThreadID, discord.ForumUnarchivePatch()); err != nil {
			return e.handleFailure(row, err)
		}
	}
	if plan.NeedRetag {
		if err := discord.PatchForumThread(token, task.ThreadID, discord.ForumSyncPatch(plan)); err != nil {
			return e.handleFailure(row, err)
		}
	}
	return model.FinishForumOutboxRow(e.DB, row.ID, "synced")
}

func (e Executor) executePostMessage(row model.ForumOutbox, token string) error {
	var payload model.ForumPostMessagePayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return model.FailForumOutboxRow(e.DB, row.ID, "invalid_payload", err.Error())
	}
	task, err := model.FindForumTaskWithError(e.DB, row.TaskID)
	if err != nil {
		return model.FailForumOutboxRow(e.DB, row.ID, "missing_task", row.TaskID)
	}
	if task.ThreadID == "" {
		return model.CancelForumOutboxRow(e.DB, row.ID, "no_thread")
	}
	var userIDs, roleIDs []string
	if e.Recipients != nil {
		userIDs, roleIDs = e.Recipients(row.TaskID, payload)
	}
	url := ""
	if e.TaskURL != nil {
		url = e.TaskURL(row.TaskID)
	}
	signal := discord.RenderForumSignal(payload.Intent, url, payload.Quote, userIDs, roleIDs)
	body := discord.ForumSignalMessage(signal, discord.ForumNonce(row.TaskID, row.ID))
	if err := model.StoreForumOutboxRendered(e.DB, row.ID, signal.Content); err != nil {
		return err
	}
	msg, err := discord.PostForumMessage(token, task.ThreadID, body)
	if err != nil {
		return e.handleFailure(row, err)
	}
	return model.FinishForumOutboxRow(e.DB, row.ID, "message="+msg.ID)
}

// executeWarnMessage posts the flap warning. It never mentions anyone.
func (e Executor) executeWarnMessage(row model.ForumOutbox, token string) error {
	task, err := model.FindForumTaskWithError(e.DB, row.TaskID)
	if err != nil {
		return model.FailForumOutboxRow(e.DB, row.ID, "missing_task", row.TaskID)
	}
	if task.ThreadID == "" {
		return model.CancelForumOutboxRow(e.DB, row.ID, "no_thread")
	}
	var payload struct {
		Transitions  int `json:"transitions"`
		SpanSeconds  int `json:"span_seconds"`
	}
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return model.FailForumOutboxRow(e.DB, row.ID, "invalid_payload", err.Error())
	}
	url := ""
	if e.TaskURL != nil {
		url = e.TaskURL(row.TaskID)
	}
	signal := discord.ForumFlapWarning(url, payload.Transitions, time.Duration(payload.SpanSeconds)*time.Second)
	body := discord.ForumSignalMessage(signal, discord.ForumNonce(row.TaskID, row.ID))
	if err := model.StoreForumOutboxRendered(e.DB, row.ID, signal.Content); err != nil {
		return err
	}
	msg, err := discord.PostForumMessage(token, task.ThreadID, body)
	if err != nil {
		return e.handleFailure(row, err)
	}
	return model.FinishForumOutboxRow(e.DB, row.ID, "message="+msg.ID)
}

// handleFailure classifies an API error and schedules the row accordingly.
func (e Executor) handleFailure(row model.ForumOutbox, err error) error {
	var apiErr *discord.ForumAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorClass() {
		case "rate_limited":
			wait := apiErr.RetryAfterDuration()
			if wait <= 0 {
				wait = 5 * time.Second
			}
			if wait > 10*time.Minute {
				wait = 10 * time.Minute
			}
			return model.ReleaseForumOutboxRow(e.DB, row.ID, e.Now().Add(wait))
		case "missing_destination":
			if row.Kind == model.ForumOutboxCreateThread {
				return model.FailForumOutboxRow(e.DB, row.ID, "missing_destination", apiErr.Error())
			}
			return e.recreateThread(row, apiErr)
		case "missing_permission":
			return e.retryLater(row, "missing_permission", 10*time.Minute)
		case "invalid_request":
			return model.FailForumOutboxRow(e.DB, row.ID, "invalid_request", apiErr.Error())
		default:
			return e.retryLater(row, apiErr.ErrorClass(), backoffFor(row.Attempts))
		}
	}
	if errors.Is(err, context.Canceled) {
		return model.RetryForumOutboxRow(e.DB, row.ID, "transient", e.Now().Add(backoffFor(row.Attempts)))
	}
	return e.retryLater(row, "transient", backoffFor(row.Attempts))
}

// recreateThread drops a thread that no longer exists and queues a new one.
func (e Executor) recreateThread(row model.ForumOutbox, apiErr *discord.ForumAPIError) error {
	generation, err := model.ClearForumTaskThread(e.DB, row.TaskID)
	if err != nil {
		return model.FailForumOutboxRow(e.DB, row.ID, "missing_destination", apiErr.Error())
	}
	if generation > 3 {
		return model.FailForumOutboxRow(e.DB, row.ID, "missing_destination", "thread lost repeatedly")
	}
	if row.Kind == model.ForumOutboxPostMessage {
		return model.CancelForumOutboxRow(e.DB, row.ID, "thread_lost")
	}
	return model.FailForumOutboxRow(e.DB, row.ID, "missing_destination", apiErr.Error())
}

func (e Executor) retryLater(row model.ForumOutbox, class string, wait time.Duration) error {
	if row.Attempts >= model.ForumMaxAttempts {
		return model.FailForumOutboxRow(e.DB, row.ID, class, "retry budget exhausted")
	}
	return model.RetryForumOutboxRow(e.DB, row.ID, class, e.Now().Add(wait))
}

func backoffFor(attempts int) time.Duration {
	wait := 5 * time.Second
	for i := 1; i < attempts && wait < 10*time.Minute; i++ {
		wait *= 3
	}
	if wait > 10*time.Minute {
		wait = 10 * time.Minute
	}
	return wait
}

// roleTag and managedTags read the Production settings for a forum.
func (e Executor) roleTag(forumID, role string) string {
	if forumID == "" || role == "" {
		return ""
	}
	settings, ok, err := model.LoadProductionForumSettings(e.DB, e.productionForForum(forumID))
	if err != nil || !ok {
		return ""
	}
	for _, tag := range settings.Tags {
		if tag.ForumChannelID == forumID && tag.Role == role {
			return tag.TagID
		}
	}
	return ""
}

func (e Executor) managedTags(forumID string) []string {
	settings, ok, err := model.LoadProductionForumSettings(e.DB, e.productionForForum(forumID))
	if err != nil || !ok {
		return nil
	}
	var tags []string
	for _, tag := range settings.Tags {
		if tag.ForumChannelID == forumID && tag.TagID != "" {
			tags = append(tags, tag.TagID)
		}
	}
	return tags
}

// productionForForum finds the Production that routes to a forum channel.
func (e Executor) productionForForum(forumID string) string {
	var route model.ProductionForumRoute
	if err := e.DB.Where("forum_channel_id = ?", forumID).First(&route).Error; err != nil {
		return ""
	}
	return route.ProductionID
}

// currentRoleAndForum resolves the role and forum for a task's current status.
func (e Executor) currentRoleAndForum(task model.ForumTask) (string, string, error) {
	settings, ok, err := model.LoadProductionForumSettings(e.DB, task.ProjectID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", fmt.Errorf("forum settings missing for %s", task.ProjectID)
	}
	role := ""
	for _, row := range settings.StatusRoles {
		if row.StatusID == task.StatusID {
			role = row.Role
			break
		}
	}
	forumID := task.ForumID
	if forumID == "" {
		for _, route := range settings.Routes {
			if route.TaskTypeID == task.TaskTypeID {
				forumID = route.ForumChannelID
				break
			}
		}
	}
	return role, forumID, nil
}

// ReconcileSendingRows re-queues rows left in flight by a crash. Idempotent
// operations (sync) are simply retried; create/post rows are retried too, but
// the nonce and the title check keep the duplicate risk bounded.
func ReconcileSendingRows(db *gorm.DB) error {
	rows, err := model.ListForumOutboxSending(db)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := model.ResetForumOutboxToPending(db, row.ID, "reconciled_after_restart"); err != nil {
			return err
		}
	}
	return nil
}

// StatusIsRetryable reports whether an HTTP status is worth retrying.
func StatusIsRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

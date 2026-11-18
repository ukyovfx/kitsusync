package forum

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	Recipients func(taskID string, payload model.ForumPostMessagePayload) (userIDs, roleIDs []string, err error)
	Now        func() time.Time
	// Sleep is injectable so tests do not wait for real backoff.
	Sleep func(time.Duration)
	// recipientCache reuses reviewer resolution within one drain pass, keyed
	// by production and task type, so several signals do not re-resolve.
	recipientCache map[string]resolvedRecipients
}

// forumRecipientResolutionMaxAttempts bounds the recipient-resolution retries:
// after this many claims a signal is posted without mentions so the thread at
// least documents the state change. 4 retries spend about 3.5 minutes of
// backoff before the unmentioned send.
const forumRecipientResolutionMaxAttempts = 5

// resolvedRecipients is one cached recipient resolution.
type resolvedRecipients struct {
	users []string
	roles []string
	err   error
}

// ErrForumRateLimited ends a drain pass: Discord reported a rate limit, so the
// rest of the outbox must wait instead of being claimed and retried now.
var ErrForumRateLimited = errors.New("forum outbox drain paused by a discord rate limit")

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
	e.recipientCache = map[string]resolvedRecipients{}
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
		e.recordAudit(*row)
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
	// A retried create may already have succeeded before the crash: adopt the
	// existing thread instead of opening a duplicate.
	if row.Attempts > 1 {
		if existingID, found := e.findExistingThread(token, payload.ForumID, payload.Name); found {
			return model.SetForumTaskThread(e.DB, row.TaskID, payload.ForumID, existingID, row.ID)
		}
	}
	thread, err := discord.CreateForumThread(token, payload.ForumID, body)
	if err != nil {
		return e.handleFailure(row, err)
	}
	return model.SetForumTaskThread(e.DB, row.TaskID, payload.ForumID, thread.ID, row.ID)
}

// findExistingThread looks for a thread this Bot already opened with the same
// name, so a create retried after a crash does not post the card twice.
func (e Executor) findExistingThread(token, forumID, name string) (string, bool) {
	guildID := e.guildForForum(forumID)
	if guildID == "" {
		// Without a resolvable guild the duplicate search cannot run: skip it
		// so the retry falls through to a normal create attempt instead of
		// panicking on a missing lookup.
		return "", false
	}
	threads, err := discord.ListActiveGuildThreads(token, guildID)
	if err == nil {
		for _, thread := range threads {
			if thread.ParentID == forumID && thread.Name == name {
				return thread.ID, true
			}
		}
	}
	archived, err := discord.ListArchivedForumThreads(token, forumID)
	if err != nil {
		return "", false
	}
	for _, thread := range archived {
		if thread.Name == name {
			return thread.ID, true
		}
	}
	return "", false
}

// guildForForum resolves the guild a forum channel belongs to: the
// production's forum settings first, then the project's guild, then the
// Executor-wide GuildID. Empty means the search above is skipped safely.
func (e Executor) guildForForum(forumID string) string {
	settings, ok, err := model.LoadProductionForumSettings(e.DB, e.productionForForum(forumID))
	if err == nil && ok {
		if guild := strings.TrimSpace(settings.Config.GuildID); guild != "" {
			return guild
		}
		if p := model.FindProjectByKitsuID(e.DB, settings.Config.ProductionID); p != nil && strings.TrimSpace(p.DiscordGuildID) != "" {
			return strings.TrimSpace(p.DiscordGuildID)
		}
	}
	if e.GuildID != nil {
		return strings.TrimSpace(e.GuildID())
	}
	return ""
}

func (e Executor) executeSyncThread(row model.ForumOutbox, token string) error {
	task, err := model.FindForumTaskWithError(e.DB, row.TaskID)
	if err != nil {
		// Only a task that is really gone is missing_task: a transient error
		// (SQLITE_BUSY and friends) must be retried, not failed forever.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.FailForumOutboxRow(e.DB, row.ID, "missing_task", row.TaskID)
		}
		return model.RetryForumOutboxRow(e.DB, row.ID, "transient", e.Now().Add(backoffFor(row.Attempts)))
	}
	if task.ThreadID == nil || *task.ThreadID == "" {
		return model.FinishForumOutboxRow(e.DB, row.ID, "no_thread")
	}
	threadID := *task.ThreadID
	// Re-read the ledger so a status change that arrived while the row waited is
	// applied in one pass.
	role, forumID, err := e.currentRoleAndForum(task)
	if err != nil {
		return model.FailForumOutboxRow(e.DB, row.ID, "missing_settings", err.Error())
	}
	thread, err := discord.GetForumThread(token, threadID)
	if err != nil {
		return e.handleFailure(row, err)
	}
	plan := discord.PlanForumSync(thread, e.managedTags(forumID), e.roleTag(forumID, role), role, nil)
	if plan.NeedUnarchive {
		if err := discord.PatchForumThread(token, threadID, discord.ForumUnarchivePatch()); err != nil {
			return e.handleFailure(row, err)
		}
	}
	if plan.NeedRetag {
		if err := discord.PatchForumThread(token, threadID, discord.ForumSyncPatch(plan)); err != nil {
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
		// Only a task that is really gone is missing_task: a transient error
		// (SQLITE_BUSY and friends) must be retried, not failed forever.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.FailForumOutboxRow(e.DB, row.ID, "missing_task", row.TaskID)
		}
		return model.RetryForumOutboxRow(e.DB, row.ID, "transient", e.Now().Add(backoffFor(row.Attempts)))
	}
	if task.ThreadID == nil || *task.ThreadID == "" {
		return model.CancelForumOutboxRow(e.DB, row.ID, "no_thread")
	}
	// The mention window coalesces every status change of a task into one
	// row, so the payload's intent is stale by the time the row runs. The
	// ledger's current status decides what is said: a status that is no
	// longer a trigger, or a return to the status the window opened from,
	// cancels the mention instead of repeating it.
	switch payload.Intent {
	case model.ForumIntentStatusWFA, model.ForumIntentStatusRetake, model.ForumIntentStatusDone:
		role, _, err := e.currentRoleAndForum(task)
		if err != nil {
			return model.FailForumOutboxRow(e.DB, row.ID, "missing_settings", err.Error())
		}
		if !IsTriggerRole(role) {
			return model.CancelForumOutboxRow(e.DB, row.ID, "not_trigger")
		}
		if payload.BaseStatusID != "" && task.StatusID == payload.BaseStatusID {
			return model.CancelForumOutboxRow(e.DB, row.ID, "round_trip")
		}
		payload.Intent = intentForRole(role)
	}
	var userIDs, roleIDs []string
	if e.Recipients != nil {
		users, roles, err := e.resolveRecipients(row, task, payload)
		switch {
		case err != nil:
			if row.Attempts >= forumRecipientResolutionMaxAttempts {
				slog.Warn("Forum recipients still unresolved; sending the signal without mentions",
					"error_class", "forum_recipient_resolution_failed",
					"task_id", row.TaskID,
					"attempts", row.Attempts,
					"err", err)
			} else {
				slog.Warn("Forum recipients unresolved; retrying later",
					"error_class", "forum_recipient_resolution_failed",
					"task_id", row.TaskID,
					"attempts", row.Attempts,
					"err", err)
				return e.retryLater(row, "forum_recipient_resolution_failed", backoffFor(row.Attempts))
			}
		case len(users)+len(roles) == 0:
			// D-06: with nobody to mention the message would only add noise.
			slog.Info("Forum signal dropped: no recipients",
				"error_class", "forum_no_recipient",
				"task_id", row.TaskID,
				"intent", payload.Intent)
			return model.CancelForumOutboxRow(e.DB, row.ID, "no_recipient")
		default:
			userIDs, roleIDs = users, roles
		}
	}
	url := ""
	if e.TaskURL != nil {
		url = e.TaskURL(row.TaskID)
	}
	signal := discord.RenderForumSignal(payload.Intent, url, payload.Quote, payload.Submitter, userIDs, roleIDs)
	body := discord.ForumSignalMessage(signal, discord.ForumNonce(row.TaskID, row.ID))
	if err := model.StoreForumOutboxRendered(e.DB, row.ID, signal.Content); err != nil {
		return err
	}
	msg, err := discord.PostForumMessage(token, *task.ThreadID, body)
	if err != nil {
		return e.handleFailure(row, err)
	}
	return model.FinishForumOutboxRow(e.DB, row.ID, "message="+msg.ID)
}

// resolveRecipients calls the configured resolver, reusing the result within
// the same drain pass for WFA signals of one (production, task type): the
// reviewer lookup is external, so several signals must not re-resolve it.
func (e Executor) resolveRecipients(row model.ForumOutbox, task model.ForumTask, payload model.ForumPostMessagePayload) ([]string, []string, error) {
	if payload.Intent != model.ForumIntentStatusWFA {
		return e.Recipients(row.TaskID, payload)
	}
	key := task.ProjectID + "\x00" + task.TaskTypeID
	if cached, ok := e.recipientCache[key]; ok {
		return cached.users, cached.roles, cached.err
	}
	users, roles, err := e.Recipients(row.TaskID, payload)
	e.recipientCache[key] = resolvedRecipients{users: users, roles: roles, err: err}
	return users, roles, err
}

// executeWarnMessage posts the flap warning. It never mentions anyone.
func (e Executor) executeWarnMessage(row model.ForumOutbox, token string) error {
	task, err := model.FindForumTaskWithError(e.DB, row.TaskID)
	if err != nil {
		// Only a task that is really gone is missing_task: a transient error
		// (SQLITE_BUSY and friends) must be retried, not failed forever.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.FailForumOutboxRow(e.DB, row.ID, "missing_task", row.TaskID)
		}
		return model.RetryForumOutboxRow(e.DB, row.ID, "transient", e.Now().Add(backoffFor(row.Attempts)))
	}
	if task.ThreadID == nil || *task.ThreadID == "" {
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
	msg, err := discord.PostForumMessage(token, *task.ThreadID, body)
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
			if err := model.ReleaseForumOutboxRow(e.DB, row.ID, e.Now().Add(wait)); err != nil {
				return err
			}
			return ErrForumRateLimited
		case "missing_destination":
			if row.Kind == model.ForumOutboxCreateThread {
				return model.FailForumOutboxRow(e.DB, row.ID, "missing_destination", apiErr.Error())
			}
			return e.recreateThread(row, apiErr)
		case "missing_permission":
			if row.Kind == model.ForumOutboxSyncThread {
				slog.Error("Forum thread sync lacks Discord permission",
					"error_class", "forum_missing_permission",
					"task_id", row.TaskID,
					"err", apiErr,
				)
				return model.FailForumOutboxRow(e.DB, row.ID, "missing_permission", "Manage Threads required")
			}
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
	// A sync row whose thread is gone needs a replacement too: without it the
	// task keeps a cleared thread and never posts again.
	role, forumID, err := e.currentRoleAndForumFor(row.TaskID)
	if err != nil {
		return model.FailForumOutboxRow(e.DB, row.ID, "missing_settings", err.Error())
	}
	if err := e.queueReplacementThread(row, forumID, role, generation); err != nil {
		return model.RetryForumOutboxRow(e.DB, row.ID, "transient", e.Now().Add(backoffFor(row.Attempts)))
	}
	return model.CancelForumOutboxRow(e.DB, row.ID, "thread_lost")
}

func (e Executor) currentRoleAndForumFor(taskID string) (string, string, error) {
	task, err := model.FindForumTaskWithError(e.DB, taskID)
	if err != nil {
		return "", "", err
	}
	return e.currentRoleAndForum(task)
}

// queueReplacementThread reuses the original create payload (card, name, forum)
// for the next thread generation.
func (e Executor) queueReplacementThread(row model.ForumOutbox, forumID, role string, generation int) error {
	var payload model.ForumCreateThreadPayload
	payload.ForumID = forumID
	payload.Role = role
	payload.ArchiveMinutes = model.ForumArchiveMinutesForRole(role)
	if previous, ok := e.pendingCreatePayload(row.TaskID); ok {
		payload.Name = previous.Name
		payload.Card = previous.Card
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := e.Now()
	_, err = model.EnqueueForumOutbox(e.DB, model.ForumOutbox{
		TaskID:    row.TaskID,
		Kind:      model.ForumOutboxCreateThread,
		DedupeKey: model.ForumCreateThreadDedupeKey(row.TaskID, generation),
		Payload:   string(encoded),
		State:     model.ForumOutboxPending,
		NotBefore: now,
		CreatedAt: now,
	})
	if errors.Is(err, model.ErrForumOutboxDuplicate) {
		return nil
	}
	return err
}

// pendingCreatePayload finds the newest create payload for a task so a
// replacement thread keeps the original name and card.
func (e Executor) pendingCreatePayload(taskID string) (model.ForumCreateThreadPayload, bool) {
	var outbox model.ForumOutbox
	if err := e.DB.Where("task_id = ? AND kind = ?", taskID, model.ForumOutboxCreateThread).
		Order("id desc").First(&outbox).Error; err != nil {
		return model.ForumCreateThreadPayload{}, false
	}
	var payload model.ForumCreateThreadPayload
	if json.Unmarshal([]byte(outbox.Payload), &payload) != nil {
		return model.ForumCreateThreadPayload{}, false
	}
	return payload, true
}

func (e Executor) retryLater(row model.ForumOutbox, class string, wait time.Duration) error {
	if row.Attempts >= model.ForumMaxAttempts {
		slog.Error("Forum outbox delivery failed: retry budget exhausted",
			"error_class", class,
			"task_id", row.TaskID,
			"kind", row.Kind,
			"attempts", row.Attempts,
		)
		return model.FailForumOutboxRow(e.DB, row.ID, class, "retry budget exhausted")
	}
	slog.Error("Forum outbox delivery retrying later",
		"error_class", class,
		"task_id", row.TaskID,
		"kind", row.Kind,
		"attempts", row.Attempts,
		"wait", wait,
	)
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

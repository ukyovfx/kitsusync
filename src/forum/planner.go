package forum

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"app/src/api/kitsu"
	"app/src/model"
)

// Planner turns one poll of Kitsu tasks into ledger updates and outbox rows.
// It only runs for Productions whose notification mode is "forum"; lane
// Productions keep using the existing pipeline untouched.

// TaskInput is one task as observed by the poller.
type TaskInput struct {
	TaskID     string
	ProjectID  string
	TaskTypeID string
	EntityID   string
	EntityKind string
	EntityName string
	ParentName string
	EntityType string
	Title      string
	// TaskTypeName is the display name of the task type ("Layout"…), the
	// embed author line.
	TaskTypeName string
	// StatusName is the display name of the task's current Kitsu status
	// (lower-cased by the planner for the embed footer).
	StatusName   string
	StatusID     string
	StatusRole   string
	Assignees    []string
	CommentDate  string
	KitsuUpdated string
	// Comment is the newest Kitsu comment, used for RETAKE and comment signals.
	Comment CommentInput
}

// CommentInput is the newest comment observed for a task.
type CommentInput struct {
	ID        string
	Text      string
	CreatedAt string
	// AuthorID and AuthorName are the comment's author. They decide whether a
	// submitter marker is trusted.
	AuthorID   string
	AuthorName string
	// Mentions are the Kitsu person IDs named in the comment.
	Mentions []string
	// Unavailable reports that the comment could not be read this poll.
	Unavailable bool
}

// PlanResult summarises what the planner did for one task.
type PlanResult struct {
	Skipped   bool
	Baselined bool
	Created   bool
	Synced    bool
	Signalled string
}

// Planner holds the Production settings needed to plan.
type Planner struct {
	DB         *gorm.DB
	Production string
	Settings   model.ProductionForumSettings
	Now        time.Time
}

// RoleForStatus returns the forum role configured for a Kitsu status.
func (p Planner) RoleForStatus(statusID string) string {
	for _, row := range p.Settings.StatusRoles {
		if row.StatusID == statusID {
			return row.Role
		}
	}
	return ""
}

// ForumForTaskType returns the forum channel routed for a task type.
func (p Planner) ForumForTaskType(taskTypeID string) (model.ProductionForumRoute, bool) {
	for _, route := range p.Settings.Routes {
		if route.TaskTypeID == taskTypeID {
			return route, true
		}
	}
	return model.ProductionForumRoute{}, false
}

// IsTriggerRole reports whether a role opens a post or signals a change.
func IsTriggerRole(role string) bool {
	return role == model.ForumRoleWFA || role == model.ForumRoleRetake || role == model.ForumRoleDone
}

// PlanTask records a baseline for new task types and plans a task change.
// card renders the first message of a new thread; it is only called when a
// thread is actually created.
func (p Planner) PlanTask(input TaskInput, card func() (json.RawMessage, error)) (PlanResult, error) {
	route, routed := p.ForumForTaskType(input.TaskTypeID)
	if !routed {
		return PlanResult{Skipped: true}, nil
	}
	seeded, err := model.ForumBaselineExists(p.DB, p.Production, input.TaskTypeID)
	if err != nil {
		return PlanResult{}, err
	}
	if !seeded {
		// Baseline: record the task as-is. No events, no posts.
		row := forumTaskRow(input, p.Now)
		if err := model.RecordForumBaselineTasks(p.DB, p.Production, input.TaskTypeID, []model.ForumTask{row}); err != nil {
			return PlanResult{}, err
		}
		return PlanResult{Baselined: true}, nil
	}
	return p.planChange(input, route, card)
}

// PlanBaseline records the tasks that exist when forum mode is switched on.
func (p Planner) PlanBaseline(taskTypeID string, inputs []TaskInput) error {
	rows := make([]model.ForumTask, 0, len(inputs))
	for _, input := range inputs {
		rows = append(rows, forumTaskRow(input, p.Now))
	}
	return model.RecordForumBaselineTasks(p.DB, p.Production, taskTypeID, rows)
}

func (p Planner) planChange(input TaskInput, route model.ProductionForumRoute, card func() (json.RawMessage, error)) (PlanResult, error) {
	existing, err := model.FindForumTaskWithError(p.DB, input.TaskID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return PlanResult{}, err
	}
	hasRow := err == nil
	pendingCreate, err := model.ForumTaskHasPendingCreate(p.DB, input.TaskID)
	if err != nil {
		return PlanResult{}, err
	}
	hasPost := (hasRow && existing.ThreadID != nil && *existing.ThreadID != "") || pendingCreate

	if !hasRow {
		return p.createTask(input, route, card)
	}

	statusChanged := existing.StatusID != input.StatusID
	assignees := model.NormalizeForumAssigneeIDs(input.Assignees)
	assigneesChanged := !sameIDs(model.ForumAssigneeIDs(existing), assignees)

	result := PlanResult{}
	// A status change must reach the thread. A task whose thread was lost
	// (generation advanced by ClearForumTaskThread) also needs its create
	// re-queued; a baselined task without a thread must stay quiet until its
	// status actually changes.
	threadLost := !hasPost && existing.ThreadGeneration > 0
	if statusChanged || threadLost {
		if err := p.applyStatusChange(input, existing, route, hasPost, card, &result); err != nil {
			return result, err
		}
	}
	if assigneesChanged && hasPost {
		if err := p.queueAssigneeSignal(input, existing, assignees); err != nil {
			return result, err
		}
	}
	// A comment attached to a trigger transition (WFA/retake/done) belongs to
	// the status signal — the retake quote carries the text — so its own
	// comment notification would only repeat it, empty text included.
	commentWithTriggerChange := statusChanged && IsTriggerRole(input.StatusRole)
	// The stored marker is the Kitsu last_comment_date, so a first poll after
	// the baseline does not announce a comment the row already recorded.
	if hasPost && !commentWithTriggerChange && !input.Comment.Unavailable && input.Comment.ID != "" && input.CommentDate != existing.LastCommentAt {
		if err := p.queueCommentSignal(input, existing); err != nil {
			return result, err
		}
	}
	if err := p.updateTaskRow(input, existing.StatusID, assignees); err != nil {
		return result, err
	}
	return result, nil
}

// createTask handles the first observation of a task after the baseline: the
// task is recorded, and a trigger status opens a thread. The row and its
// create outbox entry are written together: a row without the pending create
// would look like a posted task and the thread would never open.
func (p Planner) createTask(input TaskInput, route model.ProductionForumRoute, card func() (json.RawMessage, error)) (PlanResult, error) {
	result := PlanResult{}
	err := p.DB.Transaction(func(tx *gorm.DB) error {
		row := forumTaskRow(input, p.Now)
		if !IsTriggerRole(input.StatusRole) {
			return tx.Create(&row).Error
		}
		// Render the card before the row exists so a render failure cannot
		// leave a task that looks posted with no thread to post into.
		rendered, err := card()
		if err != nil {
			return fmt.Errorf("render forum card: %w", err)
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := p.queueCreateThreadIn(tx, input, route, input.StatusRole, 0, rendered, &result); err != nil {
			return fmt.Errorf("queue thread create: %w", err)
		}
		// A creation has no previous status, so the signal carries no base:
		// the round-trip cancel at send time must never eat the opening
		// announcement of a fresh thread.
		if err := p.queueStatusSignalIn(tx, input, model.ForumTask{}, &result); err != nil {
			return fmt.Errorf("queue status signal: %w", err)
		}
		return nil
	})
	if err != nil {
		return PlanResult{}, err
	}
	return result, nil
}

func (p Planner) applyStatusChange(input TaskInput, existing model.ForumTask, route model.ProductionForumRoute, hasPost bool, card func() (json.RawMessage, error), result *PlanResult) error {
	if !hasPost {
		if IsTriggerRole(input.StatusRole) {
			// A create that already failed permanently must not be re-queued
			// every poll: it would fail the same way again.
			failed, err := model.ForumCreateThreadFailed(p.DB, input.TaskID, existing.ThreadGeneration)
			if err != nil {
				return err
			}
			if failed {
				return nil
			}
			if err := p.queueCreateThread(input, route, input.StatusRole, existing.ThreadGeneration, card, result); err != nil {
				return err
			}
			// The thread that just opened must also announce the transition
			// that opened it, exactly like createTask does (create, then
			// signal). The signal row gets a later id, so the outbox ordering
			// rules keep it behind the thread creation; window, recipients and
			// coalescing stay identical to any other status signal.
			return p.queueStatusSignal(input, existing, result)
		}
		return nil
	}
	// A status change with a post must reach the thread in order: unarchive,
	// retag, then speak. The sync row gates the signal row (see the outbox
	// ordering rule), so the signal cannot jump ahead of the tag update.
	if err := p.queueSyncThread(input.TaskID); err != nil {
		return err
	}
	result.Synced = true
	// Flapping is a property of the transition, not of the role: a round trip
	// that ends in WIP still flapped, so the check runs before the role gate.
	if err := p.maybeWarnFlap(input, existing); err != nil {
		return err
	}
	if !IsTriggerRole(input.StatusRole) {
		return nil
	}
	return p.queueStatusSignal(input, existing, result)
}

// queueCommentSignal announces a new comment on a task that already has a post.
// A task without a post is ignored, which keeps WIP chatter out of the forum.
// An AYON publish comment becomes a publish trace: it shows the 📦 line and
// mentions nobody.
func (p Planner) queueCommentSignal(input TaskInput, existing model.ForumTask) error {
	payload := model.ForumPostMessagePayload{
		Intent:         model.ForumIntentComment,
		WindowOpenedAt: p.Now,
		Quote:          commentQuote(input.Comment.Text),
		Mentions:       model.NormalizeForumAssigneeIDs(input.Comment.Mentions),
	}
	if table, ok := ParseDeliverTable(input.Comment.Text); ok {
		payload.Intent = model.ForumIntentPublish
		payload.PublishLine = forumPublishLine(table)
		payload.Quote = ""
		payload.Mentions = nil
	}
	payload.StatusName = strings.ToLower(strings.TrimSpace(input.StatusName))
	return p.enqueueCommentPayload(input, payload)
}

// forumPublishLine renders the publish trace line of a delivery comment.
func forumPublishLine(table DeliverTable) string {
	return "📦 " + table.Name + "（" + table.Family + "）" + table.Version + " が publish されました"
}

func (p Planner) enqueueCommentPayload(input TaskInput, payload model.ForumPostMessagePayload) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := p.Now
	_, err = model.EnqueueForumOutbox(p.DB, model.ForumOutbox{
		TaskID:    input.TaskID,
		Kind:      model.ForumOutboxPostMessage,
		DedupeKey: model.ForumCommentCoalesceKey(input.Comment.ID, input.TaskID),
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

// maybeWarnFlap queues one warning per flap episode. The signal messages are
// not suppressed: the warning only adds context.
func (p Planner) maybeWarnFlap(input TaskInput, existing model.ForumTask) error {
	episode, transitions, span, flapping, err := p.flapEpisode(input.TaskID, existing.StatusID, input.StatusID)
	if err != nil || !flapping {
		return err
	}
	taskID := input.TaskID
	warned, err := model.ForumFlapWarned(p.DB, taskID, episode)
	if err != nil || warned {
		return err
	}
	now := p.Now
	_, err = model.EnqueueForumOutbox(p.DB, model.ForumOutbox{
		TaskID:    taskID,
		Kind:      model.ForumOutboxWarnMessage,
		DedupeKey: model.ForumWarnDedupeKey(taskID, episode),
		Payload:   fmt.Sprintf(`{"transitions":%d,"span_seconds":%d}`, transitions, int(span.Seconds())),
		State:     model.ForumOutboxPending,
		NotBefore: now,
		CreatedAt: now,
	})
	if err != nil && !errors.Is(err, model.ErrForumOutboxDuplicate) {
		return err
	}
	return model.MarkForumFlapWarned(p.DB, taskID, episode)
}

// flapEpisode reports whether the recent transitions look like flapping. R1 is
// a round trip inside the round-trip window, R2 is too many transitions inside
// the flap period.
//
// The episode starts at the first transition after the last quiet period, i.e.
// after the last gap of at least ForumFlapPeriod between two transitions. A
// continuous run stays one episode however long it lasts, so the count rule
// counts every transition of the episode, not only the ones still inside the
// flap period.
//
// The current change is passed in because the planner warns before the
// transition row is written.
func (p Planner) flapEpisode(taskID, fromStatusID, toStatusID string) (time.Time, int, time.Duration, bool, error) {
	// Everything ever recorded, oldest first: the episode boundary depends on
	// gaps older than the flap period, so the window alone is not enough.
	var recorded []model.ForumTransition
	if err := p.DB.Where("task_id = ?", taskID).Order("at asc, id asc").Find(&recorded).Error; err != nil {
		return time.Time{}, 0, 0, false, err
	}
	current := model.ForumTransition{TaskID: taskID, At: p.Now, FromStatusID: fromStatusID, ToStatusID: toStatusID}
	rows := append(recorded, current)
	if len(rows) < 2 || fromStatusID == "" {
		return time.Time{}, 0, 0, false, nil
	}
	// Walk back from the current change while the gaps stay under the flap
	// period. The first larger gap is the quiet period that started this
	// episode.
	start := len(rows) - 1
	for start > 0 && rows[start].At.Sub(rows[start-1].At) < model.ForumFlapPeriod {
		start--
	}
	episode := rows[start].At
	episodeRows := rows[start:]
	span := p.Now.Sub(episode)
	// A round trip comes back to a status the task had already left, so the
	// later transition lands on an earlier transition's source status. The
	// current change is the newest row and is not in the database yet, so it is
	// checked against every earlier row. Rows already recorded are checked
	// against each other, skipping the immediately previous one because it is
	// the same step of the run.
	roundTrip := false
	windowStart := p.Now.Add(-model.ForumFlapRoundTripWindow)
	for j := 0; j < len(episodeRows)-1; j++ {
		// Only a transition inside the round-trip window can be the return
		// leg: an older row is a separate episode, not this one.
		if episodeRows[j].At.Before(windowStart) {
			continue
		}
		if toStatusID != "" && toStatusID == episodeRows[j].FromStatusID {
			roundTrip = true
			break
		}
	}
	for i := 1; i < len(episodeRows)-1 && !roundTrip; i++ {
		row := episodeRows[i]
		if row.At.Before(p.Now.Add(-model.ForumFlapRoundTripWindow)) {
			continue
		}
		for j := 0; j < i-1; j++ {
			if row.ToStatusID != "" && row.ToStatusID == episodeRows[j].FromStatusID {
				roundTrip = true
				break
			}
		}
	}
	return episode, len(episodeRows), span, roundTrip || len(episodeRows) >= model.ForumFlapCount, nil
}

// commentQuote takes the first line of a comment, stripped of quoting markers
// and truncated for a one-line quote. Markdown table rows are skipped: AYON
// publish comments open with an empty "| | |" row, which must not become the
// quote. A table-only comment quotes nothing.
func commentQuote(text string) string {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		trimmed := strings.TrimLeft(strings.TrimSpace(line), "># ")
		if trimmed == "" || strings.HasPrefix(trimmed, "|") {
			continue
		}
		runes := []rune(trimmed)
		if len(runes) > model.ForumQuoteMaxRunes {
			return string(runes[:model.ForumQuoteMaxRunes]) + "…"
		}
		return string(runes)
	}
	return ""
}

// commentIDFromDate is unused; the comment marker is the stored Kitsu
// last_comment_date, compared directly in planChange.

func (p Planner) queueCreateThread(input TaskInput, route model.ProductionForumRoute, role string, generation int, card func() (json.RawMessage, error), result *PlanResult) error {
	rendered, err := card()
	if err != nil {
		return fmt.Errorf("render forum card: %w", err)
	}
	return p.queueCreateThreadIn(p.DB, input, route, role, generation, rendered, result)
}

// queueCreateThreadIn queues the thread creation on the given connection so a
// caller can keep it in the same transaction as the task row.
func (p Planner) queueCreateThreadIn(db *gorm.DB, input TaskInput, route model.ProductionForumRoute, role string, generation int, rendered json.RawMessage, result *PlanResult) error {
	title := model.TruncateForumTitle(input.Title)
	payload, err := json.Marshal(model.ForumCreateThreadPayload{
		ForumID:        route.ForumChannelID,
		Name:           title,
		Role:           role,
		ArchiveMinutes: model.ForumArchiveMinutesForRole(role),
		Card:           rendered,
	})
	if err != nil {
		return err
	}
	now := p.Now
	_, err = model.EnqueueForumOutbox(db, model.ForumOutbox{
		TaskID:    input.TaskID,
		Kind:      model.ForumOutboxCreateThread,
		DedupeKey: model.ForumCreateThreadDedupeKey(input.TaskID, generation),
		Payload:   string(payload),
		State:     model.ForumOutboxPending,
		NotBefore: now,
		CreatedAt: now,
	})
	if err != nil && !errors.Is(err, model.ErrForumOutboxDuplicate) {
		return err
	}
	result.Created = true
	return p.updateTaskRowIn(db, input, input.StatusID, model.NormalizeForumAssigneeIDs(input.Assignees))
}

func (p Planner) queueSyncThread(taskID string) error {
	now := p.Now
	_, err := model.CoalesceForumPostMessage(p.DB, model.ForumOutbox{
		TaskID:      taskID,
		Kind:        model.ForumOutboxSyncThread,
		DedupeKey:   model.ForumSyncThreadCoalesceKey(taskID) + ":" + now.UTC().Format(time.RFC3339Nano),
		CoalesceKey: model.ForumSyncThreadCoalesceKey(taskID),
		Payload:     "{}",
		State:       model.ForumOutboxPending,
		NotBefore:   now,
		CreatedAt:   now,
	})
	return err
}

func (p Planner) queueStatusSignal(input TaskInput, existing model.ForumTask, result *PlanResult) error {
	return p.queueStatusSignalIn(p.DB, input, existing, result)
}

// forumCardFields carries the plan-time heading inputs of a signal embed.
type forumCardFields struct {
	EntityType string
	ParentName string
	EntityName string
	TaskType   string
}

func forumTaskCardFields(input TaskInput) forumCardFields {
	return forumCardFields{
		EntityType: input.EntityType,
		ParentName: input.ParentName,
		EntityName: input.EntityName,
		TaskType:   input.TaskTypeName,
	}
}

func (p Planner) queueStatusSignalIn(db *gorm.DB, input TaskInput, existing model.ForumTask, result *PlanResult) error {
	intent := intentForRole(input.StatusRole)
	if intent == "" {
		return nil
	}
	fields := forumTaskCardFields(input)
	payload, err := json.Marshal(model.ForumPostMessagePayload{
		Intent:         intent,
		WindowOpenedAt: p.Now,
		BaseStatusID:   existing.StatusID,
		Quote:          p.signalQuote(input.TaskID, intent, input.Comment),
		Submitter:      submitterMarker(intent, input.Comment, p.Now),
		EntityType:     fields.EntityType,
		ParentName:     fields.ParentName,
		EntityName:     fields.EntityName,
		TaskType:       fields.TaskType,
	})
	if err != nil {
		return err
	}
	now := p.Now
	key := model.ForumStatusCoalesceKey(input.TaskID)
	_, err = model.CoalesceForumPostMessage(db, model.ForumOutbox{
		TaskID:      input.TaskID,
		Kind:        model.ForumOutboxPostMessage,
		DedupeKey:   key + ":" + now.UTC().Format(time.RFC3339Nano),
		CoalesceKey: key,
		Payload:     string(payload),
		State:       model.ForumOutboxPending,
		NotBefore:   now.Add(model.ForumMentionWindow),
		CreatedAt:   now,
	})
	if err != nil {
		return err
	}
	result.Signalled = intent
	return nil
}

func (p Planner) queueAssigneeSignal(input TaskInput, existing model.ForumTask, assignees []string) error {
	fields := forumTaskCardFields(input)
	payload, err := json.Marshal(model.ForumPostMessagePayload{
		Intent:         model.ForumIntentAssign,
		WindowOpenedAt: p.Now,
		BaseAssignees:  model.ForumAssigneeIDs(existing),
		StatusName:     strings.ToLower(strings.TrimSpace(input.StatusName)),
		EntityType:     fields.EntityType,
		ParentName:     fields.ParentName,
		EntityName:     fields.EntityName,
		TaskType:       fields.TaskType,
	})
	if err != nil {
		return err
	}
	now := p.Now
	_, err = model.CoalesceForumPostMessage(p.DB, model.ForumOutbox{
		TaskID:      input.TaskID,
		Kind:        model.ForumOutboxPostMessage,
		DedupeKey:   model.ForumPostMessageCoalesceKey(model.ForumIntentAssign, input.TaskID) + ":" + now.UTC().Format(time.RFC3339Nano),
		CoalesceKey: model.ForumPostMessageCoalesceKey(model.ForumIntentAssign, input.TaskID),
		Payload:     string(payload),
		State:       model.ForumOutboxPending,
		NotBefore:   now.Add(model.ForumMentionWindow),
		CreatedAt:   now,
	})
	return err
}

func (p Planner) updateTaskRow(input TaskInput, fromStatusID string, assignees []string) error {
	return p.updateTaskRowIn(p.DB, input, fromStatusID, assignees)
}

func (p Planner) updateTaskRowIn(db *gorm.DB, input TaskInput, fromStatusID string, assignees []string) error {
	updates := map[string]interface{}{
		"status_id":        input.StatusID,
		"assignee_ids":     encodeIDs(assignees),
		"kitsu_updated_at": input.KitsuUpdated,
		"last_seen_at":     p.Now,
		"title":            model.TruncateForumTitle(input.Title),
	}
	if !input.Comment.Unavailable {
		updates["last_comment_at"] = input.CommentDate
	} else {
		slog.Warn("forum: comments unavailable; keeping comment checkpoint", "error_class", "forum_comments_unavailable", "task_id", input.TaskID)
	}
	if fromStatusID != input.StatusID {
		updates["status_since"] = p.Now
	}
	if err := db.Model(&model.ForumTask{}).Where("task_id = ?", input.TaskID).Updates(updates).Error; err != nil {
		return err
	}
	if fromStatusID == input.StatusID {
		return nil
	}
	return db.Create(&model.ForumTransition{
		TaskID:       input.TaskID,
		At:           p.Now,
		FromStatusID: fromStatusID,
		ToStatusID:   input.StatusID,
	}).Error
}

func forumTaskRow(input TaskInput, now time.Time) model.ForumTask {
	return model.ForumTask{
		TaskID:         input.TaskID,
		ProjectID:      input.ProjectID,
		TaskTypeID:     input.TaskTypeID,
		EntityID:       input.EntityID,
		EntityKind:     input.EntityKind,
		Title:          model.TruncateForumTitle(input.Title),
		StatusID:       input.StatusID,
		StatusSince:    now,
		AssigneeIDs:    encodeIDs(model.NormalizeForumAssigneeIDs(input.Assignees)),
		LastCommentAt:  input.CommentDate,
		KitsuUpdatedAt: input.KitsuUpdated,
		FirstSeenAt:    now,
		LastSeenAt:     now,
	}
}

// retakeQuote returns the comment excerpt a RETAKE signal quotes. Only a
// comment written inside the lookback window is quoted: a RETAKE fired long
// after the comment would quote an unrelated one.
func retakeQuote(intent string, comment CommentInput, now time.Time) string {
	if intent != model.ForumIntentStatusRetake || comment.Unavailable {
		return ""
	}
	created, ok := commentCreatedAt(comment.CreatedAt)
	if !ok || now.Sub(created) > model.ForumCommentQuoteLookback {
		return ""
	}
	return commentQuote(comment.Text)
}

// signalQuote decides the quote of a status signal. A RETAKE quotes the newest
// comment, but a comment that already went out as a comment notification in
// the same aggregation window must not be quoted a second time.
func (p Planner) signalQuote(taskID, intent string, comment CommentInput) string {
	quote := retakeQuote(intent, comment, p.Now)
	if quote == "" {
		return ""
	}
	announced, err := p.commentAnnouncedInWindow(taskID, comment.ID)
	if err != nil {
		slog.Warn("forum: could not check announced comments; quoting anyway", "error_class", "forum_quote_lookup_failed", "task_id", taskID, "err", err)
		return quote
	}
	if announced {
		return ""
	}
	return quote
}

// commentAnnouncedInWindow reports whether this comment's own notification was
// enqueued or shown within the coalescing window. A failed or cancelled row
// never showed the text and does not suppress the quote.
func (p Planner) commentAnnouncedInWindow(taskID, commentID string) (bool, error) {
	var count int64
	err := p.DB.Model(&model.ForumOutbox{}).
		Where("task_id = ? AND dedupe_key = ? AND state IN ? AND created_at >= ?",
			taskID,
			model.ForumCommentCoalesceKey(commentID, taskID),
			[]string{model.ForumOutboxPending, model.ForumOutboxSending, model.ForumOutboxDone},
			p.Now.Add(-model.ForumMentionWindow),
		).Count(&count).Error
	return count > 0, err
}

// commentCreatedAt parses a Kitsu comment timestamp. Kitsu sometimes omits the
// zone ("2026-08-07T00:00:00"); that form is read as UTC so the comment is not
// silently dropped from the lookback.
func commentCreatedAt(value string) (time.Time, bool) {
	created, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return created, true
	}
	created, err = time.Parse("2006-01-02T15:04:05", value)
	if err != nil {
		return time.Time{}, false
	}
	return created, true
}

// submitterMarkerPrefix starts the machine-readable submitter line a Web
// publish writes into its comment: "kitsusync-submitter: <display name>".
const submitterMarkerPrefix = "kitsusync-submitter:"

// submitterServiceAuthor is the Kitsu account that publishes through the web
// form. Only its comments may carry a trusted submitter marker.
const submitterServiceAuthor = "Web Publisher"

// submitterMarker returns the display name of the newest comment's submitter
// line. It follows the same lookback as retakeQuote, and returns "" for a
// missing or malformed marker, or for a comment the service account did not
// write, so the signal keeps its usual shape.
func submitterMarker(intent string, comment CommentInput, now time.Time) string {
	if intent != model.ForumIntentStatusWFA || comment.Unavailable {
		return ""
	}
	if comment.AuthorName != submitterServiceAuthor {
		return ""
	}
	created, ok := commentCreatedAt(comment.CreatedAt)
	if !ok || now.Sub(created) > model.ForumCommentQuoteLookback {
		return ""
	}
	for _, line := range strings.Split(strings.ReplaceAll(comment.Text, "\r", ""), "\n") {
		if !strings.HasPrefix(line, submitterMarkerPrefix) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, submitterMarkerPrefix))
		if name == "" {
			return ""
		}
		return name
	}
	return ""
}

func intentForRole(role string) string {
	switch role {
	case model.ForumRoleWFA:
		return model.ForumIntentStatusWFA
	case model.ForumRoleRetake:
		return model.ForumIntentStatusRetake
	case model.ForumRoleDone:
		return model.ForumIntentStatusDone
	default:
		return ""
	}
}

func encodeIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func sameIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// TaskTitle builds the thread title for a task.
func TaskTitle(taskType, sequence, entity string) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{taskType, sequence, entity} {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return model.TruncateForumTitle(strings.Join(parts, " "))
}

// EntityKindFor maps a Kitsu entity type name to shot/asset.
func EntityKindFor(entityTypeName string) string {
	if strings.EqualFold(entityTypeName, "shot") || strings.EqualFold(entityTypeName, "shots") {
		return model.ForumEntityKindShot
	}
	return model.ForumEntityKindAsset
}

// KitsuTask mirrors the payload fields the planner needs.
type KitsuTask = kitsu.MessagePayload

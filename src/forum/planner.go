package forum

import (
	"encoding/json"
	"errors"
	"fmt"
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
	TaskID      string
	ProjectID   string
	TaskTypeID  string
	EntityID    string
	EntityKind  string
	EntityName  string
	ParentName  string
	EntityType  string
	Title       string
	StatusID    string
	StatusRole  string
	Assignees   []string
	CommentDate string
	KitsuUpdated string
}

// PlanResult summarises what the planner did for one task.
type PlanResult struct {
	Skipped    bool
	Baselined  bool
	Created    bool
	Synced     bool
	Signalled  string
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

// TagFor returns the configured tag for a role in a forum.
func (p Planner) TagFor(forumID, role string) string {
	for _, tag := range p.Settings.Tags {
		if tag.ForumChannelID == forumID && tag.Role == role {
			return tag.TagID
		}
	}
	return ""
}

// ManagedTags lists every tag the bot owns in a forum.
func (p Planner) ManagedTags(forumID string) []string {
	var tags []string
	for _, tag := range p.Settings.Tags {
		if tag.ForumChannelID == forumID && tag.TagID != "" {
			tags = append(tags, tag.TagID)
		}
	}
	return tags
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
	hasPost := (hasRow && existing.ThreadID != "") || pendingCreate

	if !hasRow {
		return p.createTask(input, route, card)
	}

	statusChanged := existing.StatusID != input.StatusID
	assignees := model.NormalizeForumAssigneeIDs(input.Assignees)
	assigneesChanged := !sameIDs(model.ForumAssigneeIDs(existing), assignees)

	result := PlanResult{}
	if statusChanged {
		if err := p.applyStatusChange(input, existing, route, hasPost, card, &result); err != nil {
			return result, err
		}
	}
	if assigneesChanged && hasPost {
		if err := p.queueAssigneeSignal(input, existing, assignees); err != nil {
			return result, err
		}
	}
	if err := p.updateTaskRow(input, existing.StatusID, assignees); err != nil {
		return result, err
	}
	return result, nil
}

// createTask handles the first observation of a task after the baseline: the
// task is recorded, and a trigger status opens a thread.
func (p Planner) createTask(input TaskInput, route model.ProductionForumRoute, card func() (json.RawMessage, error)) (PlanResult, error) {
	row := forumTaskRow(input, p.Now)
	if err := p.DB.Create(&row).Error; err != nil {
		return PlanResult{}, err
	}
	result := PlanResult{}
	if !IsTriggerRole(input.StatusRole) {
		return result, nil
	}
	if err := p.queueCreateThread(input, route, input.StatusRole, card, &result); err != nil {
		return result, err
	}
	return result, nil
}
func (p Planner) applyStatusChange(input TaskInput, existing model.ForumTask, route model.ProductionForumRoute, hasPost bool, card func() (json.RawMessage, error), result *PlanResult) error {
	if !hasPost {
		if IsTriggerRole(input.StatusRole) {
			return p.queueCreateThread(input, route, input.StatusRole, card, result)
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
	if !IsTriggerRole(input.StatusRole) {
		return nil
	}
	return p.queueStatusSignal(input, existing, result)
}

func (p Planner) queueCreateThread(input TaskInput, route model.ProductionForumRoute, role string, card func() (json.RawMessage, error), result *PlanResult) error {
	rendered, err := card()
	if err != nil {
		return fmt.Errorf("render forum card: %w", err)
	}
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
	_, err = model.EnqueueForumOutbox(p.DB, model.ForumOutbox{
		TaskID:    input.TaskID,
		Kind:      model.ForumOutboxCreateThread,
		DedupeKey: model.ForumCreateThreadDedupeKey(input.TaskID, 0),
		Payload:   string(payload),
		State:     model.ForumOutboxPending,
		NotBefore: now,
		CreatedAt: now,
	})
	if err != nil && !errors.Is(err, model.ErrForumOutboxDuplicate) {
		return err
	}
	result.Created = true
	if err := p.queueStatusSignal(input, model.ForumTask{StatusID: input.StatusID}, result); err != nil {
		return err
	}
	return p.updateTaskRow(input, input.StatusID, model.NormalizeForumAssigneeIDs(input.Assignees))
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
	intent := intentForRole(input.StatusRole)
	if intent == "" {
		return nil
	}
	payload, err := json.Marshal(model.ForumPostMessagePayload{
		Intent:         intent,
		WindowOpenedAt: p.Now,
		BaseStatusID:   existing.StatusID,
	})
	if err != nil {
		return err
	}
	now := p.Now
	_, err = model.CoalesceForumPostMessage(p.DB, model.ForumOutbox{
		TaskID:      input.TaskID,
		Kind:        model.ForumOutboxPostMessage,
		DedupeKey:   model.ForumPostMessageCoalesceKey(intent, input.TaskID) + ":" + now.UTC().Format(time.RFC3339Nano),
		CoalesceKey: model.ForumPostMessageCoalesceKey(intent, input.TaskID),
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
	payload, err := json.Marshal(model.ForumPostMessagePayload{
		Intent:         model.ForumIntentAssign,
		WindowOpenedAt: p.Now,
		BaseAssignees:  model.ForumAssigneeIDs(existing),
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
	updates := map[string]interface{}{
		"status_id":        input.StatusID,
		"assignee_ids":     encodeIDs(assignees),
		"last_comment_at":  input.CommentDate,
		"kitsu_updated_at": input.KitsuUpdated,
		"last_seen_at":     p.Now,
		"title":            model.TruncateForumTitle(input.Title),
	}
	if fromStatusID != input.StatusID {
		updates["status_since"] = p.Now
	}
	if err := p.DB.Model(&model.ForumTask{}).Where("task_id = ?", input.TaskID).Updates(updates).Error; err != nil {
		return err
	}
	if fromStatusID == input.StatusID {
		return nil
	}
	return p.DB.Create(&model.ForumTransition{
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

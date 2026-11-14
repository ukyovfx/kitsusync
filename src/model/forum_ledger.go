package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Forum ledger tables (stage D). Additive only: the lane pipeline keeps using
// Task/NotificationDeliveryEvent untouched until stage F.

const (
	ForumEntityKindShot  = "shot"
	ForumEntityKindAsset = "asset"

	ForumOutboxPending   = "pending"
	ForumOutboxSending   = "sending"
	ForumOutboxDone      = "done"
	ForumOutboxFailed    = "failed"
	ForumOutboxCancelled = "cancelled"

	ForumOutboxCreateThread = "create_thread"
	ForumOutboxSyncThread   = "sync_thread"
	ForumOutboxPostMessage  = "post_message"
	ForumOutboxWarnMessage  = "warn_message"

	// ForumArchiveDoneMinutes / ForumArchiveOtherMinutes follow the design
	// tuning values; they are not user-configurable yet.
	ForumArchiveDoneMinutes  = 4320
	ForumArchiveOtherMinutes = 10080

	// ForumMentionWindow is the aggregation window reserved for stage E. Stage D
	// queues post_message rows with not_before = window start + this value so
	// aggregation can be inserted later without changing the ledger shape.
	ForumMentionWindow = 3 * time.Minute

	// ForumMaxAttempts bounds transient retries before a row is failed.
	ForumMaxAttempts = 20

	forumTitleMaxRunes = 100
)

// ErrForumOutboxDuplicate reports that a dedupe_key already exists.
var ErrForumOutboxDuplicate = errors.New("forum outbox row already exists")

// ForumBaseline records that a (project, task type) pair has been seeded.
type ForumBaseline struct {
	ProjectID   string `gorm:"primaryKey;size:64"`
	TaskTypeID  string `gorm:"primaryKey;size:64"`
	CompletedAt time.Time
	TaskCount   int
}

func (ForumBaseline) TableName() string { return "forum_baselines" }

// ForumTask is the per-task ledger row.
type ForumTask struct {
	TaskID           string `gorm:"primaryKey;size:64"`
	ProjectID        string `gorm:"index:idx_forum_task_project_type,priority:1;size:64;not null"`
	TaskTypeID       string `gorm:"index:idx_forum_task_project_type,priority:2;size:64;not null"`
	EntityID         string `gorm:"size:64"`
	EntityKind       string `gorm:"size:16"`
	Title            string `gorm:"size:255"`
	StatusID         string `gorm:"size:64"`
	StatusSince      time.Time
	AssigneeIDs      string `gorm:"type:text"`
	LastCommentAt    string `gorm:"size:64"`
	KitsuUpdatedAt   string `gorm:"size:64"`
	ForumID          string `gorm:"size:64"`
	ThreadID         string `gorm:"size:64;uniqueIndex:idx_forum_task_thread"`
	ThreadGeneration int    `gorm:"not null;default:0"`
	FirstSeenAt      time.Time
	LastSeenAt       time.Time
}

func (ForumTask) TableName() string { return "forum_tasks" }

// ForumTransition is the status history used by stage E flap detection.
type ForumTransition struct {
	ID           uint      `gorm:"primaryKey"`
	TaskID       string    `gorm:"index:idx_forum_transition_task,priority:1;size:64;not null"`
	At           time.Time `gorm:"index:idx_forum_transition_task,priority:2"`
	FromStatusID string    `gorm:"size:64"`
	ToStatusID   string    `gorm:"size:64"`
}

func (ForumTransition) TableName() string { return "forum_task_transitions" }

// ForumOutbox is the durable send queue.
type ForumOutbox struct {
	ID             uint      `gorm:"primaryKey;autoIncrement"`
	TaskID         string    `gorm:"index:idx_forum_outbox_task,priority:1;size:64"`
	Kind           string    `gorm:"size:32;not null"`
	DedupeKey      string    `gorm:"size:191;uniqueIndex;not null"`
	CoalesceKey    string    `gorm:"size:191"`
	Payload        string    `gorm:"type:text"`
	State          string    `gorm:"size:16;not null;index:idx_forum_outbox_due,priority:1"`
	NotBefore      time.Time `gorm:"index:idx_forum_outbox_due,priority:2"`
	Attempts       int
	NextAttemptAt  time.Time
	ClaimedAt      *time.Time
	Rendered       string `gorm:"type:text"`
	Result         string `gorm:"type:text"`
	LastErrorClass string `gorm:"size:64"`
	Note           string `gorm:"size:191"`
	CreatedAt      time.Time
	FinishedAt     *time.Time
}

func (ForumOutbox) TableName() string { return "forum_outbox" }

// ForumCreateThreadPayload is the payload of a create_thread row.
type ForumCreateThreadPayload struct {
	ForumID        string `json:"forum_id"`
	Name           string `json:"name"`
	Role           string `json:"role"`
	ArchiveMinutes int    `json:"archive_minutes"`
	// Card is the rendered first message; stage D stores it as JSON so the
	// executor can send it without re-reading Kitsu. It is omitted once the
	// thread exists, because the executor only needs the title to recover.
	Card json.RawMessage `json:"card,omitempty"`
}

// ForumPostMessagePayload is the payload of a post_message row. Stage D sends
// the signal immediately (after the mention window); stage E will aggregate
// using the same fields.
type ForumPostMessagePayload struct {
	Intent         string    `json:"intent"`
	WindowOpenedAt time.Time `json:"window_opened_at"`
	BaseStatusID   string    `json:"base_status_id,omitempty"`
	BaseAssignees  []string  `json:"base_assignees,omitempty"`
}

// ForumPostIntent values.
const (
	ForumIntentStatusWFA    = "wfa"
	ForumIntentStatusRetake = "retake"
	ForumIntentStatusDone   = "done"
	ForumIntentAssign       = "assign"
)

// ForumTaskState is the ledger view a planner needs.
type ForumTaskState struct {
	Row     ForumTask
	Exists  bool
	HasPost bool
}

// ForumAssigneeIDs decodes the stored assignee list.
func ForumAssigneeIDs(row ForumTask) []string {
	if strings.TrimSpace(row.AssigneeIDs) == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(row.AssigneeIDs), &ids); err != nil {
		return nil
	}
	return ids
}

func encodeForumAssigneeIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// NormalizeForumAssigneeIDs drops empty entries and sorts for stable diffing.
func NormalizeForumAssigneeIDs(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// FindForumTaskWithError loads a ledger row.
func FindForumTaskWithError(db *gorm.DB, taskID string) (ForumTask, error) {
	var row ForumTask
	err := db.Where("task_id = ?", taskID).First(&row).Error
	return row, err
}

// ForumTaskHasPendingCreate reports whether a create_thread row is queued or in
// flight for the task. Such a task counts as already having a post.
func ForumTaskHasPendingCreate(db *gorm.DB, taskID string) (bool, error) {
	var count int64
	err := db.Model(&ForumOutbox{}).
		Where("task_id = ? AND kind = ? AND state IN ?", taskID, ForumOutboxCreateThread, []string{ForumOutboxPending, ForumOutboxSending}).
		Count(&count).Error
	return count > 0, err
}

// ForumBaselineExists reports whether the (project, task type) pair is seeded.
func ForumBaselineExists(db *gorm.DB, projectID, taskTypeID string) (bool, error) {
	var count int64
	err := db.Model(&ForumBaseline{}).
		Where("project_id = ? AND task_type_id = ?", projectID, taskTypeID).
		Count(&count).Error
	return count > 0, err
}

// RecordForumBaselineTasks records the tasks that exist when forum mode is
// switched on. No events and no outbox rows are produced; the baseline marker
// is written in the same transaction so an interrupted run is retried.
func RecordForumBaselineTasks(db *gorm.DB, projectID, taskTypeID string, rows []ForumTask) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			row.ProjectID = projectID
			row.TaskTypeID = taskTypeID
			row.ThreadID = ""
			row.ThreadGeneration = 0
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ForumBaseline{
			ProjectID:   projectID,
			TaskTypeID:  taskTypeID,
			CompletedAt: time.Now().UTC(),
			TaskCount:   len(rows),
		}).Error
	})
}

// EnqueueForumOutbox inserts an outbox row unless its dedupe key exists.
func EnqueueForumOutbox(db *gorm.DB, row ForumOutbox) (uint, error) {
	if row.State == "" {
		row.State = ForumOutboxPending
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	if row.NextAttemptAt.IsZero() {
		row.NextAttemptAt = row.NotBefore
	}
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dedupe_key"}},
		DoNothing: true,
	}).Create(&row).Error
	if err != nil {
		return 0, err
	}
	if row.ID == 0 {
		return 0, ErrForumOutboxDuplicate
	}
	return row.ID, nil
}

// CoalesceForumPostMessage keeps a single pending post_message row per intent
// for a task. The window start (not_before) is never extended.
func CoalesceForumPostMessage(db *gorm.DB, row ForumOutbox) (uint, error) {
	if row.State == "" {
		row.State = ForumOutboxPending
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	if row.NextAttemptAt.IsZero() {
		row.NextAttemptAt = row.NotBefore
	}
	if row.CoalesceKey == "" {
		return EnqueueForumOutbox(db, row)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		var existing ForumOutbox
		err := tx.Where("coalesce_key = ? AND state = ?", row.CoalesceKey, ForumOutboxPending).First(&existing).Error
		if err == nil {
			// Update the payload only; not_before stays at the window start.
			return tx.Model(&ForumOutbox{}).Where("id = ?", existing.ID).
				Update("payload", row.Payload).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		_, err = EnqueueForumOutbox(tx, row)
		return err
	})
	if err != nil {
		return 0, err
	}
	var saved ForumOutbox
	if err := db.Where("coalesce_key = ? AND state = ?", row.CoalesceKey, ForumOutboxPending).First(&saved).Error; err != nil {
		return 0, err
	}
	return saved.ID, nil
}

// ClaimNextForumOutboxRow selects the oldest due row whose task has no earlier
// unfinished work. It returns nil when nothing is runnable.
func ClaimNextForumOutboxRow(db *gorm.DB, now time.Time) (*ForumOutbox, error) {
	var row ForumOutbox
	err := db.Transaction(func(tx *gorm.DB) error {
		var candidates []ForumOutbox
		if err := tx.Where("state = ? AND next_attempt_at <= ?", ForumOutboxPending, now).
			Order("id asc").Limit(50).Find(&candidates).Error; err != nil {
			return err
		}
		for _, candidate := range candidates {
			runnable, err := forumOutboxRowRunnable(tx, candidate)
			if err != nil {
				return err
			}
			if !runnable {
				continue
			}
			claimedAt := now
			res := tx.Model(&ForumOutbox{}).
				Where("id = ? AND state = ?", candidate.ID, ForumOutboxPending).
				Updates(map[string]interface{}{
					"state":      ForumOutboxSending,
					"claimed_at": claimedAt,
					"attempts":   gorm.Expr("attempts + 1"),
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				continue
			}
			if err := tx.Where("id = ?", candidate.ID).First(&row).Error; err != nil {
				return err
			}
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if row.ID == 0 {
		return nil, nil
	}
	return &row, nil
}

// forumOutboxRowRunnable enforces the per-task ordering rules: nothing runs
// until create_thread is done, and post/warn rows wait for pending sync rows.
func forumOutboxRowRunnable(tx *gorm.DB, row ForumOutbox) (bool, error) {
	if row.TaskID == "" {
		return true, nil
	}
	if row.Kind != ForumOutboxCreateThread {
		// Any earlier row of the same task must finish first (id order), so a
		// post can never overtake the create_thread or sync_thread before it.
		var earlier int64
		if err := tx.Model(&ForumOutbox{}).
			Where("task_id = ? AND id < ? AND state IN ?", row.TaskID, row.ID, []string{ForumOutboxPending, ForumOutboxSending}).
			Count(&earlier).Error; err != nil {
			return false, err
		}
		if earlier > 0 {
			return false, nil
		}
	}
	if row.Kind == ForumOutboxPostMessage || row.Kind == ForumOutboxWarnMessage {
		var pendingSyncs int64
		if err := tx.Model(&ForumOutbox{}).
			Where("task_id = ? AND kind IN ? AND state IN ?", row.TaskID, []string{ForumOutboxCreateThread, ForumOutboxSyncThread}, []string{ForumOutboxPending, ForumOutboxSending}).
			Count(&pendingSyncs).Error; err != nil {
			return false, err
		}
		if pendingSyncs > 0 {
			return false, nil
		}
	}
	return true, nil
}

// FinishForumOutboxRow marks a row done.
func FinishForumOutboxRow(db *gorm.DB, id uint, result string) error {
	now := time.Now().UTC()
	return db.Model(&ForumOutbox{}).Where("id = ?", id).Updates(map[string]interface{}{
		"state":       ForumOutboxDone,
		"result":      result,
		"rendered":    "",
		"finished_at": now,
	}).Error
}

// FailForumOutboxRow marks a row failed permanently.
func FailForumOutboxRow(db *gorm.DB, id uint, errorClass, note string) error {
	now := time.Now().UTC()
	return db.Model(&ForumOutbox{}).Where("id = ?", id).Updates(map[string]interface{}{
		"state":            ForumOutboxFailed,
		"last_error_class": errorClass,
		"note":             truncateForumNote(note),
		"finished_at":      now,
	}).Error
}

// CancelForumOutboxRow cancels a pending row with a reason.
func CancelForumOutboxRow(db *gorm.DB, id uint, note string) error {
	now := time.Now().UTC()
	return db.Model(&ForumOutbox{}).Where("id = ? AND state IN ?", id, []string{ForumOutboxPending, ForumOutboxSending}).Updates(map[string]interface{}{
		"state":       ForumOutboxCancelled,
		"note":        truncateForumNote(note),
		"finished_at": now,
	}).Error
}

// RetryForumOutboxRow returns a row to pending after a transient failure.
func RetryForumOutboxRow(db *gorm.DB, id uint, errorClass string, next time.Time) error {
	return db.Model(&ForumOutbox{}).Where("id = ?", id).Updates(map[string]interface{}{
		"state":            ForumOutboxPending,
		"last_error_class": errorClass,
		"next_attempt_at":  next,
		"claimed_at":       nil,
	}).Error
}

// ReleaseForumOutboxRow returns a row to pending without consuming the attempt
// (used after a 429, which must not count against the retry budget).
func ReleaseForumOutboxRow(db *gorm.DB, id uint, next time.Time) error {
	return db.Model(&ForumOutbox{}).Where("id = ?", id).Updates(map[string]interface{}{
		"state":           ForumOutboxPending,
		"next_attempt_at": next,
		"claimed_at":      nil,
		"attempts":        gorm.Expr("CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END"),
	}).Error
}

// StoreForumOutboxRendered persists the rendered body before sending so a crash
// can be reconciled without duplicating the message.
func StoreForumOutboxRendered(db *gorm.DB, id uint, rendered string) error {
	return db.Model(&ForumOutbox{}).Where("id = ?", id).Update("rendered", rendered).Error
}

// ResetForumOutboxToPending re-queues a sending row (startup reconciliation).
func ResetForumOutboxToPending(db *gorm.DB, id uint, note string) error {
	return db.Model(&ForumOutbox{}).Where("id = ?", id).Updates(map[string]interface{}{
		"state":      ForumOutboxPending,
		"claimed_at": nil,
		"note":       truncateForumNote(note),
	}).Error
}

// ListForumOutboxSending returns rows left in flight by a crash.
func ListForumOutboxSending(db *gorm.DB) ([]ForumOutbox, error) {
	var rows []ForumOutbox
	err := db.Where("state = ?", ForumOutboxSending).Order("id asc").Find(&rows).Error
	return rows, err
}

// PendingForumOutboxForTask reports whether the task still has queued work.
func PendingForumOutboxForTask(db *gorm.DB, taskID string) (bool, error) {
	var count int64
	err := db.Model(&ForumOutbox{}).
		Where("task_id = ? AND state IN ?", taskID, []string{ForumOutboxPending, ForumOutboxSending}).
		Count(&count).Error
	return count > 0, err
}

// SetForumTaskThread records the thread created for a task and completes the
// create_thread row in the same transaction.
func SetForumTaskThread(db *gorm.DB, taskID, forumID, threadID string, outboxID uint) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&ForumTask{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{
			"thread_id":    threadID,
			"forum_id":     forumID,
			"last_seen_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		if outboxID != 0 {
			return FinishForumOutboxRow(tx, outboxID, "thread="+threadID)
		}
		return nil
	})
}

// ClearForumTaskThread drops a lost thread and bumps the generation so a fresh
// create_thread row can be queued.
func ClearForumTaskThread(db *gorm.DB, taskID string) (int, error) {
	var generation int
	err := db.Transaction(func(tx *gorm.DB) error {
		var row ForumTask
		if err := tx.Where("task_id = ?", taskID).First(&row).Error; err != nil {
			return err
		}
		generation = row.ThreadGeneration + 1
		return tx.Model(&ForumTask{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{
			"thread_id":         "",
			"thread_generation": generation,
		}).Error
	})
	return generation, err
}

// ForumCreateThreadDedupeKey builds the dedupe key for a thread creation.
func ForumCreateThreadDedupeKey(taskID string, generation int) string {
	return fmt.Sprintf("create:%s:%d", taskID, generation)
}

// ForumSyncThreadCoalesceKey builds the coalesce key for tag/archive syncs.
func ForumSyncThreadCoalesceKey(taskID string) string {
	return "sync:" + taskID
}

// ForumPostMessageCoalesceKey builds the coalesce key for a signal message.
func ForumPostMessageCoalesceKey(intent, taskID string) string {
	return fmt.Sprintf("msg:%s:%s", intent, taskID)
}

// TruncateForumTitle caps a thread title at 100 runes with an ellipsis.
func TruncateForumTitle(title string) string {
	runes := []rune(strings.TrimSpace(title))
	if len(runes) <= forumTitleMaxRunes {
		return string(runes)
	}
	return string(runes[:forumTitleMaxRunes-1]) + "…"
}

func truncateForumNote(note string) string {
	runes := []rune(note)
	if len(runes) <= 191 {
		return note
	}
	return string(runes[:190]) + "…"
}

// ForumArchiveMinutesForRole maps a forum role to the auto-archive duration.
func ForumArchiveMinutesForRole(role string) int {
	if role == ForumRoleDone {
		return ForumArchiveDoneMinutes
	}
	return ForumArchiveOtherMinutes
}

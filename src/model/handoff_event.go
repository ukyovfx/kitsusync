package model

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Handoff event states. K2 only records events; the sending side reads them,
// and the receiver's reply moves a reported event to answered or retaken.
const (
	HandoffEventPending   = "pending"
	HandoffEventNotified  = "notified"
	HandoffEventNoTarget  = "no_target" // no receiving task, or nobody to notify
	HandoffEventCancelled = "cancelled" // superseded before it was sent
	HandoffEventAnswered  = "answered"  // the receiver accepted the delivery
	HandoffEventRetaken   = "retaken"   // the receiver asked for a retake
)

// HandoffEvent is one detected material handoff: a source task delivered a
// product family and a receiving task on the same shot/asset should be told.
type HandoffEvent struct {
	ID uint `gorm:"primaryKey"`
	// CommentID is the Kitsu comment that carried the delivery table. It makes
	// detection idempotent across re-polls and restarts.
	CommentID    string `gorm:"uniqueIndex;size:64;not null"`
	ProductionID string `gorm:"index:idx_handoff_event_production;size:64;not null"`
	// Source side.
	SourceTaskID       string `gorm:"size:64;not null"`
	SourceTaskTypeID   string `gorm:"size:64"`
	SourceTaskTypeName string
	SourceAuthorID     string `gorm:"size:64"`
	SourceAuthorName   string
	EntityID           string `gorm:"size:64"`
	EntityKind         string `gorm:"size:16"`
	EntityName         string
	// Product is what the source delivered, read from the publish table.
	ProductType    string
	ProductVersion string
	ProductName    string
	// Receiving side: the task and the Kitsu person IDs that should be told.
	NotifyTaskID       string `gorm:"size:64"`
	NotifyTaskTypeID   string `gorm:"size:64"`
	NotifyTaskTypeName string
	NotifyPersonIDs    string `gorm:"type:text"`
	// FallbackUsed records that the receiving task had no assignee, so the
	// configured no-assignee handling chose the recipients.
	FallbackUsed string `gorm:"size:16"`
	State        string `gorm:"size:16;not null"`
	DetectedAt   time.Time
	// SupersededAt marks an event replaced by a newer delivery of the same
	// product to the same receiving task, so it is not reported twice.
	SupersededAt *time.Time
}

func (HandoffEvent) TableName() string { return "handoff_events" }

// SplitHandoffPersonIDs splits the stored recipient list.
func SplitHandoffPersonIDs(raw string) []string {
	return SplitHandoffNotifyIDs(raw)
}

// RecordHandoffEvent inserts an event once per comment. existing is true when
// the comment was already eventized, which is not an error.
func RecordHandoffEvent(db *gorm.DB, event HandoffEvent) (existing bool, err error) {
	if event.State == "" {
		event.State = HandoffEventPending
	}
	if event.DetectedAt.IsZero() {
		event.DetectedAt = time.Now().UTC()
	}
	event.NotifyPersonIDs = JoinHandoffNotifyIDs(SplitHandoffNotifyIDs(event.NotifyPersonIDs))
	insert := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
	if insert.Error != nil {
		return false, insert.Error
	}
	if event.ID == 0 {
		// The comment was already eventized; re-polling is not an error.
		return true, nil
	}
	if strings.TrimSpace(event.NotifyTaskID) == "" {
		return false, nil
	}
	// A newer delivery of the same product to the same receiving task replaces
	// the older pending report, which stays in the table as history. Another
	// product still waits for its own reply.
	var stale []HandoffEvent
	err = db.Where("notify_task_id = ? AND product_type = ? AND product_name = ? AND state = ? AND id <> ?",
		event.NotifyTaskID, event.ProductType, event.ProductName, HandoffEventPending, event.ID).
		Find(&stale).Error
	if err != nil || len(stale) == 0 {
		return false, err
	}
	for i := range stale {
		stale[i].State = HandoffEventCancelled
		stale[i].SupersededAt = &event.DetectedAt
	}
	return false, db.Save(&stale).Error
}

// FindHandoffEvent returns one event by comment ID.
func FindHandoffEvent(db *gorm.DB, commentID string) (HandoffEvent, error) {
	var event HandoffEvent
	err := db.Where("comment_id = ?", strings.TrimSpace(commentID)).First(&event).Error
	return event, err
}

// PendingHandoffEvents lists unsent events, oldest first.
func PendingHandoffEvents(db *gorm.DB) ([]HandoffEvent, error) {
	var events []HandoffEvent
	err := db.Where("state = ?", HandoffEventPending).Order("id asc").Find(&events).Error
	return events, err
}

// UnansweredHandoffEvents lists the events a morning digest still has to
// remind: a pending event waits for the poller to send it right away, a
// notified one is already out and waits for a reply until it is answered, so
// it stays listed. A cancelled event is answered by a newer delivery.
func UnansweredHandoffEvents(db *gorm.DB) ([]HandoffEvent, error) {
	var events []HandoffEvent
	err := db.Where("state IN ?", []string{HandoffEventPending, HandoffEventNotified}).Order("id asc").Find(&events).Error
	return events, err
}

// FallbackHandoffEvents lists the handoffs that had no assignee on the
// receiving task and went to the Production's admin fallback, newest first.
func FallbackHandoffEvents(db *gorm.DB, limit int) ([]HandoffEvent, error) {
	var events []HandoffEvent
	err := db.Where("fallback_used = ?", HandoffFallbackAdmin).
		Order("detected_at desc, id desc").Limit(limit).Find(&events).Error
	return events, err
}

// CountFallbackHandoffEvents counts every recorded admin fallback.
func CountFallbackHandoffEvents(db *gorm.DB) (int64, error) {
	var count int64
	err := db.Model(&HandoffEvent{}).Where("fallback_used = ?", HandoffFallbackAdmin).Count(&count).Error
	return count, err
}

// MarkHandoffNotified moves a pending event to sent. It reports false when the
// event was already sent, so two pollers never report the same delivery twice.
func MarkHandoffNotified(db *gorm.DB, id uint) (bool, error) {
	if db == nil || id == 0 {
		return false, nil
	}
	claim := db.Model(&HandoffEvent{}).
		Where("id = ? AND state = ?", id, HandoffEventPending).
		Update("state", HandoffEventNotified)
	return claim.RowsAffected > 0, claim.Error
}

// MarkHandoffReplied moves a reported event to the receiver's answer. It
// reports false when the event was already answered or retaken, so a double
// press never writes a second comment.
func MarkHandoffReplied(db *gorm.DB, id uint, state string) (bool, error) {
	if db == nil || id == 0 {
		return false, nil
	}
	if state != HandoffEventAnswered && state != HandoffEventRetaken {
		return false, errors.New("handoff reply state is invalid")
	}
	claim := db.Model(&HandoffEvent{}).
		Where("id = ? AND state IN ?", id, []string{HandoffEventPending, HandoffEventNotified}).
		Update("state", state)
	return claim.RowsAffected > 0, claim.Error
}

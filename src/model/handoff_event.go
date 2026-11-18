package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Handoff event states. K2 only records events; the sending side reads them.
const (
	HandoffEventPending   = "pending"
	HandoffEventNotified  = "notified"
	HandoffEventNoTarget  = "no_target" // no receiving task, or nobody to notify
	HandoffEventCancelled = "cancelled" // superseded before it was sent
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
	// A newer delivery to the same receiving task replaces the older pending
	// report, which stays in the table as history.
	var stale []HandoffEvent
	err = db.Where("notify_task_id = ? AND state = ? AND id <> ?", event.NotifyTaskID, HandoffEventPending, event.ID).
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

package model

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// NotificationAggregationWindow is how long a task×destination transition waits
// so rapid round-trips collapse into one delivery.
const NotificationAggregationWindow = 3 * time.Minute

// NotificationBulkSummaryThreshold is the group size at which a window close
// sends one summary message per destination instead of individual posts.
const NotificationBulkSummaryThreshold = 10

// NotificationFlapThreshold is the number of task transitions within an hour
// after which the task is downgraded to digest-only delivery.
const NotificationFlapThreshold = 4

// NotificationAggregationBuffer durably holds transitions whose aggregation
// window has not closed yet. Rows are keyed by task and destination and are
// only removed after their dispatch outcome is owned by the delivery ledger,
// so a crash can never lose a notification.
type NotificationAggregationBuffer struct {
	ID              uint   `gorm:"primaryKey"`
	TaskID          string `gorm:"not null;uniqueIndex:idx_aggregation_task_destination"`
	ProductionID    string `gorm:"not null;index"`
	TaskTypeID      string `gorm:"not null"`
	DestinationID   string `gorm:"not null;uniqueIndex:idx_aggregation_task_destination"`
	LaneType        string `gorm:"not null;default:channel"`
	WebhookURL      string
	TransitionCount int
	LastStatus      string
	LastPayloadJSON string `gorm:"type:text"`
	LastIdentity    string `gorm:"size:64"`
	FlapDowngraded  bool
	FirstSeenAt     time.Time `gorm:"index"`
	LastSeenAt      time.Time
	UpdatedAt       time.Time
}

// BufferNotificationTransition upserts the newest transition for a
// task×destination pair. The first transition opens the window; later ones
// refresh the payload so only the final state inside the window is delivered.
// Re-observing the same transition (polls before the window closes) is a no-op.
// Returns true when the buffer accepted a new transition.
func BufferNotificationTransition(db *gorm.DB, buffer NotificationAggregationBuffer) (bool, error) {
	if db == nil || strings.TrimSpace(buffer.TaskID) == "" || strings.TrimSpace(buffer.DestinationID) == "" {
		return false, gorm.ErrInvalidData
	}
	now := time.Now().UTC()
	buffer.FirstSeenAt = now
	buffer.LastSeenAt = now
	buffer.UpdatedAt = now
	buffered := false
	err := db.Transaction(func(tx *gorm.DB) error {
		// Flap detection looks at the delivery ledger over the last hour; once
		// a task round-trips too often it is downgraded to digest-only.
		transitions, countErr := CountTaskTransitionsSince(tx, buffer.TaskID, now.Add(-time.Hour))
		if countErr != nil {
			return countErr
		}
		var existing NotificationAggregationBuffer
		err := tx.Where("task_id = ? AND destination_id = ?", buffer.TaskID, buffer.DestinationID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			buffer.TransitionCount = 1
			buffer.FlapDowngraded = transitions >= NotificationFlapThreshold
			buffered = true
			return tx.Create(&buffer).Error
		}
		if err != nil {
			return err
		}
		if existing.LastIdentity == buffer.LastIdentity {
			// Same transition re-observed by a later poll: the window keeps its
			// schedule and its transition count.
			return nil
		}
		updates := map[string]any{
			"transition_count":  existing.TransitionCount + 1,
			"last_status":       buffer.LastStatus,
			"last_payload_json": buffer.LastPayloadJSON,
			"last_identity":     buffer.LastIdentity,
			"last_seen_at":      now,
			"updated_at":        now,
			"webhook_url":       buffer.WebhookURL,
			"lane_type":         buffer.LaneType,
			"task_type_id":      buffer.TaskTypeID,
		}
		if !existing.FlapDowngraded {
			updates["flap_downgraded"] = int(transitions)+existing.TransitionCount >= NotificationFlapThreshold
		}
		buffered = true
		return tx.Model(&NotificationAggregationBuffer{}).Where("id = ?", existing.ID).Updates(updates).Error
	})
	return buffered, err
}

// CountTaskTransitionsSince counts the task's STATUS round-trips recorded in
// the delivery journal after the given time; it is the flap-detection signal.
// Comment-only changes share the status of the preceding row, so only rows
// whose status differs from the previous row count — otherwise comment traffic
// would inflate the round-trip count and downgrade quiet tasks. The last row
// before the window provides the baseline status.
func CountTaskTransitionsSince(db *gorm.DB, taskID string, since time.Time) (int64, error) {
	if db == nil || strings.TrimSpace(taskID) == "" {
		return 0, gorm.ErrInvalidData
	}
	var baseline NotificationDeliveryEvent
	hasBaseline := db.Select("task_status").
		Where("task_id = ? AND created_at < ?", strings.TrimSpace(taskID), since).
		Order("id desc").First(&baseline).Error == nil
	previous := ""
	if hasBaseline {
		previous = strings.ToUpper(strings.TrimSpace(baseline.TaskStatus))
	}
	var rows []NotificationDeliveryEvent
	if err := db.Select("task_status").
		Where("task_id = ? AND created_at >= ?", strings.TrimSpace(taskID), since).
		Order("id asc").Find(&rows).Error; err != nil {
		return 0, err
	}
	count := int64(0)
	for _, row := range rows {
		status := strings.ToUpper(strings.TrimSpace(row.TaskStatus))
		if status != "" && status != previous {
			count++
		}
		previous = status
	}
	return count, nil
}

// TakeDueAggregatedNotifications returns every buffer whose window has closed,
// grouped by destination ID. Rows stay in the table until the caller deletes
// them after the dispatch outcome is durable.
func TakeDueAggregatedNotifications(db *gorm.DB, now time.Time, window time.Duration) (map[string][]NotificationAggregationBuffer, error) {
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var rows []NotificationAggregationBuffer
	if err := db.Where("first_seen_at <= ?", now.Add(-window)).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	grouped := make(map[string][]NotificationAggregationBuffer)
	for _, row := range rows {
		grouped[row.DestinationID] = append(grouped[row.DestinationID], row)
	}
	return grouped, nil
}

// DeleteAggregationBuffers removes buffer rows whose dispatch outcome the
// delivery ledger now owns.
func DeleteAggregationBuffers(db *gorm.DB, ids []uint) error {
	if db == nil || len(ids) == 0 {
		return nil
	}
	return db.Where("id IN ?", ids).Delete(&NotificationAggregationBuffer{}).Error
}

// DecodeAggregatedPayload unmarshals a buffered Kitsu payload.
func DecodeAggregatedPayload(raw string) ([]byte, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, gorm.ErrInvalidData
	}
	return json.RawMessage(raw), nil
}

// RecordAggregatedNotificationConsumption writes the ledger trace for a
// transition that was delivered as part of an aggregate (bulk summary or flap
// downgrade) instead of an individual post. Idempotent per transition. For
// bulk-summary members the summary message ID and webhook row are recorded so
// the summary can be reconciled when a contained WFA task resolves.
func RecordAggregatedNotificationConsumption(db *gorm.DB, taskID, productionID, taskTypeID, taskStatus, taskUpdatedAt, commentID, commentUpdatedAt string, isAssign bool, reason, summaryMessageID string, webhookRowID uint) error {
	if err := RecordNotificationOnlyDelivery(db, taskID, productionID, taskTypeID, taskStatus, taskUpdatedAt, commentID, commentUpdatedAt, isAssign); err != nil {
		return err
	}
	updates := map[string]any{"failure_class": strings.TrimSpace(reason)}
	if strings.TrimSpace(summaryMessageID) != "" {
		updates["discord_message_id"] = strings.TrimSpace(summaryMessageID)
		updates["webhook_row_id"] = webhookRowID
	}
	intent := NotificationDeliveryIntent{TaskID: taskID, ProductionID: productionID, TaskTypeID: taskTypeID, TaskStatus: taskStatus, TaskUpdatedAt: taskUpdatedAt, CommentID: commentID, CommentUpdatedAt: commentUpdatedAt, IsAssignNotification: isAssign, LaneType: LaneRecordOnly}
	return db.Model(&NotificationDeliveryEvent{}).Where("event_id = ?", NotificationEventIdentity(intent)).Updates(updates).Error
}

// NotificationDeliveryMetrics aggregates the delivery ledger by lane and
// disposition plus failure classes, and the aggregation buffer state. It backs
// the log-only metrics line; there is intentionally no UI.
type NotificationDeliveryMetrics struct {
	LaneDisposition       map[string]int64
	FailureClasses        map[string]int64
	AggregationBuffers    int64
	FlapDowngradedBuffers int64
}

func BuildNotificationDeliveryMetrics(db *gorm.DB) (*NotificationDeliveryMetrics, error) {
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	metrics := &NotificationDeliveryMetrics{
		LaneDisposition: map[string]int64{},
		FailureClasses:  map[string]int64{},
	}
	var rows []struct {
		LaneType    string
		Disposition string
		FailureClass string
		Count       int64
	}
	if err := db.Model(&NotificationDeliveryEvent{}).
		Select("lane_type, disposition, IFNULL(failure_class, '') AS failure_class, COUNT(*) AS count").
		Group("lane_type, disposition, failure_class").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		metrics.LaneDisposition[strings.ToLower(row.LaneType)+"|"+row.Disposition] += row.Count
		if strings.TrimSpace(row.FailureClass) != "" {
			metrics.FailureClasses[row.FailureClass] += row.Count
		}
	}
	if err := db.Model(&NotificationAggregationBuffer{}).Count(&metrics.AggregationBuffers).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&NotificationAggregationBuffer{}).Where("flap_downgraded = ?", true).Count(&metrics.FlapDowngradedBuffers).Error; err != nil {
		return nil, err
	}
	return metrics, nil
}

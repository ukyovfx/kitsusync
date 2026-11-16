package model

import "time"

// NotificationAggregationBuffer is a legacy lane aggregation row. The lane
// pipeline was removed; the table stays so existing rows are not lost.
type NotificationAggregationBuffer struct {
	ID           uint   `gorm:"primaryKey"`
	TaskID       string `gorm:"not null;index"`
	ProductionID string `gorm:"not null;index"`
	TaskTypeID   string `gorm:"not null;index"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

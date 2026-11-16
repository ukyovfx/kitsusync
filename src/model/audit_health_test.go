package model

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newAuditHealthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestRecentAuditHealthUsesTwentyFourHourSeverityPrecedence(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		logs []AuditLog
		want AuditHealth
	}{
		{"success only", []AuditLog{{CreatedAt: now, Success: true}}, AuditHealthNormal},
		{"retry needs review", []AuditLog{{CreatedAt: now, Success: true, RetryCount: 1}}, AuditHealthNeedsReview},
		{"warning needs review", []AuditLog{{CreatedAt: now, Success: true, NewStatus: "warning"}}, AuditHealthNeedsReview},
		{"failure is abnormal", []AuditLog{{CreatedAt: now, Success: false, ErrorMessage: "failed"}}, AuditHealthAbnormal},
		{"failure overrides later success", []AuditLog{{CreatedAt: now.Add(-time.Hour), Success: false}, {CreatedAt: now, Success: true}}, AuditHealthAbnormal},
		{"warning overrides success", []AuditLog{{CreatedAt: now.Add(-time.Hour), Success: true}, {CreatedAt: now, Success: true, NewStatus: "retry"}}, AuditHealthNeedsReview},
		{"old events are outside window", []AuditLog{{CreatedAt: now.Add(-25 * time.Hour), Success: false}, {CreatedAt: now, Success: true}}, AuditHealthNormal},
		{"no recent events", []AuditLog{{CreatedAt: now.Add(-25 * time.Hour), Success: true}}, AuditHealthNoRecords},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newAuditHealthTestDB(t)
			if err := db.AutoMigrate(&AuditLog{}); err != nil {
				t.Fatal(err)
			}
			for _, log := range tc.logs {
				if err := db.Create(&log).Error; err != nil {
					t.Fatal(err)
				}
			}
			if got := RecentAuditHealth(db, now); got != tc.want {
				t.Fatalf("RecentAuditHealth() = %q, want %q", got, tc.want)
			}
		})
	}
}

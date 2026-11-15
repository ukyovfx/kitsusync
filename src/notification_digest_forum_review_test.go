package main

import (
	"testing"
	"time"

	"app/src/model"
)

// PR #283 の指摘: a failed DM must be recorded as an unsuccessful audit entry.
func TestForumDigestAuditRecordsFailure(t *testing.T) {
	db := forumDigestDB(t)
	defer closeForumDigestDB(t, db)
	seedForumDigestTask(t, db, "task-wfa", "s-wfa", time.Date(2026, 11, 15, 9, 0, 0, 0, jst))

	fixture := newDMFixture(t, "111111111111111111")
	_ = fixture
	morning := time.Date(2026, 11, 16, 9, 0, 0, 0, jst)
	if _, err := sendForumMorningDigests(digestTestConfig(), db, morning, morning.Format("2006-01-02")); err == nil {
		t.Fatal("expected a send error")
	}

	var audits []model.AuditLog
	if err := db.Where("task_id = ? AND actor_name = ?", "morning-digest", "morning-digest").Find(&audits).Error; err != nil {
		t.Fatalf("load audit: %v", err)
	}
	if len(audits) != 1 {
		t.Fatalf("audit rows = %d", len(audits))
	}
	if audits[0].Success {
		t.Fatal("audit recorded Success=true for a failed forum digest")
	}
}

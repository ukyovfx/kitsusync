package main

import (
	"testing"
	"time"

	"app/src/model"
)

// PR #283 の指摘: the forum digest needs its own daily marker. The lane digest
// runs first and marks the same person for the same Production, so a shared key
// silently suppresses the forum DM.
func TestForumDigestUsesItsOwnDailyMarker(t *testing.T) {
	db := forumDigestDB(t)
	defer closeNotificationPipelineDB(t, db)
	seedForumDigestTask(t, db, "task-wfa", "s-wfa", time.Date(2026, 11, 15, 9, 0, 0, 0, jst))

	fixture := newDMFixture(t)
	morning := time.Date(2026, 11, 16, 9, 0, 0, 0, jst)
	today := morning.Format("2006-01-02")

	// The lane digest already sent to this person for this Production today.
	model.SetSetting(db, digestLastSentDateSettingKey+"p1.111111111111111111", today)

	sent, err := sendForumMorningDigests(pipelineConfig(), db, morning, today)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent != 1 {
		t.Fatalf("sent = %d, want the forum digest even though the lane digest ran", sent)
	}
	if fixture.sendsFor("111111111111111111") != 1 {
		t.Fatalf("sends = %d", fixture.sendsFor("111111111111111111"))
	}
	if model.GetSetting(db, forumDigestLastSentDateSettingKey+"p1.111111111111111111") != today {
		t.Fatal("forum marker not set after a successful send")
	}
}

// PR #283 の指摘: a failed DM must be recorded as an unsuccessful audit entry.
func TestForumDigestAuditRecordsFailure(t *testing.T) {
	db := forumDigestDB(t)
	defer closeNotificationPipelineDB(t, db)
	seedForumDigestTask(t, db, "task-wfa", "s-wfa", time.Date(2026, 11, 15, 9, 0, 0, 0, jst))

	fixture := newDMFixture(t, "111111111111111111")
	_ = fixture
	morning := time.Date(2026, 11, 16, 9, 0, 0, 0, jst)
	if _, err := sendForumMorningDigests(pipelineConfig(), db, morning, morning.Format("2006-01-02")); err == nil {
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

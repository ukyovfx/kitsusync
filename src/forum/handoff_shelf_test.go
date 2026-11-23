package forum

import (
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
)

// An unmapped pair whose product is handed off from another Task Type is
// recorded once for the admin. It still creates no event and notifies nobody.
func TestHandoffDetectRecordsSuspiciousShelfForUnmappedPair(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)
	payloads := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-layout", "comment-1", deliverTable),
		receiverPayload("tt-comp", "person-comp"),
	}
	result, err := (HandoffDetector{DB: db}).Detect(payloads)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if result.Events != 0 {
		t.Fatalf("an unmapped pair must not create an event, got %d", result.Events)
	}
	marks := model.ListSuspiciousShelves(db, "prod-1")
	if len(marks) != 1 {
		t.Fatalf("suspicious shelves = %d, want 1", len(marks))
	}
	if marks[0].TaskTypeID != "tt-layout" || marks[0].ProductType != "matchmove" || marks[0].OtherTaskTypeName != "Matchmove" {
		t.Fatalf("mark = %+v", marks[0])
	}
	// Meeting the same pair again records nothing new and audits nothing new.
	if _, err := (HandoffDetector{DB: db}).Detect(payloads); err != nil {
		t.Fatalf("detect again: %v", err)
	}
	if got := model.ListSuspiciousShelves(db, "prod-1"); len(got) != 1 {
		t.Fatalf("after second detect: shelves = %d, want 1", len(got))
	}
	var audits int64
	db.Model(&model.AuditLog{}).Where("task_id = ?", "handoff-shelve-check").Count(&audits)
	if audits != 1 {
		t.Fatalf("shelf audit entries = %d, want 1", audits)
	}
}

// An explicitly shelved pair is recorded the same way when another Task Type
// hands the product off.
func TestHandoffDetectRecordsSuspiciousShelfForShelvedPair(t *testing.T) {
	db := newHandoffDB(t)
	err := model.SaveProductionHandoffSettings(db, model.ProductionHandoffSettings{
		Config: model.ProductionHandoffConfig{
			ProductionID:   "prod-1",
			NotifyTiming:   model.HandoffTimingImmediate,
			NoAssignee:     model.HandoffFallbackAdmin,
			FallbackUserID: "person-lead",
		},
		ProductTypes: []model.ProductionHandoffProductType{{Name: "matchmove"}},
		Routes: []model.ProductionHandoffRoute{
			{TaskTypeID: "tt-mm", TaskTypeName: "Matchmove", ProductType: "matchmove", Flow: model.HandoffFlowHand, NotifyTaskTypeIDs: "tt-comp"},
			{TaskTypeID: "tt-layout", TaskTypeName: "Layout", ProductType: "matchmove", Flow: model.HandoffFlowShelve},
		},
	})
	if err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	payloads := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-layout", "comment-1", deliverTable),
		receiverPayload("tt-comp", "person-comp"),
	}
	result, err := (HandoffDetector{DB: db}).Detect(payloads)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if result.Events != 0 {
		t.Fatalf("a shelved pair must not create an event, got %d", result.Events)
	}
	marks := model.ListSuspiciousShelves(db, "prod-1")
	if len(marks) != 1 || marks[0].TaskTypeID != "tt-layout" {
		t.Fatalf("marks = %+v, want the Layout pair", marks)
	}
}

// A handoff that falls back to the admins leaves an audit entry naming the
// comment, and the fallback count the system status shows.
func TestHandoffDetectFallbackLeavesAuditAndCount(t *testing.T) {
	db := newHandoffDB(t)
	seedHandoffRoutes(t, db, model.HandoffFlowHand, model.HandoffFallbackAdmin)
	payloads := []kitsu.MessagePayload{
		deliveryPayload("task-src", "tt-mm", "comment-1", deliverTable),
		receiverPayload("tt-comp"),
	}
	if _, err := (HandoffDetector{DB: db}).Detect(payloads); err != nil {
		t.Fatalf("detect: %v", err)
	}
	count, err := model.CountFallbackHandoffEvents(db)
	if err != nil {
		t.Fatalf("count fallback: %v", err)
	}
	if count != 1 {
		t.Fatalf("fallback count = %d, want 1", count)
	}
	var audits int64
	db.Model(&model.AuditLog{}).Where("new_status LIKE ?", "%handoff fallback to admins%").Count(&audits)
	if audits != 1 {
		t.Fatalf("fallback audit entries = %d, want 1", audits)
	}
}

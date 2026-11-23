package model

import (
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newShelfCheckDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:shelf-check-%s?mode=memory&cache=shared&_busy_timeout=5000", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&Setting{}, &ProductionHandoffConfig{}, &ProductionHandoffProductType{},
		&ProductionHandoffRoute{}, &HandoffEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func shelfSettings(flow string) ProductionHandoffSettings {
	return ProductionHandoffSettings{
		Config: ProductionHandoffConfig{
			ProductionID: "prod-1",
			NotifyTiming: HandoffTimingImmediate,
			NoAssignee:   HandoffFallbackAdmin,
		},
		ProductTypes: []ProductionHandoffProductType{{Name: "matchmove"}},
		Routes: []ProductionHandoffRoute{{
			TaskTypeID:        "tt-a",
			TaskTypeName:      "Layout",
			ProductType:       "matchmove",
			Flow:              flow,
			NotifyTaskTypeIDs: "tt-comp",
		}},
	}
}

// The same pair is stored once, however often the detector meets it.
func TestRecordSuspiciousShelfRecordsOnce(t *testing.T) {
	db := newShelfCheckDB(t)
	mark := SuspiciousShelf{TaskTypeID: "tt-a", TaskTypeName: "Layout", ProductType: "matchmove", OtherTaskTypeName: "Matchmove"}
	added, err := RecordSuspiciousShelf(db, "prod-1", mark)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if !added {
		t.Fatal("first record must report added")
	}
	added, err = RecordSuspiciousShelf(db, "prod-1", mark)
	if err != nil {
		t.Fatalf("record again: %v", err)
	}
	if added {
		t.Fatal("second record of the same pair must be a duplicate")
	}
	if got := ListSuspiciousShelves(db, "prod-1"); len(got) != 1 {
		t.Fatalf("marks = %d, want 1", len(got))
	}
}

// Saving settings that turn the pair into a handoff clears its mark; saving
// settings that keep it shelved keeps the mark.
func TestSaveProductionHandoffSettingsReconcilesSuspiciousShelves(t *testing.T) {
	db := newShelfCheckDB(t)
	if err := SaveProductionHandoffSettings(db, shelfSettings(HandoffFlowShelve)); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	mark := SuspiciousShelf{TaskTypeID: "tt-a", TaskTypeName: "Layout", ProductType: "matchmove", OtherTaskTypeName: "Matchmove"}
	if _, err := RecordSuspiciousShelf(db, "prod-1", mark); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := SaveProductionHandoffSettings(db, shelfSettings(HandoffFlowShelve)); err != nil {
		t.Fatalf("save shelve: %v", err)
	}
	if got := ListSuspiciousShelves(db, "prod-1"); len(got) != 1 {
		t.Fatalf("still shelved: marks = %d, want 1", len(got))
	}
	if err := SaveProductionHandoffSettings(db, shelfSettings(HandoffFlowHand)); err != nil {
		t.Fatalf("save hand: %v", err)
	}
	if got := ListSuspiciousShelves(db, "prod-1"); len(got) != 0 {
		t.Fatalf("handed off now: marks = %d, want 0", len(got))
	}
}

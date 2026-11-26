package model

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newPolicyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&StudioStatusPolicy{}, &StudioTaskRoute{}, &ProductionTaskRoute{}, &NotificationDecision{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// The classification dictionary settles the known names and refuses to guess
// the rest: unknown names are ambiguous, never silently none.
func TestClassifyStatusName(t *testing.T) {
	cases := map[string]string{
		"Waiting For Approval": StatusNatureReview,
		"waiting for approval": StatusNatureReview,
		"Retake":               StatusNatureRetake,
		"Done":                 StatusNatureDone,
		"Approved":             StatusNatureDone,
		"Work In Progress":     StatusNatureWIP,
		"Todo":                 StatusNatureNone,
		"Ready To Start":       StatusNatureNone,
		"Neutral":              StatusNatureNone,
		"Rejected":             StatusNatureAmbiguous,
		"Final Check":          StatusNatureAmbiguous,
		"":                     StatusNatureAmbiguous,
	}
	for name, want := range cases {
		if got := ClassifyStatusName(name); got != want {
			t.Errorf("ClassifyStatusName(%q) = %q, want %q", name, got, want)
		}
	}
}

// The reconcile classifies new statuses, queues the ambiguous ones, and a
// rename that would change the nature is queued instead of applied.
func TestReconcileStudioStatusPolicies(t *testing.T) {
	db := newPolicyDB(t)
	statuses := []StatusInfo{
		{ID: "s-done", Name: "Done"},
		{ID: "s-mystery", Name: "Rejected"},
	}
	if err := ReconcileStudioStatusPolicies(db, statuses); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	policies, err := LoadStudioStatusPolicies(db)
	if err != nil {
		t.Fatal(err)
	}
	if policies["s-done"].Nature != StatusNatureDone {
		t.Fatalf("done nature = %q", policies["s-done"].Nature)
	}
	if policies["s-mystery"].Nature != StatusNatureAmbiguous {
		t.Fatalf("mystery nature = %q", policies["s-mystery"].Nature)
	}

	// A rename Done -> Approved keeps the nature, so the snapshot just moves.
	if err := ReconcileStudioStatusPolicies(db, []StatusInfo{{ID: "s-done", Name: "Approved"}}); err != nil {
		t.Fatal(err)
	}
	policies, _ = LoadStudioStatusPolicies(db)
	if policies["s-done"].StatusName != "Approved" || policies["s-done"].Nature != StatusNatureDone {
		t.Fatalf("after harmless rename: %+v", policies["s-done"])
	}

	// A rename Done -> Retake updates the auto-derived nature without queueing an exception.
	if err := ReconcileStudioStatusPolicies(db, []StatusInfo{{ID: "s-done", Name: "Retake"}}); err != nil {
		t.Fatal(err)
	}
	policies, _ = LoadStudioStatusPolicies(db)
	if policies["s-done"].Nature != StatusNatureRetake {
		t.Fatalf("nature not updated on auto-derived rename: %+v", policies["s-done"])
	}

	// A manual classification is never re-derived.
	if err := SetStudioStatusPolicyNature(db, "s-mystery", "Rejected", StatusNatureNone); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileStudioStatusPolicies(db, []StatusInfo{{ID: "s-mystery", Name: "Done"}}); err != nil {
		t.Fatal(err)
	}
	policies, _ = LoadStudioStatusPolicies(db)
	if policies["s-mystery"].Nature != StatusNatureNone {
		t.Fatalf("manual nature overwritten: %+v", policies["s-mystery"])
	}
}

// Route resolution: a production override wins over the studio default, and
// an explicit "none" override silences the row for that Production only.
func TestEffectiveTaskRouteInheritance(t *testing.T) {
	db := newPolicyDB(t)
	if err := SaveStudioTaskRoute(db, StudioTaskRoute{FromTaskTypeID: "tt-layout", FromTaskTypeName: "Layout", NotifyTaskTypeIDs: "tt-anim,tt-light"}); err != nil {
		t.Fatal(err)
	}
	ids, origin, found, err := EffectiveTaskRoute(db, "prod-1", "tt-layout")
	if err != nil || !found || origin != "studio" || len(ids) != 2 {
		t.Fatalf("studio: ids=%v origin=%q found=%v err=%v", ids, origin, found, err)
	}
	if err := SaveProductionTaskRoute(db, ProductionTaskRoute{ProductionID: "prod-1", FromTaskTypeID: "tt-layout", NotifyTaskTypeIDs: TaskRouteNone}); err != nil {
		t.Fatal(err)
	}
	ids, origin, found, err = EffectiveTaskRoute(db, "prod-1", "tt-layout")
	if err != nil || !found || origin != "override" || len(ids) != 0 {
		t.Fatalf("override none: ids=%v origin=%q found=%v err=%v", ids, origin, found, err)
	}
	// Another Production still sees the studio default.
	ids, origin, found, err = EffectiveTaskRoute(db, "prod-2", "tt-layout")
	if err != nil || !found || origin != "studio" || len(ids) != 2 {
		t.Fatalf("other production: ids=%v origin=%q found=%v err=%v", ids, origin, found, err)
	}
	// Deleting the override restores the inheritance.
	if err := DeleteProductionTaskRoute(db, "prod-1", "tt-layout"); err != nil {
		t.Fatal(err)
	}
	_, origin, found, err = EffectiveTaskRoute(db, "prod-1", "tt-layout")
	if err != nil || !found || origin != "studio" {
		t.Fatalf("after delete: origin=%q found=%v err=%v", origin, found, err)
	}
	// An unwired task type reports found=false.
	if _, _, found, err := EffectiveTaskRoute(db, "prod-1", "tt-unknown"); err != nil || found {
		t.Fatalf("unknown: found=%v err=%v", found, err)
	}
}

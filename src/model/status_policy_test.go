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
	if err := db.AutoMigrate(&StudioStatusPolicy{}, &StudioTaskRoute{}, &ProductionTaskRoute{}, &AdminException{}, &NotificationDecision{}); err != nil {
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
	open, err := OpenAdminExceptions(db)
	if err != nil || len(open) != 1 || open[0].Kind != ExceptionStatusAmbiguous || open[0].SubjectID != "s-mystery" {
		t.Fatalf("exceptions = %+v err=%v", open, err)
	}

	// A rename Done -> Approved keeps the nature, so the snapshot just moves.
	if err := ReconcileStudioStatusPolicies(db, []StatusInfo{{ID: "s-done", Name: "Approved"}}); err != nil {
		t.Fatal(err)
	}
	policies, _ = LoadStudioStatusPolicies(db)
	if policies["s-done"].StatusName != "Approved" || policies["s-done"].Nature != StatusNatureDone {
		t.Fatalf("after harmless rename: %+v", policies["s-done"])
	}

	// A rename Done -> Retake would change the nature: queued, not applied.
	if err := ReconcileStudioStatusPolicies(db, []StatusInfo{{ID: "s-done", Name: "Retake"}}); err != nil {
		t.Fatal(err)
	}
	policies, _ = LoadStudioStatusPolicies(db)
	if policies["s-done"].Nature != StatusNatureDone {
		t.Fatalf("nature changed silently: %+v", policies["s-done"])
	}
	open, _ = OpenAdminExceptions(db)
	found := false
	for _, item := range open {
		if item.Kind == ExceptionStatusRenamed && item.SubjectID == "s-done" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rename exception missing: %+v", open)
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

// Exceptions aggregate repeats of the same subject into one open row.
func TestAdminExceptionAggregation(t *testing.T) {
	db := newPolicyDB(t)
	for i := 0; i < 3; i++ {
		if err := RaiseAdminException(db, ExceptionNoRoute, "tt-fx", "FX", "prod-1", "no wiring"); err != nil {
			t.Fatal(err)
		}
	}
	open, err := OpenAdminExceptions(db)
	if err != nil || len(open) != 1 || open[0].SeenCount != 3 {
		t.Fatalf("open = %+v err=%v", open, err)
	}
	if err := ResolveAdminException(db, open[0].ID, false); err != nil {
		t.Fatal(err)
	}
	open, _ = OpenAdminExceptions(db)
	if len(open) != 0 {
		t.Fatalf("still open: %+v", open)
	}
}

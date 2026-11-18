package model

import (
	"fmt"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newForumSettingsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&ProductionForumConfig{}, &ProductionForumRoute{}, &ProductionForumStatusRole{}, &ProductionForumTag{}, &ForumBaseline{}, &ForumTask{}); err != nil {
		t.Fatalf("failed to migrate schema: %v", err)
	}
	return db
}

func completeForumSettings() ProductionForumSettings {
	return ProductionForumSettings{
		Config: ProductionForumConfig{ProductionID: "p1", GuildID: "g1"},
		Routes: []ProductionForumRoute{
			{TaskTypeID: "tt-anim", TaskTypeName: "Animation", ForumChannelID: "f-anim"},
			{TaskTypeID: "tt-comp", TaskTypeName: "Compositing", ForumChannelID: "f-comp"},
		},
		StatusRoles: []ProductionForumStatusRole{
			{StatusID: "s-wfa", Role: ForumRoleWFA}, {StatusID: "s-retake", Role: ForumRoleRetake},
			{StatusID: "s-done", Role: ForumRoleDone}, {StatusID: "s-wip", Role: ForumRoleWIP},
		},
		Tags: []ProductionForumTag{
			{ForumChannelID: "f-anim", Role: ForumRoleWFA, TagID: "a1"}, {ForumChannelID: "f-anim", Role: ForumRoleRetake, TagID: "a2"},
			{ForumChannelID: "f-anim", Role: ForumRoleDone, TagID: "a3"}, {ForumChannelID: "f-anim", Role: ForumRoleWIP, TagID: "a4"},
			{ForumChannelID: "f-comp", Role: ForumRoleWFA, TagID: "c1"}, {ForumChannelID: "f-comp", Role: ForumRoleRetake, TagID: "c2"},
			{ForumChannelID: "f-comp", Role: ForumRoleDone, TagID: "c3"}, {ForumChannelID: "f-comp", Role: ForumRoleWIP, TagID: "c4"},
		},
	}
}

func TestSaveAndLoadForumSettingsRoundTrip(t *testing.T) {
	db := newForumSettingsTestDB(t)
	if err := SaveProductionForumSettings(db, completeForumSettings()); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, ok, err := LoadProductionForumSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if len(loaded.Routes) != 2 || len(loaded.StatusRoles) != 4 || len(loaded.Tags) != 8 || loaded.Config.GuildID != "g1" {
		t.Fatalf("unexpected loaded settings: %+v", loaded)
	}
}

func TestSaveForumSettingsReplacesPreviousRowsAndKeepsOtherProductions(t *testing.T) {
	db := newForumSettingsTestDB(t)
	other := completeForumSettings()
	other.Config.ProductionID = "p2"
	if err := SaveProductionForumSettings(db, other); err != nil {
		t.Fatal(err)
	}
	if err := SaveProductionForumSettings(db, completeForumSettings()); err != nil {
		t.Fatal(err)
	}
	smaller := completeForumSettings()
	smaller.Routes = smaller.Routes[:1]
	smaller.Tags = smaller.Tags[:4]
	if err := SaveProductionForumSettings(db, smaller); err != nil {
		t.Fatalf("second save: %v", err)
	}
	loaded, _, _ := LoadProductionForumSettings(db, "p1")
	if len(loaded.Routes) != 1 || len(loaded.Tags) != 4 {
		t.Fatalf("save must replace, not append: %d routes, %d tags", len(loaded.Routes), len(loaded.Tags))
	}
	untouched, _, _ := LoadProductionForumSettings(db, "p2")
	if len(untouched.Routes) != 2 || len(untouched.Tags) != 8 {
		t.Fatalf("another Production's settings changed: %+v", untouched)
	}
	var configs int64
	db.Model(&ProductionForumConfig{}).Where("production_id = ?", "p1").Count(&configs)
	if configs != 1 {
		t.Fatalf("saving twice must keep one config row, got %d", configs)
	}
}

func TestInvalidForumSettingsAreRejectedAndNotStored(t *testing.T) {
	db := newForumSettingsTestDB(t)
	bad := completeForumSettings()
	bad.Tags = bad.Tags[:7] // forum f-comp loses its wip tag, still has the trigger roles
	bad.Tags = append(bad.Tags[:5], bad.Tags[6:]...)
	if err := SaveProductionForumSettings(db, bad); err == nil {
		t.Fatal("settings without a complete tag set must be rejected")
	}
	if _, ok, _ := LoadProductionForumSettings(db, "p1"); ok {
		t.Fatal("a rejected save must not leave a config row")
	}
}

func TestValidateForumSettingsProblems(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ProductionForumSettings)
		want   string
	}{
		{"missing guild", func(s *ProductionForumSettings) { s.Config.GuildID = "" }, "guild"},
		{"no routes", func(s *ProductionForumSettings) { s.Routes = nil; s.Tags = nil }, "at least one Task Type"},
		{"duplicate task type", func(s *ProductionForumSettings) { s.Routes[1].TaskTypeID = "tt-anim" }, "routed more than once"},
		{"missing retake status", func(s *ProductionForumSettings) { s.StatusRoles = append(s.StatusRoles[:1], s.StatusRoles[2:]...) }, "role retake"},
		{"bad role", func(s *ProductionForumSettings) { s.StatusRoles[0].Role = "review" }, "no valid role"},
		{"status mapped twice", func(s *ProductionForumSettings) { s.StatusRoles[1].StatusID = "s-wfa" }, "mapped more than once"},
		{"tag reused for two roles", func(s *ProductionForumSettings) { s.Tags[1].TagID = "a1" }, "more than one role"},
		{"tag for unrouted forum", func(s *ProductionForumSettings) {
			s.Tags = append(s.Tags, ProductionForumTag{ForumChannelID: "f-other", Role: ForumRoleWFA, TagID: "x"})
		}, "no Task Type routes to"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := completeForumSettings()
			tc.mutate(&settings)
			problems := ValidateProductionForumSettings(settings)
			for _, problem := range problems {
				if strings.Contains(problem, tc.want) {
					return
				}
			}
			t.Fatalf("want a problem containing %q, got %v", tc.want, problems)
		})
	}
	if problems := ValidateProductionForumSettings(completeForumSettings()); len(problems) != 0 {
		t.Fatalf("complete settings must validate, got %v", problems)
	}
}

// Saving a configuration that drops a Task Type must also drop its baseline:
// otherwise re-routing it later keeps the stale baseline and never posts.
func TestSaveForumSettingsDropsBaselinesOfRemovedRoutes(t *testing.T) {
	db := newForumSettingsTestDB(t)
	if err := SaveProductionForumSettings(db, completeForumSettings()); err != nil {
		t.Fatal(err)
	}
	for _, row := range []ForumBaseline{
		{ProjectID: "p1", TaskTypeID: "tt-anim"},
		{ProjectID: "p1", TaskTypeID: "tt-comp"},
		{ProjectID: "p2", TaskTypeID: "tt-anim"},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed baseline: %v", err)
		}
	}
	for _, row := range []ForumTask{
		{TaskID: "task-anim", ProjectID: "p1", TaskTypeID: "tt-anim"},
		{TaskID: "task-comp", ProjectID: "p1", TaskTypeID: "tt-comp"},
		{TaskID: "task-other", ProjectID: "p2", TaskTypeID: "tt-anim"},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed task: %v", err)
		}
	}

	smaller := completeForumSettings()
	smaller.Routes = smaller.Routes[1:] // tt-comp only
	smaller.Tags = smaller.Tags[4:]
	if err := SaveProductionForumSettings(db, smaller); err != nil {
		t.Fatalf("second save: %v", err)
	}

	seeded, err := ForumBaselineExists(db, "p1", "tt-anim")
	if err != nil {
		t.Fatal(err)
	}
	if seeded {
		t.Fatal("the removed route kept its baseline")
	}
	seeded, err = ForumBaselineExists(db, "p1", "tt-comp")
	if err != nil || !seeded {
		t.Fatalf("the still-routed baseline must be kept: seeded=%v err=%v", seeded, err)
	}
	if seeded, _ := ForumBaselineExists(db, "p2", "tt-anim"); !seeded {
		t.Fatal("another Production's baseline must be untouched")
	}
	var tasks []ForumTask
	db.Order("task_id asc").Find(&tasks)
	if len(tasks) != 2 || tasks[0].TaskID != "task-comp" || tasks[1].TaskID != "task-other" {
		t.Fatalf("only the removed route's ledger rows may go, got %+v", tasks)
	}
}

func TestDeleteProductionForumSettingsRemovesRows(t *testing.T) {
	db := newForumSettingsTestDB(t)
	if err := SaveProductionForumSettings(db, completeForumSettings()); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ForumBaseline{ProjectID: "p1", TaskTypeID: "tt-anim"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ForumTask{TaskID: "task-anim", ProjectID: "p1", TaskTypeID: "tt-anim"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := DeleteProductionForumSettings(db, "p1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := LoadProductionForumSettings(db, "p1"); ok {
		t.Fatal("settings must be gone")
	}
	if seeded, _ := ForumBaselineExists(db, "p1", "tt-anim"); seeded {
		t.Fatal("a deleted configuration must not keep baselines")
	}
	var tasks int64
	db.Model(&ForumTask{}).Where("project_id = ?", "p1").Count(&tasks)
	if tasks != 0 {
		t.Fatalf("a deleted configuration must not keep ledger rows, %d left", tasks)
	}
}

func TestDeleteProductionOperationalStateRemovesForumSettings(t *testing.T) {
	db := newForumSettingsTestDB(t)
	if err := SaveProductionForumSettings(db, completeForumSettings()); err != nil {
		t.Fatal(err)
	}
	if err := DeleteProductionOperationalState(db, "p1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := LoadProductionForumSettings(db, "p1"); ok {
		t.Fatal("a deleted Production must not keep forum settings")
	}
	var rows int64
	db.Model(&ProductionForumTag{}).Count(&rows)
	if rows != 0 {
		t.Fatalf("forum tags must be removed with the Production, %d left", rows)
	}
}

func TestForumSettingsProblemsKeepAStableOrder(t *testing.T) {
	settings := completeForumSettings()
	settings.Tags = nil // every forum is missing every role tag
	first := ValidateProductionForumSettings(settings)
	for i := 0; i < 20; i++ {
		again := ValidateProductionForumSettings(settings)
		if strings.Join(again, "|") != strings.Join(first, "|") {
			t.Fatalf("problem order changed between runs:\n%v\n%v", first, again)
		}
	}
}

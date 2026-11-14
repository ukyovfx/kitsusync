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
	if err := db.AutoMigrate(&ProductionForumConfig{}, &ProductionForumRoute{}, &ProductionForumStatusRole{}, &ProductionForumTag{}); err != nil {
		t.Fatalf("failed to migrate schema: %v", err)
	}
	return db
}

func completeForumSettings(mode string) ProductionForumSettings {
	return ProductionForumSettings{
		Config: ProductionForumConfig{ProductionID: "p1", Mode: mode, GuildID: "g1"},
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

func TestNotificationModeDefaultsToLane(t *testing.T) {
	db := newForumSettingsTestDB(t)
	if got := NotificationModeForProduction(db, "p1"); got != NotificationModeLane {
		t.Fatalf("a Production without a row must run in lane mode, got %q", got)
	}
	if got := NotificationModeForProduction(nil, "p1"); got != NotificationModeLane {
		t.Fatalf("a nil database must fall back to lane mode, got %q", got)
	}
}

func TestSaveAndLoadForumSettingsRoundTrip(t *testing.T) {
	db := newForumSettingsTestDB(t)
	if err := SaveProductionForumSettings(db, completeForumSettings(NotificationModeForum)); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := NotificationModeForProduction(db, "p1"); got != NotificationModeForum {
		t.Fatalf("mode = %q, want forum", got)
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
	other := completeForumSettings(NotificationModeForum)
	other.Config.ProductionID = "p2"
	if err := SaveProductionForumSettings(db, other); err != nil {
		t.Fatal(err)
	}
	if err := SaveProductionForumSettings(db, completeForumSettings(NotificationModeForum)); err != nil {
		t.Fatal(err)
	}
	smaller := completeForumSettings(NotificationModeLane)
	smaller.Routes = smaller.Routes[:1]
	smaller.Tags = smaller.Tags[:4]
	if err := SaveProductionForumSettings(db, smaller); err != nil {
		t.Fatalf("second save: %v", err)
	}
	loaded, _, _ := LoadProductionForumSettings(db, "p1")
	if len(loaded.Routes) != 1 || len(loaded.Tags) != 4 {
		t.Fatalf("save must replace, not append: %d routes, %d tags", len(loaded.Routes), len(loaded.Tags))
	}
	if got := NotificationModeForProduction(db, "p1"); got != NotificationModeLane {
		t.Fatalf("mode must follow the latest save, got %q", got)
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
	bad := completeForumSettings(NotificationModeForum)
	bad.Tags = bad.Tags[:7] // forum f-comp loses its wip tag, still has the trigger roles
	bad.Tags = append(bad.Tags[:5], bad.Tags[6:]...)
	if err := SaveProductionForumSettings(db, bad); err == nil {
		t.Fatal("forum mode without a complete tag set must be rejected")
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
		{"unknown mode", func(s *ProductionForumSettings) { s.Config.Mode = "webhook" }, "unknown notification mode"},
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
			settings := completeForumSettings(NotificationModeForum)
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
	if problems := ValidateProductionForumSettings(completeForumSettings(NotificationModeForum)); len(problems) != 0 {
		t.Fatalf("complete settings must validate, got %v", problems)
	}
}

func TestLaneModeAllowsHalfFinishedForumSettings(t *testing.T) {
	db := newForumSettingsTestDB(t)
	draft := ProductionForumSettings{
		Config: ProductionForumConfig{ProductionID: "p1", Mode: NotificationModeLane},
		Routes: []ProductionForumRoute{{TaskTypeID: "tt-anim", ForumChannelID: "f-anim"}},
	}
	if err := SaveProductionForumSettings(db, draft); err != nil {
		t.Fatalf("a draft kept in lane mode must be saveable: %v", err)
	}
	if got := NotificationModeForProduction(db, "p1"); got != NotificationModeLane {
		t.Fatalf("draft must stay in lane mode, got %q", got)
	}
}

func TestDeleteProductionForumSettingsFallsBackToLane(t *testing.T) {
	db := newForumSettingsTestDB(t)
	if err := SaveProductionForumSettings(db, completeForumSettings(NotificationModeForum)); err != nil {
		t.Fatal(err)
	}
	if err := DeleteProductionForumSettings(db, "p1"); err != nil {
		t.Fatal(err)
	}
	if got := NotificationModeForProduction(db, "p1"); got != NotificationModeLane {
		t.Fatalf("mode after delete = %q, want lane", got)
	}
	if _, ok, _ := LoadProductionForumSettings(db, "p1"); ok {
		t.Fatal("settings must be gone")
	}
}

func TestDeleteProductionOperationalStateRemovesForumSettings(t *testing.T) {
	db := newForumSettingsTestDB(t)
	if err := SaveProductionForumSettings(db, completeForumSettings(NotificationModeForum)); err != nil {
		t.Fatal(err)
	}
	if err := DeleteProductionOperationalState(db, "p1"); err != nil {
		t.Fatal(err)
	}
	if got := NotificationModeForProduction(db, "p1"); got != NotificationModeLane {
		t.Fatalf("a deleted Production must not keep forum mode, got %q", got)
	}
	var rows int64
	db.Model(&ProductionForumTag{}).Count(&rows)
	if rows != 0 {
		t.Fatalf("forum tags must be removed with the Production, %d left", rows)
	}
}

func TestForumSettingsProblemsKeepAStableOrder(t *testing.T) {
	settings := completeForumSettings(NotificationModeForum)
	settings.Tags = nil // every forum is missing every role tag
	first := ValidateProductionForumSettings(settings)
	for i := 0; i < 20; i++ {
		again := ValidateProductionForumSettings(settings)
		if strings.Join(again, "|") != strings.Join(first, "|") {
			t.Fatalf("problem order changed between runs:\n%v\n%v", first, again)
		}
	}
}

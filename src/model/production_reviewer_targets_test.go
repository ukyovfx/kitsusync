package model

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func reviewerTargetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectUserMap{}, &CheckerMap{}, &ProjectCheckerMap{}, &ProjectReviewerTarget{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestProjectReviewerTargetsAreAdditiveScopedAndResettable(t *testing.T) {
	db := reviewerTargetTestDB(t)
	first := Project{KitsuProjectID: "reviewer-production-1"}
	second := Project{KitsuProjectID: "reviewer-production-2"}
	db.Create(&first)
	db.Create(&second)
	legacy := ProjectCheckerMap{ProjectID: first.ID, TaskType: "Comp", TaskTypeID: "task-comp", OverrideDiscordID: "123456789012345678"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasIndex(&ProjectReviewerTarget{}, "idx_projreviewer_target") {
		t.Fatal("unique explicit Reviewer target index was not created")
	}

	for _, association := range []ProjectUserMap{
		{ProjectID: first.ID, KitsuName: "User 1", DiscordUserID: "123456789012345679"},
		{ProjectID: first.ID, KitsuName: "User 2", DiscordUserID: "123456789012345681"},
		{ProjectID: second.ID, KitsuName: "User 3", DiscordUserID: "123456789012345682"},
	} {
		if err := db.Create(&association).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := UpsertProjectReviewerTarget(db, first.ID, "task-comp", "Compositing Updated", ReviewerTargetUser, "123456789012345679"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, first.ID, "task-comp", "Compositing Updated", ReviewerTargetUser, "123456789012345679"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, first.ID, "task-comp", "Compositing", ReviewerTargetRole, "123456789012345680"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, first.ID, "task-anim", "Animation", ReviewerTargetUser, "123456789012345681"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, second.ID, "task-comp", "Compositing", ReviewerTargetUser, "123456789012345682"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, first.ID, "task-comp", "Compositing Updated", "invalid", "123456789012345683"); err == nil {
		t.Fatal("unsupported target kind was accepted")
	}

	targets, exists, err := ListProjectReviewerTargetsForTaskType(db, first.ID, "task-comp")
	if err != nil || !exists || len(targets) != 2 {
		t.Fatalf("targets=%+v exists=%v err=%v", targets, exists, err)
	}
	if targets[0].TargetKind != ReviewerTargetRole || targets[1].DiscordID != "123456789012345679" {
		t.Fatalf("targets are not deterministic: %+v", targets)
	}
	if targets[1].TaskTypeName != "Compositing Updated" {
		t.Fatalf("Task Type rename was not reflected by stable ID: %+v", targets[1])
	}
	if err := db.Create(&ProjectReviewerTarget{ProjectID: first.ID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", TargetKind: ReviewerTargetUser, DiscordID: "123456789012345679"}).Error; err == nil {
		t.Fatal("duplicate target identity was not rejected by the unique index")
	}
	if got := ListProjectReviewerTargets(db, second.ID); len(got) != 1 || got[0].DiscordID != "123456789012345682" {
		t.Fatalf("Production targets were not isolated: %+v", got)
	}
	if err := DeleteProjectReviewerTarget(db, first.ID, targets[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := ListProjectReviewerTargets(db, first.ID); len(got) != 2 {
		t.Fatalf("removing one target changed another Task Type: %+v", got)
	}
	if err := DeleteProjectReviewerTargetsForTaskType(db, first.ID, "task-comp"); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := ListProjectReviewerTargetsForTaskType(db, first.ID, "task-comp"); err != nil || exists {
		t.Fatalf("reset did not clear selected Task Type: exists=%v err=%v", exists, err)
	}
	var preserved ProjectCheckerMap
	if err := db.First(&preserved, legacy.ID).Error; err != nil || preserved.OverrideDiscordID != legacy.OverrideDiscordID {
		t.Fatalf("additive target storage damaged legacy row: %+v err=%v", preserved, err)
	}
}

func TestExplicitReviewerTargetsOverrideLegacyAndRequireGlobalUserLink(t *testing.T) {
	db := reviewerTargetTestDB(t)
	project := Project{KitsuProjectID: "reviewer-resolution"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	legacy := ProjectCheckerMap{ProjectID: project.ID, TaskTypeID: "task-comp", TaskType: "Compositing", OverrideDiscordID: "123456789012345678"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	user := ProjectUserMap{ProjectID: project.ID, KitsuName: "Linked", DiscordUserID: "123456789012345679"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	globalUser := UserMap{KitsuID: "person-linked", KitsuName: "Linked", DiscordID: user.DiscordUserID}
	if err := db.Create(&globalUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", ReviewerTargetUser, user.DiscordUserID); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", ReviewerTargetRole, "123456789012345680"); err != nil {
		t.Fatal(err)
	}

	targets, explicit, err := ResolveReviewerTargetsForProjectWithSupervisors(db, "", "", project.KitsuProjectID, "task-comp", "Compositing")
	if err != nil || !explicit || len(targets) != 2 {
		t.Fatalf("explicit targets did not win: targets=%+v explicit=%v err=%v", targets, explicit, err)
	}
	if targets[0].TargetKind != ReviewerTargetRole || targets[1].DiscordID != user.DiscordUserID {
		t.Fatalf("explicit targets are not deterministic: %+v", targets)
	}
	if err := db.Delete(&globalUser).Error; err != nil {
		t.Fatal(err)
	}
	targets, explicit, err = ResolveReviewerTargetsForProjectWithSupervisors(db, "", "", project.KitsuProjectID, "task-comp", "Compositing")
	if !explicit || err != nil || len(targets) != 1 || targets[0].TargetKind != ReviewerTargetRole {
		t.Fatalf("unlinked User target was not skipped while preserving the valid Role: targets=%+v explicit=%v err=%v", targets, explicit, err)
	}
	if err := db.Where("project_id = ? AND target_kind = ?", project.ID, ReviewerTargetRole).Delete(&ProjectReviewerTarget{}).Error; err != nil {
		t.Fatal(err)
	}
	targets, explicit, err = ResolveReviewerTargetsForProjectWithSupervisors(db, "", "", project.KitsuProjectID, "task-comp", "Compositing")
	if !explicit || err == nil || len(targets) != 0 {
		t.Fatalf("all stale explicit targets did not fail closed: targets=%+v explicit=%v err=%v", targets, explicit, err)
	}
}

func TestReviewerResolutionKeepsLegacyProductionOverrideWhenNoNewTargets(t *testing.T) {
	db := reviewerTargetTestDB(t)
	project := Project{KitsuProjectID: "legacy-reviewer-resolution"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	legacy := ProjectCheckerMap{ProjectID: project.ID, TaskTypeID: "task-comp", TaskType: "Compositing", OverrideDiscordID: "123456789012345678"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	targets, explicit, err := ResolveReviewerTargetsForProjectWithSupervisors(db, "", "", project.KitsuProjectID, "task-comp", "Compositing")
	if err != nil || explicit || len(targets) != 1 || targets[0].DiscordID != legacy.OverrideDiscordID {
		t.Fatalf("legacy Production override was not retained: targets=%+v explicit=%v err=%v", targets, explicit, err)
	}
}

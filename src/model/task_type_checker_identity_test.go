package model

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTaskTypeCheckerIdentityDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Project{}, &CheckerMap{}, &ProjectCheckerMap{}); err != nil {
		t.Fatalf("migrate checker schema: %v", err)
	}
	return db
}

func TestCheckerLookupsPreferStableTaskTypeIDAndDeduplicate(t *testing.T) {
	db := openTaskTypeCheckerIdentityDB(t)
	project := Project{KitsuProjectID: "production-1", Name: "Production"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	projectRows := []ProjectCheckerMap{
		{ProjectID: project.ID, TaskTypeID: "tt-1", TaskType: "Renamed", OverrideDiscordID: "same"},
		{ProjectID: project.ID, TaskTypeID: "tt-1", TaskType: "Duplicate ID row", OverrideDiscordID: "same"},
		{ProjectID: project.ID, TaskType: "Old Name", OverrideDiscordID: "legacy-conflict"},
	}
	for _, row := range projectRows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	globalRows := []CheckerMap{
		{TaskTypeID: "tt-1", TaskType: "Renamed", DiscordID: "global-id"},
		{TaskTypeID: "tt-1", TaskType: "Duplicate ID row", DiscordID: "global-id"},
		{TaskType: "Old Name", DiscordID: "global-id"},
	}
	for _, row := range globalRows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	if got := GetProjectCheckerForTaskTypeID(db, "production-1", "tt-1", "Old Name"); !reflect.DeepEqual(got, []string{"same"}) {
		t.Fatalf("ID-backed Production mapping = %v", got)
	}
	if got := FindCheckersByTaskTypeID(db, "tt-1", "Old Name"); !reflect.DeepEqual(got, []string{"global-id"}) {
		t.Fatalf("ID-backed global mapping = %v", got)
	}
}

func TestCheckerLookupsFallBackToLegacyNameOnlyRows(t *testing.T) {
	db := openTaskTypeCheckerIdentityDB(t)
	project := Project{KitsuProjectID: "production-1", Name: "Production"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectCheckerMap{ProjectID: project.ID, TaskType: "Animation", OverrideDiscordID: "production-legacy"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&CheckerMap{TaskType: "Animation", DiscordID: "global-legacy"}).Error; err != nil {
		t.Fatal(err)
	}
	if got := GetProjectCheckerForTaskTypeID(db, "production-1", "tt-new", "Animation"); !reflect.DeepEqual(got, []string{"production-legacy"}) {
		t.Fatalf("legacy Production mapping = %v", got)
	}
	if got := FindCheckersByTaskTypeID(db, "tt-new", "Animation"); !reflect.DeepEqual(got, []string{"global-legacy"}) {
		t.Fatalf("legacy global mapping = %v", got)
	}
}

func TestUpsertProjectCheckerMapWithTaskTypeIDRetainsName(t *testing.T) {
	db := openTaskTypeCheckerIdentityDB(t)
	project := Project{KitsuProjectID: "production-1", Name: "Production"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	UpsertProjectCheckerMapWithTaskTypeID(db, project.ID, "tt-1", "Animation", "discord-reviewer")
	rows := ListProjectCheckerMaps(db, project.ID)
	if len(rows) != 1 || rows[0].TaskTypeID != "tt-1" || rows[0].TaskType != "Animation" {
		t.Fatalf("stored mapping = %+v", rows)
	}
}

func TestUpsertProjectCheckerMapWithTaskTypeIDUpdatesRenamedTaskTypeRow(t *testing.T) {
	db := openTaskTypeCheckerIdentityDB(t)
	project := Project{KitsuProjectID: "production-1", Name: "Production"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectCheckerMap{ProjectID: project.ID, TaskTypeID: "tt-1", TaskType: "Old Name", OverrideDiscordID: "reviewer"}).Error; err != nil {
		t.Fatal(err)
	}
	UpsertProjectCheckerMapWithUserAndTaskTypeID(db, project.ID, "tt-1", "Renamed", "Reviewer", "reviewer@example.test", "discord-reviewer", "")
	rows := ListProjectCheckerMaps(db, project.ID)
	if len(rows) != 1 || rows[0].TaskType != "Renamed" || rows[0].TaskTypeID != "tt-1" || rows[0].KitsuName != "Reviewer" {
		t.Fatalf("renamed mapping was not updated by stable ID: %+v", rows)
	}
}

func TestEmptyTaskTypeIDAndAutoMigratePreserveLegacyCheckerRows(t *testing.T) {
	db := openTaskTypeCheckerIdentityDB(t)
	legacyProject := Project{KitsuProjectID: "production-1", Name: "Production"}
	if err := db.Create(&legacyProject).Error; err != nil {
		t.Fatal(err)
	}
	projectRow := ProjectCheckerMap{ProjectID: legacyProject.ID, TaskType: "Animation", OverrideDiscordID: "production-legacy"}
	globalRow := CheckerMap{TaskType: "Animation", DiscordID: "global-legacy"}
	if err := db.Create(&projectRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&globalRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&CheckerMap{}, &ProjectCheckerMap{}); err != nil {
		t.Fatalf("repeat AutoMigrate: %v", err)
	}
	var gotProject ProjectCheckerMap
	if err := db.First(&gotProject, projectRow.ID).Error; err != nil {
		t.Fatal(err)
	}
	var gotGlobal CheckerMap
	if err := db.First(&gotGlobal, globalRow.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotProject.TaskTypeID != "" || gotProject.TaskType != "Animation" || gotGlobal.TaskTypeID != "" || gotGlobal.TaskType != "Animation" {
		t.Fatalf("legacy rows changed during migration: project=%+v global=%+v", gotProject, gotGlobal)
	}
}

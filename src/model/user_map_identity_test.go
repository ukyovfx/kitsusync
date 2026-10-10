package model

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func userMapIdentityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &UserMap{}, &ProjectUserMap{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestGetUserMapForProjectWithIdentityRejectsConflictingStablePersonFallback(t *testing.T) {
	db := userMapIdentityTestDB(t)
	if err := db.Create(&Project{KitsuProjectID: "p-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&UserMap{KitsuID: "person-b", KitsuEmail: "same@example.test", KitsuName: "Shared Name", DiscordID: "discord-b"}).Error; err != nil {
		t.Fatal(err)
	}
	if got := GetUserMapForProjectWithIdentity(db, "p-1", "person-a", "Shared Name", "same@example.test"); got != "" {
		t.Fatalf("conflicting stable Person ID must not resolve through shared name/email, got %q", got)
	}
	if got := GetUserMapForProject(db, "p-1", "Shared Name", "same@example.test"); got != "" {
		t.Fatalf("identity-less lookup must reject a stable-identity row, got %q", got)
	}
}

func TestGetUserMapForProjectWithIdentityUsesExactIDAndUnambiguousLegacyFallback(t *testing.T) {
	db := userMapIdentityTestDB(t)
	if err := db.Create(&Project{KitsuProjectID: "p-1"}).Error; err != nil {
		t.Fatal(err)
	}
	rows := []UserMap{
		{KitsuID: "person-a", KitsuEmail: "same@example.test", KitsuName: "Shared Name", DiscordID: "discord-a"},
		{KitsuID: "person-b", KitsuEmail: "same@example.test", KitsuName: "Shared Name", DiscordID: "discord-b"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if got := GetUserMapForProjectWithIdentity(db, "p-1", "person-a", "Shared Name", "same@example.test"); got != "discord-a" {
		t.Fatalf("exact stable identity should win over conflicting fallback rows, got %q", got)
	}
	if err := db.Where("kitsu_id <> ?", "").Delete(&UserMap{}).Error; err != nil {
		t.Fatal(err)
	}
	legacy := UserMap{KitsuName: "Legacy Person", KitsuEmail: "legacy@example.test", DiscordID: "discord-legacy"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if got := GetUserMapForProjectWithIdentity(db, "p-1", "person-new", "Legacy Person", "legacy@example.test"); got != "discord-legacy" {
		t.Fatalf("one identity-less legacy row should remain usable, got %q", got)
	}
	duplicate := legacy
	duplicate.ID = 0
	if err := db.Create(&duplicate).Error; err != nil {
		t.Fatal(err)
	}
	if got := GetUserMapForProjectWithIdentity(db, "p-1", "person-new", "Legacy Person", "legacy@example.test"); got != "" {
		t.Fatalf("ambiguous identity-less legacy rows must fail closed, got %q", got)
	}
}

func TestGetUserMapForProjectWithIdentityRejectsAmbiguousLegacyProjectRows(t *testing.T) {
	db := userMapIdentityTestDB(t)
	project := Project{KitsuProjectID: "p-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	legacy := []ProjectUserMap{
		{ProjectID: project.ID, KitsuName: "Legacy Person", KitsuEmail: "legacy@example.test", DiscordUserID: "discord-one"},
		{ProjectID: project.ID, KitsuName: "Legacy Person", KitsuEmail: "legacy@example.test", DiscordUserID: "discord-two"},
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if got := GetUserMapForProjectWithIdentity(db, project.KitsuProjectID, "person-new", "Legacy Person", "legacy@example.test"); got != "" {
		t.Fatalf("ambiguous legacy project mappings must fail closed, got %q", got)
	}
}

package forum

import (
	"testing"

	"app/src/api/discord"
	"app/src/model"

	"gorm.io/gorm"
)

// identityRecipients wires the real project user map through the same hook
// main installs, so the RETAKE mention depends on the assignee identity the
// forum side supplies.
func identityRecipients(t *testing.T, db *gorm.DB, identity func(string) (string, string)) Recipients {
	t.Helper()
	oldResolver := discord.UserMapResolver
	discord.UserMapResolver = func(projectID, kitsuPersonID, kitsuName, kitsuEmail string) string {
		return model.GetUserMapForProjectWithIdentity(db, projectID, kitsuPersonID, kitsuName, kitsuEmail)
	}
	t.Cleanup(func() { discord.UserMapResolver = oldResolver })
	return Recipients{
		DB: db,
		ResolveAssignees: func(projectID string, kitsuPersonIDs []string) []string {
			return discord.ResolveAssigneeDiscordIDsWithIdentity(projectID, kitsuPersonIDs, identity)
		},
	}
}

func retakeUsers(t *testing.T, r Recipients) []string {
	t.Helper()
	if err := r.DB.Model(&model.ForumTask{}).Where("task_id = ?", "task-1").Updates(map[string]any{
		"status_id":  "s-retake",
		"project_id": "prod-1",
	}).Error; err != nil {
		t.Fatalf("seed retake: %v", err)
	}
	users, _, err := r.ForTask("task-1", model.ForumPostMessagePayload{Intent: model.ForumIntentStatusRetake})
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	return users
}

func onlyID(t *testing.T, users []string, want string) {
	t.Helper()
	if len(users) != 1 || users[0] != want {
		t.Fatalf("users = %v, want [%s]", users, want)
	}
}

// identityDB is a forum DB with the user map tables, which the person-identity
// lookup reads.
func identityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newForumDB(t)
	if err := db.AutoMigrate(&model.Project{}, &model.UserMap{}); err != nil {
		t.Fatalf("migrate user map: %v", err)
	}
	return db
}

// (a) a link made from the name only still reaches the assignee.
func TestRetakeMentionsNameOnlyLink(t *testing.T) {
	db := identityDB(t)
	if err := db.Create(&model.Project{KitsuProjectID: "prod-1"}).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := db.Create(&model.UserMap{KitsuName: "Name Only", DiscordID: "444444444444444444"}).Error; err != nil {
		t.Fatalf("seed user map: %v", err)
	}
	seedRecipientTask(t, db, "person-1")

	users := retakeUsers(t, identityRecipients(t, db, func(string) (string, string) {
		return "Name Only", "name@example.test"
	}))
	onlyID(t, users, "444444444444444444")
}

// (b) an exact Person ID link wins over a name link.
func TestRetakePrefersPersonIDLink(t *testing.T) {
	db := identityDB(t)
	if err := db.Create(&model.Project{KitsuProjectID: "prod-1"}).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	rows := []model.UserMap{
		{KitsuID: "person-1", KitsuName: "Name Only", DiscordID: "333333333333333333"},
		{KitsuName: "Name Only", DiscordID: "444444444444444444"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed user map: %v", err)
	}
	seedRecipientTask(t, db, "person-1")

	users := retakeUsers(t, identityRecipients(t, db, func(string) (string, string) {
		return "Name Only", "name@example.test"
	}))
	onlyID(t, users, "333333333333333333")
}

// (c) two links sharing the name are ambiguous, so nobody is mentioned.
func TestRetakeSkipsAmbiguousName(t *testing.T) {
	db := identityDB(t)
	if err := db.Create(&model.Project{KitsuProjectID: "prod-1"}).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	rows := []model.UserMap{
		{KitsuName: "Same Name", DiscordID: "555555555555555555"},
		{KitsuName: "Same Name", DiscordID: "666666666666666666"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed user map: %v", err)
	}
	seedRecipientTask(t, db, "person-1")

	users := retakeUsers(t, identityRecipients(t, db, func(string) (string, string) {
		return "Same Name", ""
	}))
	if len(users) != 0 {
		t.Fatalf("ambiguous name must not mention anyone, got %v", users)
	}
}

// (d) a row holding another person's ID is never matched by name.
func TestRetakeSkipsLinkOfAnotherPerson(t *testing.T) {
	db := identityDB(t)
	if err := db.Create(&model.Project{KitsuProjectID: "prod-1"}).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-2", KitsuName: "Same Name", DiscordID: "777777777777777777"}).Error; err != nil {
		t.Fatalf("seed user map: %v", err)
	}
	seedRecipientTask(t, db, "person-1")

	users := retakeUsers(t, identityRecipients(t, db, func(string) (string, string) {
		return "Same Name", ""
	}))
	if len(users) != 0 {
		t.Fatalf("a link holding another Person ID must not match by name, got %v", users)
	}
}

// Without the identity the old behaviour stands: only Person ID links resolve.
func TestRetakeWithoutIdentityOnlyResolvesPersonID(t *testing.T) {
	db := identityDB(t)
	if err := db.Create(&model.Project{KitsuProjectID: "prod-1"}).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := db.Create(&model.UserMap{KitsuName: "Name Only", DiscordID: "444444444444444444"}).Error; err != nil {
		t.Fatalf("seed user map: %v", err)
	}
	seedRecipientTask(t, db, "person-1")

	users := retakeUsers(t, identityRecipients(t, db, nil))
	if len(users) != 0 {
		t.Fatalf("no identity must not mention a name-only link, got %v", users)
	}
}

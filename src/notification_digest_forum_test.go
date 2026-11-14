package main

import (
	"encoding/json"
	"testing"
	"time"

	"app/src/api/discord"
	"app/src/model"

	"gorm.io/gorm"
)

// forumDigestDB is the pipeline DB plus the forum ledger tables, with one
// forum-mode Production ("p1") whose reviewer is a user target.
func forumDigestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newNotificationPipelineDB(t)
	if err := db.AutoMigrate(&model.ProductionForumConfig{}, &model.ProductionForumRoute{}, &model.ProductionForumStatusRole{}, &model.ProductionForumTag{}, &model.ForumBaseline{}, &model.ForumTask{}, &model.ForumTransition{}, &model.ForumOutbox{}, &model.ForumFlapState{}, &model.ForumDMChannel{}); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p1", "Production", "", "guild-1", "category-1", "ja"); err != nil {
		t.Fatal(err)
	}
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "p1", Mode: model.NotificationModeForum, GuildID: "guild-1"},
		Routes: []model.ProductionForumRoute{{ProductionID: "p1", TaskTypeID: "tt-comp", TaskTypeName: "Compositing", ForumChannelID: "forum-1", ForumChannelName: "Compositing"}},
		StatusRoles: []model.ProductionForumStatusRole{
			{ProductionID: "p1", StatusID: "s-wfa", StatusName: "WFA", Role: model.ForumRoleWFA},
			{ProductionID: "p1", StatusID: "s-retake", StatusName: "Retake", Role: model.ForumRoleRetake},
			{ProductionID: "p1", StatusID: "s-done", StatusName: "Done", Role: model.ForumRoleDone},
		},
		Tags: []model.ProductionForumTag{
			{ProductionID: "p1", ForumChannelID: "forum-1", Role: model.ForumRoleWFA, TagID: "tag-wfa"},
			{ProductionID: "p1", ForumChannelID: "forum-1", Role: model.ForumRoleRetake, TagID: "tag-retake"},
			{ProductionID: "p1", ForumChannelID: "forum-1", Role: model.ForumRoleDone, TagID: "tag-done"},
		},
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatal(err)
	}
	project := model.ListProjects(db)[0]
	if err := model.UpsertProjectReviewerTarget(db, project.ID, "tt-comp", "Compositing", model.ReviewerTargetUser, "111111111111111111"); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedForumDigestTask(t *testing.T, db *gorm.DB, taskID, statusID string, since time.Time, assignees ...string) {
	t.Helper()
	row := model.ForumTask{
		TaskID:      taskID,
		ProjectID:   "p1",
		TaskTypeID:  "tt-comp",
		EntityKind:  model.ForumEntityKindShot,
		Title:       "Compositing sq010 " + taskID,
		StatusID:    statusID,
		StatusSince: since,
		ForumID:     "forum-1",
		ThreadID:    "thread-" + taskID,
	}
	if len(assignees) > 0 {
		encoded, err := json.Marshal(model.NormalizeForumAssigneeIDs(assignees))
		if err != nil {
			t.Fatal(err)
		}
		row.AssigneeIDs = string(encoded)
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

// TestForumDigestGroupsByPerson covers the selection rules: WFA goes to the
// reviewer, RETAKE to the assignee, items opened today are left for tomorrow,
// and tasks without a post never appear.
func TestForumDigestGroupsByPerson(t *testing.T) {
	db := forumDigestDB(t)
	defer closeNotificationPipelineDB(t, db)

	now := time.Date(2026, 11, 16, 9, 0, 0, 0, jst)
	yesterday := now.AddDate(0, 0, -1)
	project := model.ListProjects(db)[0]
	settings, ok, err := model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("settings: ok=%v err=%v", ok, err)
	}
	seedForumDigestTask(t, db, "task-wfa", "s-wfa", yesterday)
	seedForumDigestTask(t, db, "task-retake", "s-retake", yesterday, "kitsu-person-1")
	seedForumDigestTask(t, db, "task-today", "s-wfa", now.Add(-time.Hour))
	// A WIP task with a post is not a digest item.
	db.Model(&model.ForumTask{}).Where("task_id = ?", "task-today").Update("status_id", "s-wfa")
	if err := db.Create(&model.ForumTask{TaskID: "task-nopost", ProjectID: "p1", TaskTypeID: "tt-comp", StatusID: "s-wfa", StatusSince: yesterday}).Error; err != nil {
		t.Fatal(err)
	}

	oldResolver := discord.UserMapResolver
	discord.UserMapResolver = func(projectID, kitsuPersonID, kitsuName, kitsuEmail string) string {
		if kitsuPersonID == "kitsu-person-1" {
			return "222222222222222222"
		}
		return ""
	}
	t.Cleanup(func() { discord.UserMapResolver = oldResolver })

	recipients, err := forumDigestRecipients(db, project, settings, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 2 {
		t.Fatalf("recipients = %#v", recipients)
	}
	if recipients[0].DiscordID != "111111111111111111" || len(recipients[0].Waiting) != 1 || recipients[0].Waiting[0].TaskID != "task-wfa" {
		t.Fatalf("reviewer digest = %#v", recipients[0])
	}
	if len(recipients[0].Retakes) != 0 {
		t.Fatalf("reviewer should not get retakes: %#v", recipients[0].Retakes)
	}
	if recipients[1].DiscordID != "222222222222222222" || len(recipients[1].Retakes) != 1 || recipients[1].Retakes[0].TaskID != "task-retake" {
		t.Fatalf("assignee digest = %#v", recipients[1])
	}
}

// TestForumDigestSendsOncePerDayAndDropsAfterCatchup checks the marker
// behaviour: a successful DM is not repeated, a failure before 12:00 is left
// for the next poll, and a failure after 12:00 is dropped.
func TestForumDigestSendsOncePerDayAndDropsAfterCatchup(t *testing.T) {
	db := forumDigestDB(t)
	defer closeNotificationPipelineDB(t, db)
	seedForumDigestTask(t, db, "task-wfa", "s-wfa", time.Date(2026, 11, 15, 9, 0, 0, 0, jst))

	fixture := newDMFixture(t)
	morning := time.Date(2026, 11, 16, 9, 0, 0, 0, jst)
	today := morning.Format("2006-01-02")

	sent, err := sendForumMorningDigests(pipelineConfig(), db, morning, today)
	if err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	if fixture.sendsFor("111111111111111111") != 1 {
		t.Fatalf("sends = %d", fixture.sendsFor("111111111111111111"))
	}
	if model.GetSetting(db, digestLastSentDateSettingKey+"p1.111111111111111111") != today {
		t.Fatal("marker not set after a successful send")
	}
	if sent, _ := sendForumMorningDigests(pipelineConfig(), db, morning.Add(time.Hour), today); sent != 0 {
		t.Fatalf("digest repeated: sent=%d", sent)
	}

	// A new day with a failing DM: before the deadline the marker stays unset.
	nextDay := morning.AddDate(0, 0, 1)
	nextToday := nextDay.Format("2006-01-02")
	fixture.failingIDs["111111111111111111"] = true
	discord.ResetDMChannelCache()
	if _, err := sendForumMorningDigests(pipelineConfig(), db, nextDay, nextToday); err == nil {
		t.Fatal("expected a send error before the catch-up deadline")
	}
	if model.GetSetting(db, digestLastSentDateSettingKey+"p1.111111111111111111") == nextToday {
		t.Fatal("marker set despite a failed send before 12:00")
	}

	// After 12:00 the failure is dropped so it does not retry all day.
	afternoon := nextDay.Add(3 * time.Hour)
	if _, err := sendForumMorningDigests(pipelineConfig(), db, afternoon, nextToday); err != nil {
		t.Fatalf("unexpected error after catch-up: %v", err)
	}
	if model.GetSetting(db, digestLastSentDateSettingKey+"p1.111111111111111111") != nextToday {
		t.Fatal("marker not set after the catch-up deadline")
	}
}

package main

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"app/src/model"
	"app/src/setup"
)

// PR #283 の指摘: WFA digest items must be routed to the reviewers of the item's
// own Task Type, including the automatic supervisors the runtime resolver adds.
func TestForumDigestResolvesReviewersPerTaskType(t *testing.T) {
	db := forumDigestDB(t)
	defer closeForumDigestDB(t, db)

	// A second routed Task Type with its own automatic supervisor.
	settings, ok, err := model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("settings: ok=%v err=%v", ok, err)
	}
	settings.Routes = append(settings.Routes, model.ProductionForumRoute{
		ProductionID: "p1", TaskTypeID: "tt-anim", TaskTypeName: "Animation",
		ForumChannelID: "forum-2", ForumChannelName: "Animation",
	})
	settings.Tags = append(settings.Tags,
		model.ProductionForumTag{ProductionID: "p1", ForumChannelID: "forum-2", Role: model.ForumRoleWFA, TagID: "tag-anim-wfa"},
		model.ProductionForumTag{ProductionID: "p1", ForumChannelID: "forum-2", Role: model.ForumRoleRetake, TagID: "tag-anim-retake"},
		model.ProductionForumTag{ProductionID: "p1", ForumChannelID: "forum-2", Role: model.ForumRoleDone, TagID: "tag-anim-done"},
	)
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	original := forumDigestReviewerResolver
	t.Cleanup(func() { forumDigestReviewerResolver = original })
	forumDigestReviewerResolver = func(db *gorm.DB, kitsuBaseURL, kitsuToken, discordToken, kitsuProjectID, taskTypeID string) ([]model.ProjectReviewerTarget, error) {
		if taskTypeID == "tt-anim" {
			return []model.ProjectReviewerTarget{{TargetKind: model.ReviewerTargetUser, DiscordID: "333333333333333333"}}, nil
		}
		return []model.ProjectReviewerTarget{{TargetKind: model.ReviewerTargetUser, DiscordID: "111111111111111111"}}, nil
	}

	now := time.Date(2026, 11, 16, 9, 0, 0, 0, jst)
	yesterday := now.AddDate(0, 0, -1)
	seedForumDigestTask(t, db, "task-comp", "s-wfa", yesterday)
	seedForumDigestTask(t, db, "task-anim", "s-wfa", yesterday)
	if err := db.Model(&model.ForumTask{}).Where("task_id = ?", "task-anim").Updates(map[string]interface{}{
		"task_type_id": "tt-anim", "forum_id": "forum-2",
	}).Error; err != nil {
		t.Fatal(err)
	}

	project := model.ListProjects(db)[0]
	recipients, err := forumDigestRecipients(db, project, settings, now)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]forumDigestRecipient{}
	for _, recipient := range recipients {
		byID[recipient.DiscordID] = recipient
	}
	if len(byID["111111111111111111"].Waiting) != 1 || byID["111111111111111111"].Waiting[0].TaskID != "task-comp" {
		t.Fatalf("compositing reviewer queue = %#v", byID["111111111111111111"].Waiting)
	}
	if len(byID["333333333333333333"].Waiting) != 1 || byID["333333333333333333"].Waiting[0].TaskID != "task-anim" {
		t.Fatalf("animation reviewer queue = %#v", byID["333333333333333333"].Waiting)
	}
}

var _ = setup.ResolveForumWFAReviewerTargets

package forum

import (
	"encoding/json"
	"testing"
	"time"

	"app/src/api/kitsu"
	"app/src/model"
)

// A routed Task Type with no visible task must still be baselined, otherwise
// its first task is later recorded as a baseline and never posted.
func TestPollProjectsBaselinesRoutedTaskTypesWithoutTasks(t *testing.T) {
	db := newForumDB(t)
	settings := testSettings()
	settings.Routes = append(settings.Routes, model.ProductionForumRoute{
		ProductionID:     "prod-1",
		TaskTypeID:       "tt-anim",
		TaskTypeName:     "Animation",
		ForumChannelID:   "forum-2",
		ForumChannelName: "Animation",
	})
	for _, role := range []string{model.ForumRoleWFA, model.ForumRoleRetake, model.ForumRoleDone} {
		settings.Tags = append(settings.Tags, model.ProductionForumTag{
			ProductionID:   "prod-1",
			ForumChannelID: "forum-2",
			Role:           role,
			TagID:          "tag-2-" + role,
		})
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	renderCard := func(kitsu.MessagePayload) (json.RawMessage, error) {
		card, err := stubCard()()
		return card, err
	}
	poller := Poller{DB: db, Now: func() time.Time { return now }, RenderCard: renderCard}
	result, err := poller.PollProjects([]kitsu.MessagePayload{payload("task-1", "tt-comp")})
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if result.Baselined != 2 {
		t.Fatalf("baselined = %d, want 2 (both routed task types)", result.Baselined)
	}
	for _, taskTypeID := range []string{"tt-comp", "tt-anim"} {
		seeded, err := model.ForumBaselineExists(db, "prod-1", taskTypeID)
		if err != nil {
			t.Fatalf("baseline exists: %v", err)
		}
		if !seeded {
			t.Fatalf("task type %s was not baselined", taskTypeID)
		}
	}
}

func payload(taskID, taskTypeID string) kitsu.MessagePayload {
	var item kitsu.MessagePayload
	item.Project.ID = "prod-1"
	item.Task.ID = taskID
	item.TaskType.ID = taskTypeID
	item.TaskType.Name = taskTypeID
	item.TaskStatus.ID = "s-wip"
	item.Entity.ID = "entity-1"
	item.Entity.Name = "shot_010"
	return item
}

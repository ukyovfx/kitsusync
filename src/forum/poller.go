package forum

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"app/src/api/kitsu"
	"app/src/model"
)

// Poller feeds one Kitsu poll into the forum ledger for every Production that
// is in forum mode. Lane-mode Productions are left to the existing pipeline.

// PollResult summarises one forum poll.
type PollResult struct {
	Productions int
	Baselined   int
	Created     int
	Synced      int
	Signalled   int
	Skipped     int
}

// Poller holds the poll dependencies.
type Poller struct {
	DB   *gorm.DB
	Now  func() time.Time
	// RenderCard renders the first message for a task.
	RenderCard func(kitsu.MessagePayload) (json.RawMessage, error)
}

// PollProjects processes the payloads of every forum-mode Production.
func (p Poller) PollProjects(payloads []kitsu.MessagePayload) (PollResult, error) {
	result := PollResult{}
	if p.DB == nil {
		return result, nil
	}
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now()
	}
	byProduction := map[string][]kitsu.MessagePayload{}
	for _, payload := range payloads {
		if payload.Project.ID == "" || payload.Task.ID == "" {
			continue
		}
		byProduction[payload.Project.ID] = append(byProduction[payload.Project.ID], payload)
	}
	for productionID, items := range byProduction {
		settings, ok, err := model.LoadProductionForumSettings(p.DB, productionID)
		if err != nil {
			return result, err
		}
		if !ok || settings.Config.Mode != model.NotificationModeForum {
			continue
		}
		result.Productions++
		planner := Planner{DB: p.DB, Production: productionID, Settings: settings, Now: now}
		// Group by task type so a new task type is baselined in one transaction.
		byType := map[string][]kitsu.MessagePayload{}
		for _, item := range items {
			byType[item.TaskType.ID] = append(byType[item.TaskType.ID], item)
		}
		for taskTypeID, group := range byType {
			seeded, err := model.ForumBaselineExists(p.DB, productionID, taskTypeID)
			if err != nil {
				return result, err
			}
			if !seeded {
				inputs := make([]TaskInput, 0, len(group))
				for _, item := range group {
					inputs = append(inputs, taskInputFromPayload(item))
				}
				if err := planner.PlanBaseline(taskTypeID, inputs); err != nil {
					return result, err
				}
				result.Baselined++
				continue
			}
			for _, item := range group {
				input := taskInputFromPayload(item)
				input.StatusRole = planner.RoleForStatus(input.StatusID)
				card := func() (json.RawMessage, error) {
					if p.RenderCard == nil {
						return nil, errors.New("no card renderer configured")
					}
					return p.RenderCard(item)
				}
				plan, err := planner.PlanTask(input, card)
				if err != nil {
					return result, err
				}
				switch {
				case plan.Skipped:
					result.Skipped++
				case plan.Baselined:
					result.Baselined++
				default:
					if plan.Created {
						result.Created++
					}
					if plan.Synced {
						result.Synced++
					}
					if plan.Signalled != "" {
						result.Signalled++
					}
				}
			}
		}
	}
	return result, nil
}

// taskInputFromPayload maps a Kitsu payload to a planner input.
func taskInputFromPayload(payload kitsu.MessagePayload) TaskInput {
	assignees := make([]string, 0, len(payload.Assignees))
	for _, person := range payload.Assignees {
		if person.IsBot {
			continue
		}
		assignees = append(assignees, person.ID)
	}
	entityKind := EntityKindFor(payload.EntityType.EntityType.Name)
	// Shots use the sequence name, assets use the asset type name (Q-02).
	middle := payload.Parent.Entity.Name
	if entityKind == model.ForumEntityKindAsset {
		middle = payload.EntityType.EntityType.Name
	}
	title := TaskTitle(payload.TaskType.TaskType.Name, middle, payload.Entity.Entity.Name)
	return TaskInput{
		TaskID:       payload.Task.Task.ID,
		ProjectID:    payload.Project.Project.ID,
		TaskTypeID:   payload.TaskType.TaskType.ID,
		EntityID:     payload.Entity.Entity.ID,
		EntityKind:   entityKind,
		EntityName:   payload.Entity.Entity.Name,
		ParentName:   payload.Parent.Entity.Name,
		EntityType:   payload.EntityType.EntityType.Name,
		Title:        title,
		StatusID:     payload.TaskStatus.TaskStatus.ID,
		Assignees:    assignees,
		CommentDate:  payload.Task.Task.LastCommentDate,
		KitsuUpdated: payload.Task.Task.UpdatedAt,
		Comment: CommentInput{
			ID:          payload.LatestComment.Comment.ID,
			Text:        payload.LatestComment.Comment.Text,
			CreatedAt:   payload.LatestComment.Comment.CreatedAt,
			Mentions:    commentMentionIDs(payload),
			Unavailable: payload.CommentsUnavailable,
		},
	}
}

// commentMentionIDs returns the Kitsu person IDs named in the newest comment.
// Kitsu exposes the mentions on the comment itself.
func commentMentionIDs(payload kitsu.MessagePayload) []string {
	return model.NormalizeForumAssigneeIDs(payload.LatestComment.Comment.Mentions)
}

// TaskURLFor builds the Kitsu task link for a ledger row.
func TaskURLFor(db *gorm.DB, publicURL func() string, taskID string) string {
	task, err := model.FindForumTaskWithError(db, taskID)
	if err != nil {
		return ""
	}
	kind := task.EntityKind
	if kind != model.ForumEntityKindShot {
		kind = model.ForumEntityKindAsset
	}
	base := ""
	if publicURL != nil {
		base = strings.TrimRight(publicURL(), "/")
	}
	if base == "" {
		return ""
	}
	return base + "/productions/" + task.ProjectID + "/" + kind + "s/tasks/" + task.TaskID
}

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
	DB  *gorm.DB
	Now func() time.Time
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
		if !ok {
			continue
		}
		if problems := model.ForumSettingsProblems(settings); len(problems) > 0 {
			// Incomplete draft settings (e.g. leftovers from old Mode=lane data) must
			// not be acted on; the old poller skipped them.
			continue
		}
		routed := make(map[string]struct{}, len(settings.Routes))
		result.Productions++
		planner := Planner{DB: p.DB, Production: productionID, Settings: settings, Now: now}
		// Group by task type so a new task type is baselined in one transaction.
		// Only routed Task Types are considered: an unrouted type's tasks do not
		// change here, so baselining them would go stale and replay old work as
		// fresh notifications once the type is routed later.
		byType := map[string][]kitsu.MessagePayload{}
		for _, route := range settings.Routes {
			routed[route.TaskTypeID] = struct{}{}
			byType[route.TaskTypeID] = nil
		}
		for _, item := range items {
			if _, ok := routed[item.TaskType.ID]; !ok {
				continue
			}
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
		TaskTypeName: payload.TaskType.TaskType.Name,
		StatusName:   payload.TaskStatus.TaskStatus.Name,
		StatusID:     payload.TaskStatus.TaskStatus.ID,
		Assignees:    assignees,
		CommentDate:  payload.Task.Task.LastCommentDate,
		KitsuUpdated: payload.Task.Task.UpdatedAt,
		Comment: CommentInput{
			ID:          payload.LatestComment.Comment.ID,
			Text:        payload.LatestComment.Comment.Text,
			CreatedAt:   payload.LatestComment.Comment.CreatedAt,
			AuthorID:    payload.LatestComment.Author.Person.ID,
			AuthorName:  payload.LatestComment.Author.Person.FullName,
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

// HandoffDispatch is one immediate handoff signal to place: the recorded
// event, the recipient Kitsu person IDs to pin as mentions (empty = the
// receiving task's current assignees), and the pre-built reply-button rows.
type HandoffDispatch struct {
	Event      model.HandoffEvent
	Mentions   []string
	Components json.RawMessage
}

// DispatchHandoffs places the immediate handoff signals of one poll into the
// forum outbox and returns the IDs of the events that were queued. Events
// whose Production is not in forum mode, whose receiving task type is not
// routed, or whose receiving task is not in the ledger yet are skipped, so
// the caller can leave them pending for a later poll or the morning digest.
func (p Poller) DispatchHandoffs(payloads []kitsu.MessagePayload, items []HandoffDispatch) (map[uint]bool, error) {
	queued := map[uint]bool{}
	if p.DB == nil || len(items) == 0 {
		return queued, nil
	}
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now()
	}
	byTask := map[string]kitsu.MessagePayload{}
	for _, payload := range payloads {
		if payload.Task.ID != "" {
			byTask[payload.Task.ID] = payload
		}
	}
	planners := map[string]*Planner{}
	for _, item := range items {
		payload, ok := byTask[item.Event.NotifyTaskID]
		if !ok {
			continue
		}
		planner, cached := planners[item.Event.ProductionID]
		if !cached {
			settings, found, err := model.LoadProductionForumSettings(p.DB, item.Event.ProductionID)
			if err != nil {
				return queued, err
			}
			if !found || len(model.ForumSettingsProblems(settings)) > 0 {
				planners[item.Event.ProductionID] = nil
				continue
			}
			pl := Planner{DB: p.DB, Production: item.Event.ProductionID, Settings: settings, Now: now}
			planners[item.Event.ProductionID] = &pl
			planner = &pl
		}
		if planner == nil {
			continue
		}
		input := taskInputFromPayload(payload)
		input.StatusRole = planner.RoleForStatus(input.StatusID)
		card := func() (json.RawMessage, error) {
			if p.RenderCard == nil {
				return nil, errors.New("no card renderer configured")
			}
			return p.RenderCard(payload)
		}
		done, err := planner.PlanHandoff(input, card, item.Event, item.Mentions, item.Components)
		if err != nil {
			return queued, err
		}
		if done {
			queued[item.Event.ID] = true
		}
	}
	return queued, nil
}

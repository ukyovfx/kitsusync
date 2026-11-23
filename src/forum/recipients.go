package forum

import (
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
)

// Recipients resolves the mention targets of a signal. WFA goes to the
// Production's reviewer table (stage C); RETAKE/DONE go to the current
// assignees; an assignee change goes to the newly added assignees.
type Recipients struct {
	DB *gorm.DB
	// ResolveAssignees maps Kitsu person IDs to Discord IDs.
	ResolveAssignees func(projectID string, kitsuPersonIDs []string) []string
	// ResolveReviewers returns the reviewer targets for a task type.
	ResolveReviewers func(projectID, taskTypeID, taskTypeName string) ([]discord.ReviewerTarget, error)
	// TaskTypeName resolves a display name for the reviewer lookup.
	TaskTypeName func(taskTypeID string) string
	// NotifyAssigneesOnComment includes the current assignees in a comment
	// signal (settings key notification.comment_notify_assignee, conf.toml
	// fallback, default false).
	NotifyAssigneesOnComment bool
}

// ForTask returns the user and role IDs to mention. A resolution error is
// returned so the executor can retry instead of posting without mentions.
func (r Recipients) ForTask(taskID string, payload model.ForumPostMessagePayload) ([]string, []string, error) {
	task, err := model.FindForumTaskWithError(r.DB, taskID)
	if err != nil {
		return nil, nil, err
	}
	switch payload.Intent {
	case model.ForumIntentStatusWFA:
		return r.reviewers(task.ProjectID, task.TaskTypeID)
	case model.ForumIntentStatusRetake, model.ForumIntentStatusDone:
		users, roles := r.assignees(task)
		return users, roles, nil
	case model.ForumIntentAssign:
		added := addedIDs(payload.BaseAssignees, model.ForumAssigneeIDs(task))
		if len(added) == 0 {
			return nil, nil, nil
		}
		return r.mapAssignees(task.ProjectID, added), nil, nil
	case model.ForumIntentComment:
		// Q-01: the comment's mentions plus the current assignees.
		users := r.mapAssignees(task.ProjectID, payload.Mentions)
		if r.NotifyAssigneesOnComment {
			assigneeUsers, _ := r.assignees(task)
			users = append(users, assigneeUsers...)
		}
		return uniqueIDs(users), nil, nil
	case model.ForumIntentHandoff:
		// A handoff pings the people the dispatcher pinned (the receiving
		// task's assignees at detection, or the fallback admins); without a
		// pinned set it falls back to the task's current assignees.
		if len(payload.Mentions) > 0 {
			return r.mapAssignees(task.ProjectID, payload.Mentions), nil, nil
		}
		users, roles := r.assignees(task)
		return users, roles, nil
	case model.ForumIntentPublish:
		// The publish trace informs the thread; it pings nobody (spec).
		return nil, nil, nil
	default:
		return nil, nil, nil
	}
}

func uniqueIDs(ids []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (r Recipients) reviewers(projectID, taskTypeID string) ([]string, []string, error) {
	if r.ResolveReviewers == nil {
		return nil, nil, nil
	}
	name := ""
	if r.TaskTypeName != nil {
		name = r.TaskTypeName(taskTypeID)
	}
	targets, err := r.ResolveReviewers(projectID, taskTypeID, name)
	if err != nil {
		return nil, nil, err
	}
	var users, roles []string
	for _, target := range targets {
		switch target.Kind {
		case model.ReviewerTargetUser:
			users = append(users, target.ID)
		case model.ReviewerTargetRole:
			roles = append(roles, target.ID)
		}
	}
	return users, roles, nil
}

func (r Recipients) assignees(task model.ForumTask) ([]string, []string) {
	return r.mapAssignees(task.ProjectID, model.ForumAssigneeIDs(task)), nil
}

func (r Recipients) mapAssignees(projectID string, ids []string) []string {
	if len(ids) == 0 || r.ResolveAssignees == nil {
		return nil
	}
	return r.ResolveAssignees(projectID, ids)
}

func addedIDs(base, current []string) []string {
	existing := map[string]struct{}{}
	for _, id := range base {
		existing[id] = struct{}{}
	}
	var added []string
	for _, id := range current {
		if _, ok := existing[id]; !ok {
			added = append(added, id)
		}
	}
	return added
}

// Render builds a card payload for a task.
func RenderCard(payload kitsu.MessagePayload, build func(kitsu.MessagePayload) (discord.Payload, error)) (json.RawMessage, error) {
	if build == nil {
		return nil, errors.New("no card builder configured")
	}
	card, err := build(payload)
	if err != nil {
		return nil, err
	}
	// The first message carries the card only; mentions are suppressed.
	card.Content = ""
	card.AllowedMentions = &discord.AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}}
	encoded, err := json.Marshal(card)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// ValidateForumTargets checks that the configured forum and tag IDs still exist
// in the guild. Problems are returned as human-readable Japanese messages.
func ValidateForumTargets(db *gorm.DB, productionID, guildID string, lister func(guildID, botToken string) ([]ForumChannel, error), botToken string) []string {
	settings, ok, err := model.LoadProductionForumSettings(db, productionID)
	if err != nil || !ok {
		return nil
	}
	channels, err := lister(guildID, botToken)
	if err != nil {
		return []string{fmt.Sprintf("Discordのフォーラム一覧を取得できませんでした: %v", err)}
	}
	byID := map[string]ForumChannel{}
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	var problems []string
	for _, route := range settings.Routes {
		channel, found := byID[route.ForumChannelID]
		if !found {
			problems = append(problems, fmt.Sprintf("フォーラム %s（%s）が見つかりません", route.TaskTypeName, route.ForumChannelID))
			continue
		}
		tags := map[string]struct{}{}
		for _, tag := range channel.Tags {
			tags[tag.ID] = struct{}{}
		}
		for _, tag := range settings.Tags {
			if tag.ForumChannelID != route.ForumChannelID {
				continue
			}
			if _, ok := tags[tag.TagID]; !ok {
				problems = append(problems, fmt.Sprintf("フォーラム %s のタグ %s が見つかりません", channel.Name, tag.TagID))
			}
		}
	}
	return problems
}

// ForumChannel is the minimal channel shape the validator needs.
type ForumChannel struct {
	ID   string
	Name string
	Tags []ForumTag
}

// ForumTag is a forum tag with its ID and name.
type ForumTag struct {
	ID   string
	Name string
}

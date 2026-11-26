package forum

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	// ResolveAdmins returns the Discord user IDs of the Production's guild
	// administrators, the WFA fallback when the reviewer chain (reviewer,
	// then department supervisor) resolves to nobody (core v2 §2.3).
	ResolveAdmins func(projectID string) ([]string, error)
	// TaskTypeName resolves a display name for the reviewer lookup.
	TaskTypeName func(taskTypeID string) string
	// Comment signals relay @mentions only (redesign ruling B, 2026-11-23):
	// the people named in the comment are the recipients, unconditionally.
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
		users, roles, err := r.reviewers(task.ProjectID, task.TaskTypeID)
		if err != nil || len(users)+len(roles) > 0 {
			return users, roles, err
		}
		return r.adminFallback(task)
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
		// Ruling B: mentions only, no assignee broadcast.
		return uniqueIDs(r.mapAssignees(task.ProjectID, payload.Mentions)), nil, nil
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

// adminFallback delivers a WFA signal whose reviewer chain resolved to
// nobody: the guild administrators are mentioned instead, the fallback is
// written to the decision log, and the miss is queued for a human as a
// recipient_unresolved exception (core v2 §2.3, §8.3 condition 1). The
// exception aggregates per task type, like no_route, so a Production whose
// reviewer data is simply unregistered raises it once, not once per task.
// When no administrator can be resolved either, the signal stays
// recipient-less and the executor cancels it as before — but the exception
// is still raised, because that is exactly what an admin must see.
func (r Recipients) adminFallback(task model.ForumTask) ([]string, []string, error) {
	if r.ResolveAdmins == nil {
		return nil, nil, nil
	}
	admins, err := r.ResolveAdmins(task.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	admins = uniqueIDs(admins)
	if r.DB != nil {
		name := ""
		if r.TaskTypeName != nil {
			name = r.TaskTypeName(task.TaskTypeID)
		}
		_ = model.RecordNotificationDecision(r.DB, model.NotificationDecision{
			ProductionID: task.ProjectID,
			TaskID:       task.TaskID,
			TaskTypeName: name,
			SignalKind:   "status",
			RuleID:       "status.review.admin_fallback",
			Facts: model.DecisionFactsJSON(map[string]interface{}{
				"admin_discord_ids": admins,
				"task_title":        task.Title,
			}),
			Outcome: model.DecisionOutcomeQueued,
			RefID:   task.StatusID,
		})
	}
	return admins, nil, nil
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
	if CardEmbedEmpty(card) {
		// Non-trigger statuses render through the preset template files;
		// when those cannot be read the result is an embed with no content
		// at all, and Discord rejects a thread opened with it (400 Invalid
		// Form Body), losing the signal that opened the thread. Fall back
		// to the standard card built from the task's current data.
		card = FallbackCard(payload)
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

// CardEmbedEmpty reports whether a card payload carries no renderable
// embed content at all: no title, description, fields, image, author name
// or footer text on any embed. Discord rejects a message whose only embed
// is empty, so a thread must never be opened with one.
func CardEmbedEmpty(card discord.Payload) bool {
	for _, embed := range card.Embeds {
		if embed.Title != "" || embed.Description != "" || len(embed.Fields) > 0 ||
			embed.Image != nil || embed.Author.Name != "" || embed.Footer.Text != "" {
			return false
		}
	}
	return true
}

// FallbackCard builds the standard signal-shaped card from the task's
// current data — task type, shot name, status and assignees, the same
// fields a rendered card shows — for the cases where the configured
// renderer produced an empty embed.
func FallbackCard(payload kitsu.MessagePayload) discord.Payload {
	assignees := make([]string, 0, len(payload.Assignees))
	for _, person := range payload.Assignees {
		if person.IsBot {
			continue
		}
		if name := strings.TrimSpace(person.FullName); name != "" {
			assignees = append(assignees, name)
		}
	}
	statusName := strings.TrimSpace(payload.TaskStatus.TaskStatus.Name)
	if statusName == "" {
		statusName = strings.TrimSpace(payload.TaskStatus.TaskStatus.ShortName)
	}
	signal := discord.RenderForumSignalCard(discord.ForumCardInput{
		TaskType:   payload.TaskType.TaskType.Name,
		EntityType: payload.EntityType.EntityType.Name,
		ParentName: payload.Parent.Entity.Name,
		EntityName: payload.Entity.Entity.Name,
		StatusName: statusName,
		Assignees:  assignees,
	})
	return discord.Payload{Embeds: signal.Embeds}
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

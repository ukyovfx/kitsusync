package forum

import (
	"strings"

	"gorm.io/gorm"

	"app/src/api/kitsu"
	"app/src/model"
)

// HandoffResult counts one handoff detection pass.
type HandoffResult struct {
	Events int // new events recorded
	Known  int // deliveries whose comment was already eventized
}

// HandoffDetector turns AYON publish comments into handoff events. It reads the
// K1 mapping table (source Task Type × product type) and, when the flow is a
// handoff, resolves the receiving task on the same shot/asset.
type HandoffDetector struct {
	DB *gorm.DB
	// Comments reads one task's comments, used when the payload's newest
	// comment carries no text. Tests inject a stub.
	Comments func(taskID string) (kitsu.Comments, error)
}

// DeliverTable is the AYON custom_comment_template table: the publish marker.
type DeliverTable struct {
	Family  string
	Version string
	Name    string
}

// ParseDeliverTable reads the version/family/name rows of the delivery table.
// Any free text may precede it; a comment without all three rows is not a
// delivery.
func ParseDeliverTable(text string) (DeliverTable, bool) {
	var table DeliverTable
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 2 {
			continue
		}
		value := cleanTableCell(cells[1])
		if value == "" {
			continue
		}
		switch strings.ToLower(cleanTableCell(cells[0])) {
		case "version":
			if table.Version == "" {
				table.Version = value
			}
		case "family":
			if table.Family == "" {
				table.Family = value
			}
		case "name":
			if table.Name == "" {
				table.Name = value
			}
		}
	}
	if table.Version == "" || table.Family == "" || table.Name == "" {
		return DeliverTable{}, false
	}
	return table, true
}

// cleanTableCell strips the markdown emphasis AYON may wrap cells in.
func cleanTableCell(cell string) string {
	return strings.Trim(cell, " \t`*_")
}

// Detect reads the poll payloads and records one event per handoff delivery.
func (d HandoffDetector) Detect(payloads []kitsu.MessagePayload) (HandoffResult, error) {
	var result HandoffResult
	if d.DB == nil {
		return result, nil
	}
	settingsByProduction := map[string]model.ProductionHandoffSettings{}
	unconfigured := map[string]bool{}
	for _, payload := range payloads {
		if payload.CommentsUnavailable || strings.TrimSpace(payload.LatestComment.Comment.Comment.ID) == "" {
			continue
		}
		// A publish comment carries no status change. A task that just moved
		// belongs to the submit flow (WFA and later) and must stay there.
		if strings.TrimSpace(payload.StatusChangeAuthor.Person.ID) != "" || payload.PreviousStatusName != "" {
			continue
		}
		comment, ok := d.deliverComment(payload)
		if !ok {
			continue
		}
		table, ok := ParseDeliverTable(comment.Text)
		if !ok {
			continue
		}
		productionID := payload.Project.Project.ID
		if unconfigured[productionID] {
			continue
		}
		settings, loaded := settingsByProduction[productionID]
		if !loaded {
			var found bool
			var err error
			settings, found, err = model.LoadProductionHandoffSettings(d.DB, productionID)
			if err != nil {
				return result, err
			}
			if !found {
				unconfigured[productionID] = true
				continue
			}
			settingsByProduction[productionID] = settings
		}
		route, routed := handoffRoute(settings, payload.TaskType.TaskType.ID, table.Family)
		if !routed {
			continue
		}
		receiving, found := receivingPayload(payloads, payload, model.SplitHandoffNotifyIDs(route.NotifyTaskTypeIDs))
		if !found {
			continue
		}
		event := handoffEventFor(payload, comment, table, route, receiving, settings.Config)
		if route.Flow != model.HandoffFlowHand {
			// The submit and shelve flows already tell the receiving artist,
			// so this row is a handoff that needs no separate report.
			continue
		}
		existing, err := model.RecordHandoffEvent(d.DB, event)
		if err != nil {
			return result, err
		}
		if existing {
			result.Known++
			continue
		}
		result.Events++
	}
	return result, nil
}

// deliverComment returns the delivery comment. The payload's newest comment
// normally carries the text; when it does not, the task's comments are read.
func (d HandoffDetector) deliverComment(payload kitsu.MessagePayload) (kitsu.Comment, bool) {
	latest := payload.LatestComment.Comment.Comment
	if strings.TrimSpace(latest.Text) != "" {
		return latest, true
	}
	if d.Comments == nil || strings.TrimSpace(payload.Task.Task.ID) == "" {
		return kitsu.Comment{}, false
	}
	comments, err := d.Comments(payload.Task.Task.ID)
	if err != nil {
		return kitsu.Comment{}, false
	}
	for _, comment := range comments.Each {
		if comment.ID == latest.ID && strings.TrimSpace(comment.Text) != "" {
			return comment, true
		}
	}
	return kitsu.Comment{}, false
}

// handoffRoute finds the mapping row for this source Task Type and product
// family.
func handoffRoute(settings model.ProductionHandoffSettings, taskTypeID, family string) (model.ProductionHandoffRoute, bool) {
	family = strings.TrimSpace(family)
	for _, route := range settings.Routes {
		if strings.TrimSpace(route.TaskTypeID) != strings.TrimSpace(taskTypeID) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(route.ProductType), family) {
			return route, true
		}
	}
	return model.ProductionHandoffRoute{}, false
}

// receivingPayload finds the task that receives the delivery: the same shot or
// asset, one of the route's notify Task Types.
func receivingPayload(payloads []kitsu.MessagePayload, source kitsu.MessagePayload, notifyTaskTypeIDs []string) (kitsu.MessagePayload, bool) {
	entityID := strings.TrimSpace(source.Entity.Entity.ID)
	if entityID == "" {
		return kitsu.MessagePayload{}, false
	}
	for _, candidate := range payloads {
		if candidate.Task.Task.ID == source.Task.Task.ID {
			continue
		}
		if strings.TrimSpace(candidate.Entity.Entity.ID) != entityID {
			continue
		}
		if containsID(notifyTaskTypeIDs, candidate.TaskType.TaskType.ID) {
			return candidate, true
		}
	}
	return kitsu.MessagePayload{}, false
}

// handoffEventFor builds the record K3 reads to send the message.
func handoffEventFor(source kitsu.MessagePayload, comment kitsu.Comment, table DeliverTable, route model.ProductionHandoffRoute, receiving kitsu.MessagePayload, conf model.ProductionHandoffConfig) model.HandoffEvent {
	event := model.HandoffEvent{
		CommentID:          comment.ID,
		ProductionID:       source.Project.Project.ID,
		SourceTaskID:       source.Task.Task.ID,
		SourceTaskTypeID:   source.TaskType.TaskType.ID,
		SourceTaskTypeName: source.TaskType.TaskType.Name,
		SourceAuthorID:     source.LatestComment.Author.Person.ID,
		SourceAuthorName:   source.LatestComment.Author.Person.FullName,
		EntityID:           source.Entity.Entity.ID,
		EntityKind:         EntityKindFor(source.EntityType.EntityType.Name),
		EntityName:         source.Entity.Entity.Name,
		ProductType:        table.Family,
		ProductVersion:     table.Version,
		ProductName:        table.Name,
		NotifyTaskID:       receiving.Task.Task.ID,
		NotifyTaskTypeID:   receiving.TaskType.TaskType.ID,
		NotifyTaskTypeName: receiving.TaskType.TaskType.Name,
		State:              model.HandoffEventPending,
	}
	ids := assigneeIDs(receiving)
	if len(ids) == 0 {
		// Nobody is assigned to the receiving task, so the Production's
		// no-assignee handling decides who is told.
		event.FallbackUsed = conf.NoAssignee
		if conf.NoAssignee == model.HandoffFallbackUsers {
			ids = model.SplitHandoffPersonIDs(conf.FallbackUserID)
		}
		if conf.NoAssignee == model.HandoffFallbackDisabled {
			// Nobody is told, so nothing waits for a reply.
			event.State = model.HandoffEventNoTarget
		}
	} else {
		// The receiving task has an assignee, so no fallback was used and the
		// mixed timing sends this handoff immediately.
		event.FallbackUsed = model.HandoffFallbackNone
	}
	event.NotifyPersonIDs = model.JoinHandoffNotifyIDs(ids)
	return event
}

// assigneeIDs mirrors the planner: bot accounts are not recipients.
func assigneeIDs(payload kitsu.MessagePayload) []string {
	ids := make([]string, 0, len(payload.Assignees))
	for _, person := range payload.Assignees {
		if person.IsBot || strings.TrimSpace(person.ID) == "" {
			continue
		}
		ids = append(ids, strings.TrimSpace(person.ID))
	}
	return ids
}

func containsID(ids []string, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, candidate := range ids {
		if strings.TrimSpace(candidate) == id {
			return true
		}
	}
	return false
}

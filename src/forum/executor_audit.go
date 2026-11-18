package forum

import (
	"strings"

	"app/src/model"
)

// recordAudit writes one audit row for a finished outbox unit so forum
// deliveries surface in the same audit trail as the rest of the app. It is
// best-effort: audit failures never interrupt the drain, and rows scheduled
// for another retry are skipped here and audited when they finally finish.
func (e Executor) recordAudit(claimed model.ForumOutbox) {
	var fresh model.ForumOutbox
	if err := e.DB.First(&fresh, claimed.ID).Error; err != nil {
		return
	}
	success := fresh.State == model.ForumOutboxDone
	switch fresh.State {
	case model.ForumOutboxDone, model.ForumOutboxFailed, model.ForumOutboxCancelled:
		// Terminal: audit the final outcome below.
	default:
		// Mid-flight rows (retry scheduled) are audited when they finish.
		return
	}
	entry := model.AuditLog{
		TaskID:       fresh.TaskID,
		NewStatus:    fresh.Result,
		DiscordMsgID: extractDiscordMsgID(fresh.Result),
		Success:      success,
		ErrorMessage: auditFailureSummary(fresh),
		ActorKind:    model.AuditActorSystem,
		ActorName:    "forum-outbox",
		RetryCount:   max(0, fresh.Attempts-1),
	}
	var task model.ForumTask
	if err := e.DB.Where("task_id = ?", fresh.TaskID).First(&task).Error; err == nil {
		entry.ProjectID = task.ProjectID
		entry.EntityName = task.Title
		var route model.ProductionForumRoute
		if err := e.DB.Where("task_type_id = ?", task.TaskTypeID).First(&route).Error; err == nil {
			entry.TaskType = route.TaskTypeName
		}
		var project model.Project
		if err := e.DB.Where("kitsu_project_id = ?", task.ProjectID).First(&project).Error; err == nil {
			entry.ProjectName = project.Name
			entry.GuildID = project.DiscordGuildID
		}
	}
	model.WriteAuditLog(e.DB, entry)
}

// extractDiscordMsgID pulls the Discord object ID out of an outbox result
// string ("thread=<id>" or "message=<id>"); "" when there is none.
func extractDiscordMsgID(result string) string {
	for _, prefix := range []string{"thread=", "message="} {
		if id, ok := strings.CutPrefix(result, prefix); ok && id != "" {
			return id
		}
	}
	return ""
}

// auditFailureSummary condenses a non-success outbox row into a
// secrets-free one-line summary for the audit trail.
func auditFailureSummary(row model.ForumOutbox) string {
	if row.State == model.ForumOutboxDone {
		return ""
	}
	var parts []string
	if strings.TrimSpace(row.LastErrorClass) != "" {
		parts = append(parts, strings.TrimSpace(row.LastErrorClass))
	}
	if strings.TrimSpace(row.Note) != "" {
		parts = append(parts, strings.TrimSpace(row.Note))
	}
	return strings.Join(parts, ": ")
}

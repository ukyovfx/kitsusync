package main

import (
	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type productionRoutingPlan struct {
	Payload       kitsu.MessagePayload
	WebhookURL    string
	DestinationID string
	RuleID        uint
	ShouldSend    bool
	SkipReason    string
}

type ProductionRoutingDryRun struct {
	ProductionID     string `json:"production_id"`
	TaskTypeID       string `json:"task_type_id"`
	MatchedRule      string `json:"matched_rule,omitempty"`
	DestinationID    string `json:"destination_id,omitempty"`
	RenderedPreview  string `json:"rendered_preview"`
	User             string `json:"user,omitempty"`
	Link             string `json:"link,omitempty"`
	SkipReason       string `json:"skip_reason,omitempty"`
	StaleIDDiagnosis string `json:"stale_id_diagnosis,omitempty"`
}

func planProductionNotification(db *gorm.DB, payload kitsu.MessagePayload) productionRoutingPlan {
	plan := productionRoutingPlan{Payload: payload}
	productionID := strings.TrimSpace(payload.Project.ID)
	taskTypeID := strings.TrimSpace(payload.TaskType.ID)
	config := model.FindProductionNotificationConfig(db, productionID)
	if config == nil {
		plan.SkipReason = "production notification routing is not configured"
		return plan
	}
	if !config.Enabled {
		plan.SkipReason = "production notification routing is paused"
		return plan
	}
	for _, route := range model.ListProductionNotificationRoutes(db, productionID) {
		if strings.TrimSpace(route.TaskTypeID) != taskTypeID {
			continue
		}
		plan.RuleID = route.ID
		webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID)
		if webhook == nil || webhook.KitsuProjectID != productionID || strings.TrimSpace(webhook.WebhookURL) == "" {
			plan.SkipReason = "matched route has a stale or invalid destination"
			return plan
		}
		plan.WebhookURL = webhook.WebhookURL
		plan.DestinationID = strings.TrimSpace(webhook.DiscordChannelID)
		if plan.DestinationID == "" {
			plan.SkipReason = "matched route has no destination identifier"
			return plan
		}
		plan.ShouldSend = true
		return plan
	}
	plan.SkipReason = "no Task Type ID route matched; notification was not dispatched"
	return plan
}

func productionTaskLink(payload kitsu.MessagePayload) string {
	host := ""
	if discord.KitsuPublicURLResolver != nil {
		host = strings.TrimSpace(discord.KitsuPublicURLResolver())
	}
	if host == "" {
		return "not supplied"
	}
	if link := discord.KitsuTaskURL(host, payload.Project.ID, payload.EntityType.Name, payload.Task.ID); link != "" {
		return link
	}
	return "not supplied"
}

func renderProductionDryRunPreview(payload kitsu.MessagePayload) string {
	status := strings.TrimSpace(payload.TaskStatus.ShortName)
	if status == "" {
		status = strings.TrimSpace(payload.TaskStatus.Name)
	}
	user := strings.TrimSpace(payload.LatestComment.Author.FullName)
	if user == "" && len(payload.Assignees) > 0 {
		user = strings.TrimSpace(payload.Assignees[0].FullName)
	}
	if user == "" {
		user = "not supplied"
	}
	return fmt.Sprintf("Production=%s; entity/task=%s; Task Type=%s; status=%s; user=%s; link=%s", strings.TrimSpace(payload.Project.Name), strings.TrimSpace(payload.Entity.Name), strings.TrimSpace(payload.TaskType.Name), status, user, productionTaskLink(payload))
}

func dryRunProductionNotification(db *gorm.DB, payload kitsu.MessagePayload, knownTaskTypeIDs map[string]struct{}) ProductionRoutingDryRun {
	plan := planProductionNotification(db, payload)
	result := ProductionRoutingDryRun{ProductionID: strings.TrimSpace(payload.Project.ID), TaskTypeID: strings.TrimSpace(payload.TaskType.ID), DestinationID: plan.DestinationID, RenderedPreview: renderProductionDryRunPreview(payload), User: strings.TrimSpace(payload.LatestComment.Author.FullName), Link: productionTaskLink(payload), SkipReason: plan.SkipReason}
	if plan.RuleID != 0 {
		result.MatchedRule = fmt.Sprintf("route-%d", plan.RuleID)
	}
	if len(knownTaskTypeIDs) > 0 {
		if _, ok := knownTaskTypeIDs[result.TaskTypeID]; !ok {
			result.StaleIDDiagnosis = "Task Type ID is not present in the current Kitsu metadata; configuration was not changed"
			if result.SkipReason == "" || plan.ShouldSend {
				result.SkipReason = "stale Task Type ID; notification was not dispatched"
			}
		}
	}
	return result
}

func recordProductionRoutingSkip(db *gorm.DB, payload kitsu.MessagePayload, reason string) {
	model.RecordNotificationRoutingDiagnosis(db, model.NotificationRoutingDiagnosis{TaskID: strings.TrimSpace(payload.Task.ID), ProductionID: strings.TrimSpace(payload.Project.ID), TaskTypeID: strings.TrimSpace(payload.TaskType.ID), Reason: "notification skipped", Detail: reason})
}

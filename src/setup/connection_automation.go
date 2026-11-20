package setup

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"app/src/model"
)

// Owner decision (zero-touch): the settings pages only confirm and adjust.
// Everything a new Production needs is created by the connection itself, from
// one place — the save that turns "not connected" into "connected".

// forumAutoCreateCheckKey is the setting that holds the operator-facing reason
// the automatic forum creation of a Production failed. It is the Production's
// needs-check mark: set on failure, cleared by the next successful creation
// (including the repair button).
func forumAutoCreateCheckKey(productionID string) string {
	return "forum_autocreate_check:" + productionID
}

// runNewProductionAutomation runs once, right after a Production is connected
// for the first time. Neither step may fail the connection: the Production is
// already saved, so a failure is logged and shown as needs-check instead.
func runNewProductionAutomation(db *gorm.DB, project model.Project, botToken string) {
	if err := seedHandoffTemplate(db, project.KitsuProjectID); err != nil {
		slog.Warn("handoff template seeding failed", "project_id", project.KitsuProjectID, "error", err)
	}
	autoCreateForums(db, project, botToken)
}

// autoCreateForums creates the forums and tags of a newly connected Production
// and records the outcome in the audit log. A failure keeps the connection and
// leaves a needs-check mark for the admin page; the "create forums" button is
// the repair path.
func autoCreateForums(db *gorm.DB, project model.Project, botToken string) {
	entry := model.AuditLog{
		TaskID:      "forum-autocreate",
		ProjectID:   project.KitsuProjectID,
		ProjectName: project.Name,
		GuildID:     project.DiscordGuildID,
		NewStatus:   "forums auto-created on connection",
		ActorKind:   model.AuditActorSystem,
		ActorName:   "connection-automation",
	}
	_, err := createForumsForTaskTypes(db, project.KitsuProjectID, botToken, project.DiscordGuildID, model.ProductionForumSettings{})
	if err != nil {
		entry.ErrorMessage = err.Error()
		model.SetSetting(db, forumAutoCreateCheckKey(project.KitsuProjectID), err.Error())
		slog.Warn("forum auto-create after connection failed", "project_id", project.KitsuProjectID, "error", err)
	}
	entry.Success = err == nil
	model.WriteAuditLog(db, entry)
}

// seedHandoffTemplate starts a new Production's handoff mapping from the
// standard template: the preset rows whose source Task Type exists in the
// Production, with their product types registered. A Production that already
// has handoff settings is left untouched.
func seedHandoffTemplate(db *gorm.DB, productionID string) error {
	if _, ok, err := model.LoadProductionHandoffSettings(db, productionID); err != nil || ok {
		return err
	}
	taskTypes := handoffTaskTypeLister(db, productionID)
	if len(taskTypes) == 0 {
		return errors.New("no Task Types to apply the handoff template to")
	}
	settings := model.ProductionHandoffSettings{Config: model.ProductionHandoffConfig{
		ProductionID: productionID,
		NotifyTiming: model.HandoffTimingMixed,
		NoAssignee:   model.HandoffFallbackAdmin,
	}}
	seenProducts := map[string]struct{}{}
	for _, preset := range handoffPresets {
		for _, taskType := range taskTypes {
			if !strings.EqualFold(strings.TrimSpace(taskType.Name), strings.TrimSpace(preset.TaskType)) {
				continue
			}
			route, ok := handoffPresetRoute(taskType, preset.Product, taskTypes)
			if !ok {
				continue
			}
			settings.Routes = append(settings.Routes, route)
			if key := strings.ToLower(preset.Product); !productSeen(seenProducts, key) {
				seenProducts[key] = struct{}{}
				settings.ProductTypes = append(settings.ProductTypes, model.ProductionHandoffProductType{Name: preset.Product})
			}
			break
		}
	}
	if len(settings.Routes) == 0 {
		return fmt.Errorf("none of the handoff template's Task Types exist in this Production")
	}
	return model.SaveProductionHandoffSettings(db, settings)
}

func productSeen(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

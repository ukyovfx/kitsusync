package setup

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"app/src/api/kitsu"
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

// seedHandoffTemplate starts a new Production's handoff handling from the
// standard template: the folding table (core v2 §3.6) becomes studio-common
// routes — shared by every Production, so it is only seeded while no studio
// routes exist at all — and the Production gets the admin no-assignee
// handling. A Production that already has handoff settings is left
// untouched.
func seedHandoffTemplate(db *gorm.DB, productionID string) error {
	if _, ok, err := model.LoadProductionHandoffSettings(db, productionID); err != nil || ok {
		return err
	}
	taskTypes := handoffTaskTypeLister(db, productionID)
	if len(taskTypes) == 0 {
		return errors.New("no Task Types to apply the handoff template to")
	}
	byName := map[string]kitsu.TaskType{}
	for _, taskType := range taskTypes {
		byName[strings.ToLower(strings.TrimSpace(taskType.Name))] = taskType
	}
	if existing, err := model.LoadStudioTaskRoutes(db); err != nil {
		return err
	} else if len(existing) == 0 {
		seeded := 0
		for _, template := range handoffTemplateRoutes {
			from, ok := byName[strings.ToLower(template.From)]
			if !ok {
				continue
			}
			var notifyIDs []string
			for _, name := range template.Notify {
				if target, ok := byName[strings.ToLower(name)]; ok {
					notifyIDs = append(notifyIDs, target.ID)
				}
			}
			if len(notifyIDs) == 0 {
				continue
			}
			if err := model.SaveStudioTaskRoute(db, model.StudioTaskRoute{
				FromTaskTypeID:    from.ID,
				FromTaskTypeName:  from.Name,
				NotifyTaskTypeIDs: model.JoinHandoffNotifyIDs(notifyIDs),
			}); err != nil {
				return err
			}
			seeded++
		}
		if seeded == 0 {
			return fmt.Errorf("none of the handoff template's Task Types exist in this Production")
		}
	}
	settings := model.ProductionHandoffSettings{Config: model.ProductionHandoffConfig{
		ProductionID: productionID,
		NoAssignee:   model.HandoffFallbackAdmin,
	}}
	return model.SaveProductionHandoffSettings(db, settings)
}

func productSeen(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

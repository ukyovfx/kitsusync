package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// ProductionRoutingHandler is local-only: it never calls a Discord API.
func ProductionRoutingHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lang := currentLang(r)
		message := ""
		if r.Method == http.MethodPost {
			if r.FormValue("action") == "dry_run" {
				message = dryRunProductionRoutingAction(db, r)
			} else {
				message = saveProductionRoutingAction(db, r)
			}
		}
		selectedID := strings.TrimSpace(r.URL.Query().Get("project"))
		if selectedID == "" {
			selectedID = strings.TrimSpace(r.FormValue("production_id"))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, adminPage(lang, "Production Notification Routing", r, renderProductionRouting(db, r, selectedID, message)))
	}
}

func routingTaskTypes() []kitsu.TaskType {
	// Unit tests and setup-required pages must remain local and fast when no
	// runtime Kitsu session exists. The authenticated runtime supplies the list.
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) == "" {
		return nil
	}
	return kitsu.GetTaskTypes().Each
}

func taskTypeName(taskTypeID string, taskTypes []kitsu.TaskType) string {
	for _, taskType := range taskTypes {
		if taskType.ID == taskTypeID {
			return strings.TrimSpace(taskType.Name)
		}
	}
	return ""
}

func dryRunProductionRoutingAction(db *gorm.DB, r *http.Request) string {
	productionID := strings.TrimSpace(r.FormValue("production_id"))
	taskTypeID := strings.TrimSpace(r.FormValue("dry_run_task_type_id"))
	project := model.FindProjectByKitsuID(db, productionID)
	if project == nil || taskTypeID == "" {
		return "Dry-run skipped: select a connected Production and Task Type."
	}
	taskTypes := routingTaskTypes()
	taskTypeLabel := taskTypeName(taskTypeID, taskTypes)
	if len(taskTypes) > 0 && taskTypeLabel == "" {
		return "Dry-run skipped: the selected Task Type is stale in current Kitsu metadata; configuration was not changed."
	}
	if taskTypeLabel == "" {
		taskTypeLabel = "unknown Task Type"
	}
	config := model.FindProductionNotificationConfig(db, productionID)
	result := fmt.Sprintf("Dry-run | Production ID: %s | Task Type ID: %s | Production: %s | Task Type: %s | matched rule: none | intended destination: none | preview: Production=%s; entity/task=not supplied; Task Type=%s; status=not supplied; user=not supplied; link=not supplied", productionID, taskTypeID, project.Name, taskTypeLabel, project.Name, taskTypeLabel)
	if config == nil {
		return result + " | skip reason: Production is unconfigured"
	}
	if !config.Enabled {
		return result + " | skip reason: Production routing is paused"
	}
	for _, route := range model.ListProductionNotificationRoutes(db, productionID) {
		if route.TaskTypeID != taskTypeID {
			continue
		}
		webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID)
		if webhook == nil || webhook.KitsuProjectID != productionID || strings.TrimSpace(webhook.WebhookURL) == "" || strings.TrimSpace(webhook.DiscordChannelID) == "" {
			return result + fmt.Sprintf(" | matched rule: route-%d | skip reason: destination is stale or invalid", route.ID)
		}
		return fmt.Sprintf("Dry-run | Production ID: %s | Task Type ID: %s | Production: %s | Task Type: %s | matched rule: route-%d | intended destination: configured channel | preview: Production=%s; entity/task=not supplied; Task Type=%s; status=not supplied; user=not supplied; link=not supplied | skip reason: none", productionID, taskTypeID, project.Name, taskTypeLabel, route.ID, project.Name, taskTypeLabel)
	}
	return result + " | skip reason: no Task Type ID route matched"
}

func saveProductionRoutingAction(db *gorm.DB, r *http.Request) string {
	productionID := strings.TrimSpace(r.FormValue("production_id"))
	project := model.FindProjectByKitsuID(db, productionID)
	if project == nil {
		return "The selected Production is not connected locally."
	}
	action := strings.TrimSpace(r.FormValue("action"))
	config := model.FindProductionNotificationConfig(db, productionID)
	if action == "pause" || action == "resume" {
		if config == nil {
			return "No saved routing configuration exists for this Production."
		}
		if action == "resume" {
			routes := model.ListProductionNotificationRoutes(db, productionID)
			if issues := model.ValidateProductionNotificationConfig(db, productionID, routes); len(issues) > 0 {
				return "Cannot resume: " + strings.Join(issues, "; ")
			}
		}
		config.Enabled = action == "resume"
		if err := db.Save(config).Error; err != nil {
			return "Could not update routing state."
		}
		if config.Enabled {
			return "Routing resumed."
		}
		return "Routing paused."
	}
	if action != "save" {
		return ""
	}
	_ = r.ParseForm()
	taskTypeIDs := r.Form["task_type_id"]
	destinationIDs := r.Form["destination_webhook_id"]
	taskTypeNames := r.Form["task_type_name"]
	knownTaskTypes := routingTaskTypes()
	knownTaskTypeIDs := make(map[string]struct{}, len(knownTaskTypes))
	for _, taskType := range knownTaskTypes {
		knownTaskTypeIDs[taskType.ID] = struct{}{}
	}
	routes := make([]model.ProductionNotificationRoute, 0, len(taskTypeIDs))
	for i, taskTypeID := range taskTypeIDs {
		taskTypeID = strings.TrimSpace(taskTypeID)
		if taskTypeID == "" && strings.TrimSpace(valueAt(destinationIDs, i)) == "" {
			continue
		}
		destinationID, _ := strconv.ParseUint(strings.TrimSpace(valueAt(destinationIDs, i)), 10, 64)
		displayName := strings.TrimSpace(valueAt(taskTypeNames, i))
		if displayName == "" {
			displayName = taskTypeName(taskTypeID, knownTaskTypes)
		}
		routes = append(routes, model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: taskTypeID, TaskTypeName: displayName, DestinationWebhookID: uint(destinationID)})
		if len(knownTaskTypes) > 0 {
			if _, ok := knownTaskTypeIDs[taskTypeID]; !ok {
				return "Configuration was not activated: selected Task Type is stale in current Kitsu metadata; configuration was not changed."
			}
		}
	}
	if issues := model.ValidateProductionNotificationConfig(db, productionID, routes); len(issues) > 0 {
		return "Configuration was not activated: " + strings.Join(issues, "; ")
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: productionID, ProductionName: project.Name, Enabled: true}, routes); err != nil {
		return "Could not save routing configuration."
	}
	return "Valid configuration saved and activated automatically."
}

func valueAt(values []string, index int) string {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return ""
}

func renderProductionRouting(db *gorm.DB, r *http.Request, selectedID, message string) string {
	projects := model.ListProjects(db)
	taskTypes := routingTaskTypes()
	var selected *model.Project
	for i := range projects {
		if projects[i].KitsuProjectID == selectedID {
			selected = &projects[i]
			break
		}
	}
	var b strings.Builder
	b.WriteString(`<div class="section-stack"><div class="section-card glass"><h2>Production Notification Routing</h2><p class="hint">Choose a Production, Kitsu Task Type, and configured destination. Unconfigured or paused Productions send nothing.</p>`)
	if message != "" {
		b.WriteString(`<div class="notice" role="status" aria-live="polite">` + esc(message) + `</div>`)
	}
	b.WriteString(`<form method="get"><label for="production-select">Production <select id="production-select" name="project" onchange="this.form.submit()"><option value="">Select a connected Production</option>`)
	for _, project := range projects {
		selectedAttr := ""
		if project.KitsuProjectID == selectedID {
			selectedAttr = " selected"
		}
		b.WriteString(`<option value="` + esc(project.KitsuProjectID) + `"` + selectedAttr + `>` + esc(project.Name) + `</option>`)
	}
	b.WriteString(`</select></label></form></div>`)
	if selected == nil {
		b.WriteString(`<div class="section-card glass"><p class="hint">Select a Production to configure routes and inspect non-secret diagnoses.</p></div></div>`)
		return b.String()
	}
	config := model.FindProductionNotificationConfig(db, selected.KitsuProjectID)
	state := "unconfigured"
	if config != nil {
		if config.Enabled {
			state = "enabled"
		} else {
			state = "paused"
		}
	}
	b.WriteString(`<div class="section-card glass"><h3>` + esc(selected.Name) + `</h3><p>Status: <strong class="status-text">` + esc(state) + `</strong></p><details><summary>Advanced identifiers</summary><p class="hint">Production ID: <code>` + esc(selected.KitsuProjectID) + `</code></p></details>`)
	b.WriteString(`<form method="post"><input type="hidden" name="production_id" value="` + esc(selected.KitsuProjectID) + `"><input type="hidden" name="action" value="save"><table><thead><tr><th>Task Type</th><th>Display name</th><th>Destination</th></tr></thead><tbody>`)
	for _, route := range model.ListProductionNotificationRoutes(db, selected.KitsuProjectID) {
		b.WriteString(routingRow(selected.KitsuProjectID, route, db, taskTypes))
	}
	b.WriteString(routingRow(selected.KitsuProjectID, model.ProductionNotificationRoute{}, db, taskTypes))
	b.WriteString(`</tbody></table><p class="hint">Task Type choices come from current Kitsu metadata. IDs are used internally and are not required for normal setup.</p><button class="btn" type="submit">Save and activate</button></form>`)
	b.WriteString(`<form method="post" style="margin-top:12px"><input type="hidden" name="production_id" value="` + esc(selected.KitsuProjectID) + `"><input type="hidden" name="action" value="dry_run"><label for="dry-run-task-type">Task Type for dry-run <select id="dry-run-task-type" name="dry_run_task_type_id"><option value="">Select a Task Type</option>`)
	for _, taskType := range taskTypes {
		b.WriteString(`<option value="` + esc(taskType.ID) + `">` + esc(taskType.Name) + `</option>`)
	}
	b.WriteString(`</select></label><button class="btn secondary" type="submit">Inspect dry-run</button></form>`)
	if config != nil {
		action, label := "pause", "Pause routing"
		if !config.Enabled {
			action, label = "resume", "Resume routing"
		}
		b.WriteString(`<form method="post" style="margin-top:12px"><input type="hidden" name="production_id" value="` + esc(selected.KitsuProjectID) + `"><input type="hidden" name="action" value="` + action + `"><button class="btn secondary" type="submit">` + label + `</button></form>`)
	}
	b.WriteString(`</div><div class="section-card glass"><h3>Recent routing diagnoses</h3>`)
	diagnoses := model.ListNotificationRoutingDiagnoses(db, selected.KitsuProjectID, 20)
	if len(diagnoses) == 0 {
		b.WriteString(`<p class="hint">No routing skips recorded.</p>`)
	} else {
		for _, diagnosis := range diagnoses {
			b.WriteString(`<p><code>` + esc(diagnosis.TaskTypeID) + `</code>: ` + esc(diagnosis.Detail) + `</p>`)
		}
	}
	b.WriteString(`</div></div>`)
	return b.String()
}

func routingRow(projectID string, route model.ProductionNotificationRoute, db *gorm.DB, taskTypes []kitsu.TaskType) string {
	var b strings.Builder
	fieldID := "task-type-new"
	if route.ID != 0 {
		fieldID = "task-type-" + strconv.FormatUint(uint64(route.ID), 10)
	}
	b.WriteString(`<tr><td><label class="sr-only" for="` + fieldID + `">Task Type</label><select id="` + fieldID + `" name="task_type_id"><option value="">Select a Task Type</option>`)
	found := false
	for _, taskType := range taskTypes {
		selected := ""
		if taskType.ID == route.TaskTypeID {
			selected, found = " selected", true
		}
		b.WriteString(`<option value="` + esc(taskType.ID) + `"` + selected + `>` + esc(taskType.Name) + `</option>`)
	}
	if route.TaskTypeID != "" && !found {
		b.WriteString(`<option value="` + esc(route.TaskTypeID) + `" selected>Stale Task Type reference</option>`)
	}
	b.WriteString(`</select><input type="hidden" name="task_type_name" value="` + esc(route.TaskTypeName) + `"></td><td>` + esc(route.TaskTypeName) + `</td><td><label class="sr-only">Destination</label><select name="destination_webhook_id"><option value="">Select destination</option>`)
	for _, webhook := range model.ListProjectWebhooks(db, projectID) {
		selected := ""
		if webhook.ID == route.DestinationWebhookID {
			selected = " selected"
		}
		label := webhook.ChannelName
		if label == "" {
			label = "Configured destination"
		}
		b.WriteString(`<option value="` + strconv.FormatUint(uint64(webhook.ID), 10) + `"` + selected + `>` + esc(label) + `</option>`)
	}
	b.WriteString(`</select></td></tr>`)
	return b.String()
}

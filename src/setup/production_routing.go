package setup

import (
	"app/src/model"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// ProductionRoutingHandler is deliberately local-only: it persists routing
// metadata and renders dry-run information, but never calls a Discord API.
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

func dryRunProductionRoutingAction(db *gorm.DB, r *http.Request) string {
	productionID := strings.TrimSpace(r.FormValue("production_id"))
	taskTypeID := strings.TrimSpace(r.FormValue("dry_run_task_type_id"))
	project := model.FindProjectByKitsuID(db, productionID)
	if project == nil || taskTypeID == "" {
		return "Dry-run skipped: Production ID and Task Type ID are required."
	}
	config := model.FindProductionNotificationConfig(db, productionID)
	result := "Dry-run | Production ID: " + productionID + " | Task Type ID: " + taskTypeID + " | matched rule: none | intended destination: none | preview: [dry-run] Task Type " + taskTypeID
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
		return fmt.Sprintf("Dry-run | Production ID: %s | Task Type ID: %s | matched rule: route-%d | intended destination: %s | preview: [dry-run] Task Type %s | skip reason: none", productionID, taskTypeID, route.ID, webhook.DiscordChannelID, taskTypeID)
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
	routes := make([]model.ProductionNotificationRoute, 0, len(taskTypeIDs))
	for i, taskTypeID := range taskTypeIDs {
		destinationID, _ := strconv.ParseUint(strings.TrimSpace(valueAt(destinationIDs, i)), 10, 64)
		routes = append(routes, model.ProductionNotificationRoute{
			ProductionID:         productionID,
			TaskTypeID:           strings.TrimSpace(taskTypeID),
			TaskTypeName:         strings.TrimSpace(valueAt(taskTypeNames, i)),
			DestinationWebhookID: uint(destinationID),
		})
	}
	if issues := model.ValidateProductionNotificationConfig(db, productionID, routes); len(issues) > 0 {
		return "Configuration was not activated: " + strings.Join(issues, "; ")
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{
		ProductionID:   productionID,
		ProductionName: project.Name,
		Enabled:        true,
	}, routes); err != nil {
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
	var selected *model.Project
	for i := range projects {
		if projects[i].KitsuProjectID == selectedID {
			selected = &projects[i]
			break
		}
	}
	var b strings.Builder
	b.WriteString(`<div class="section-stack"><div class="section-card glass"><h2>Production Notification Routing</h2><p class="hint">Routes use stable Kitsu Production IDs and Task Type IDs. Names are display metadata only. An unconfigured or paused Production sends nothing.</p>`)
	if message != "" {
		b.WriteString(`<div class="notice">` + esc(message) + `</div>`)
	}
	b.WriteString(`<form method="get"><label>Production <select name="project" onchange="this.form.submit()"><option value="">Select a connected Production</option>`)
	for _, project := range projects {
		selectedAttr := ""
		if project.KitsuProjectID == selectedID {
			selectedAttr = " selected"
		}
		b.WriteString(`<option value="` + esc(project.KitsuProjectID) + `"` + selectedAttr + `>` + esc(project.Name) + ` (` + esc(project.KitsuProjectID) + `)</option>`)
	}
	b.WriteString(`</select></label></form></div>`)
	if selected == nil {
		b.WriteString(`<div class="section-card glass"><p class="hint">Select a Production to configure routes and inspect non-secret routing diagnoses.</p></div></div>`)
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
	b.WriteString(`<div class="section-card glass"><h3>` + esc(selected.Name) + `</h3><p>Status: <strong>` + esc(state) + `</strong> · Production ID: <code>` + esc(selected.KitsuProjectID) + `</code></p>`)
	b.WriteString(`<form method="post"><input type="hidden" name="production_id" value="` + esc(selected.KitsuProjectID) + `"><input type="hidden" name="action" value="save"><table><thead><tr><th>Task Type ID</th><th>Display name</th><th>Destination</th></tr></thead><tbody>`)
	routes := model.ListProductionNotificationRoutes(db, selected.KitsuProjectID)
	for _, route := range routes {
		b.WriteString(routingRow(selected.KitsuProjectID, route, db))
	}
	b.WriteString(routingRow(selected.KitsuProjectID, model.ProductionNotificationRoute{}, db))
	b.WriteString(`</tbody></table><p class="hint">Leave the blank row empty when no additional route is needed. Destination URLs are never rendered.</p><button class="btn" type="submit">Save and activate</button></form>`)
	b.WriteString(`<form method="post" style="margin-top:12px"><input type="hidden" name="production_id" value="` + esc(selected.KitsuProjectID) + `"><input type="hidden" name="action" value="dry_run"><label>Dry-run Task Type ID <input name="dry_run_task_type_id" placeholder="Kitsu Task Type ID"></label><button class="btn secondary" type="submit">Inspect dry-run</button></form>`)
	if config != nil {
		action := "pause"
		label := "Pause routing"
		if !config.Enabled {
			action = "resume"
			label = "Resume routing"
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

func routingRow(projectID string, route model.ProductionNotificationRoute, db *gorm.DB) string {
	var b strings.Builder
	b.WriteString(`<tr><td><input name="task_type_id" value="` + esc(route.TaskTypeID) + `" placeholder="Kitsu Task Type ID"></td><td><input name="task_type_name" value="` + esc(route.TaskTypeName) + `" placeholder="display only"></td><td><select name="destination_webhook_id"><option value="">Select destination</option>`)
	for _, webhook := range model.ListProjectWebhooks(db, projectID) {
		selected := ""
		if webhook.ID == route.DestinationWebhookID {
			selected = " selected"
		}
		label := webhook.ChannelName
		if label == "" {
			label = webhook.DiscordChannelID
		}
		b.WriteString(`<option value="` + strconv.FormatUint(uint64(webhook.ID), 10) + `"` + selected + `>` + esc(label) + ` (channel ` + esc(webhook.DiscordChannelID) + `)</option>`)
	}
	b.WriteString(`</select></td></tr>`)
	return b.String()
}

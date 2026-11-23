package setup

import (
	"app/src/model"
	"net/http"
	"strings"

	"gorm.io/gorm"
)

// The Kitsu status -> forum role mapping is a Bot-wide setting: it moved here
// from the per-Production forum settings page (redesign front half, 2026-11-23)
// because operators think of it as part of the shared Bot configuration. The
// data itself stays in production_forum_status_roles; this section edits the
// saved rows of every Production in one form.

func renderStatusRolesSection(db *gorm.DB, lang string, r *http.Request) string {
	projects := availableProjects(db)
	statuses, statusErr := forumStatusLister(db)
	roleOptions := make([][2]string, 0, len(forumRoleOptions))
	for _, role := range forumRoleOptions {
		roleOptions = append(roleOptions, [2]string{role, forumRoleLabel(lang, role)})
	}
	var blocks strings.Builder
	for _, project := range projects {
		productionID := project.KitsuProjectID
		current, ok, err := model.LoadProductionForumSettings(db, productionID)
		if err != nil || !ok {
			continue
		}
		roleByStatus := map[string]string{}
		for _, row := range current.StatusRoles {
			roleByStatus[row.StatusID] = row.Role
		}
		if len(current.StatusRoles) == 0 && statusErr == nil {
			for _, row := range forumTemplateStatusRolesFrom(statuses) {
				roleByStatus[row.StatusID] = row.Role
			}
		}
		type statusRow struct{ id, name string }
		var statusList []statusRow
		seen := map[string]struct{}{}
		if statusErr == nil {
			for _, status := range statuses {
				name := strings.TrimSpace(status.Name)
				if name == "" {
					name = status.ShortName
				}
				statusList = append(statusList, statusRow{status.ID, name})
				seen[status.ID] = struct{}{}
			}
		}
		for _, saved := range current.StatusRoles {
			if _, ok := seen[saved.StatusID]; !ok {
				name := saved.StatusName
				if name == "" {
					name = saved.StatusID
				}
				statusList = append(statusList, statusRow{saved.StatusID, name})
				seen[saved.StatusID] = struct{}{}
			}
		}
		if len(statusList) == 0 {
			continue
		}
		blocks.WriteString(`<h3>` + esc(project.Name) + `</h3><table class="data-table forum-map-table"><tbody>`)
		for _, status := range statusList {
			blocks.WriteString(`<tr><td>` + esc(status.name) +
				`<input type="hidden" name="srole_project" value="` + esc(productionID) + `">` +
				`<input type="hidden" name="srole_id" value="` + esc(status.id) + `">` +
				`<input type="hidden" name="srole_name" value="` + esc(status.name) + `"></td><td>` +
				forumSelect("srole_role", roleByStatus[status.id], roleOptions, t(lang, "（使わない）", "(unused)")) + `</td></tr>`)
		}
		blocks.WriteString(`</tbody></table>`)
	}
	if blocks.Len() == 0 {
		return ""
	}
	note := ""
	if statusErr != nil {
		note = `<p class="field-help" role="status">` + esc(t(lang, "Kitsuのステータス一覧を取得できませんでした。保存済みのステータス設定だけを表示しています。", "Could not read the Kitsu status list. Only saved status settings are shown.")) + `</p>`
	}
	return `<form method="post" action="` + esc(withLang("/bot/admin/bot?edit=1", r)) + `" class="section-stack">` +
		`<input type="hidden" name="action" value="save_status_roles">` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "Kitsuのステータス → 役割", "Kitsu status → role")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "ステータス名から既定の役割を自動で割り当てています。確認待ち・リテイク・完了にそれぞれ1つ以上のステータスが必要です。", "Default roles are pre-assigned from the status names. Waiting, Retake and Done each need at least one status.")) + `</p>` +
		note + blocks.String() +
		`<div class="button-row"><button type="submit" class="btn">` + esc(t(lang, "保存", "Save")) + `</button></div></section></form>`
}

// saveStatusRoles applies one bot-page submit to every Production it carries
// rows for. Each Production's forum settings are loaded, only their
// StatusRoles are replaced, and the bundle is saved back through the same
// validation the forum page uses.
func saveStatusRoles(db *gorm.DB, r *http.Request) error {
	_ = r.ParseForm()
	projects, ids, names, roles := r.Form["srole_project"], r.Form["srole_id"], r.Form["srole_name"], r.Form["srole_role"]
	byProject := map[string][]model.ProductionForumStatusRole{}
	for i := range ids {
		if i >= len(projects) || i >= len(roles) {
			continue
		}
		productionID := strings.TrimSpace(projects[i])
		role := strings.TrimSpace(roles[i])
		if productionID == "" || role == "" {
			continue
		}
		row := model.ProductionForumStatusRole{StatusID: strings.TrimSpace(ids[i]), Role: role}
		if i < len(names) {
			row.StatusName = strings.TrimSpace(names[i])
		}
		byProject[productionID] = append(byProject[productionID], row)
	}
	for productionID, rows := range byProject {
		settings, ok, err := model.LoadProductionForumSettings(db, productionID)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		settings.StatusRoles = rows
		if err := model.SaveProductionForumSettings(db, settings); err != nil {
			return err
		}
	}
	return nil
}

package setup

import (
	"app/src/model"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// The Kitsu status -> forum role mapping section lives on the connection
// settings page (redesign front half, 2026-11-23) because operators think
// of it as part of the shared Bot configuration. The data it edits is NOT
// studio-wide, though: the rows stay in production_forum_status_roles, one
// set per Production, and this section edits every Production's set in one
// form (the page says so, and a selector jumps to one Production's block).
// The studio-wide layer is the separate nature classification
// (studio_status_policies, keyed by Kitsu status ID alone), which the
// exception queue on the system status page maintains.

// statusRoleBlock is one Production's status -> role assignment, shared by
// the editable section and the read-only display on the connection page.
type statusRoleBlock struct {
	productionID string
	name         string
	statuses     []statusRoleEntry
	roleByStatus map[string]string
}

type statusRoleEntry struct{ id, name string }

func statusRoleBlocks(db *gorm.DB) ([]statusRoleBlock, error) {
	projects := availableProjects(db)
	statuses, statusErr := forumStatusLister(db)
	var out []statusRoleBlock
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
		var statusList []statusRoleEntry
		seen := map[string]struct{}{}
		if statusErr == nil {
			for _, status := range statuses {
				name := strings.TrimSpace(status.Name)
				if name == "" {
					name = status.ShortName
				}
				statusList = append(statusList, statusRoleEntry{status.ID, name})
				seen[status.ID] = struct{}{}
			}
		}
		for _, saved := range current.StatusRoles {
			if _, ok := seen[saved.StatusID]; !ok {
				name := saved.StatusName
				if name == "" {
					name = saved.StatusID
				}
				statusList = append(statusList, statusRoleEntry{saved.StatusID, name})
				seen[saved.StatusID] = struct{}{}
			}
		}
		if len(statusList) == 0 {
			continue
		}
		out = append(out, statusRoleBlock{productionID: productionID, name: project.Name, statuses: statusList, roleByStatus: roleByStatus})
	}
	return out, statusErr
}

// speakingRoles are the three roles that produce notifications, in display
// order, with the dot color the mocks assign each role.
var speakingRoles = []struct {
	role string
	dot  string
}{
	{model.ForumRoleWFA, "#D4A72C"},
	{model.ForumRoleRetake, "#C45656"},
	{model.ForumRoleDone, "#4FAF78"},
}

func roleCountsOf(block statusRoleBlock) map[string]int {
	counts := map[string]int{}
	for _, status := range block.statuses {
		counts[block.roleByStatus[status.id]]++
	}
	return counts
}

// roleSummaryLine renders the per-Production completeness line: the counts
// per speaking role, and whether the set is complete. A speaking role with
// no status assigned can never fire, so a missing role is called out
// instead of failing silently.
func roleSummaryLine(lang string, block statusRoleBlock) string {
	counts := roleCountsOf(block)
	complete := true
	var line strings.Builder
	for i, speaking := range speakingRoles {
		if i > 0 {
			line.WriteString(` ・ `)
		}
		label := forumRoleLabel(lang, speaking.role)
		// The mock's summary uses the short role names （確認待ち/リテイク/完了）.
		switch speaking.role {
		case model.ForumRoleWFA:
			label = t(lang, "確認待ち", "Waiting")
		case model.ForumRoleRetake:
			label = t(lang, "リテイク", "Retake")
		case model.ForumRoleDone:
			label = t(lang, "完了", "Done")
		}
		line.WriteString(esc(label) + ` ` + esc(intLabel(counts[speaking.role], lang)))
		if counts[speaking.role] == 0 {
			complete = false
		}
	}
	if complete {
		line.WriteString(` — ` + esc(t(lang, "3つの役割が揃っています", "all three roles are assigned")))
	} else {
		line.WriteString(` — ` + esc(t(lang, "未設定の役割は通知されません", "a role with no status is never notified")))
	}
	return line.String()
}

func intLabel(n int, lang string) string {
	return strings.TrimSpace(strconv.Itoa(n) + t(lang, "件", ""))
}

// renderRoleBoxes renders the three speaking-role boxes plus the
// unassigned tray for one Production. In edit mode each status chip carries
// its role select (the stored form contract is unchanged); in display mode
// the chips are static.
func renderRoleBoxes(lang string, block statusRoleBlock, roleOptions [][2]string, edit bool) string {
	byRole := map[string][]statusRoleEntry{}
	var tray []statusRoleEntry
	for _, status := range block.statuses {
		role := block.roleByStatus[status.id]
		switch role {
		case model.ForumRoleWFA, model.ForumRoleRetake, model.ForumRoleDone:
			byRole[role] = append(byRole[role], status)
		default:
			tray = append(tray, status)
		}
	}
	chip := func(status statusRoleEntry) string {
		if !edit {
			return `<span class="role-chip role-chip-static">` + esc(status.name) + `</span>`
		}
		return `<span class="role-chip" data-role-chip>` + esc(status.name) +
			`<input type="hidden" name="srole_project" value="` + esc(block.productionID) + `">` +
			`<input type="hidden" name="srole_id" value="` + esc(status.id) + `">` +
			`<input type="hidden" name="srole_name" value="` + esc(status.name) + `">` +
			forumSelect("srole_role", block.roleByStatus[status.id], roleOptions, t(lang, "未割り当て", "Unassigned")) + `</span>`
	}
	var b strings.Builder
	b.WriteString(`<div class="role-boxes-group" data-role-boxes-group data-count-suffix="` + esc(t(lang, "件", "")) + `"><div class="role-boxes">`)
	for _, speaking := range speakingRoles {
		label := forumRoleLabel(lang, speaking.role)
		switch speaking.role {
		case model.ForumRoleWFA:
			label = t(lang, "確認待ち", "Waiting")
		case model.ForumRoleRetake:
			label = t(lang, "リテイク", "Retake")
		case model.ForumRoleDone:
			label = t(lang, "完了", "Done")
		}
		b.WriteString(`<div class="role-box" data-role-box="` + esc(speaking.role) + `"><div class="role-box-head"><span class="dot" style="background:` + speaking.dot + `"></span>` + esc(label) + `<span class="cnt" data-role-count>` + esc(intLabel(len(byRole[speaking.role]), lang)) + `</span></div>`)
		for _, status := range byRole[speaking.role] {
			b.WriteString(chip(status))
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div><div class="role-tray" data-role-tray><div class="role-tray-head">` + esc(t(lang, "未割り当て", "Unassigned")) + `</div>`)
	for _, status := range tray {
		b.WriteString(chip(status))
	}
	b.WriteString(`</div></div>`)
	return b.String()
}

const statusRolesEditorScript = `<script data-status-roles-editor>(function(){document.querySelectorAll("[data-role-boxes-group]").forEach(function(group){var suffix=group.getAttribute("data-count-suffix")||"";var tray=group.querySelector("[data-role-tray]");function updateCounts(){group.querySelectorAll("[data-role-box]").forEach(function(box){var cnt=box.querySelector("[data-role-count]");if(cnt){cnt.textContent=String(box.querySelectorAll("[data-role-chip]").length)+suffix}})}group.querySelectorAll("select[name=\"srole_role\"]").forEach(function(sel){sel.addEventListener("change",function(){var chip=sel.closest("[data-role-chip]");if(!chip)return;var target=group.querySelector("[data-role-box=\""+sel.value+"\"]")||tray;if(target){target.appendChild(chip);updateCounts()}})})})})();</script>`

func renderStatusRolesSection(db *gorm.DB, lang string, r *http.Request) string {
	blocks, statusErr := statusRoleBlocks(db)
	if len(blocks) == 0 {
		return ""
	}
	roleOptions := make([][2]string, 0, len(forumRoleOptions))
	for _, role := range forumRoleOptions {
		roleOptions = append(roleOptions, [2]string{role, forumRoleLabel(lang, role)})
	}
	var body strings.Builder
	var selectorLinks strings.Builder
	for _, block := range blocks {
		selectorLinks.WriteString(`<a class="nav-chip" href="#srole-` + esc(block.productionID) + `">` + esc(block.name) + `</a>`)
		body.WriteString(`<h3 id="srole-` + esc(block.productionID) + `">` + esc(block.name) + `</h3>`)
		body.WriteString(`<p class="role-summary-line">` + roleSummaryLine(lang, block) + `</p>`)
		body.WriteString(renderRoleBoxes(lang, block, roleOptions, true))
	}
	note := ""
	if statusErr != nil {
		note = `<p class="field-help" role="status">` + esc(t(lang, "Kitsuのステータス一覧を取得できませんでした。保存済みのステータス設定だけを表示しています。", "Could not read the Kitsu status list. Only saved status settings are shown.")) + `</p>`
	}
	selector := ""
	if selectorLinks.Len() > 0 {
		selector = `<div class="audit-filter" role="navigation" aria-label="` + esc(t(lang, "プロダクションを選ぶ", "Choose a Production")) + `"><span class="field-help">` + esc(t(lang, "プロダクションを選ぶ", "Choose a Production")) + `</span>` + selectorLinks.String() + `</div>`
	}
	return `<form method="post" action="` + esc(withLang("/bot/admin/bot/statuses", r)) + `" class="section-stack">` +
		`<input type="hidden" name="action" value="save_status_roles">` +
		`<section class="flat-section" id="status-roles"><div class="flat-section-head"><h2>` + esc(t(lang, "タスクステータス", "Task statuses")) + `</h2></div>` +
		note + selector + body.String() +
		`<div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(t(lang, "接続設定トップへ戻る", "Back to Connections hub")) + `</a><button type="submit" class="btn">` + esc(t(lang, "保存", "Save")) + `</button></div></section></form>` + statusRolesEditorScript
}

// renderStatusRolesDisplay is the read-only form of the same assignment on
// the connection page's normal mode: the boxes are visible without entering
// edit mode, with an edit link per the approved mock.
func renderStatusRolesDisplay(db *gorm.DB, lang string, r *http.Request) string {
	blocks, _ := statusRoleBlocks(db)
	if len(blocks) == 0 {
		return ""
	}
	return `<section class="flat-section" aria-labelledby="status-roles-display-title"><div class="flat-section-head"><h2 id="status-roles-display-title">` + esc(t(lang, "タスクステータス", "Task statuses")) + `</h2><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot/statuses", r)) + `">` + esc(t(lang, "編集", "Edit")) + `</a></div>` + renderRoleBoxes(lang, blocks[0], nil, false) + `</section>`
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

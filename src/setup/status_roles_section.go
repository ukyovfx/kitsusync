package setup

import (
	"app/src/model"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// The Kitsu status -> forum role mapping page (/bot/admin/bot/statuses) edits
// ONE studio-wide assignment stored in studio_status_policies (keyed by Kitsu
// status ID). Manual assignments win over the name dictionary; dictionary-
// derived placements are displayed for statuses without a saved policy yet.

// statusRoleBlock is the studio-wide status -> role assignment.
type statusRoleBlock struct {
	statuses     []statusRoleEntry
	roleByStatus map[string]string
}

type statusRoleEntry struct{ id, name string }

func studioStatusRoleBlock(db *gorm.DB) (statusRoleBlock, error) {
	statuses, statusErr := forumStatusLister(db)
	policies, err := model.LoadStudioStatusPolicies(db)
	if err != nil {
		policies = map[string]model.StudioStatusPolicy{}
	}
	roleByStatus := map[string]string{}
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
			if policy, ok := policies[status.ID]; ok {
				roleByStatus[status.ID] = model.StatusNatureRole(policy.Nature)
			} else {
				roleByStatus[status.ID] = model.StatusNatureRole(model.ClassifyStatusName(name))
			}
		}
	}
	var savedExtras []model.StudioStatusPolicy
	for _, policy := range policies {
		if _, ok := seen[policy.StatusID]; !ok {
			savedExtras = append(savedExtras, policy)
		}
	}
	sort.Slice(savedExtras, func(i, j int) bool {
		if savedExtras[i].StatusName != savedExtras[j].StatusName {
			return savedExtras[i].StatusName < savedExtras[j].StatusName
		}
		return savedExtras[i].StatusID < savedExtras[j].StatusID
	})
	for _, policy := range savedExtras {
		name := strings.TrimSpace(policy.StatusName)
		if name == "" {
			name = policy.StatusID
		}
		statusList = append(statusList, statusRoleEntry{policy.StatusID, name})
		roleByStatus[policy.StatusID] = model.StatusNatureRole(policy.Nature)
	}
	return statusRoleBlock{statuses: statusList, roleByStatus: roleByStatus}, statusErr
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

func intLabel(n int, lang string) string {
	return strings.TrimSpace(strconv.Itoa(n) + t(lang, "件", ""))
}

// renderRoleBoxes renders the three speaking-role boxes plus the
// unassigned tray for one Production. In edit mode each box carries its own
// add control (a select of unassigned statuses + add button) and each
// assigned chip has a remove button; the hidden srole_* inputs remain the
// single source of truth so the stored form contract is unchanged. In display
// mode the chips are static.
func renderRoleBoxes(lang string, block statusRoleBlock, edit bool) string {
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
	var b strings.Builder
	b.WriteString(`<div class="role-boxes-group" data-role-boxes-group data-count-suffix="` + esc(t(lang, "件", "")) + `">`)
	if edit {
		b.WriteString(`<div class="role-hidden-inputs" style="display:none">`)
		for _, status := range block.statuses {
			b.WriteString(`<span data-role-input="` + esc(status.id) + `">` +
				`<input type="hidden" name="srole_id" value="` + esc(status.id) + `">` +
				`<input type="hidden" name="srole_name" value="` + esc(status.name) + `">` +
				`<input type="hidden" name="srole_role" value="` + esc(block.roleByStatus[status.id]) + `">` +
				`</span>`)
		}
		b.WriteString(`</div>`)
	}
	var unassignedOptions strings.Builder
	if edit {
		unassignedOptions.WriteString(`<option value="">` + esc(t(lang, "ステータスを選ぶ", "Choose a status")) + `</option>`)
		for _, status := range tray {
			unassignedOptions.WriteString(`<option value="` + esc(status.id) + `">` + esc(status.name) + `</option>`)
		}
	}
	b.WriteString(`<div class="role-boxes">`)
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
		if edit {
			b.WriteString(`<div class="role-box-chips" data-role-chips>`)
			for _, status := range byRole[speaking.role] {
				b.WriteString(`<span class="role-chip" data-role-chip="` + esc(status.id) + `">` + esc(status.name) + `<button type="button" data-role-remove aria-label="` + esc(status.name) + `">×</button></span>`)
			}
			b.WriteString(`</div>`)
			b.WriteString(`<div class="role-box-add"><select data-role-add-select aria-label="` + esc(label+": "+t(lang, "ステータスを選ぶ", "Choose a status")) + `">` + unassignedOptions.String() + `</select><button type="button" class="btn sm" data-role-add>` + esc(t(lang, "追加", "Add")) + `</button></div>`)
		} else {
			for _, status := range byRole[speaking.role] {
				b.WriteString(`<span class="role-chip role-chip-static">` + esc(status.name) + `</span>`)
			}
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div><div class="role-tray" data-role-tray><div class="role-tray-head">` + esc(t(lang, "未割り当て", "Unassigned")) + `</div>`)
	if edit {
		b.WriteString(`<div class="role-tray-list" data-role-tray-list>`)
		for _, status := range tray {
			b.WriteString(`<span class="role-chip role-chip-static" data-role-tray-item="` + esc(status.id) + `">` + esc(status.name) + `</span>`)
		}
		b.WriteString(`</div>`)
	} else {
		for _, status := range tray {
			b.WriteString(`<span class="role-chip role-chip-static">` + esc(status.name) + `</span>`)
		}
	}
	b.WriteString(`</div></div>`)
	return b.String()
}

const statusRolesEditorScript = `<script data-status-roles-editor>(function(){document.querySelectorAll("[data-role-boxes-group]").forEach(function(group){var suffix=group.getAttribute("data-count-suffix")||"";var trayList=group.querySelector("[data-role-tray-list]");var inputs=Array.prototype.slice.call(group.querySelectorAll("[data-role-input]"));function rebuild(){var byRole={wfa:[],retake:[],done:[]};var unassigned=[];inputs.forEach(function(item){var id=item.getAttribute("data-role-input")||"";var nameInput=item.querySelector("input[name=\"srole_name\"]");var roleInput=item.querySelector("input[name=\"srole_role\"]");if(!roleInput)return;var entry={id:id,name:nameInput?nameInput.value:id,roleInput:roleInput};if(roleInput.value==="wfa"||roleInput.value==="retake"||roleInput.value==="done"){byRole[roleInput.value].push(entry)}else{unassigned.push(entry)}});group.querySelectorAll("[data-role-box]").forEach(function(box){var role=box.getAttribute("data-role-box")||"";var list=byRole[role]||[];var cnt=box.querySelector("[data-role-count]");if(cnt){cnt.textContent=String(list.length)+suffix}var chips=box.querySelector("[data-role-chips]");if(chips){chips.innerHTML="";list.forEach(function(entry){var chip=document.createElement("span");chip.className="role-chip";chip.setAttribute("data-role-chip",entry.id);chip.textContent=entry.name;var remove=document.createElement("button");remove.type="button";remove.setAttribute("data-role-remove","");remove.setAttribute("aria-label",entry.name);remove.textContent="×";remove.addEventListener("click",function(){entry.roleInput.value="";rebuild()});chip.appendChild(remove);chips.appendChild(chip)})}var select=box.querySelector("[data-role-add-select]");if(select){var first=select.querySelector("option[value=\"\"]");select.innerHTML="";if(first)select.appendChild(first);unassigned.forEach(function(entry){var option=document.createElement("option");option.value=entry.id;option.textContent=entry.name;select.appendChild(option)});select.value=""}});if(trayList){trayList.innerHTML="";unassigned.forEach(function(entry){var item=document.createElement("span");item.className="role-chip role-chip-static";item.setAttribute("data-role-tray-item",entry.id);item.textContent=entry.name;trayList.appendChild(item)})}}group.querySelectorAll("[data-role-box]").forEach(function(box){var role=box.getAttribute("data-role-box")||"";var select=box.querySelector("[data-role-add-select]");var add=box.querySelector("[data-role-add]");if(add&&select){add.addEventListener("click",function(){var id=select.value;if(!id)return;inputs.forEach(function(item){if(item.getAttribute("data-role-input")===id){var roleInput=item.querySelector("input[name=\"srole_role\"]");if(roleInput)roleInput.value=role}});rebuild()})}});rebuild()})})();</script>`

func renderStatusRolesSection(db *gorm.DB, lang string, r *http.Request) string {
	block, statusErr := studioStatusRoleBlock(db)
	if len(block.statuses) == 0 {
		if statusErr != nil {
			return `<section class="section-card glass"><p class="state-explanation" role="alert">` + esc(t(lang, "Kitsuからステータス一覧を取得できませんでした。Kitsu接続を確認して再試行してください。", "Could not load task statuses from Kitsu. Check the Kitsu connection and try again.")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(t(lang, "接続設定へ戻る", "Back to Connections")) + `</a></div></section>`
		}
		return ""
	}
	note := ""
	if statusErr != nil {
		note = `<p class="field-help" role="status">` + esc(t(lang, "Kitsuのステータス一覧を取得できませんでした。保存済みのステータス設定だけを表示しています。", "Could not read the Kitsu status list. Only saved status settings are shown.")) + `</p>`
	}
	return `<form method="post" action="` + esc(withLang("/bot/admin/bot/statuses", r)) + `" class="section-stack">` +
		`<input type="hidden" name="action" value="save_status_roles">` +
		`<section class="flat-section" id="status-roles">` +
		note + renderRoleBoxes(lang, block, true) +
		`<div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(t(lang, "接続設定へ戻る", "Back to Connections")) + `</a><button type="submit" class="btn">` + esc(t(lang, "保存", "Save")) + `</button></div></section></form>` + statusRolesEditorScript
}

// renderStatusRolesDisplay is the read-only form of the studio-wide assignment.
func renderStatusRolesDisplay(db *gorm.DB, lang string, r *http.Request) string {
	block, _ := studioStatusRoleBlock(db)
	if len(block.statuses) == 0 {
		return ""
	}
	return `<section class="flat-section" aria-labelledby="status-roles-display-title"><div class="flat-section-head"><h2 id="status-roles-display-title">` + esc(t(lang, "タスクステータス", "Task statuses")) + `</h2><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot/statuses", r)) + `">` + esc(t(lang, "編集", "Edit")) + `</a></div>` + renderRoleBoxes(lang, block, false) + `</section>`
}

func natureForForumRole(role string) (string, error) {
	switch role {
	case model.ForumRoleWFA:
		return model.StatusNatureReview, nil
	case model.ForumRoleRetake:
		return model.StatusNatureRetake, nil
	case model.ForumRoleDone:
		return model.StatusNatureDone, nil
	case model.ForumRoleWIP:
		return model.StatusNatureWIP, nil
	case "":
		return model.StatusNatureNone, nil
	default:
		return "", fmt.Errorf("unknown status role %q", role)
	}
}

// saveStatusRoles stores the studio-wide status -> role assignment in
// studio_status_policies as manual overrides.
func saveStatusRoles(db *gorm.DB, r *http.Request) error {
	_ = r.ParseForm()
	ids, names, roles := r.Form["srole_id"], r.Form["srole_name"], r.Form["srole_role"]
	for i := range ids {
		id := strings.TrimSpace(ids[i])
		if id == "" {
			continue
		}
		name := ""
		if i < len(names) {
			name = strings.TrimSpace(names[i])
		}
		role := ""
		if i < len(roles) {
			role = strings.TrimSpace(roles[i])
		}
		nature, err := natureForForumRole(role)
		if err != nil {
			return err
		}
		if err := model.SetStudioStatusPolicyNature(db, id, name, nature); err != nil {
			return err
		}
	}
	return nil
}

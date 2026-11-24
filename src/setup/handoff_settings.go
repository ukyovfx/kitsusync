package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"gorm.io/gorm"
)

// The handoff wiring (redesign core v2 §3.3, 2026-11-23). The wiring is
// task type -> task types: the product dimension is gone, because the
// trigger is the status transition, not the publish. Studio-common routes
// are the default; a Production may override individual rows (or switch a
// row off). The no-assignee handling stays per-Production.
//
// Placement (owner correction, 2026-11-24): the studio-common editor is
// global, so it lives on the connection settings page next to the status
// -> role mapping, not inside one Production's page. The Production page
// (/admin/handoff?project=...) carries only the overrides and the
// no-assignee handling, plus a pointer to the connection settings page.
// Both editors render one row per source Task Type with the target
// checkboxes folded into a <details> summary, so the page stays compact
// however many Task Types the studio has.

var handoffTaskTypeLister = setupKitsuTaskTypes

var handoffFallbackOptions = []string{model.HandoffFallbackDisabled, model.HandoffFallbackUsers, model.HandoffFallbackAdmin}

func handoffFallbackLabel(lang, fallback string) string {
	switch fallback {
	case model.HandoffFallbackAdmin:
		return t(lang, "サーバーの管理者権限を持つ人に通知", "Notify members with server administrator permissions")
	case model.HandoffFallbackUsers:
		return t(lang, "指定したユーザーに通知", "Notify a chosen user")
	case model.HandoffFallbackDisabled:
		return t(lang, "通知しない", "Do not notify")
	}
	return fallback
}

// handoffTemplateRoutes is the standard wiring the connection automation
// seeds as studio routes: the folding of the old product-dimension table
// (core v2 §3.6). Storyboard and Concept intentionally have no row.
var handoffTemplateRoutes = []struct {
	From   string
	Notify []string
}{
	{"素材取り込み", []string{"Layout", "Compositing"}},
	{"Compositing", []string{"Layout", "Animation"}},
	{"Layout", []string{"Animation", "Lighting", "Compositing"}},
	{"Modeling", []string{"Rigging", "Shading", "Layout"}},
	{"Rigging", []string{"Animation"}},
	{"Animation", []string{"Lighting", "FX", "Rendering"}},
	{"FX", []string{"Lighting", "Compositing"}},
	{"Lighting", []string{"Rendering"}},
	{"Rendering", []string{"Compositing"}},
}

// studioTaskTypes returns the union of the Task Types of every Production:
// the studio wiring is global, so its editor cannot borrow one Production's
// list the way the per-Production page does.
func studioTaskTypes(db *gorm.DB) []kitsu.TaskType {
	seen := map[string]struct{}{}
	var out []kitsu.TaskType
	for _, project := range availableProjects(db) {
		for _, taskType := range handoffTaskTypeLister(db, project.KitsuProjectID) {
			if _, dup := seen[taskType.ID]; dup {
				continue
			}
			seen[taskType.ID] = struct{}{}
			out = append(out, taskType)
		}
	}
	return out
}

func taskTypeNamesByID(taskTypes []kitsu.TaskType) map[string]string {
	names := map[string]string{}
	for _, taskType := range taskTypes {
		names[taskType.ID] = taskType.Name
	}
	return names
}

// HandoffSettingsHandler serves /admin/handoff?project=<Kitsu project ID>:
// this Production's overrides and no-assignee handling.
func HandoffSettingsHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		projectID := strings.TrimSpace(r.FormValue("project"))
		project := model.FindProjectByKitsuID(db, projectID)
		if project == nil {
			http.Error(w, "unknown Production", http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPost {
			if project.ValidationOnly || ValidationOnlyModeEnabled() {
				http.Error(w, "validation-only Production is read-only", http.StatusForbidden)
				return
			}
			if err := saveProductionHandoffForm(db, r, projectID); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, renderHandoffSettingsPage(db, r, lang, *project, err))
				return
			}
			http.Redirect(w, r, withLang("/bot/admin/handoff?project="+url.QueryEscape(projectID), r)+"&msg=saved", http.StatusSeeOther)
			return
		}
		fmt.Fprint(w, renderHandoffSettingsPage(db, r, lang, *project, nil))
	}
}

// errHandoffRoutesInvalid marks a wiring the page refuses to store; the page
// renders its message as-is.
var errHandoffRoutesInvalid = errors.New("handoff routes invalid")

// taskRouteFormJoiner validates notify targets against the known Task
// Types: a route naming a Task Type that does not exist would silently
// never fire (core v2 §3.3 save-time validation).
func taskRouteFormJoiner(taskTypes []kitsu.TaskType) func(values []string) (string, error) {
	known := map[string]string{}
	for _, taskType := range taskTypes {
		known[taskType.ID] = taskType.Name
	}
	return func(values []string) (string, error) {
		var ids []string
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := known[value]; !ok {
				return "", fmt.Errorf("%w: 通知先のタスクタイプ %s がこの製作にありません", errHandoffRoutesInvalid, value)
			}
			ids = append(ids, value)
		}
		return model.JoinHandoffNotifyIDs(ids), nil
	}
}

// parseStudioRoutesForm reads the studio layer of a wiring form. An
// unchecked row deletes its route; the explicit none checkbox stores the
// no-handoff mark instead.
func parseStudioRoutesForm(r *http.Request, taskTypes []kitsu.TaskType) (routes []model.StudioTaskRoute, deletes []string, err error) {
	_ = r.ParseForm()
	joinNotify := taskRouteFormJoiner(taskTypes)
	for _, taskType := range taskTypes {
		if _, none := r.Form["st_none_"+taskType.ID]; none {
			routes = append(routes, model.StudioTaskRoute{FromTaskTypeID: taskType.ID, FromTaskTypeName: taskType.Name, NotifyTaskTypeIDs: model.TaskRouteNone})
		} else if joined, jerr := joinNotify(r.Form["st_notify_"+taskType.ID]); jerr != nil {
			return nil, nil, jerr
		} else if joined != "" {
			routes = append(routes, model.StudioTaskRoute{FromTaskTypeID: taskType.ID, FromTaskTypeName: taskType.Name, NotifyTaskTypeIDs: joined})
		} else {
			deletes = append(deletes, taskType.ID)
		}
	}
	return routes, deletes, nil
}

// parseProductionRoutesForm reads the override layer of a wiring form.
func parseProductionRoutesForm(r *http.Request, productionID string, taskTypes []kitsu.TaskType) (overrides []model.ProductionTaskRoute, deletes []string, err error) {
	_ = r.ParseForm()
	joinNotify := taskRouteFormJoiner(taskTypes)
	for _, taskType := range taskTypes {
		switch mode := strings.TrimSpace(r.FormValue("ov_mode_" + taskType.ID)); mode {
		case "", "inherit":
			deletes = append(deletes, taskType.ID)
		case "none":
			overrides = append(overrides, model.ProductionTaskRoute{ProductionID: productionID, FromTaskTypeID: taskType.ID, FromTaskTypeName: taskType.Name, NotifyTaskTypeIDs: model.TaskRouteNone})
		case "custom":
			joined, jerr := joinNotify(r.Form["ov_notify_"+taskType.ID])
			if jerr != nil {
				return nil, nil, jerr
			}
			if joined == "" {
				return nil, nil, fmt.Errorf("%w: %s の上書きで受け渡し先が選ばれていません", errHandoffRoutesInvalid, taskType.Name)
			}
			overrides = append(overrides, model.ProductionTaskRoute{ProductionID: productionID, FromTaskTypeID: taskType.ID, FromTaskTypeName: taskType.Name, NotifyTaskTypeIDs: joined})
		default:
			return nil, nil, fmt.Errorf("%w: %s の上書き方法が不正です", errHandoffRoutesInvalid, taskType.Name)
		}
	}
	return overrides, deletes, nil
}

// saveStudioRoutes stores one submit of the studio wiring form on the
// connection settings page.
func saveStudioRoutes(db *gorm.DB, r *http.Request) error {
	routes, deletes, err := parseStudioRoutesForm(r, studioTaskTypes(db))
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, route := range routes {
			if err := model.SaveStudioTaskRoute(tx, route); err != nil {
				return err
			}
		}
		for _, id := range deletes {
			if err := model.DeleteStudioTaskRoute(tx, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// saveProductionHandoffForm stores one submit of the Production handoff
// page: this Production's overrides and the no-assignee handling. The
// studio routes are not part of this form (they are edited on the
// connection settings page), so a submit here never touches them.
func saveProductionHandoffForm(db *gorm.DB, r *http.Request, productionID string) error {
	overrides, overrideDeletes, err := parseProductionRoutesForm(r, productionID, handoffTaskTypeLister(db, productionID))
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, route := range overrides {
			if err := model.SaveProductionTaskRoute(tx, route); err != nil {
				return err
			}
		}
		for _, id := range overrideDeletes {
			if err := model.DeleteProductionTaskRoute(tx, productionID, id); err != nil {
				return err
			}
		}
		// The no-assignee handling rides the existing handoff config; the
		// legacy product routes are carried over untouched (they are history
		// now, and the production reset removes them deliberately).
		settings := model.ProductionHandoffSettings{Config: model.ProductionHandoffConfig{
			ProductionID:   productionID,
			NoAssignee:     strings.TrimSpace(r.FormValue("no_assignee")),
			FallbackUserID: strings.TrimSpace(r.FormValue("fallback_user")),
		}}
		if existing, ok, lerr := model.LoadProductionHandoffSettings(tx, productionID); lerr == nil && ok {
			settings.Routes = existing.Routes
			settings.ProductTypes = existing.ProductTypes
			settings.Config.NotifyTiming = existing.Config.NotifyTiming
		}
		return model.SaveProductionHandoffSettings(tx, settings)
	})
}

// routeSelectionSummary renders a stored wiring value as one compact line:
// the no-handoff mark, the receiving Task Type names, or "no wiring". The
// names follow the canonical Task Type order (the order of taskTypes), not
// the order they were selected or stored in.
func routeSelectionSummary(joined string, taskTypes []kitsu.TaskType, namesByID map[string]string, lang string) string {
	if joined == model.TaskRouteNone {
		return t(lang, "受け渡しなし", "No handoff")
	}
	selected := map[string]struct{}{}
	for _, id := range model.SplitHandoffNotifyIDs(joined) {
		selected[id] = struct{}{}
	}
	var names []string
	seen := map[string]struct{}{}
	for _, taskType := range taskTypes {
		if _, ok := selected[taskType.ID]; ok {
			if name := namesByID[taskType.ID]; name != "" {
				names = append(names, name)
				seen[taskType.ID] = struct{}{}
			}
		}
	}
	for _, id := range model.SplitHandoffNotifyIDs(joined) {
		if _, ok := seen[id]; !ok {
			if name := namesByID[id]; name != "" {
				names = append(names, name)
				seen[id] = struct{}{}
			}
		}
	}
	if len(names) == 0 {
		return t(lang, "配線なし", "No wiring")
	}
	return strings.Join(names, ", ")
}

// routeSummaryClass marks a wiring summary line as wired or unwired so the
// stylesheet can keep unwired rows muted and reserve the accent color for
// the open/interactive state.
func routeSummaryClass(joined string) string {
	if joined == "" || joined == model.TaskRouteNone {
		return "handoff-route-summary is-unwired"
	}
	return "handoff-route-summary is-wired"
}

// routeTargetCheckboxes renders one row's notify checkboxes, folded by the
// caller into a <details> block. The source Task Type itself is never
// offered — a Task Type cannot hand off to itself — but a stored
// self-route (possible from the older editor, which did offer it) is
// carried through a hidden input, so saving the page does not silently
// drop stored data the UI no longer offers.
func routeTargetCheckboxes(prefix, fromID string, taskTypes []kitsu.TaskType, selected []string) string {
	chosen := map[string]struct{}{}
	for _, id := range selected {
		chosen[id] = struct{}{}
	}
	var b strings.Builder
	for _, taskType := range taskTypes {
		if taskType.ID == fromID {
			if _, ok := chosen[fromID]; ok {
				b.WriteString(`<input type="hidden" name="` + esc(prefix+fromID) + `" value="` + esc(fromID) + `">`)
			}
			continue
		}
		checked := ""
		if _, ok := chosen[taskType.ID]; ok {
			checked = " checked"
		}
		b.WriteString(`<label class="checkbox-label"><input type="checkbox" name="` + esc(prefix+fromID) + `" value="` + esc(taskType.ID) + `"` + checked + `> ` + esc(taskType.Name) + `</label>`)
	}
	return b.String()
}

// studioRouteState is one source Task Type's stored studio wiring.
type studioRouteState struct {
	taskType kitsu.TaskType
	joined   string
	none     bool
	selected []string
}

func studioRouteStates(db *gorm.DB, taskTypes []kitsu.TaskType) []studioRouteState {
	studioByFrom := map[string]model.StudioTaskRoute{}
	if routes, err := model.LoadStudioTaskRoutes(db); err == nil {
		for _, route := range routes {
			studioByFrom[route.FromTaskTypeID] = route
		}
	}
	states := make([]studioRouteState, 0, len(taskTypes))
	for _, taskType := range taskTypes {
		studio := studioByFrom[taskType.ID]
		none := studio.NotifyTaskTypeIDs == model.TaskRouteNone
		selected := model.SplitHandoffNotifyIDs(studio.NotifyTaskTypeIDs)
		if none {
			selected = nil
		}
		states = append(states, studioRouteState{taskType: taskType, joined: studio.NotifyTaskTypeIDs, none: none, selected: selected})
	}
	return states
}

// routeChipsStatic renders a stored wiring value as read-only chips: the
// target names, the explicit no-handoff mark, or "no wiring".
func routeChipsStatic(state studioRouteState, namesByID map[string]string, lang string) string {
	if state.none {
		return `<span class="route-chip route-chip-static">` + esc(t(lang, "受け渡しなし", "No handoff")) + `</span>`
	}
	if len(state.selected) == 0 {
		return `<span class="route-none-label">` + esc(t(lang, "配線なし", "No wiring")) + `</span>`
	}
	var b strings.Builder
	for _, id := range state.selected {
		name := namesByID[id]
		if name == "" {
			name = id
		}
		b.WriteString(`<span class="route-chip route-chip-static">` + esc(name) + `</span>`)
	}
	return b.String()
}

// renderStudioRoutesSection is the studio-common wiring editor on the
// connection settings page, beside the status -> role mapping: both are
// studio-wide notification design, set once. Only sources that carry a
// wiring (or the explicit no-handoff mark) show a row; the rest appear
// through the "add a source" line. The submitted form contract is the
// long-standing one (st_notify_<id> checkboxes plus the st_none_<id>
// mark); the checkboxes live in a hidden group and the visible chips,
// select and add button drive them.
func renderStudioRoutesSection(db *gorm.DB, lang string, r *http.Request) string {
	taskTypes := studioTaskTypes(db)
	if len(taskTypes) == 0 {
		return ""
	}
	namesByID := taskTypeNamesByID(taskTypes)
	states := studioRouteStates(db, taskTypes)
	var rows strings.Builder
	var sourceOptions strings.Builder
	for _, state := range states {
		taskType := state.taskType
		wired := state.none || len(state.selected) > 0
		rowHidden := ""
		if !wired {
			rowHidden = " hidden"
			sourceOptions.WriteString(`<option value="` + esc(taskType.ID) + `">` + esc(taskType.Name) + `</option>`)
		}
		noneChecked := ""
		if state.none {
			noneChecked = " checked"
		}
		var addOptions strings.Builder
		addOptions.WriteString(`<option value="">` + esc(t(lang, "行き先を選ぶ", "Choose a destination")) + `</option>`)
		selectedSet := map[string]struct{}{}
		for _, id := range state.selected {
			selectedSet[id] = struct{}{}
		}
		for _, target := range taskTypes {
			if target.ID == taskType.ID {
				continue
			}
			if _, ok := selectedSet[target.ID]; ok {
				continue
			}
			addOptions.WriteString(`<option value="` + esc(target.ID) + `">` + esc(target.Name) + `</option>`)
		}
		modeWired := !state.none
		wiredSegClass := " on"
		noneSegClass := ""
		targetsEditorHidden := ""
		if state.none {
			wiredSegClass = ""
			noneSegClass = " on"
			targetsEditorHidden = " hidden"
		}
		rows.WriteString(`<div class="route-item" data-route-item="` + esc(taskType.ID) + `"` + rowHidden + `>` +
			`<div class="route-row" data-route-row="` + esc(taskType.ID) + `">` +
			`<span class="route-src">` + esc(taskType.Name) + `</span>` +
			`<span class="route-chips" data-route-chips>` + routeChipsStatic(state, namesByID, lang) + `</span>` +
			`<span class="route-row-actions"><button type="button" class="btn-ghost" data-route-toggle>` + esc(t(lang, "変更", "Edit")) + `</button></span></div>` +
			`<div class="route-editor" data-route-editor="` + esc(taskType.ID) + `" hidden>` +
			`<div class="route-hidden-inputs" style="display:none">` + routeTargetCheckboxes("st_notify_", taskType.ID, taskTypes, state.selected) +
			`<input type="checkbox" data-route-none name="st_none_` + esc(taskType.ID) + `" value="1"` + noneChecked + `></div>` +
			`<div class="route-mode-row"><span class="route-mode-label">` + esc(t(lang, "受け渡しの状態", "Handoff mode")) + `</span>` +
			`<div class="seg" role="group" aria-label="` + esc(t(lang, "受け渡しの状態", "Handoff mode")) + `">` +
			`<button type="button" class="` + strings.TrimSpace(wiredSegClass) + `" data-route-mode="wired" aria-pressed="` + esc(fmt.Sprint(modeWired)) + `">` + esc(t(lang, "配線あり", "Wired")) + `</button>` +
			`<button type="button" class="` + strings.TrimSpace(noneSegClass) + `" data-route-mode="none" aria-pressed="` + esc(fmt.Sprint(state.none)) + `">` + esc(t(lang, "受け渡しなし", "No handoff")) + `</button>` +
			`</div></div>` +
			`<div class="route-targets-editor" data-route-targets-editor` + targetsEditorHidden + `>` +
			`<div class="route-add-line"><span class="route-add-label">` + esc(t(lang, "行き先を追加", "Add destination")) + `</span><select data-route-add-select aria-label="` + esc(t(lang, "行き先を選ぶ", "Choose a destination")) + `">` + addOptions.String() + `</select>` +
			`<button type="button" class="btn sm" data-route-add>` + esc(t(lang, "追加", "Add")) + `</button></div></div>` +
			`</div></div>`)
	}
	addSource := `<div class="route-source-adder"><span class="route-add-label">` + esc(t(lang, "新しい起点タスクタイプを追加", "Add source task type")) + `</span>` +
		`<div class="route-add-line"><select data-route-source-select aria-label="` + esc(t(lang, "起点を選ぶ", "Choose a source")) + `"><option value="">` + esc(t(lang, "起点を選ぶ", "Choose a source")) + `</option>` + sourceOptions.String() + `</select>` +
		`<button type="button" class="btn" data-route-source-add>` + esc(t(lang, "追加", "Add")) + `</button></div></div>`
	return `<form method="post" action="` + esc(withLang("/bot/admin/bot/wiring", r)) + `" class="section-stack" data-route-editor-root data-none-label="` + esc(t(lang, "受け渡しなし", "No handoff")) + `" data-no-wiring-label="` + esc(t(lang, "配線なし", "No wiring")) + `" data-edit-label="` + esc(t(lang, "変更", "Edit")) + `" data-close-label="` + esc(t(lang, "閉じる", "Close")) + `">` +
		`<input type="hidden" name="action" value="save_studio_routes">` +
		`<section class="flat-section" id="studio-routes"><div class="flat-section-head"><h2>` + esc(t(lang, "受け渡しの配線", "Handoff wiring")) + `</h2></div>` +
		`<p class="field-help">` + esc(t(lang, "タスクが完了したとき、同じショット/アセットの次工程へ「進めていい」と伝える配線です。", "Wiring that tells the next task types on the same shot or asset they may proceed when a task finishes.")) + `</p>` +
		rows.String() + addSource +
		`<div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(t(lang, "接続設定トップへ戻る", "Back to Connections hub")) + `</a><button type="submit" class="btn">` + esc(t(lang, "保存", "Save")) + `</button></div></section></form>` + studioRoutesEditorScript
}

// studioRoutesEditorScript drives the wiring editor's chips: the hidden
// checkboxes stay the single source of truth, and the chips, the add
// select and the no-handoff segmented choice mirror them. Choosing
// 「受け渡しなし」 clears the row's targets and hides the target editor;
// choosing 「配線あり」 or adding a target clears the no-handoff state.
const studioRoutesEditorScript = `<script data-studio-routes-editor>(function(){var root=document.querySelector("[data-route-editor-root]");if(!root)return;var noneLabel=root.getAttribute("data-none-label")||"受け渡しなし",noWiringLabel=root.getAttribute("data-no-wiring-label")||"配線なし",editLabel=root.getAttribute("data-edit-label")||"変更",closeLabel=root.getAttribute("data-close-label")||"閉じる";function rebuild(item){var row=item.querySelector("[data-route-row]");var editor=item.querySelector("[data-route-editor]");if(!row||!editor)return;var chips=row.querySelector("[data-route-chips]");var boxes=editor.querySelectorAll("input[type=checkbox][name^=\"st_notify_\"]");var none=editor.querySelector("[data-route-none]");var targetsEditor=editor.querySelector("[data-route-targets-editor]");var wiredBtn=editor.querySelector("[data-route-mode=\"wired\"]");var noneBtn=editor.querySelector("[data-route-mode=\"none\"]");var select=editor.querySelector("[data-route-add-select]");var checked=[];boxes.forEach(function(box){if(box.checked){checked.push(box)}});var isNone=!!(none&&none.checked);if(wiredBtn){wiredBtn.classList.toggle("on",!isNone);wiredBtn.setAttribute("aria-pressed",String(!isNone))}if(noneBtn){noneBtn.classList.toggle("on",isNone);noneBtn.setAttribute("aria-pressed",String(isNone))}if(targetsEditor){targetsEditor.hidden=isNone}chips.innerHTML="";if(isNone){var mark=document.createElement("span");mark.className="route-chip route-chip-static";mark.textContent=noneLabel;chips.appendChild(mark)}else if(checked.length===0){var empty=document.createElement("span");empty.className="route-none-label";empty.textContent=noWiringLabel;chips.appendChild(empty)}else{checked.forEach(function(box){var chip=document.createElement("span");chip.className="route-chip";chip.textContent=box.getAttribute("data-route-name")||box.value;var remove=document.createElement("button");remove.type="button";remove.textContent="×";remove.setAttribute("aria-label",chip.textContent);remove.addEventListener("click",function(){box.checked=false;rebuild(item)});chip.appendChild(remove);chips.appendChild(chip)})}if(select){var current=select.value;var first=select.querySelector("option[value=\"\"]");select.innerHTML="";if(first)select.appendChild(first);boxes.forEach(function(box){if(box.checked)return;var option=document.createElement("option");option.value=box.value;option.textContent=box.getAttribute("data-route-name")||box.value;select.appendChild(option)});select.value=current}}root.querySelectorAll("[data-route-item]").forEach(function(item){var row=item.querySelector("[data-route-row]");var editor=item.querySelector("[data-route-editor]");if(!row||!editor)return;var toggle=row.querySelector("[data-route-toggle]");if(toggle){toggle.addEventListener("click",function(){editor.hidden=!editor.hidden;item.classList.toggle("is-editing",!editor.hidden);toggle.textContent=editor.hidden?editLabel:closeLabel})}var none=editor.querySelector("[data-route-none]");var boxes=editor.querySelectorAll("input[type=checkbox][name^=\"st_notify_\"]");var wiredBtn=editor.querySelector("[data-route-mode=\"wired\"]");var noneBtn=editor.querySelector("[data-route-mode=\"none\"]");if(wiredBtn){wiredBtn.addEventListener("click",function(){if(none)none.checked=false;rebuild(item)})}if(noneBtn){noneBtn.addEventListener("click",function(){if(none)none.checked=true;boxes.forEach(function(box){box.checked=false});rebuild(item)})}var add=editor.querySelector("[data-route-add]");var select=editor.querySelector("[data-route-add-select]");if(add&&select){add.addEventListener("click",function(){var value=select.value;if(!value)return;boxes.forEach(function(box){if(box.value===value){box.checked=true}});if(none)none.checked=false;rebuild(item)})}rebuild(item)});var sourceSelect=root.querySelector("[data-route-source-select]");var sourceAdd=root.querySelector("[data-route-source-add]");if(sourceSelect&&sourceAdd){sourceAdd.addEventListener("click",function(){var id=sourceSelect.value;if(!id)return;var item=root.querySelector("[data-route-item=\""+id+"\"]");var editor=item&&item.querySelector("[data-route-editor]");var toggle=item&&item.querySelector("[data-route-toggle]");if(item){item.hidden=false;item.classList.add("is-editing")}if(editor){editor.hidden=false}if(toggle){toggle.textContent=closeLabel}var option=sourceSelect.querySelector("option[value=\""+id+"\"]");if(option)option.remove();sourceSelect.value=""})}})();</script>`

// renderStudioRoutesDisplay is the read-only wiring list on the connection
// page's normal mode: every source with its targets as chips.
func renderStudioRoutesDisplay(db *gorm.DB, lang string, r *http.Request) string {
	taskTypes := studioTaskTypes(db)
	if len(taskTypes) == 0 {
		return ""
	}
	namesByID := taskTypeNamesByID(taskTypes)
	states := studioRouteStates(db, taskTypes)
	var rows strings.Builder
	for _, state := range states {
		rows.WriteString(`<div class="route-row"><span class="route-src">` + esc(state.taskType.Name) + `</span><span class="route-chips">` + routeChipsStatic(state, namesByID, lang) + `</span></div>`)
	}
	return `<section class="flat-section" aria-labelledby="studio-routes-display-title"><div class="flat-section-head"><h2 id="studio-routes-display-title">` + esc(t(lang, "受け渡しの配線", "Handoff wiring")) + `</h2><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot/wiring", r)) + `">` + esc(t(lang, "編集", "Edit")) + `</a></div>` +
		`<p class="field-help">` + esc(t(lang, "タスクが完了したとき、同じショット/アセットの次工程へ「進めていい」と伝える配線です。", "Wiring that tells the next task types on the same shot or asset they may proceed when a task finishes.")) + `</p>` +
		rows.String() + `</section>`
}

func renderHandoffSettingsPage(db *gorm.DB, r *http.Request, lang string, project model.Project, saveErr error) string {
	productionID := project.KitsuProjectID
	taskTypes := handoffTaskTypeLister(db, productionID)
	namesByID := taskTypeNamesByID(taskTypes)
	studioByFrom := map[string]model.StudioTaskRoute{}
	if routes, err := model.LoadStudioTaskRoutes(db); err == nil {
		for _, route := range routes {
			studioByFrom[route.FromTaskTypeID] = route
		}
	}
	overrideByFrom := map[string]model.ProductionTaskRoute{}
	if routes, err := model.LoadProductionTaskRoutes(db, productionID); err == nil {
		for _, route := range routes {
			overrideByFrom[route.FromTaskTypeID] = route
		}
	}
	var config model.ProductionHandoffConfig
	if settings, ok, err := model.LoadProductionHandoffSettings(db, productionID); err == nil && ok {
		config = settings.Config
	}

	var notices strings.Builder
	if r.FormValue("msg") == "saved" {
		notices.WriteString(`<div class="notice" role="status">` + esc(t(lang, "設定を保存しました。", "Settings saved.")) + `</div>`)
	}
	errorHTML := ""
	if saveErr != nil {
		errorHTML = `<div class="notice notice-error" role="alert"><strong>` + esc(t(lang, "保存できませんでした", "Could not save")) + `</strong><p>` + esc(saveErr.Error()) + `</p></div>`
	}

	var overrideRows strings.Builder
	for _, taskType := range taskTypes {
		override, hasOverride := overrideByFrom[taskType.ID]
		mode := "inherit"
		var overrideSelected []string
		if hasOverride {
			if override.NotifyTaskTypeIDs == model.TaskRouteNone {
				mode = "none"
			} else {
				mode = "custom"
				overrideSelected = model.SplitHandoffNotifyIDs(override.NotifyTaskTypeIDs)
			}
		}
		inheritedSummary := t(lang, "配線なし", "no wiring")
		if route, ok := studioByFrom[taskType.ID]; ok {
			inheritedSummary = routeSelectionSummary(route.NotifyTaskTypeIDs, taskTypes, namesByID, lang)
		}
		modeOptions := [][2]string{
			{"inherit", t(lang, "スタジオ共通を継承", "Inherit the studio default")},
			{"none", t(lang, "この製作では受け渡しなし", "No handoff in this Production")},
			{"custom", t(lang, "この製作だけ指定", "Override for this Production")},
		}
		inheritHidden, noneHidden, customHidden, editorHidden, resetHidden := "", "", " hidden", " hidden", " hidden"
		if mode == "custom" {
			inheritHidden, customHidden, editorHidden, resetHidden = " hidden", "", "", ""
		} else if mode == "none" {
			inheritHidden, noneHidden, resetHidden = " hidden", "", ""
		}
		var addOptions strings.Builder
		addOptions.WriteString(`<option value="">` + esc(t(lang, "行き先を選ぶ", "Choose a destination")) + `</option>`)
		selectedSet := map[string]struct{}{}
		for _, id := range overrideSelected {
			selectedSet[id] = struct{}{}
		}
		for _, target := range taskTypes {
			if target.ID == taskType.ID {
				continue
			}
			if _, ok := selectedSet[target.ID]; ok {
				continue
			}
			addOptions.WriteString(`<option value="` + esc(target.ID) + `">` + esc(target.Name) + `</option>`)
		}
		overrideRows.WriteString(`<div class="handoff-override-row" data-ov-row="` + esc(taskType.ID) + `">` +
			`<span class="src">` + esc(taskType.Name) + `</span>` +
			forumSelect("ov_mode_"+taskType.ID, mode, modeOptions, "") +
			`<span class="inh" data-handoff-summary="inherit"` + inheritHidden + `>` + esc(t(lang, "（継承：", "(Inheriting: ")) + esc(inheritedSummary) + esc(t(lang, "）", ")")) + `</span>` +
			`<span class="inh is-override" data-handoff-summary="none"` + noneHidden + `>` + esc(t(lang, "受け渡しなし（継承：", "No handoff (inherited: ")) + esc(inheritedSummary) + esc(t(lang, "）", ")")) + `</span>` +
			`<span class="inh is-override route-chips" data-handoff-summary="custom" data-ov-chips` + customHidden + `></span>` +
			`<button type="button" class="btn-ghost" data-ov-reset` + resetHidden + `>` + esc(t(lang, "解除", "Reset")) + `</button>` +
			`</div>` +
			`<div class="handoff-override-editor" data-ov-editor="` + esc(taskType.ID) + `"` + editorHidden + `>` +
			`<div class="route-hidden-inputs" style="display:none">` + routeTargetCheckboxes("ov_notify_", taskType.ID, taskTypes, overrideSelected) + `</div>` +
			`<div class="route-add-line"><select data-ov-add-select aria-label="` + esc(t(lang, "行き先を選ぶ", "Choose a destination")) + `">` + addOptions.String() + `</select>` +
			`<button type="button" class="btn sm" data-ov-add>` + esc(t(lang, "追加", "Add")) + `</button></div>` +
			`</div>`)
	}

	fallbackOptionsList := make([][2]string, 0, len(handoffFallbackOptions))
	for _, fallback := range handoffFallbackOptions {
		fallbackOptionsList = append(fallbackOptionsList, [2]string{fallback, handoffFallbackLabel(lang, fallback)})
	}

	noAssigneeValue := strings.TrimSpace(config.NoAssignee)
	if noAssigneeValue == "" {
		noAssigneeValue = model.HandoffFallbackDisabled
	}
	fallbackUserHidden := ""
	if noAssigneeValue != model.HandoffFallbackUsers {
		fallbackUserHidden = " hidden"
	}
	fallbackUserOptions := handoffFallbackMemberOptions(db, project.DiscordGuildID, config.FallbackUserID, lang)
	form := `<form method="post" action="` + esc(withLang("/bot/admin/handoff", r)) + `" class="section-stack" data-handoff-override-root data-empty-label="` + esc(t(lang, "行き先を追加してください", "Add a destination")) + `">` +
		`<input type="hidden" name="project" value="` + esc(productionID) + `">` +
		`<section class="flat-section"><h2>` + esc(t(lang, "この製作の上書き", "Overrides for this Production")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "通常は継承のままで構いません。この製作だけ配線を変える行だけ指定します。", "Leave rows inherited unless this Production wires differently.")) + `</p>` +
		`<div class="handoff-routes">` + overrideRows.String() + `</div></section>` +
		`<section class="flat-section"><h2>` + esc(t(lang, "担当者なしの扱い", "When nobody is assigned")) + `</h2>` +
		`<label class="conn-field-label" for="handoff-noassignee">` + esc(t(lang, "担当者なしの通知先", "Recipient when nobody is assigned")) + `</label>` +
		forumSelect("no_assignee", noAssigneeValue, fallbackOptionsList, "") +
		`<p class="conn-note">` + esc(t(lang, "「指定したユーザーに通知」を選ぶと、通知先のメンバー選択欄が表示されます。", "Choosing \"Notify a chosen user\" reveals the member selector.")) + `</p>` +
		`<div id="handoff-fallback-user-field"` + fallbackUserHidden + `>` +
		`<label class="conn-field-label" for="handoff-fallback-user">` + esc(t(lang, "通知するメンバー", "Member to notify")) + `</label>` +
		`<select id="handoff-fallback-user" name="fallback_user">` + fallbackUserOptions + `</select>` +
		`</div>` +
		`</section>` +
		`<div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(productionID)+"&tab=notifications", r)) + `">` + esc(t(lang, "通知設定に戻る", "Back to notifications")) + `</a> <button type="submit" class="btn">` + esc(t(lang, "保存", "Save")) + `</button></div></form>`

	breadcrumb := `<div class="prodname"><span class="lbl">` + esc(t(lang, "プロダクション", "Production")) + `</span><span class="name">` + esc(project.Name) + `</span></div>`
	body := `<div class="section-stack">` + breadcrumb + errorHTML + notices.String() + form + `</div>` + handoffPageScript
	return adminPage(lang, t(lang, "受け渡しの配線", "Handoff wiring"), r, body)
}

// handoffPageScript drives the Production handoff page's presentation
// toggles: a row's target chip editor and one-line summary follow its
// mode select (the editor shows only in custom mode; switching to the
// no-handoff or inherit mode clears the row's target checks), and the
// fallback user ID field shows only while the no-assignee handling
// targets a chosen user.
const handoffPageScript = `<script data-handoff-page>(function(){var root=document.querySelector("[data-handoff-override-root]");var emptyLabel=(root&&root.getAttribute("data-empty-label"))||"行き先を追加してください";document.querySelectorAll("[data-ov-row]").forEach(function(row){var id=row.getAttribute("data-ov-row");var select=row.querySelector("select[name=\"ov_mode_"+id+"\"]");var editor=document.querySelector("[data-ov-editor=\""+id+"\"]");var chips=row.querySelector("[data-ov-chips]");var resetBtn=row.querySelector("[data-ov-reset]");if(!select||!editor)return;var boxes=editor.querySelectorAll("input[type=checkbox][name^=\"ov_notify_\"]");var addSelect=editor.querySelector("[data-ov-add-select]");var addBtn=editor.querySelector("[data-ov-add]");function rebuildChips(){if(!chips)return;var checked=[];boxes.forEach(function(b){if(b.checked)checked.push(b)});chips.innerHTML="";if(checked.length===0){var empty=document.createElement("span");empty.className="route-none-label";empty.textContent=emptyLabel;chips.appendChild(empty)}else{checked.forEach(function(b){var chip=document.createElement("span");chip.className="route-chip";chip.textContent=b.getAttribute("data-route-name")||b.value;var rm=document.createElement("button");rm.type="button";rm.textContent="×";rm.setAttribute("aria-label",chip.textContent);rm.addEventListener("click",function(){b.checked=false;rebuildChips()});chip.appendChild(rm);chips.appendChild(chip)})}if(addSelect){var cur=addSelect.value;var first=addSelect.querySelector("option[value=\"\"]");addSelect.innerHTML="";if(first)addSelect.appendChild(first);boxes.forEach(function(b){if(b.checked)return;var opt=document.createElement("option");opt.value=b.value;opt.textContent=b.getAttribute("data-route-name")||b.value;addSelect.appendChild(opt)});addSelect.value=cur}}function apply(){var mode=select.value;row.querySelectorAll("[data-handoff-summary]").forEach(function(span){span.hidden=span.getAttribute("data-handoff-summary")!==mode});editor.hidden=mode!=="custom";if(resetBtn)resetBtn.hidden=mode==="inherit";if(mode==="none"){boxes.forEach(function(b){b.checked=false})}rebuildChips()}select.addEventListener("change",apply);if(resetBtn){resetBtn.addEventListener("click",function(){select.value="inherit";apply()})}if(addBtn&&addSelect){addBtn.addEventListener("click",function(){var val=addSelect.value;if(!val)return;boxes.forEach(function(b){if(b.value===val)b.checked=true});rebuildChips()})}apply()});var fallback=document.querySelector("select[name=\"no_assignee\"]"),field=document.getElementById("handoff-fallback-user-field");if(fallback&&field){var applyFallback=function(){field.hidden=fallback.value!=="users"};fallback.addEventListener("change",applyFallback);applyFallback()}})();</script>`

func handoffFallbackMemberOptions(db *gorm.DB, guildID, current, lang string) string {
	current = strings.TrimSpace(current)
	var b strings.Builder
	b.WriteString(`<option value="">` + esc(t(lang, "（選択）", "(choose)")) + `</option>`)
	var members []DiscordGuildMember
	if botToken := storedRuntimeDiscordBotToken(db); botToken != "" && isDiscordSnowflake(guildID) {
		loaded, status, err := cachedGuildMembers(guildID, botToken)
		if err == nil && status != guildDirectoryPending {
			members = loaded
		}
	}
	foundCurrent := false
	for _, member := range members {
		if member.User.Bot {
			continue
		}
		id := strings.TrimSpace(member.User.ID)
		if !isDiscordSnowflake(id) {
			continue
		}
		selected := ""
		if id == current {
			selected = " selected"
			foundCurrent = true
		}
		b.WriteString(`<option value="` + esc(id) + `"` + selected + `>` + esc(discordGuildMemberDisplayName(member)) + `</option>`)
	}
	if current != "" && !foundCurrent {
		b.WriteString(`<option value="` + esc(current) + `" selected>` + esc(current) + `</option>`)
	}
	return b.String()
}

// renderHandoffSettingsLink is the production page's entry to the wiring
// editor.
func renderHandoffSettingsLink(r *http.Request, project model.Project, lang string) string {
	return `<section class="section-card glass"><h2>` + esc(t(lang, "素材の受け渡し", "Material handoff")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "タスクが完了になったとき、同じショット/アセットのどのタスクへ知らせるかを設定します（スタジオ共通＋この製作の上書き）。", "Choose which tasks on the same shot/asset are told when a task finishes (studio defaults plus this Production's overrides).")) + `</p>` +
		`<div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/handoff?project="+url.QueryEscape(project.KitsuProjectID), r)) + `">` + esc(t(lang, "受け渡しの対応表を開く", "Open handoff mapping")) + `</a></div></section>`
}

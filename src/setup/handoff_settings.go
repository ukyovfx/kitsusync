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

// The handoff settings page configures the material handoff mapping for one
// Production: which flow a source Task Type's product takes, which Task Types
// are notified, and the two notification preferences. Detection, notification
// delivery and the handoff buttons are later milestones; this page only stores
// the configuration the mapping table will be driven by.
//
// Rows are generated from (the Production's existing Task Types) × (the product
// types registered here), so an operator only ever selects. A Production with
// no product type has no rows and stays fully inactive.

// handoffTaskTypeLister is a variable so tests run without Kitsu.
var handoffTaskTypeLister = setupKitsuTaskTypes

// handoffPreset is one row of the starter table applied when a Task Type name
// and a product type name both match. Owner decision 2026-11-19.
type handoffPreset struct {
	TaskType string
	Product  string
	Flow     string
	Notify   []string
}

var handoffPresets = []handoffPreset{
	{"素材取り込み", "plate", model.HandoffFlowHand, []string{"Layout", "Compositing"}},
	{"Compositing", "matchmove", model.HandoffFlowHand, []string{"Layout", "Animation"}},
	{"Layout", "camera", model.HandoffFlowHand, []string{"Animation", "Lighting", "Compositing"}},
	{"Layout", "layout", model.HandoffFlowHand, []string{"Animation", "Lighting"}},
	{"Modeling", "model", model.HandoffFlowHand, []string{"Rigging", "Shading", "Layout"}},
	{"Rigging", "rig", model.HandoffFlowHand, []string{"Animation"}},
	{"Animation", "pointcache", model.HandoffFlowHand, []string{"Lighting", "FX", "Rendering"}},
	{"FX", "fxcache", model.HandoffFlowHand, []string{"Lighting", "Compositing"}},
	{"Lighting", "lighting", model.HandoffFlowHand, []string{"Rendering"}},
	{"Rendering", "renderPass", model.HandoffFlowHand, []string{"Compositing"}},
	{"Compositing", "render", model.HandoffFlowSubmit, nil},
	{"Storyboard", "storyboard", model.HandoffFlowShelve, nil},
	{"Concept", "concept", model.HandoffFlowShelve, nil},
	{"Compositing", "workfile", model.HandoffFlowShelve, nil},
}

var handoffFlowOptions = []string{model.HandoffFlowSubmit, model.HandoffFlowHand, model.HandoffFlowShelve}

var handoffFallbackOptions = []string{model.HandoffFallbackAdmin, model.HandoffFallbackUsers, model.HandoffFallbackDisabled}

func handoffFlowLabel(lang, flow string) string {
	switch flow {
	case model.HandoffFlowSubmit:
		return t(lang, "提出", "Submit")
	case model.HandoffFlowHand:
		return t(lang, "受け渡し", "Hand off")
	case model.HandoffFlowShelve:
		return t(lang, "棚置き", "Shelve")
	}
	return flow
}

func handoffTimingLabel(lang, timing string) string {
	switch timing {
	case model.HandoffTimingImmediate:
		return t(lang, "即時", "Immediately")
	case model.HandoffTimingDigest:
		return t(lang, "朝のダイジェスト", "Morning digest")
	case model.HandoffTimingMixed:
		return t(lang, "指名は即時・それ以外は朝のダイジェスト", "Immediately when assigned, morning digest otherwise")
	}
	return timing
}

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

// handoffPresetFor returns the starter row for a Task Type × product type pair,
// or false when the pair is not in the preset table (it then defaults to shelve).
func handoffPresetFor(taskTypeName, productType string) (handoffPreset, bool) {
	for _, preset := range handoffPresets {
		if strings.EqualFold(strings.TrimSpace(preset.TaskType), strings.TrimSpace(taskTypeName)) &&
			strings.EqualFold(preset.Product, productType) {
			return preset, true
		}
	}
	return handoffPreset{}, false
}

// handoffPresetRoute builds the stored row a preset maps to. Notify names are
// resolved against the Production's own Task Types, so a preset that names a
// Task Type this Production does not have notifies only the ones it has.
func handoffPresetRoute(taskType kitsu.TaskType, productType string, taskTypes []kitsu.TaskType) (model.ProductionHandoffRoute, bool) {
	preset, ok := handoffPresetFor(taskType.Name, productType)
	if !ok {
		return model.ProductionHandoffRoute{}, false
	}
	var ids, names []string
	for _, want := range preset.Notify {
		for _, candidate := range taskTypes {
			if strings.EqualFold(strings.TrimSpace(candidate.Name), want) {
				ids = append(ids, candidate.ID)
				names = append(names, candidate.Name)
				break
			}
		}
	}
	return model.ProductionHandoffRoute{
		TaskTypeID:         taskType.ID,
		TaskTypeName:       taskType.Name,
		ProductType:        productType,
		Flow:               preset.Flow,
		NotifyTaskTypeIDs:  strings.Join(ids, ","),
		NotifyTaskTypeName: strings.Join(names, ","),
	}, true
}

// HandoffSettingsHandler serves /admin/handoff?project=<Kitsu project ID>.
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
			settings := parseHandoffSettingsForm(r, projectID)
			if err := model.SaveProductionHandoffSettings(db, settings); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, renderHandoffSettingsPage(db, r, lang, *project, &settings, err))
				return
			}
			http.Redirect(w, r, withLang("/bot/admin/handoff?project="+url.QueryEscape(projectID), r)+"&msg=saved", http.StatusSeeOther)
			return
		}
		fmt.Fprint(w, renderHandoffSettingsPage(db, r, lang, *project, nil, nil))
	}
}

// parseHandoffSettingsForm reads the page's parallel-array fields. Product types
// are added by separate inputs, so the table's rows always derive from them.
func parseHandoffSettingsForm(r *http.Request, productionID string) model.ProductionHandoffSettings {
	_ = r.ParseForm()
	settings := model.ProductionHandoffSettings{Config: model.ProductionHandoffConfig{
		ProductionID:   productionID,
		NotifyTiming:   strings.TrimSpace(r.FormValue("notify_timing")),
		NoAssignee:     strings.TrimSpace(r.FormValue("no_assignee")),
		FallbackUserID: strings.TrimSpace(r.FormValue("fallback_user")),
	}}
	seenProducts := map[string]struct{}{}
	for _, name := range r.Form["product_name"] {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		// The form repeats a product's name in its rows; keep each one once.
		key := strings.ToLower(name)
		if _, dup := seenProducts[key]; dup {
			continue
		}
		seenProducts[key] = struct{}{}
		settings.ProductTypes = append(settings.ProductTypes, model.ProductionHandoffProductType{Name: name})
	}
	// Product types still waiting to be classified stay marked until one of
	// their rows leaves the shelved default, so a save of something else does
	// not quietly clear the mark.
	stillUnclassified := map[string]struct{}{}
	for _, name := range r.Form["product_unclassified"] {
		stillUnclassified[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	ids, names, products := r.Form["row_task"], r.Form["row_task_name"], r.Form["row_product"]
	flows := r.Form["row_flow"]
	// Notify boxes are per row, so each row reads its own checkbox group.
	notifyByRow := map[string][]string{}
	for key, values := range r.Form {
		if rowKey, ok := strings.CutPrefix(key, "row_notify_"); ok {
			notifyByRow[rowKey] = values
		}
	}
	for i := range ids {
		if i >= len(products) {
			continue
		}
		route := model.ProductionHandoffRoute{
			TaskTypeID:        strings.TrimSpace(ids[i]),
			ProductType:       strings.TrimSpace(products[i]),
			NotifyTaskTypeIDs: model.JoinHandoffNotifyIDs(notifyByRow[strings.TrimSpace(ids[i])+"|"+strings.TrimSpace(products[i])]),
		}
		if i < len(names) {
			route.TaskTypeName = strings.TrimSpace(names[i])
		}
		if i < len(flows) {
			route.Flow = strings.TrimSpace(flows[i])
		}
		if route.Flow != model.HandoffFlowShelve || route.NotifyTaskTypeIDs != "" {
			delete(stillUnclassified, strings.ToLower(route.ProductType))
		}
		settings.Routes = append(settings.Routes, route)
	}
	for i := range settings.ProductTypes {
		_, settings.ProductTypes[i].Unclassified = stillUnclassified[strings.ToLower(settings.ProductTypes[i].Name)]
	}
	// Keep the display names of the notified Task Types for the reloaded page.
	// The row only carries its own Task Type name, so the rest of the Production's
	// Task Types are read once from the same lookup the page uses.
	namesByID := map[string]string{}
	for i := range ids {
		if i < len(names) {
			namesByID[strings.TrimSpace(ids[i])] = strings.TrimSpace(names[i])
		}
	}
	for _, taskType := range handoffTaskTypeLister(nil, productionID) {
		if _, ok := namesByID[taskType.ID]; !ok {
			namesByID[taskType.ID] = taskType.Name
		}
	}
	for i := range settings.Routes {
		var notifyNames []string
		for _, id := range model.SplitHandoffNotifyIDs(settings.Routes[i].NotifyTaskTypeIDs) {
			notifyNames = append(notifyNames, namesByID[id])
		}
		settings.Routes[i].NotifyTaskTypeName = strings.Join(notifyNames, ",")
	}
	return settings
}

func renderHandoffSettingsPage(db *gorm.DB, r *http.Request, lang string, project model.Project, submitted *model.ProductionHandoffSettings, saveErr error) string {
	productionID := project.KitsuProjectID
	var current model.ProductionHandoffSettings
	if submitted != nil {
		current = *submitted
	} else if loaded, ok, err := model.LoadProductionHandoffSettings(db, productionID); err == nil && ok {
		current = loaded
	} else {
		current.Config = model.ProductionHandoffConfig{ProductionID: productionID}
	}
	// An untouched Production starts from the mixed rule the owner chose: a
	// chosen fallback only appears once the operator picks it.
	if strings.TrimSpace(current.Config.NotifyTiming) == "" && submitted == nil {
		current.Config.NotifyTiming = model.HandoffTimingMixed
	}

	var notices strings.Builder
	if r.FormValue("msg") == "saved" {
		notices.WriteString(`<div class="notice" role="status">` + esc(t(lang, "設定を保存しました。", "Settings saved.")) + `</div>`)
	}

	taskTypes := handoffTaskTypeLister(db, productionID)
	savedByName := map[string]model.ProductionHandoffRoute{}
	for _, route := range current.Routes {
		savedByName[handoffRouteKey(route.TaskTypeID, route.ProductType)] = route
	}

	// Product types are the stored list first, then the ones typed on the form
	// (a failed save must not lose a newly typed name), then any the rows still
	// reference so a whole-replace save cannot silently drop a row.
	type productRow struct{ name string }
	var products []productRow
	seenProducts := map[string]struct{}{}
	addProduct := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if _, dup := seenProducts[key]; dup {
			return
		}
		seenProducts[key] = struct{}{}
		products = append(products, productRow{name: name})
	}
	unclassified := map[string]bool{}
	for _, product := range current.ProductTypes {
		addProduct(product.Name)
		if product.Unclassified {
			unclassified[strings.ToLower(strings.TrimSpace(product.Name))] = true
		}
	}
	for _, route := range current.Routes {
		addProduct(route.ProductType)
	}

	errorHTML := ""
	if saveErr != nil {
		errorHTML = `<div class="notice notice-error" role="alert"><strong>` + esc(t(lang, "保存できませんでした", "Could not save")) + `</strong>` + handoffSaveErrorMarkup(lang, saveErr) + `</div>`
	}

	notifyOptions := make([][2]string, 0, len(taskTypes))
	for _, taskType := range taskTypes {
		notifyOptions = append(notifyOptions, [2]string{taskType.ID, taskType.Name})
	}

	// The matrix: one row per Task Type (Kitsu order) and one column per
	// registered product type. Every cell is a flow select plus the Task Types
	// to notify; nothing is typed per cell.
	var matrixHead, matrixBody strings.Builder
	presetsApplied := 0
	matrixHead.WriteString(`<tr><th scope="col" class="handoff-matrix-corner"><span class="handoff-axis-task">` + esc(t(lang, "タスクタイプ ↓", "Task Type ↓")) + `</span> <span class="handoff-axis-product">` + esc(t(lang, "製品タイプ →", "Product type →")) + `</span></th>`)
	for _, product := range products {
		badge := ""
		if unclassified[strings.ToLower(product.name)] {
			badge = ` ` + handoffUnclassifiedBadge(lang)
		}
		matrixHead.WriteString(`<th scope="col">` + esc(product.name) + badge + `</th>`)
	}
	matrixHead.WriteString(`</tr>`)
	for _, taskType := range taskTypes {
		matrixBody.WriteString(`<tr><th scope="row">` + esc(taskType.Name) + `</th>`)
		for _, product := range products {
			route, saved := savedByName[handoffRouteKey(taskType.ID, product.name)]
			if !saved && submitted == nil {
				if preset, ok := handoffPresetRoute(taskType, product.name, taskTypes); ok {
					route = preset
					presetsApplied++
				}
			}
			if route.Flow == "" {
				route.Flow = model.HandoffFlowShelve
			}
			matrixBody.WriteString(handoffRouteCell(lang, taskType, product.name, route, notifyOptions))
		}
		matrixBody.WriteString(`</tr>`)
	}
	matrixHTML := `<div class="table-wrap handoff-matrix-wrap"><table class="data-table handoff-matrix"><thead>` + matrixHead.String() + `</thead><tbody>` + matrixBody.String() + `</tbody></table></div>`
	if len(products) == 0 || len(taskTypes) == 0 {
		hint := t(lang, "製品タイプを1つ以上登録すると、対応表の行が自動で並びます。", "Register at least one product type and the mapping rows appear automatically.")
		if len(products) > 0 {
			hint = t(lang, "Kitsuからタスクタイプを取得できませんでした。接続を確認してください。", "Could not read the Task Types from Kitsu. Check the connection.")
		}
		matrixHTML = `<p class="field-help">` + esc(hint) + `</p>`
	}

	var productInputs strings.Builder
	for _, product := range products {
		productInputs.WriteString(`<div class="handoff-product-chip"><input class="handoff-product-input" name="product_name" value="` + esc(product.name) + `" aria-label="` + esc(t(lang, "製品タイプ名", "Product type name")) + `">`)
		if unclassified[strings.ToLower(product.name)] {
			productInputs.WriteString(` ` + handoffUnclassifiedBadge(lang) + `<input type="hidden" name="product_unclassified" value="` + esc(product.name) + `">`)
		}
		productInputs.WriteString(`</div>`)
	}
	productInputs.WriteString(`<div class="handoff-product-chip handoff-product-chip-add"><input class="handoff-product-input handoff-product-input-add" name="product_name" value="" placeholder="` + esc(t(lang, "+ 製品タイプ名を追加", "+ Add a product type")) + `" aria-label="` + esc(t(lang, "製品タイプ名", "Product type name")) + `"></div>`)

	timingOptions := make([][2]string, 0, 3)
	for _, timing := range []string{model.HandoffTimingImmediate, model.HandoffTimingDigest, model.HandoffTimingMixed} {
		timingOptions = append(timingOptions, [2]string{timing, handoffTimingLabel(lang, timing)})
	}
	fallbackOptionsList := make([][2]string, 0, len(handoffFallbackOptions))
	for _, fallback := range handoffFallbackOptions {
		fallbackOptionsList = append(fallbackOptionsList, [2]string{fallback, handoffFallbackLabel(lang, fallback)})
	}

	form := `<form method="post" action="` + esc(withLang("/bot/admin/handoff", r)) + `" class="section-stack">` +
		`<input type="hidden" name="project" value="` + esc(productionID) + `">` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "製品タイプ", "Product types")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "この製作で使う製品タイプの名前を登録します。行は「タスクタイプ × 製品タイプ」で自動的に並びます。", "Register the product type names this Production uses. The rows are arranged automatically as Task Type × product type.")) + `</p>` +
		`<div class="handoff-products-wrap">` + productInputs.String() + `</div></section>` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "受け渡しの対応表", "Handoff mapping")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "行がタスクタイプ、列が製品タイプです。セルごとに、流れと通知先のタスクタイプを選びます。プリセットは一致する行にだけ入り、それ以外は棚置きです。", "Rows are Task Types and columns are product types. In each cell choose the flow and the Task Types to notify. The preset fills only matching rows; the rest default to shelving.")) + `</p>` +
		matrixHTML + `</section>` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "通知の設定", "Notification settings")) + `</h2>` +
		`<label class="field-label" for="handoff-timing">` + esc(t(lang, "通知タイミング", "Notification timing")) + `</label>` +
		forumSelect("notify_timing", current.Config.NotifyTiming, timingOptions, t(lang, "（選択）", "(choose)")) +
		`<label class="field-label" for="handoff-noassignee">` + esc(t(lang, "担当者なしの通知先", "Recipient when nobody is assigned")) + `</label>` +
		forumSelect("no_assignee", current.Config.NoAssignee, fallbackOptionsList, t(lang, "（選択）", "(choose)")) +
		`<label class="field-label" for="handoff-fallback-user">` + esc(t(lang, "指定ユーザーID", "Chosen user ID")) + `</label>` +
		`<input id="handoff-fallback-user" name="fallback_user" value="` + esc(current.Config.FallbackUserID) + `" autocomplete="off">` +
		`</section>` +
		`<div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(productionID)+"&tab=notifications", r)) + `">` + esc(t(lang, "通知設定に戻る", "Back to notifications")) + `</a> <button type="submit" class="btn">` + esc(t(lang, "保存", "Save")) + `</button></div></form>`

	headline := `<section class="section-card glass"><h2>` + esc(project.Name) + `</h2>`
	if presetsApplied > 0 {
		notices.WriteString(`<p class="field-help" role="status">` + esc(fmt.Sprintf(t(lang, "プリセットを%d行に適用しました。保存すると確定します。", "The preset is applied to %d rows. Save to keep it."), presetsApplied)) + `</p>`)
	}
	body := `<div class="section-stack">` + headline + `</section>` + errorHTML + notices.String() + form + handoffMatrixScript + `</div>`
	return adminPage(lang, t(lang, "受け渡しの対応表", "Handoff mapping"), r, body)
}

func handoffRouteKey(taskTypeID, product string) string {
	return strings.TrimSpace(taskTypeID) + "\x00" + strings.ToLower(strings.TrimSpace(product))
}

// handoffUnclassifiedBadge marks a product type the detector registered by
// itself and nobody has classified yet.
func handoffUnclassifiedBadge(lang string) string {
	return `<span class="status-pill warning">` + esc(t(lang, "未分類", "Unclassified")) + `</span>`
}

// handoffRouteCell is one matrix cell. It carries the same parallel fields the
// save handler reads (row_task / row_task_name / row_product / row_flow and the
// per-cell notify boxes), so saving works the same as before.
func handoffRouteCell(lang string, taskType kitsu.TaskType, product string, route model.ProductionHandoffRoute, notifyOptions [][2]string) string {
	label := taskType.Name + " × " + product
	flow := `<select name="row_flow" aria-label="` + esc(label+" — "+t(lang, "流れ", "Flow")) + `">`
	for _, option := range handoffFlowOptions {
		flow += `<option value="` + esc(option) + `"` + selectedOptionAttr(option == route.Flow) + `>` + esc(handoffFlowLabel(lang, option)) + `</option>`
	}
	if !handoffContains(handoffFlowOptions, route.Flow) {
		flow += `<option value="` + esc(route.Flow) + `" selected>` + esc(route.Flow) + `</option>`
	}
	flow += `</select>`
	selectedNotify := model.SplitHandoffNotifyIDs(route.NotifyTaskTypeIDs)
	var boxes strings.Builder
	var chosenNames []string
	found := map[string]struct{}{}
	name := "row_notify_" + taskType.ID + "|" + product
	for _, option := range notifyOptions {
		chosen := handoffContains(selectedNotify, option[0])
		if chosen {
			found[option[0]] = struct{}{}
			chosenNames = append(chosenNames, option[1])
		}
		boxes.WriteString(`<label class="checkbox-label"><input type="checkbox" name="` + esc(name) + `" value="` + esc(option[0]) + `"` + handoffChecked(chosen) + `> ` + esc(option[1]) + `</label>`)
	}
	for _, id := range selectedNotify { // saved but no longer listed by Kitsu
		if _, ok := found[id]; ok {
			continue
		}
		chosenNames = append(chosenNames, id)
		boxes.WriteString(`<label class="checkbox-label"><input type="checkbox" checked name="` + esc(name) + `" value="` + esc(id) + `"> ` + esc(id) + `</label>`)
	}
	viewHTML, viewTitle := handoffCellView(lang, route.Flow, chosenNames)

	editHTML := `<div class="handoff-edit" role="dialog" aria-modal="false" aria-label="` + esc(label) + `">` +
		`<div class="handoff-edit-header">` +
		`<span class="handoff-edit-title">` + esc(label) + `</span>` +
		`<button type="button" class="handoff-edit-close" aria-label="` + esc(t(lang, "閉じる", "Close")) + `">×</button>` +
		`</div>` +
		`<div class="handoff-edit-field">` +
		`<label class="field-label-sm">` + esc(t(lang, "流れ", "Flow")) + `</label>` +
		flow +
		`</div>` +
		`<div class="handoff-edit-field handoff-notify-group">` +
		`<label class="field-label-sm">` + esc(t(lang, "通知先", "Notify")) + `</label>` +
		`<div class="handoff-notify-list">` + boxes.String() + `</div>` +
		`</div>` +
		`<div class="handoff-edit-footer">` +
		`<button type="button" class="btn btn-sm handoff-edit-done">` + esc(t(lang, "完了", "Done")) + `</button>` +
		`</div>` +
		`</div>`

	return `<td class="handoff-cell" data-handoff-cell="` + esc(taskType.ID+"|"+product) + `" data-cell-label="` + esc(label) + `" data-flow="` + esc(route.Flow) + `">` +
		`<input type="hidden" name="row_task" value="` + esc(taskType.ID) + `">` +
		`<input type="hidden" name="row_task_name" value="` + esc(taskType.Name) + `">` +
		`<input type="hidden" name="row_product" value="` + esc(product) + `">` +
		`<div class="handoff-view" role="button" tabindex="0" aria-haspopup="dialog" aria-expanded="false" aria-label="` + esc(label+" — "+viewTitle) + `">` +
		viewHTML +
		`</div>` +
		editHTML +
		`</td>`
}

func handoffCellView(lang string, flow string, chosenNames []string) (string, string) {
	switch flow {
	case model.HandoffFlowHand:
		flowLabel := t(lang, "受け渡し", "Hand off")
		title := flowLabel
		targets := ""
		if len(chosenNames) > 0 {
			targets = " → " + strings.Join(chosenNames, ", ")
			title += targets
		}
		// The flow word never truncates; the destination list ellipsizes when the
		// column is narrow, and the title always carries the full list.
		return `<div class="handoff-chip handoff-chip-hand" title="` + esc(title) + `">` +
			`<span class="handoff-chip-flow">` + esc(flowLabel) + `</span>` +
			`<span class="handoff-chip-targets">` + esc(targets) + `</span></div>`, title
	case model.HandoffFlowSubmit:
		text := t(lang, "提出", "Submit")
		return `<div class="handoff-chip handoff-chip-submit" title="` + esc(text) + `"><span class="handoff-chip-flow">` + esc(text) + `</span></div>`, text
	case model.HandoffFlowShelve:
		fallthrough
	default:
		title := t(lang, "棚置き", "Shelve")
		return `<div class="handoff-dot" title="` + esc(title) + `">·</div>`, title
	}
}

const handoffMatrixScript = `<script>
(function() {
  var matrix = document.querySelector('.handoff-matrix');
  if (!matrix) return;

  var currentCell = null;

  function closeEditing() {
    if (!currentCell) return;
    currentCell.classList.remove('is-editing');
    var view = currentCell.querySelector('.handoff-view');
    if (view) view.setAttribute('aria-expanded', 'false');
    currentCell = null;
  }

  function openEditing(cell) {
    if (currentCell === cell) return;
    closeEditing();
    cell.classList.add('is-editing');
    currentCell = cell;
    var view = cell.querySelector('.handoff-view');
    if (view) view.setAttribute('aria-expanded', 'true');
    var select = cell.querySelector('select[name="row_flow"]');
    if (select) select.focus();
  }

  function updateCellView(cell) {
    var select = cell.querySelector('select[name="row_flow"]');
    if (!select) return;
    var flow = select.value;
    cell.setAttribute('data-flow', flow);

    var checkboxes = cell.querySelectorAll('input[type="checkbox"]:checked');
    var names = [];
    checkboxes.forEach(function(cb) {
      var label = cb.closest('label');
      if (label) {
        names.push(label.textContent.trim());
      }
    });

    var view = cell.querySelector('.handoff-view');
    if (!view) return;

    var lang = document.documentElement.lang || 'ja';
    var isJa = !lang.startsWith('en');

    var title = '';
    var html = '';
    var esc = function(value) {
      return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
    };

    if (flow === 'hand') {
      var flowLabel = isJa ? '受け渡し' : 'Hand off';
      var targets = names.length > 0 ? ' → ' + names.join(', ') : '';
      title = flowLabel + targets;
      html = '<div class="handoff-chip handoff-chip-hand" title="' + esc(title) + '">' +
        '<span class="handoff-chip-flow">' + esc(flowLabel) + '</span>' +
        '<span class="handoff-chip-targets">' + esc(targets) + '</span></div>';
    } else if (flow === 'submit') {
      title = isJa ? '提出' : 'Submit';
      html = '<div class="handoff-chip handoff-chip-submit" title="' + esc(title) + '"><span class="handoff-chip-flow">' + esc(title) + '</span></div>';
    } else {
      title = isJa ? '棚置き' : 'Shelve';
      html = '<div class="handoff-dot" title="' + esc(title) + '">·</div>';
    }

    view.innerHTML = html;
    var cellLabel = cell.getAttribute('data-cell-label') || '';
    view.setAttribute('aria-label', cellLabel + ' — ' + title);
  }

  matrix.addEventListener('click', function(e) {
    var closeBtn = e.target.closest('.handoff-edit-close');
    var doneBtn = e.target.closest('.handoff-edit-done');
    if (closeBtn || doneBtn) {
      e.preventDefault();
      e.stopPropagation();
      var cell = e.target.closest('.handoff-cell');
      closeEditing();
      if (cell) {
        var view = cell.querySelector('.handoff-view');
        if (view) view.focus();
      }
      return;
    }

    var editBox = e.target.closest('.handoff-edit');
    if (editBox) {
      return;
    }

    var cell = e.target.closest('.handoff-cell');
    if (cell) {
      e.preventDefault();
      openEditing(cell);
    }
  });

  matrix.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
      if (currentCell) {
        var cell = currentCell;
        closeEditing();
        var view = cell.querySelector('.handoff-view');
        if (view) view.focus();
      }
      return;
    }
    if (e.key === 'Enter' || e.key === ' ') {
      var view = e.target.closest('.handoff-view');
      if (view && !e.target.closest('.handoff-edit')) {
        e.preventDefault();
        var cell = view.closest('.handoff-cell');
        if (cell) openEditing(cell);
      }
    }
  });

  matrix.addEventListener('change', function(e) {
    var cell = e.target.closest('.handoff-cell');
    if (cell) {
      updateCellView(cell);
    }
  });

  document.addEventListener('click', function(e) {
    if (!currentCell) return;
    if (!currentCell.contains(e.target)) {
      closeEditing();
    }
  });
})();
</script>`

func handoffContains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func handoffChecked(checked bool) string {
	if checked {
		return " checked"
	}
	return ""
}

// renderHandoffSettingsLink adds the entry to the handoff page under the
// Production's notifications tab.
func renderHandoffSettingsLink(r *http.Request, p model.Project, lang string) string {
	return `<section class="section-card glass"><h2>` + esc(t(lang, "素材の受け渡し", "Material handoff")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "製品タイプごとに、受け渡し先のタスクタイプと流れを決める対応表を設定します。", "Configure the mapping of flows and notified Task Types for each product type.")) + `</p>` +
		`<div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/handoff?project="+url.QueryEscape(p.KitsuProjectID), r)) + `">` + esc(t(lang, "受け渡しの対応表を開く", "Open handoff mapping")) + `</a></div></section>`
}

func handoffSaveErrorMarkup(lang string, err error) string {
	var settingsErr *model.HandoffSettingsError
	if !errors.As(err, &settingsErr) {
		return `<p>` + esc(t(lang, "設定を保存できませんでした。しばらくしてからもう一度お試しください。", "The settings could not be stored. Please try again in a moment.")) + `</p>`
	}
	var b strings.Builder
	b.WriteString(`<ul>`)
	for _, problem := range settingsErr.Problems {
		b.WriteString(`<li>` + esc(handoffProblemText(lang, problem)) + `</li>`)
	}
	b.WriteString(`</ul>`)
	return b.String()
}

func handoffProblemText(lang string, problem model.HandoffSettingsProblem) string {
	arg := func(i int) string {
		if i < len(problem.Args) {
			return problem.Args[i]
		}
		return ""
	}
	switch problem.Code {
	case model.ProblemHandoffProductionRequired:
		return t(lang, "プロダクションを特定できません。画面を開き直してください。", "The Production could not be identified. Reopen this page.")
	case model.ProblemHandoffProductTypeRequired:
		return t(lang, "製品タイプの名前が空です。", "A product type name is empty.")
	case model.ProblemHandoffProductTypeDuplicate:
		return t(lang, "同じ製品タイプが複数回登録されています: ", "The same product type is registered more than once: ") + arg(0)
	case model.ProblemHandoffRouteIncomplete:
		return t(lang, "タスクタイプと製品タイプの組み合わせが不完全です。", "A Task Type / product type pair is incomplete.")
	case model.ProblemHandoffRouteProductUnknown:
		return t(lang, "登録されていない製品タイプが使われています（タスクタイプ: ", "A row uses a product type that is not registered (Task Type: ") + arg(0) + t(lang, "、製品タイプ: ", ", product type: ") + arg(1) + t(lang, "）。", ").")
	case model.ProblemHandoffRouteDuplicate:
		return t(lang, "同じ組み合わせが複数回設定されています（タスクタイプ: ", "The same pair is set more than once (Task Type: ") + arg(0) + t(lang, "、製品タイプ: ", ", product type: ") + arg(1) + t(lang, "）。", ").")
	case model.ProblemHandoffFlowInvalid:
		return t(lang, "流れが選ばれていない行があります（タスクタイプ: ", "A row has no flow (Task Type: ") + arg(0) + t(lang, "、製品タイプ: ", ", product type: ") + arg(1) + t(lang, "）。", ").")
	case model.ProblemHandoffNotifyInvalid:
		return t(lang, "通知先タスクタイプの指定が正しくありません（タスクタイプ: ", "A notified Task Type is not valid (Task Type: ") + arg(0) + t(lang, "、製品タイプ: ", ", product type: ") + arg(1) + t(lang, "）。", ").")
	case model.ProblemHandoffTimingInvalid:
		return t(lang, "通知タイミングを選んでください。", "Choose a notification timing.")
	case model.ProblemHandoffFallbackInvalid:
		return t(lang, "担当者なしの通知先を選んでください。", "Choose who is notified when nobody is assigned.")
	case model.ProblemHandoffFallbackUserRequired:
		return t(lang, "「指定したユーザーに通知」を選んだ場合は、ユーザーIDを入力してください。", "Choosing a user for no-assignee notifications needs that user's ID.")
	}
	return t(lang, "設定に問題があります。", "There is a problem with the settings.")
}

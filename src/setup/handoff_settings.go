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
	{"Rendering", "render", model.HandoffFlowHand, []string{"Compositing"}},
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
		return t(lang, "管理者ロールに通知", "Notify the admin role")
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
		settings.Routes = append(settings.Routes, route)
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
	for _, product := range current.ProductTypes {
		addProduct(product.Name)
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

	// The table: the Production's Task Types in Kitsu order, then the registered
	// product types. Every cell is a select; nothing is typed per row.
	var rowTable strings.Builder
	presetsApplied := 0
	for _, taskType := range taskTypes {
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
			rowTable.WriteString(handoffRouteRow(lang, taskType, product.name, route, notifyOptions))
		}
	}
	if len(products) == 0 || len(taskTypes) == 0 {
		hint := t(lang, "製品タイプを1つ以上登録すると、対応表の行が自動で並びます。", "Register at least one product type and the mapping rows appear automatically.")
		if len(products) > 0 {
			hint = t(lang, "Kitsuからタスクタイプを取得できませんでした。接続を確認してください。", "Could not read the Task Types from Kitsu. Check the connection.")
		}
		rowTable.WriteString(`<tr><td colspan="3"><p class="field-help">` + esc(hint) + `</p></td></tr>`)
	}

	var productInputs strings.Builder
	for _, product := range products {
		productInputs.WriteString(`<li class="field-row"><input name="product_name" value="` + esc(product.name) + `" aria-label="` + esc(t(lang, "製品タイプ名", "Product type name")) + `"></li>`)
	}
	productInputs.WriteString(`<li class="field-row"><input name="product_name" value="" placeholder="` + esc(t(lang, "製品タイプ名を追加", "Add a product type")) + `" aria-label="` + esc(t(lang, "製品タイプ名", "Product type name")) + `"></li>`)

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
		`<ul class="field-list">` + productInputs.String() + `</ul></section>` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "受け渡しの対応表", "Handoff mapping")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "出す側のタスクタイプと製品タイプごとに、流れと通知先のタスクタイプを選びます。プリセットは一致する行にだけ入り、それ以外は棚置きです。", "For each source Task Type and product type, choose the flow and the Task Types to notify. The preset fills only matching rows; the rest default to shelving.")) + `</p>` +
		`<table class="data-table forum-map-table"><thead><tr><th scope="col">` + esc(t(lang, "タスクタイプ × 製品タイプ", "Task Type × product type")) + `</th><th scope="col">` + esc(t(lang, "流れ", "Flow")) + `</th><th scope="col">` + esc(t(lang, "通知先タスクタイプ", "Notify Task Types")) + `</th></tr></thead>` +
		`<tbody>` + rowTable.String() + `</tbody></table></section>` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "通知の設定", "Notification settings")) + `</h2>` +
		`<label class="field-label" for="handoff-timing">` + esc(t(lang, "通知タイミング", "Notification timing")) + `</label>` +
		forumSelect("notify_timing", current.Config.NotifyTiming, timingOptions, t(lang, "（選択）", "(choose)")) +
		`<label class="field-label" for="handoff-noassignee">` + esc(t(lang, "担当者なしの通知先", "Recipient when nobody is assigned")) + `</label>` +
		forumSelect("no_assignee", current.Config.NoAssignee, fallbackOptionsList, t(lang, "（選択）", "(choose)")) +
		`<label class="field-label" for="handoff-fallback-user">` + esc(t(lang, "指定ユーザーID", "Chosen user ID")) + `</label>` +
		`<input id="handoff-fallback-user" name="fallback_user" value="` + esc(current.Config.FallbackUserID) + `" autocomplete="off">` +
		`</section>` +
		`<div class="button-row"><button type="submit" class="btn-ghost">` + esc(t(lang, "保存", "Save")) + `</button> <a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(productionID)+"&tab=notifications", r)) + `">` + esc(t(lang, "通知設定に戻る", "Back to notifications")) + `</a></div></form>`

	headline := `<section class="section-card glass"><h2>` + esc(project.Name) + `</h2>`
	if presetsApplied > 0 {
		notices.WriteString(`<p class="field-help" role="status">` + esc(fmt.Sprintf(t(lang, "プリセットを%d行に適用しました。保存すると確定します。", "The preset is applied to %d rows. Save to keep it."), presetsApplied)) + `</p>`)
	}
	body := `<div class="section-stack">` + headline + `</section>` + errorHTML + notices.String() + form + `</div>`
	return adminPage(lang, t(lang, "受け渡しの対応表", "Handoff mapping"), r, body)
}

func handoffRouteKey(taskTypeID, product string) string {
	return strings.TrimSpace(taskTypeID) + "\x00" + strings.ToLower(strings.TrimSpace(product))
}

func handoffRouteRow(lang string, taskType kitsu.TaskType, product string, route model.ProductionHandoffRoute, notifyOptions [][2]string) string {
	flowOptions := make([][2]string, 0, len(handoffFlowOptions))
	for _, flow := range handoffFlowOptions {
		flowOptions = append(flowOptions, [2]string{flow, handoffFlowLabel(lang, flow)})
	}
	selectedNotify := model.SplitHandoffNotifyIDs(route.NotifyTaskTypeIDs)
	var notify strings.Builder
	notify.WriteString(`<div>`)
	found := map[string]struct{}{}
	for _, option := range notifyOptions {
		chosen := handoffContains(selectedNotify, option[0])
		if chosen {
			found[option[0]] = struct{}{}
		}
		notify.WriteString(`<label class="checkbox-label"><input type="checkbox" name="row_notify_` + esc(taskType.ID+"|"+product) + `" value="` + esc(option[0]) + `"` + handoffChecked(chosen) + `> ` + esc(option[1]) + `</label>`)
	}
	for _, id := range selectedNotify { // saved but no longer listed by Kitsu
		if _, ok := found[id]; ok {
			continue
		}
		notify.WriteString(`<label class="checkbox-label"><input type="checkbox" checked name="row_notify_` + esc(taskType.ID+"|"+product) + `" value="` + esc(id) + `> ` + esc(id) + `</label>`)
	}
	notify.WriteString(`</div>`)
	return `<tr><td>` + esc(taskType.Name) + ` × ` + esc(product) +
		`<input type="hidden" name="row_task" value="` + esc(taskType.ID) + `">` +
		`<input type="hidden" name="row_task_name" value="` + esc(taskType.Name) + `">` +
		`<input type="hidden" name="row_product" value="` + esc(product) + `"></td><td>` +
		forumSelect("row_flow", route.Flow, flowOptions, t(lang, "（選択）", "(choose)")) + `</td><td>` + notify.String() + `</td></tr>`
}

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

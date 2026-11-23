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

var handoffFallbackOptions = []string{model.HandoffFallbackAdmin, model.HandoffFallbackUsers, model.HandoffFallbackDisabled}

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
// the no-handoff mark, the receiving Task Type names, or "no wiring".
func routeSelectionSummary(joined string, namesByID map[string]string, lang string) string {
	if joined == model.TaskRouteNone {
		return t(lang, "受け渡しなし", "No handoff")
	}
	var names []string
	for _, id := range model.SplitHandoffNotifyIDs(joined) {
		if name := namesByID[id]; name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return t(lang, "配線なし", "No wiring")
	}
	return strings.Join(names, ", ")
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

// renderStudioRoutesSection is the studio-common wiring editor on the
// connection settings page, beside the status -> role mapping: both are
// studio-wide notification design, set once.
func renderStudioRoutesSection(db *gorm.DB, lang string, r *http.Request) string {
	taskTypes := studioTaskTypes(db)
	if len(taskTypes) == 0 {
		return ""
	}
	namesByID := taskTypeNamesByID(taskTypes)
	studioByFrom := map[string]model.StudioTaskRoute{}
	if routes, err := model.LoadStudioTaskRoutes(db); err == nil {
		for _, route := range routes {
			studioByFrom[route.FromTaskTypeID] = route
		}
	}
	var rows strings.Builder
	for _, taskType := range taskTypes {
		studio := studioByFrom[taskType.ID]
		studioNone := studio.NotifyTaskTypeIDs == model.TaskRouteNone
		studioSelected := model.SplitHandoffNotifyIDs(studio.NotifyTaskTypeIDs)
		if studioNone {
			studioSelected = nil
		}
		noneChecked := ""
		if studioNone {
			noneChecked = " checked"
		}
		rows.WriteString(`<tr><th scope="row">` + esc(taskType.Name) + `</th><td>` +
			`<details class="handoff-route-details"><summary>` + esc(routeSelectionSummary(studio.NotifyTaskTypeIDs, namesByID, lang)) + `</summary>` +
			`<div class="handoff-route-options">` + routeTargetCheckboxes("st_notify_", taskType.ID, taskTypes, studioSelected) +
			`<label class="checkbox-label"><input type="checkbox" name="st_none_` + esc(taskType.ID) + `" value="1"` + noneChecked + `> ` + esc(t(lang, "受け渡しなし", "No handoff")) + `</label></div></details></td></tr>`)
	}
	return `<form method="post" action="` + esc(withLang("/bot/admin/bot?edit=1", r)) + `" class="section-stack">` +
		`<input type="hidden" name="action" value="save_studio_routes">` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "受け渡しの配線（スタジオ共通）", "Handoff wiring (studio default)")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "タスクが完了（Done系）になったとき、同じショット/アセットのどのタスクに「揃った、進めていい」と伝えるかです。全製作の既定になります。", "When a task finishes (Done nature), which tasks on the same shot/asset are told they can proceed. This is the default for every Production.")) + `</p>` +
		`<table class="data-table handoff-routes"><tbody>` + rows.String() + `</tbody></table>` +
		`<div class="button-row"><button type="submit" class="btn">` + esc(t(lang, "保存", "Save")) + `</button></div></section></form>`
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
		inherited := t(lang, "（継承：", "(inherited: ")
		if route, ok := studioByFrom[taskType.ID]; ok {
			inherited += routeSelectionSummary(route.NotifyTaskTypeIDs, namesByID, lang)
		} else {
			inherited += t(lang, "配線なし", "no wiring")
		}
		inherited += ")"
		modeOptions := [][2]string{
			{"inherit", t(lang, "スタジオ共通を継承", "Inherit the studio default")},
			{"none", t(lang, "この製作では受け渡しなし", "No handoff in this Production")},
			{"custom", t(lang, "この製作だけ指定", "Override for this Production")},
		}
		// The target editor is folded away unless this row overrides: in
		// inherit and none modes there is nothing to pick.
		detailsOpen := ""
		summary := t(lang, "指定なし", "Not set")
		if mode == "custom" {
			detailsOpen = " open"
			summary = routeSelectionSummary(override.NotifyTaskTypeIDs, namesByID, lang)
		} else if mode == "none" {
			summary = routeSelectionSummary(model.TaskRouteNone, namesByID, lang)
		}
		overrideRows.WriteString(`<tr><th scope="row">` + esc(taskType.Name) + `</th><td>` +
			forumSelect("ov_mode_"+taskType.ID, mode, modeOptions, "") +
			`<p class="field-help">` + esc(inherited) + `</p>` +
			`<details class="handoff-route-details"` + detailsOpen + `><summary>` + esc(summary) + `</summary>` +
			`<div class="handoff-route-options">` + routeTargetCheckboxes("ov_notify_", taskType.ID, taskTypes, overrideSelected) + `</div></details></td></tr>`)
	}

	fallbackOptionsList := make([][2]string, 0, len(handoffFallbackOptions))
	for _, fallback := range handoffFallbackOptions {
		fallbackOptionsList = append(fallbackOptionsList, [2]string{fallback, handoffFallbackLabel(lang, fallback)})
	}

	studioNote := `<section class="section-card glass"><h2>` + esc(t(lang, "受け渡しの配線（スタジオ共通）", "Handoff wiring (studio default)")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "スタジオ共通の配線は接続設定で編集します。このページでは、この製作だけの上書きと担当者なしの扱いを設定します。", "The studio-default wiring is edited in Connection settings. This page sets only this Production's overrides and the no-assignee handling.")) + `</p>` +
		`<div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot?edit=1", r)) + `">` + esc(t(lang, "接続設定を開く", "Open connection settings")) + `</a></div></section>`

	form := `<form method="post" action="` + esc(withLang("/bot/admin/handoff", r)) + `" class="section-stack">` +
		`<input type="hidden" name="project" value="` + esc(productionID) + `">` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "この製作の上書き", "Overrides for this Production")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "通常は継承のままで構いません。この製作だけ配線を変える行だけ指定します。", "Leave rows inherited unless this Production wires differently.")) + `</p>` +
		`<table class="data-table handoff-routes"><tbody>` + overrideRows.String() + `</tbody></table></section>` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "担当者なしの扱い", "When nobody is assigned")) + `</h2>` +
		`<label class="field-label" for="handoff-noassignee">` + esc(t(lang, "担当者なしの通知先", "Recipient when nobody is assigned")) + `</label>` +
		forumSelect("no_assignee", config.NoAssignee, fallbackOptionsList, t(lang, "（選択）", "(choose)")) +
		`<label class="field-label" for="handoff-fallback-user">` + esc(t(lang, "指定ユーザーID", "Chosen user ID")) + `</label>` +
		`<input id="handoff-fallback-user" name="fallback_user" value="` + esc(config.FallbackUserID) + `" autocomplete="off">` +
		`</section>` +
		`<div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(productionID)+"&tab=notifications", r)) + `">` + esc(t(lang, "通知設定に戻る", "Back to notifications")) + `</a> <button type="submit" class="btn">` + esc(t(lang, "保存", "Save")) + `</button></div></form>`

	body := `<div class="section-stack"><section class="section-card glass"><h2>` + esc(project.Name) + `</h2></section>` + errorHTML + notices.String() + studioNote + form + `</div>`
	return adminPage(lang, t(lang, "受け渡しの配線", "Handoff wiring"), r, body)
}

// renderHandoffSettingsLink is the production page's entry to the wiring
// editor.
func renderHandoffSettingsLink(r *http.Request, project model.Project, lang string) string {
	return `<section class="section-card glass"><h2>` + esc(t(lang, "素材の受け渡し", "Material handoff")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "タスクが完了になったとき、同じショット/アセットのどのタスクへ知らせるかを設定します（スタジオ共通＋この製作の上書き）。", "Choose which tasks on the same shot/asset are told when a task finishes (studio defaults plus this Production's overrides).")) + `</p>` +
		`<div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/handoff?project="+url.QueryEscape(project.KitsuProjectID), r)) + `">` + esc(t(lang, "受け渡しの対応表を開く", "Open handoff mapping")) + `</a></div></section>`
}

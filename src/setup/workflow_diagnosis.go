package setup

import (
	"app/src/model"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const workflowDiagnosisRoute = "/bot/admin/workflow-diagnosis"

type workflowDiagnosisClient struct {
	host   string
	token  string
	client *http.Client
}

type workflowProduction struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type workflowTaskType struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ShortName    string `json:"short_name"`
	ForEntity    string `json:"for_entity"`
	DepartmentID string `json:"department_id"`
}

type workflowTaskStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ShortName    string `json:"short_name"`
	IsDone       bool   `json:"is_done"`
	IsRetake     bool   `json:"is_retake"`
	IsFeedback   bool   `json:"is_feedback_request"`
	IsWIP        bool   `json:"is_wip"`
	IsReviewable bool   `json:"is_reviewable"`
}

type workflowEntityType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type workflowDepartment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type workflowAssetType struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

type workflowDiagnosisData struct {
	Lang                   string
	Production             *workflowProduction
	Disconnected           bool
	Error                  string
	SectionErrors          []string
	GlobalTaskTypes        []workflowTaskType
	ProductionTaskTypes    []workflowTaskType
	GlobalTaskStatuses     []workflowTaskStatus
	ProductionTaskStatuses []workflowTaskStatus
	EntityTypes            []workflowEntityType
	ProductionAssetTypes   []workflowAssetType
	Departments            []workflowDepartment
	TemplateEntries        []workflowTemplateDiagnosis
	ActualEntries          []workflowActualDiagnosis
	Summary                workflowDiagnosisSummary
	RoutingNameBased       bool
}

type workflowTemplateDiagnosis struct {
	ExpectedTaskType  string
	ExpectedChannel   string
	GlobalMatches     []workflowTaskType
	ProductionMatches []workflowTaskType
	EntityScope       string
	Department        string
	CurrentChannels   []string
	StableID          string
	Classification    string
	SimilarCandidates []string
}

type workflowActualDiagnosis struct {
	TaskType        workflowTaskType
	Department      string
	CurrentChannels []string
	TemplateRefs    []string
}

type workflowDiagnosisSummary struct {
	ProductionTaskTypes int
	UniqueResolved      int
	Routed              int
	Unrouted            int
	MissingTemplate     int
	Ambiguous           int
	NotifiableStatuses  int
}

type workflowDiagnosisStatus struct {
	Status     workflowTaskStatus
	Notifiable bool
}

func newWorkflowDiagnosisClient(host, token string) *workflowDiagnosisClient {
	return &workflowDiagnosisClient{
		host:   strings.TrimRight(strings.TrimSpace(host), "/"),
		token:  strings.TrimSpace(token),
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *workflowDiagnosisClient) get(path string, target interface{}) error {
	if c == nil || c.host == "" || c.token == "" {
		return errors.New("Kitsu runtime is disconnected; reconnect is required")
	}
	req, err := http.NewRequest(http.MethodGet, c.host+path, nil)
	if err != nil {
		return errors.New("could not prepare the Kitsu read request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return errors.New("Kitsu read request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Kitsu read request returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return errors.New("Kitsu returned an unreadable response")
	}
	return nil
}

func (c *workflowDiagnosisClient) load(projectID string) (workflowDiagnosisData, error) {
	data := workflowDiagnosisData{RoutingNameBased: true}
	var productions []workflowProduction
	if err := c.get("/api/data/projects", &productions); err != nil {
		return data, err
	}
	if projectID == "" {
		if len(productions) != 1 {
			if len(productions) == 0 {
				return data, errors.New("no Kitsu Production was found")
			}
			return data, errors.New("multiple Kitsu Productions were found; select one explicitly")
		}
		projectID = productions[0].ID
	}
	for _, production := range productions {
		if production.ID == projectID {
			p := production
			data.Production = &p
			break
		}
	}
	if data.Production == nil {
		return data, errors.New("the selected Kitsu Production was not found")
	}

	var globalTaskTypes []workflowTaskType
	var globalStatuses []workflowTaskStatus
	var entityTypes []workflowEntityType
	var departments []workflowDepartment
	var productionTaskTypes []workflowTaskType
	var productionStatuses []workflowTaskStatus
	var productionAssetTypes []workflowAssetType
	data.GlobalTaskTypes, data.GlobalTaskStatuses = globalTaskTypes, globalStatuses
	data.EntityTypes, data.Departments = entityTypes, departments
	if err := c.get("/api/data/task-types", &globalTaskTypes); err != nil {
		data.SectionErrors = append(data.SectionErrors, "Global Task Types: "+err.Error())
	}
	if err := c.get("/api/data/task-status", &globalStatuses); err != nil {
		data.SectionErrors = append(data.SectionErrors, "Global Task Statuses: "+err.Error())
	}
	if err := c.get("/api/data/entity-types", &entityTypes); err != nil {
		data.SectionErrors = append(data.SectionErrors, "Entity Types: "+err.Error())
	}
	if err := c.get("/api/data/departments", &departments); err != nil {
		data.SectionErrors = append(data.SectionErrors, "Departments: "+err.Error())
	}
	if err := c.get("/api/data/projects/"+url.PathEscape(projectID)+"/task-types", &productionTaskTypes); err != nil {
		data.SectionErrors = append(data.SectionErrors, "Production Task Types: "+err.Error())
	}
	if err := c.get("/api/data/projects/"+url.PathEscape(projectID)+"/settings/task-status", &productionStatuses); err != nil {
		data.SectionErrors = append(data.SectionErrors, "Production Task Statuses: "+err.Error())
	}
	if err := c.get("/api/data/user/projects/"+url.PathEscape(projectID)+"/asset-types", &productionAssetTypes); err != nil {
		data.SectionErrors = append(data.SectionErrors, "Production Asset Types: "+err.Error())
	}
	data.GlobalTaskTypes = globalTaskTypes
	data.GlobalTaskStatuses = globalStatuses
	data.EntityTypes = entityTypes
	data.Departments = departments
	data.ProductionTaskTypes = productionTaskTypes
	data.ProductionTaskStatuses = productionStatuses
	data.ProductionAssetTypes = productionAssetTypes
	return data, nil
}

func WorkflowDiagnosisHandler(db *gorm.DB, credentials func() (string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lang := currentLang(r)
		host, token := credentials()
		data := workflowDiagnosisData{Lang: lang}
		if strings.TrimSpace(host) == "" || strings.TrimSpace(token) == "" {
			data.Disconnected = true
			data.Error = "Kitsu runtime is disconnected; reconnect is required before diagnosis can run."
		} else {
			loaded, err := newWorkflowDiagnosisClient(host, token).load(strings.TrimSpace(r.URL.Query().Get("project")))
			loaded.Lang = lang
			if err != nil {
				data = loaded
				data.Error = err.Error()
			} else {
				data = loaded
				buildWorkflowDiagnosis(&data, db)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, adminPage(lang, "Workflow Diagnosis", r, renderWorkflowDiagnosis(data, r)))
	}
}

func buildWorkflowDiagnosis(data *workflowDiagnosisData, db *gorm.DB) {
	if data == nil || data.Production == nil {
		return
	}
	projectID := data.Production.ID
	var routes []model.ProjectWebhook
	if db != nil {
		routes = model.ListProjectWebhooks(db, projectID)
	}
	data.TemplateEntries = buildWorkflowTemplateDiagnosis(data.GlobalTaskTypes, data.ProductionTaskTypes, routes)
	templateRefs := make(map[string][]string)
	for _, entry := range data.TemplateEntries {
		templateRefs[strings.ToLower(entry.ExpectedTaskType)] = append(templateRefs[strings.ToLower(entry.ExpectedTaskType)], entry.ExpectedChannel)
	}
	departmentNames := make(map[string]string)
	for _, department := range data.Departments {
		departmentNames[department.ID] = department.Name
	}
	for _, taskType := range data.ProductionTaskTypes {
		data.ActualEntries = append(data.ActualEntries, workflowActualDiagnosis{
			TaskType:        taskType,
			Department:      departmentNames[taskType.DepartmentID],
			CurrentChannels: routeChannels(routes, taskType.Name),
			TemplateRefs:    uniqueStrings(templateRefs[strings.ToLower(taskType.Name)]),
		})
	}
	data.Summary.ProductionTaskTypes = len(data.ProductionTaskTypes)
	for _, entry := range data.TemplateEntries {
		switch entry.Classification {
		case "Ready":
			data.Summary.UniqueResolved++
			data.Summary.Routed++
		case "Unrouted":
			data.Summary.UniqueResolved++
			data.Summary.Unrouted++
		case "Missing":
			data.Summary.MissingTemplate++
		case "Ambiguous":
			data.Summary.Ambiguous++
		}
	}
	for _, status := range data.ProductionTaskStatuses {
		if workflowStatusNotifiable(status.ShortName) {
			data.Summary.NotifiableStatuses++
		}
	}
}

func buildWorkflowTemplateDiagnosis(global, production []workflowTaskType, routes []model.ProjectWebhook) []workflowTemplateDiagnosis {
	template, ok := Templates["cg"]
	if !ok {
		return nil
	}
	result := make([]workflowTemplateDiagnosis, 0, len(template.Channels))
	for _, entry := range template.Channels {
		globalMatches := exactWorkflowTaskTypes(global, entry.TaskType)
		productionMatches := exactWorkflowTaskTypes(production, entry.TaskType)
		classification := "Missing"
		switch {
		case len(globalMatches) > 1 || len(productionMatches) > 1:
			classification = "Ambiguous"
		case len(productionMatches) == 1:
			if len(routeChannels(routes, entry.TaskType)) > 0 {
				classification = "Ready"
			} else {
				classification = "Unrouted"
			}
		case len(globalMatches) > 0:
			classification = "Global only"
		}
		entityScope, department := "", ""
		if len(productionMatches) == 1 {
			entityScope = productionMatches[0].ForEntity
		}
		if len(globalMatches) == 1 {
			entityScope = fallbackString(entityScope, globalMatches[0].ForEntity)
		}
		result = append(result, workflowTemplateDiagnosis{
			ExpectedTaskType:  entry.TaskType,
			ExpectedChannel:   entry.Name("en"),
			GlobalMatches:     globalMatches,
			ProductionMatches: productionMatches,
			EntityScope:       entityScope,
			Department:        department,
			CurrentChannels:   routeChannels(routes, entry.TaskType),
			StableID:          uniqueWorkflowID(productionMatches),
			Classification:    classification,
			SimilarCandidates: similarWorkflowNames(entry.TaskType, global),
		})
	}
	return result
}

func exactWorkflowTaskTypes(taskTypes []workflowTaskType, name string) []workflowTaskType {
	result := make([]workflowTaskType, 0)
	for _, taskType := range taskTypes {
		if strings.EqualFold(strings.TrimSpace(taskType.Name), strings.TrimSpace(name)) {
			result = append(result, taskType)
		}
	}
	return result
}

func routeChannels(routes []model.ProjectWebhook, taskType string) []string {
	channels := make([]string, 0)
	for _, route := range routes {
		if strings.EqualFold(strings.TrimSpace(route.TaskType), strings.TrimSpace(taskType)) {
			channels = append(channels, route.ChannelName)
		}
	}
	return uniqueStrings(channels)
}

func similarWorkflowNames(name string, taskTypes []workflowTaskType) []string {
	normalized := normalizeWorkflowName(name)
	result := make([]string, 0)
	for _, taskType := range taskTypes {
		candidate := normalizeWorkflowName(taskType.Name)
		if candidate != normalized && (strings.Contains(candidate, normalized) || strings.Contains(normalized, candidate) || workflowNameDistance(normalized, candidate) <= 2) {
			result = append(result, taskType.Name)
		}
	}
	return uniqueStrings(result)
}

func normalizeWorkflowName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "-", "")
	return value
}

func workflowNameDistance(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, ra := range a {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j, rb := range b {
			cost := 0
			if ra != rb {
				cost = 1
			}
			current[j+1] = minWorkflowInt(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func workflowStatusNotifiable(shortName string) bool {
	switch strings.ToLower(strings.TrimSpace(shortName)) {
	case "wfa", "retake", "done":
		return true
	default:
		return false
	}
}

func renderWorkflowDiagnosis(data workflowDiagnosisData, r *http.Request) string {
	var out strings.Builder
	out.WriteString(`<div class="section-stack">`)
	out.WriteString(`<div class="section-card glass"><h2>` + esc(tr(data.Lang, "workflow.title")) + `</h2><p class="hint">` + esc(tr(data.Lang, "workflow.read_only")) + `</p>`)
	if data.Disconnected {
		out.WriteString(`<div class="status-pill warn">` + esc(tr(data.Lang, "workflow.disconnected")) + `</div>`)
		out.WriteString(`</div></div>`)
		return out.String()
	}
	if data.Error != "" {
		out.WriteString(`<div class="status-pill bad">` + esc(data.Error) + `</div>`)
	}
	if data.Production != nil {
		out.WriteString(`<p><strong>Production:</strong> ` + esc(data.Production.Name) + ` <code>` + esc(data.Production.ID) + `</code></p>`)
	}
	if len(data.SectionErrors) > 0 {
		out.WriteString(`<div class="status-pill warn">` + esc(strings.Join(data.SectionErrors, " | ")) + `</div>`)
	}
	out.WriteString(`</div>`)
	if data.Production == nil {
		out.WriteString(`</div>`)
		return out.String()
	}

	out.WriteString(renderWorkflowSummary(data.Lang, data.Summary))
	out.WriteString(renderWorkflowTemplateSection(data))
	out.WriteString(renderWorkflowActualSection(data))
	out.WriteString(renderWorkflowStatusSection(data))
	out.WriteString(renderWorkflowReferenceSection(data))
	out.WriteString(`<p class="hint"><a href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(data.Production.ID), r)) + `">` + esc(tr(data.Lang, "workflow.back")) + `</a></p></div>`)
	return out.String()
}

func renderWorkflowSummary(lang string, summary workflowDiagnosisSummary) string {
	return fmt.Sprintf(`<div class="section-card glass"><h3>%s</h3><div class="metric-grid"><div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div><div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div><div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div><div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div><div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div><div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div><div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div></div><p class="hint">%s</p></div>`, esc(tr(lang, "workflow.summary")), esc(t(lang, "Production Task Types", "Production Task Types")), summary.ProductionTaskTypes, esc(t(lang, "解決済み", "Unique resolved")), summary.UniqueResolved, esc(t(lang, "ルート済み", "Routed")), summary.Routed, esc(t(lang, "未ルート", "Unrouted")), summary.Unrouted, esc(t(lang, "template 不足", "Missing template entries")), summary.MissingTemplate, esc(t(lang, "曖昧な項目", "Ambiguous entries")), summary.Ambiguous, esc(t(lang, "通知対象 status", "Notifiable statuses")), summary.NotifiableStatuses, esc(t(lang, "現在の routing は名前ベースです。複数の Task Type が 1 つの Discord channel を共有する場合があります。", "Current routing is name-based. Multiple Task Types may share one Discord channel.")))
}

func renderWorkflowTemplateSection(data workflowDiagnosisData) string {
	var rows strings.Builder
	for _, entry := range data.TemplateEntries {
		rows.WriteString(`<tr><td>` + esc(entry.ExpectedTaskType) + `</td><td>` + esc(entry.ExpectedChannel) + `</td><td>` + esc(fmt.Sprintf("global=%d / production=%d", len(entry.GlobalMatches), len(entry.ProductionMatches))) + `</td><td>` + esc(entry.EntityScope) + `</td><td>` + esc(strings.Join(entry.CurrentChannels, ", ")) + `</td><td>` + esc(entry.StableID) + `</td><td><span class="status-pill ` + workflowClass(entry.Classification) + `">` + esc(entry.Classification) + `</span>` + renderSimilar(entry.SimilarCandidates) + `</td></tr>`)
	}
	return `<div class="section-card glass"><h3>` + esc(tr(data.Lang, "workflow.template")) + `</h3><p class="hint">` + esc(tr(data.Lang, "workflow.similar_names")) + `</p><div style="overflow:auto"><table><thead><tr><th>` + esc(t(data.Lang, "Expected Task Type", "Expected Task Type")) + `</th><th>` + esc(t(data.Lang, "Expected Discord channel", "Expected Discord channel")) + `</th><th>` + esc(t(data.Lang, "Kitsu matches", "Kitsu matches")) + `</th><th>Entity scope</th><th>Current routing</th><th>Stable ID</th><th>Classification</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div></div>`
}

func renderWorkflowActualSection(data workflowDiagnosisData) string {
	var rows strings.Builder
	for _, entry := range data.ActualEntries {
		channel := strings.Join(entry.CurrentChannels, ", ")
		if channel == "" {
			channel = t(data.Lang, "未割り当て", "Unassigned")
		}
		refs := strings.Join(entry.TemplateRefs, ", ")
		if refs == "" {
			refs = t(data.Lang, "cg template に未参照", "Not referenced by cg template")
		}
		rows.WriteString(`<tr><td>` + esc(entry.TaskType.Name) + `</td><td><code>` + esc(entry.TaskType.ID) + `</code></td><td>` + esc(entry.TaskType.ForEntity) + `</td><td>` + esc(entry.Department) + `</td><td>` + esc(channel) + `</td><td>` + esc(refs) + `</td></tr>`)
	}
	return `<div class="section-card glass"><h3>` + esc(t(data.Lang, "Production Task Types", "Production Task Types")) + `</h3><div style="overflow:auto"><table><thead><tr><th>` + esc(t(data.Lang, "名前", "Name")) + `</th><th>Stable ID</th><th>` + esc(t(data.Lang, "対象", "Scope")) + `</th><th>` + esc(t(data.Lang, "Department", "Department")) + `</th><th>Discord routing</th><th>cg template reference</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div>` + renderProductionAssets(data) + `</div>`
}

func renderProductionAssets(data workflowDiagnosisData) string {
	var rows strings.Builder
	for _, asset := range data.ProductionAssetTypes {
		rows.WriteString(`<li>` + esc(asset.Name) + ` <code>` + esc(asset.ID) + `</code></li>`)
	}
	if rows.Len() == 0 {
		return `<p class="hint">` + esc(t(data.Lang, "Production Asset Types: 利用できないか、返却されませんでした。", "Production Asset Types: unavailable or none returned.")) + `</p>`
	}
	return `<p><strong>` + esc(t(data.Lang, "Production Asset Types", "Production Asset Types")) + `</strong></p><ul>` + rows.String() + `</ul>`
}

func renderWorkflowStatusSection(data workflowDiagnosisData) string {
	var rows strings.Builder
	for _, status := range data.ProductionTaskStatuses {
		notifiable := workflowStatusNotifiable(status.ShortName)
		flags := fmt.Sprintf("done=%t retake=%t feedback=%t wip=%t reviewable=%t", status.IsDone, status.IsRetake, status.IsFeedback, status.IsWIP, status.IsReviewable)
		rows.WriteString(`<tr><td>` + esc(status.Name) + `</td><td>` + esc(status.ShortName) + `</td><td><code>` + esc(status.ID) + `</code></td><td>` + esc(flags) + `</td><td><span class="status-pill ` + workflowClass(fmt.Sprintf("%t", notifiable)) + `">` + esc(fmt.Sprintf("%t", notifiable)) + `</span></td></tr>`)
	}
	return `<div class="section-card glass"><h3>` + esc(t(data.Lang, "Production Task Statuses", "Production Task Statuses")) + `</h3><p class="hint">` + esc(t(data.Lang, "現在 KitsuSync が通知する short name は wfa、retake、done だけです。", "KitsuSync currently notifies only short names wfa, retake, and done.")) + `</p><div style="overflow:auto"><table><thead><tr><th>` + esc(t(data.Lang, "名前", "Name")) + `</th><th>Short name</th><th>Stable ID</th><th>Semantic flags</th><th>Would notify</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div></div>`
}

func renderWorkflowReferenceSection(data workflowDiagnosisData) string {
	var entities strings.Builder
	for _, entity := range data.EntityTypes {
		entities.WriteString(`<li>` + esc(entity.Name) + ` <code>` + esc(entity.ID) + `</code></li>`)
	}
	var departments strings.Builder
	for _, department := range data.Departments {
		departments.WriteString(`<li>` + esc(department.Name) + ` <code>` + esc(department.ID) + `</code></li>`)
	}
	return `<div class="section-card glass"><h3>` + esc(t(data.Lang, "Kitsu 参照データ", "Kitsu reference data")) + `</h3><div class="section-stack"><div><strong>` + esc(t(data.Lang, "Global Entity Types", "Global Entity Types")) + `</strong><ul>` + entities.String() + `</ul></div><div><strong>` + esc(t(data.Lang, "Departments", "Departments")) + `</strong><ul>` + departments.String() + `</ul></div></div></div>`
}

func renderSimilar(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return `<div class="hint">Similar candidate only: ` + esc(strings.Join(names, ", ")) + `</div>`
}

func workflowClass(value string) string {
	if value == "Ready" || value == "true" {
		return "ok"
	}
	if value == "Missing" || value == "false" {
		return "bad"
	}
	return "warn"
}

func uniqueWorkflowID(values []workflowTaskType) string {
	if len(values) != 1 {
		return ""
	}
	return values[0].ID
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func minWorkflowInt(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

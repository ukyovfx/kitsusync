package setup

import (
	"crypto/sha256"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

const discordTextChannelNameLimit = 100

// TaskTypeChannelPlan is a network-free proposal. It is deliberately separate
// from the Discord mutation code so every write can require an explicit,
// user-visible confirmation of the exact plan.
type TaskTypeChannelPlan struct {
	ProductionID    string
	GuildID         string
	CategoryID      string
	CategoryOptions []DiscordGuildChannel
	Entries         []TaskTypeChannelPlanEntry
	Conflicts       []string
	DuplicateNames  []string
}

type TaskTypeChannelPlanOverride struct {
	ChannelName string
	Order       int
}

// taskTypePlanRequest returns the active Task Types and their user-reviewed
// channel overrides. An absent inclusion field means the initial plan, where
// every Production-valid Task Type is included. Once the form emits inclusion
// fields, only those stable IDs remain in the plan; this makes exclusion a
// routing decision rather than a Discord deletion request.
func taskTypePlanRequest(r *http.Request, taskTypes []kitsu.TaskType) ([]kitsu.TaskType, map[string]TaskTypeChannelPlanOverride) {
	_ = r.ParseForm()
	includedValues, hasInclusionFields := r.Form["included_task_type_id"]
	included := map[string]bool{}
	for _, value := range includedValues {
		if id := strings.TrimSpace(value); id != "" {
			included[id] = true
		}
	}
	action := strings.TrimSpace(r.FormValue("action"))
	target := strings.TrimSpace(r.FormValue("task_type_id"))
	if target == "" {
		target = strings.TrimSpace(r.FormValue("exclude_task_type_id"))
	}
	if action == "exclude" {
		delete(included, target)
		hasInclusionFields = true
	} else if action == "include" {
		included[target] = true
		hasInclusionFields = true
	} else if added := strings.TrimSpace(r.FormValue("add_task_type")); added != "" {
		included[added] = true
		hasInclusionFields = true
	}

	active := make([]kitsu.TaskType, 0, len(taskTypes))
	for _, taskType := range taskTypes {
		id := strings.TrimSpace(taskType.ID)
		if hasInclusionFields && !included[id] {
			continue
		}
		active = append(active, taskType)
	}
	return active, planOverridesFromRequest(r, active)
}

func taskTypePlanRequestInvalid(r *http.Request, taskTypes []kitsu.TaskType) bool {
	valid := map[string]bool{}
	for _, taskType := range taskTypes {
		if id := strings.TrimSpace(taskType.ID); id != "" {
			valid[id] = true
		}
	}
	included := map[string]bool{}
	for _, value := range r.URL.Query()["included_task_type_id"] {
		id := strings.TrimSpace(value)
		if id == "" || !valid[id] {
			return true
		}
		included[id] = true
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	target := strings.TrimSpace(r.URL.Query().Get("task_type_id"))
	if target == "" {
		target = strings.TrimSpace(r.URL.Query().Get("exclude_task_type_id"))
	}
	if action == "exclude" {
		return target == "" || !valid[target] || !included[target]
	}
	if action == "include" {
		return target == "" || !valid[target] || included[target]
	}
	if added := strings.TrimSpace(r.URL.Query().Get("add_task_type")); added != "" && !valid[added] {
		return true
	}
	return false
}

func renderTaskTypeChannelPlanCard(project model.Project, webhooks []model.ProjectWebhook, taskTypes []kitsu.TaskType, lang string) string {
	if len(taskTypes) == 0 {
		return `<section class="section-card glass"><h3>` + esc(tr(lang, "channel_plan.title")) + `</h3><p class="hint">` + esc(t(lang, "Kitsu runtime session が接続されるまで Task Types は利用できません。Discord への変更は提案されません。", "Task Types are unavailable until the Kitsu runtime session is connected. No Discord changes are proposed.")) + `</p></section>`
	}
	existing := map[string]string{}
	for _, webhook := range webhooks {
		name := strings.TrimSpace(webhook.ChannelName)
		id := strings.TrimSpace(webhook.DiscordChannelID)
		if name != "" && id != "" {
			existing[NormalizeTaskTypeChannelName(name)] = id
		}
	}
	plan := BuildTaskTypeChannelPlan(project.KitsuProjectID, project.DiscordGuildID, taskTypes, existing)
	var rows strings.Builder
	for _, entry := range plan.Entries {
		rows.WriteString(`<tr><td>` + html.EscapeString(entry.DisplayName()) + `</td><td><code>` + html.EscapeString(entry.ChannelName) + `</code></td><td>` + html.EscapeString(entry.Action) + `</td></tr>`)
	}
	status := t(lang, "明示確認の準備ができています", "Ready for explicit confirmation")
	if !plan.Valid() {
		status = t(lang, "要確認: Discord への書き込み前に所有権または名前の競合を解消してください", "Needs attention: resolve ownership or naming conflicts before any Discord write")
	}
	return fmt.Sprintf(`<section class="section-card glass"><div class="page-heading"><div><h3>Task Type Channels</h3><p class="hint">One channel is proposed per Kitsu Task Type in the linked Discord Guild. Names are deterministic; IDs remain routing identity.</p></div><span class="status-pill %s">%s</span></div><p class="field-help">Production: %s · Linked Discord Server: %s · Channels to create: %d</p><table><caption class="sr-only">Task Type channel creation and reuse plan</caption><thead><tr><th>Task Type</th><th>Proposed channel</th><th>Action</th></tr></thead><tbody>%s</tbody></table><p class="field-help">This is a network-free preview. No Discord write occurs until the exact plan is shown again and explicitly confirmed.</p></section>`, map[bool]string{true: "ok", false: "warn"}[plan.Valid()], html.EscapeString(status), html.EscapeString(project.Name), html.EscapeString(fallbackText(project.DiscordGuildID, "not linked")), plan.CreateCount(), rows.String())
}

func renderExplicitTaskTypeChannelPlan(project model.Project, taskTypes []kitsu.TaskType, botToken string, r *http.Request, lang string, db *gorm.DB) string {
	selectedGuild := strings.TrimSpace(r.URL.Query().Get("plan_guild"))
	var guilds []DiscordGuild
	if strings.TrimSpace(botToken) != "" {
		guilds, _ = ListBotGuilds(botToken)
	}
	var options strings.Builder
	options.WriteString(`<option value="">` + esc(tr(lang, "channel_plan.select_server")) + `</option>`)
	for _, guild := range guilds {
		id := strings.TrimSpace(guild.ID)
		if id == "" {
			continue
		}
		selected := ""
		if id == selectedGuild {
			selected = " selected"
		}
		options.WriteString(`<option value="` + html.EscapeString(id) + `"` + selected + `>` + html.EscapeString(strings.TrimSpace(guild.Name)) + `</option>`)
	}
	var body strings.Builder
	body.WriteString(`<section class="section-card glass"><h3>` + esc(tr(lang, "channel_plan.title")) + `</h3><p class="hint">` + esc(tr(lang, "channel_plan.description")) + `</p>`)
	body.WriteString(`<form method="GET" class="section-stack"><input type="hidden" name="project" value="` + html.EscapeString(project.KitsuProjectID) + `"><label for="plan-guild-` + html.EscapeString(project.KitsuProjectID) + `">` + esc(tr(lang, "channel_plan.server")) + `</label><select id="plan-guild-` + html.EscapeString(project.KitsuProjectID) + `" name="plan_guild">` + options.String() + `</select><button class="btn" type="submit">` + esc(tr(lang, "channel_plan.preview")) + `</button></form>`)
	if selectedGuild == "" || strings.TrimSpace(botToken) == "" {
		body.WriteString(`<p class="field-help">` + html.EscapeString(map[bool]string{true: tr(lang, "channel_plan.no_token"), false: tr(lang, "channel_plan.no_guild")}[strings.TrimSpace(botToken) == ""]) + ` ` + esc(tr(lang, "channel_plan.no_write")) + `</p></section>`)
		return body.String()
	}
	channels, err := ListGuildChannels(selectedGuild, botToken)
	if err != nil {
		body.WriteString(`<p class="field-help" role="status">` + esc(tr(lang, "channel_plan.read_failed")) + `</p></section>`)
		return body.String()
	}
	plan := BuildTaskTypeChannelPlan(project.KitsuProjectID, selectedGuild, taskTypes, existingChannelsForPlanWithLegacy(channels, model.ListProductionChannelMappings(db, project.KitsuProjectID), model.ListProjectWebhooks(db, project.KitsuProjectID)))
	body.WriteString(`<div class="table-wrap"><table><caption class="sr-only">` + esc(tr(lang, "channel_plan.exact_plan")) + `</caption><thead><tr><th>` + esc(tr(lang, "channel_plan.task_type")) + `</th><th>` + esc(tr(lang, "channel_plan.channel")) + `</th><th>` + esc(tr(lang, "channel_plan.action")) + `</th></tr></thead><tbody>`)
	for _, entry := range plan.Entries {
		body.WriteString(`<tr><td>` + html.EscapeString(entry.DisplayName()) + `</td><td><code>` + html.EscapeString(entry.ChannelName) + `</code></td><td>` + html.EscapeString(entry.Action) + `</td></tr>`)
	}
	body.WriteString(`</tbody></table></div>`)
	if len(plan.DuplicateNames) > 0 {
		body.WriteString(`<p class="field-help" role="alert">` + esc(tr(lang, "channel_plan.duplicate_name")) + `</p>`)
	}
	if plan.Valid() {
		body.WriteString(`<p class="field-help">` + esc(tr(lang, "channel_plan.create_only")) + `</p><form method="POST" class="section-stack"><input type="hidden" name="action" value="confirm_task_type_channels"><input type="hidden" name="project_id" value="` + html.EscapeString(project.KitsuProjectID) + `"><input type="hidden" name="guild_id" value="` + html.EscapeString(selectedGuild) + `"><input type="hidden" name="plan_fingerprint" value="` + html.EscapeString(plan.Fingerprint()) + `"><label><input type="checkbox" name="confirm_plan" value="yes" required> ` + esc(tr(lang, "channel_plan.confirmed")) + `</label><button class="btn" type="submit">` + esc(tr(lang, "channel_plan.confirm")) + `</button></form>`)
	} else {
		body.WriteString(`<p class="field-help" role="alert">` + esc(tr(lang, "channel_plan.blocked")) + `</p>`)
	}
	body.WriteString(`</section>`)
	return body.String()
}

type TaskTypeChannelPlanEntry struct {
	TaskTypeID     string
	TaskTypeName   string
	TaskTypeLabel  string
	ForEntity      string
	DepartmentID   string
	DepartmentName string
	ChannelName    string
	ExistingID     string
	Order          int
	Action         string // create, reuse, conflict, blocked
}

func (e TaskTypeChannelPlanEntry) DisplayName() string {
	if strings.TrimSpace(e.TaskTypeLabel) != "" {
		return e.TaskTypeLabel
	}
	return e.TaskTypeName
}

func NormalizeTaskTypeChannelName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	separator := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if separator && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			separator = false
			continue
		}
		separator = b.Len() > 0
	}
	result := strings.Trim(b.String(), "-")
	result = regexp.MustCompile(`-+`).ReplaceAllString(result, "-")
	if len(result) > discordTextChannelNameLimit {
		result = strings.TrimRight(result[:discordTextChannelNameLimit], "-")
	}
	if result == "" {
		return "task-type"
	}
	return result
}

// BuildTaskTypeChannelPlan uses IDs as identity and names only as display
// metadata. Existing channels are scoped to the selected guild by the caller.
func BuildTaskTypeChannelPlan(productionID, guildID string, taskTypes []kitsu.TaskType, existing map[string]string) TaskTypeChannelPlan {
	return buildTaskTypeChannelPlan(productionID, guildID, taskTypes, existing, nil)
}

func BuildTaskTypeChannelPlanWithOverrides(productionID, guildID string, taskTypes []kitsu.TaskType, existing map[string]string, overrides map[string]TaskTypeChannelPlanOverride) TaskTypeChannelPlan {
	return buildTaskTypeChannelPlan(productionID, guildID, taskTypes, existing, overrides)
}

func planOverridesFromRequest(r *http.Request, taskTypes []kitsu.TaskType) map[string]TaskTypeChannelPlanOverride {
	overrides := map[string]TaskTypeChannelPlanOverride{}
	for index, taskType := range taskTypes {
		id := strings.TrimSpace(taskType.ID)
		if id == "" {
			continue
		}
		override := TaskTypeChannelPlanOverride{
			ChannelName: strings.TrimSpace(r.FormValue("channel_name_" + id)),
			Order:       index + 1,
		}
		if raw := strings.TrimSpace(r.FormValue("channel_order_" + id)); raw != "" {
			if order, err := strconv.Atoi(raw); err == nil && order > 0 {
				override.Order = order
			}
		}
		if override.ChannelName != "" || strings.TrimSpace(r.FormValue("channel_order_"+id)) != "" {
			overrides[id] = override
		}
	}
	return overrides
}

func KitsuSyncCategoryName(productionName string) string {
	name := strings.TrimSpace(productionName)
	if name == "" {
		name = "Production"
	}
	const prefix = "KitsuSync – "
	result := prefix + name
	if len([]rune(result)) <= 100 {
		return result
	}
	runes := []rune(result)
	return strings.TrimRight(string(runes[:100]), " –")
}

func buildTaskTypeChannelPlan(productionID, guildID string, taskTypes []kitsu.TaskType, existing map[string]string, overrides map[string]TaskTypeChannelPlanOverride) TaskTypeChannelPlan {
	plan := TaskTypeChannelPlan{ProductionID: strings.TrimSpace(productionID), GuildID: strings.TrimSpace(guildID)}
	seen := map[string]string{}
	nameCounts := map[string]int{}
	for _, taskType := range taskTypes {
		nameCounts[strings.ToLower(strings.TrimSpace(taskType.Name))]++
	}
	duplicateNames := map[string]bool{}
	nameOrdinals := map[string]int{}
	channelOrdinals := map[string]int{}
	ordered := append([]kitsu.TaskType(nil), taskTypes...)
	sort.SliceStable(ordered, func(i, j int) bool { return strings.TrimSpace(ordered[i].ID) < strings.TrimSpace(ordered[j].ID) })
	for _, taskType := range ordered {
		id := strings.TrimSpace(taskType.ID)
		name := strings.TrimSpace(taskType.Name)
		channelName := NormalizeTaskTypeChannelName(name)
		label := name
		nameKey := strings.ToLower(name)
		context := strings.TrimSpace(taskType.ForEntity)
		labelContext := context
		if labelContext == "" {
			labelContext = strings.TrimSpace(taskType.DepartmentName)
		}
		if nameCounts[nameKey] > 1 {
			nameOrdinals[nameKey]++
			if strings.TrimSpace(taskType.ForEntity) != "" {
				label = strings.TrimSpace(taskType.ForEntity) + " / " + name
			} else if labelContext != "" {
				label = labelContext + " / " + name
			} else {
				label = fmt.Sprintf("%s (%d)", name, nameOrdinals[nameKey])
			}
			duplicateNames[nameKey] = true
		}
		if nameCounts[nameKey] > 1 && context != "" {
			channelName = NormalizeTaskTypeChannelName(name + "-" + context)
		}
		order := len(plan.Entries) + 1
		if override, ok := overrides[id]; ok {
			if strings.TrimSpace(override.ChannelName) != "" {
				channelName = NormalizeTaskTypeChannelName(override.ChannelName)
			}
			if override.Order > 0 {
				order = override.Order
			}
		}
		entry := TaskTypeChannelPlanEntry{
			TaskTypeID: id, TaskTypeName: name, TaskTypeLabel: label,
			ForEntity: taskType.ForEntity, DepartmentID: taskType.DepartmentID,
			DepartmentName: taskType.DepartmentName, ChannelName: channelName, Order: order, Action: "create",
		}
		if id == "" || name == "" || plan.ProductionID == "" || plan.GuildID == "" {
			entry.Action = "blocked"
		}
		if previous, ok := seen[channelName]; ok && previous != id {
			_, manuallyNamed := overrides[id]
			if !manuallyNamed && nameCounts[nameKey] > 1 && context != "" {
				channelOrdinals[channelName]++
				channelName = fmt.Sprintf("%s-%d", channelName, channelOrdinals[channelName]+1)
				entry.ChannelName = channelName
			} else {
				entry.Action = "conflict"
				plan.Conflicts = append(plan.Conflicts, "Task Types "+previous+" and "+id+" normalize to #"+channelName)
			}
		}
		seen[channelName] = id
		if existingID, present := existing[channelName]; present && entry.Action == "create" {
			if strings.TrimSpace(existingID) == "" {
				entry.Action = "blocked"
				plan.Conflicts = append(plan.Conflicts, "Discord channel name #"+channelName+" has no stable Production mapping")
			} else {
				entry.Action = "reuse"
				entry.ExistingID = strings.TrimSpace(existingID)
			}
		}
		plan.Entries = append(plan.Entries, entry)
	}
	for _, taskType := range ordered {
		key := strings.ToLower(strings.TrimSpace(taskType.Name))
		if duplicateNames[key] {
			plan.DuplicateNames = append(plan.DuplicateNames, strings.TrimSpace(taskType.Name))
			delete(duplicateNames, key)
		}
	}
	if len(overrides) == 0 {
		sort.SliceStable(plan.Entries, func(i, j int) bool { return plan.Entries[i].ChannelName < plan.Entries[j].ChannelName })
		for i := range plan.Entries {
			plan.Entries[i].Order = i + 1
		}
	} else {
		sort.SliceStable(plan.Entries, func(i, j int) bool {
			if plan.Entries[i].Order != plan.Entries[j].Order {
				return plan.Entries[i].Order < plan.Entries[j].Order
			}
			return plan.Entries[i].TaskTypeID < plan.Entries[j].TaskTypeID
		})
		for i := range plan.Entries {
			plan.Entries[i].Order = i + 1
		}
	}
	return plan
}

func (p TaskTypeChannelPlan) CreateCount() int {
	count := 0
	for _, entry := range p.Entries {
		if entry.Action == "create" {
			count++
		}
	}
	return count
}

func (p TaskTypeChannelPlan) ReuseCount() int {
	count := 0
	for _, entry := range p.Entries {
		if entry.Action == "reuse" {
			count++
		}
	}
	return count
}

func (p TaskTypeChannelPlan) RequiresConfirmation() bool {
	return p.CreateCount() > 0
}

func (p TaskTypeChannelPlan) Valid() bool {
	return p.ProductionID != "" && p.GuildID != "" && len(p.Entries) > 0 && len(p.Conflicts) == 0 && func() bool {
		for _, entry := range p.Entries {
			if entry.Action == "blocked" || entry.Action == "conflict" {
				return false
			}
		}
		return true
	}()
}

func (p TaskTypeChannelPlan) Fingerprint() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\x00%s\x00%s\x00", p.ProductionID, p.GuildID, p.CategoryID)
	for _, entry := range p.Entries {
		fmt.Fprintf(&b, "%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00", entry.Order, entry.TaskTypeID, entry.TaskTypeName, entry.ChannelName, entry.ExistingID, entry.Action)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", sum[:])
}

func existingChannelsForPlan(channels []DiscordGuildChannel, mappings []model.ProductionChannelMapping) map[string]string {
	return existingChannelsForPlanWithLegacy(channels, mappings, nil)
}

func existingChannelsForPlanWithLegacy(channels []DiscordGuildChannel, mappings []model.ProductionChannelMapping, legacy []model.ProjectWebhook) map[string]string {
	existing := map[string]string{}
	for _, channel := range channels {
		if channel.Type != 0 || strings.TrimSpace(channel.ID) == "" || strings.TrimSpace(channel.Name) == "" {
			continue
		}
		name := NormalizeTaskTypeChannelName(channel.Name)
		// A Discord name alone does not prove ownership. It is blocked until a
		// stable mapping for this Production confirms an exact reuse.
		if _, present := existing[name]; !present {
			existing[name] = ""
		}
	}
	for _, webhook := range legacy {
		name := NormalizeTaskTypeChannelName(webhook.ChannelName)
		id := strings.TrimSpace(webhook.DiscordChannelID)
		if name == "" || id == "" {
			continue
		}
		if previous, present := existing[name]; present && previous != "" && previous != id {
			existing[name] = ""
			continue
		}
		existing[name] = id
	}
	for _, mapping := range mappings {
		if strings.TrimSpace(mapping.ChannelName) != "" && strings.TrimSpace(mapping.ChannelID) != "" {
			name := NormalizeTaskTypeChannelName(mapping.ChannelName)
			if previous, present := existing[name]; present && previous != "" && previous != strings.TrimSpace(mapping.ChannelID) {
				existing[name] = ""
				continue
			}
			existing[name] = strings.TrimSpace(mapping.ChannelID)
		}
	}
	return existing
}

type channelPlanCreateFunc func(guildID, name string) (string, error)

func applyTaskTypeChannelPlan(plan TaskTypeChannelPlan, create channelPlanCreateFunc) ([]model.ProductionChannelMapping, int, error) {
	if !plan.Valid() {
		return nil, 0, fmt.Errorf("channel plan is blocked")
	}
	rows := make([]model.ProductionChannelMapping, 0, len(plan.Entries))
	created := 0
	for _, entry := range plan.Entries {
		channelID := strings.TrimSpace(entry.ExistingID)
		if entry.Action == "create" {
			var err error
			channelID, err = create(plan.GuildID, entry.ChannelName)
			if err != nil {
				return rows, created, fmt.Errorf("channel creation stopped after %d writes: %w", created, err)
			}
			created++
		}
		if channelID == "" {
			return rows, created, fmt.Errorf("channel mapping result was empty for task type %s", entry.TaskTypeID)
		}
		rows = append(rows, model.ProductionChannelMapping{ProductionID: plan.ProductionID, GuildID: plan.GuildID, TaskTypeID: entry.TaskTypeID, TaskTypeName: entry.TaskTypeName, ChannelID: channelID, ChannelName: entry.ChannelName, Active: true, MigrationState: "current"})
	}
	return rows, created, nil
}

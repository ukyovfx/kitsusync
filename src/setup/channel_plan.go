package setup

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"app/src/api/kitsu"
	"app/src/model"
)

const discordTextChannelNameLimit = 100

// TaskTypeChannelPlan is a network-free proposal. It is deliberately separate
// from the Discord mutation code so every write can require an explicit,
// user-visible confirmation of the exact plan.
type TaskTypeChannelPlan struct {
	ProductionID string
	GuildID      string
	Entries      []TaskTypeChannelPlanEntry
	Conflicts    []string
}

func renderTaskTypeChannelPlanCard(project model.Project, webhooks []model.ProjectWebhook, taskTypes []kitsu.TaskType, lang string) string {
	if len(taskTypes) == 0 {
		return `<section class="section-card glass"><h3>Task Type Channels</h3><p class="hint">Task Types are unavailable until the Kitsu runtime session is connected. No Discord changes are proposed.</p></section>`
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
		rows.WriteString(`<tr><td>` + html.EscapeString(entry.TaskTypeName) + `</td><td><code>` + html.EscapeString(entry.ChannelName) + `</code></td><td>` + html.EscapeString(entry.Action) + `</td></tr>`)
	}
	status := "Ready for explicit confirmation"
	if !plan.Valid() {
		status = "Needs attention: resolve ownership or naming conflicts before any Discord write"
	}
	return fmt.Sprintf(`<section class="section-card glass"><div class="page-heading"><div><h3>Task Type Channels</h3><p class="hint">One channel is proposed per Kitsu Task Type in the linked Discord Guild. Names are deterministic; IDs remain routing identity.</p></div><span class="status-pill %s">%s</span></div><p class="field-help">Production: %s · Linked Discord Server: %s · Channels to create: %d</p><table><caption class="sr-only">Task Type channel creation and reuse plan</caption><thead><tr><th>Task Type</th><th>Proposed channel</th><th>Action</th></tr></thead><tbody>%s</tbody></table><p class="field-help">This is a network-free preview. No Discord write occurs until the exact plan is shown again and explicitly confirmed.</p></section>`, map[bool]string{true: "ok", false: "warn"}[plan.Valid()], html.EscapeString(status), html.EscapeString(project.Name), html.EscapeString(fallbackText(project.DiscordGuildID, "not linked")), plan.CreateCount(), rows.String())
}

type TaskTypeChannelPlanEntry struct {
	TaskTypeID   string
	TaskTypeName string
	ChannelName  string
	ExistingID   string
	Action       string // create, reuse, conflict, blocked
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
	plan := TaskTypeChannelPlan{ProductionID: strings.TrimSpace(productionID), GuildID: strings.TrimSpace(guildID)}
	seen := map[string]string{}
	for _, taskType := range taskTypes {
		id := strings.TrimSpace(taskType.ID)
		name := strings.TrimSpace(taskType.Name)
		channelName := NormalizeTaskTypeChannelName(name)
		entry := TaskTypeChannelPlanEntry{TaskTypeID: id, TaskTypeName: name, ChannelName: channelName, Action: "create"}
		if id == "" || name == "" || plan.ProductionID == "" || plan.GuildID == "" {
			entry.Action = "blocked"
		}
		if previous, ok := seen[channelName]; ok && previous != id {
			entry.Action = "conflict"
			plan.Conflicts = append(plan.Conflicts, "Task Types "+previous+" and "+id+" normalize to #"+channelName)
		}
		seen[channelName] = id
		if existingID := strings.TrimSpace(existing[channelName]); existingID != "" && entry.Action == "create" {
			entry.Action = "reuse"
			entry.ExistingID = existingID
		}
		plan.Entries = append(plan.Entries, entry)
	}
	sort.SliceStable(plan.Entries, func(i, j int) bool { return plan.Entries[i].ChannelName < plan.Entries[j].ChannelName })
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

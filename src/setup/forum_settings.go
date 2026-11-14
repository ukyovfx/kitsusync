package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"gorm.io/gorm"
)

// The forum settings page configures the forum notification path for one
// Production. It lives beside, not inside, the lane routing editor: the two
// paths coexist until the lane path is removed, and a Production only uses the
// forum path once its mode is switched to forum here.

const discordChannelTypeForum = 15

type forumTagInfo struct {
	ID   string
	Name string
}

type forumChannelInfo struct {
	ID   string
	Name string
	Tags []forumTagInfo
}

// Lookups are variables so tests run without Kitsu or Discord.
var (
	forumTaskTypeLister = setupKitsuTaskTypes
	forumStatusLister   = func(db *gorm.DB) ([]kitsu.TaskStatus, error) {
		statuses, err := kitsu.GetTaskStatusesWithError()
		return statuses.Each, err
	}
	forumChannelLister = listGuildForumChannels
)

// listGuildForumChannels returns the guild's forum channels with their tags.
func listGuildForumChannels(guildID, botToken string) ([]forumChannelInfo, error) {
	body, status, err := botDo(http.MethodGet, fmt.Sprintf("%s/guilds/%s/channels", discordAPI, url.PathEscape(strings.TrimSpace(guildID))), nil, botToken)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, discordBotAPIError("discord guild channel list failed", status, body)
	}
	var raw []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Type          int    `json:"type"`
		AvailableTags []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"available_tags"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("discord guild channel list response was invalid")
	}
	var forums []forumChannelInfo
	for _, channel := range raw {
		if channel.Type != discordChannelTypeForum {
			continue
		}
		forum := forumChannelInfo{ID: channel.ID, Name: channel.Name}
		for _, tag := range channel.AvailableTags {
			forum.Tags = append(forum.Tags, forumTagInfo{ID: tag.ID, Name: tag.Name})
		}
		forums = append(forums, forum)
	}
	return forums, nil
}

var forumRoleOptions = []string{model.ForumRoleWFA, model.ForumRoleRetake, model.ForumRoleDone, model.ForumRoleWIP}

func forumRoleLabel(lang, role string) string {
	switch role {
	case model.ForumRoleWFA:
		return t(lang, "確認待ち（WFA）", "Waiting for approval (WFA)")
	case model.ForumRoleRetake:
		return t(lang, "リテイク", "Retake")
	case model.ForumRoleDone:
		return t(lang, "完了", "Done")
	case model.ForumRoleWIP:
		return t(lang, "作業中（タグだけ更新）", "In progress (tag only)")
	}
	return role
}

// ForumSettingsHandler serves /admin/forum?project=<Kitsu project ID>.
func ForumSettingsHandler(db *gorm.DB, discordSettings func() (botToken, fallbackGuildID string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		projectID := strings.TrimSpace(r.FormValue("project"))
		project := model.FindProjectByKitsuID(db, projectID)
		if project == nil {
			http.Error(w, "unknown Production", http.StatusNotFound)
			return
		}
		botToken, fallbackGuildID := discordSettings()
		if r.Method == http.MethodPost {
			if project.ValidationOnly {
				http.Error(w, "validation-only Production is read-only", http.StatusForbidden)
				return
			}
			settings := parseForumSettingsForm(r, projectID)
			if len(settings.Routes) > 0 || settings.Config.Mode == model.NotificationModeForum {
				settings.Tags = tagsForRoutedForums(settings)
			}
			if err := model.SaveProductionForumSettings(db, settings); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, renderForumSettingsPage(db, r, lang, *project, botToken, fallbackGuildID, &settings, err))
				return
			}
			http.Redirect(w, r, withLang("/bot/admin/forum?project="+url.QueryEscape(projectID), r)+"&msg=saved", http.StatusSeeOther)
			return
		}
		fmt.Fprint(w, renderForumSettingsPage(db, r, lang, *project, botToken, fallbackGuildID, nil, nil))
	}
}

// parseForumSettingsForm reads the page's parallel-array fields. It needs no
// Kitsu or Discord lookup, so a save works while either is unreachable.
func parseForumSettingsForm(r *http.Request, productionID string) model.ProductionForumSettings {
	_ = r.ParseForm()
	settings := model.ProductionForumSettings{Config: model.ProductionForumConfig{
		ProductionID: productionID,
		Mode:         strings.TrimSpace(r.FormValue("mode")),
		GuildID:      strings.TrimSpace(r.FormValue("guild_id")),
	}}
	ids, names, forums := r.Form["tt_id"], r.Form["tt_name"], r.Form["tt_forum"]
	for i := range ids {
		if i >= len(forums) || strings.TrimSpace(forums[i]) == "" {
			continue
		}
		route := model.ProductionForumRoute{TaskTypeID: strings.TrimSpace(ids[i]), ForumChannelID: strings.TrimSpace(forums[i])}
		if i < len(names) {
			route.TaskTypeName = strings.TrimSpace(names[i])
		}
		settings.Routes = append(settings.Routes, route)
	}
	stIDs, stNames, stRoles := r.Form["st_id"], r.Form["st_name"], r.Form["st_role"]
	for i := range stIDs {
		if i >= len(stRoles) || strings.TrimSpace(stRoles[i]) == "" {
			continue
		}
		row := model.ProductionForumStatusRole{StatusID: strings.TrimSpace(stIDs[i]), Role: strings.TrimSpace(stRoles[i])}
		if i < len(stNames) {
			row.StatusName = strings.TrimSpace(stNames[i])
		}
		settings.StatusRoles = append(settings.StatusRoles, row)
	}
	tagForums, tagRoles, tagIDs := r.Form["tag_forum"], r.Form["tag_role"], r.Form["tag_id"]
	for i := range tagForums {
		if i >= len(tagRoles) || i >= len(tagIDs) || strings.TrimSpace(tagIDs[i]) == "" {
			continue
		}
		settings.Tags = append(settings.Tags, model.ProductionForumTag{ForumChannelID: strings.TrimSpace(tagForums[i]), Role: strings.TrimSpace(tagRoles[i]), TagID: strings.TrimSpace(tagIDs[i])})
	}
	return settings
}

// tagsForRoutedForums drops tag rows of forums no Task Type routes to any more,
// so re-pointing a Task Type does not leave stale tags that fail validation.
func tagsForRoutedForums(settings model.ProductionForumSettings) []model.ProductionForumTag {
	routed := map[string]struct{}{}
	for _, route := range settings.Routes {
		routed[route.ForumChannelID] = struct{}{}
	}
	var kept []model.ProductionForumTag
	for _, tag := range settings.Tags {
		if _, ok := routed[tag.ForumChannelID]; ok {
			kept = append(kept, tag)
		}
	}
	return kept
}

func forumSelect(name, selected string, options [][2]string, emptyLabel string) string {
	var b strings.Builder
	b.WriteString(`<select name="` + esc(name) + `"><option value="">` + esc(emptyLabel) + `</option>`)
	found := selected == ""
	for _, option := range options {
		if option[0] == selected {
			found = true
		}
		b.WriteString(`<option value="` + esc(option[0]) + `"` + selectedAttr(option[0] == selected) + `>` + esc(option[1]) + `</option>`)
	}
	if !found {
		// A saved value the live list no longer offers stays visible and kept.
		b.WriteString(`<option value="` + esc(selected) + `" selected>` + esc(selected) + `</option>`)
	}
	b.WriteString(`</select>`)
	return b.String()
}

func renderForumSettingsPage(db *gorm.DB, r *http.Request, lang string, project model.Project, botToken, fallbackGuildID string, submitted *model.ProductionForumSettings, saveErr error) string {
	productionID := project.KitsuProjectID
	var current model.ProductionForumSettings
	if submitted != nil {
		current = *submitted
	} else if loaded, ok, err := model.LoadProductionForumSettings(db, productionID); err == nil && ok {
		current = loaded
	} else {
		current.Config = model.ProductionForumConfig{ProductionID: productionID, Mode: model.NotificationModeLane}
	}
	guildID := strings.TrimSpace(current.Config.GuildID)
	if guildID == "" {
		guildID = strings.TrimSpace(project.DiscordGuildID)
	}
	if guildID == "" {
		guildID = strings.TrimSpace(fallbackGuildID)
	}

	var notices strings.Builder
	if saveErr != nil {
		notices.WriteString(`<div class="alert warn" role="alert"><strong>` + esc(t(lang, "保存できませんでした", "Could not save")) + `</strong><p>` + esc(saveErr.Error()) + `</p></div>`)
	}

	taskTypes := forumTaskTypeLister(db, productionID)
	statuses, statusErr := forumStatusLister(db)
	var forums []forumChannelInfo
	var forumErr error
	if strings.TrimSpace(botToken) != "" && guildID != "" {
		forums, forumErr = forumChannelLister(guildID, botToken)
	} else {
		forumErr = fmt.Errorf("no Discord bot token or server ID")
	}
	if statusErr != nil {
		notices.WriteString(`<p class="field-help" role="status">` + esc(t(lang, "Kitsuのステータス一覧を取得できませんでした。", "Could not read the Kitsu status list.")) + `</p>`)
	}
	if forumErr != nil {
		notices.WriteString(`<p class="field-help" role="status">` + esc(t(lang, "Discordのフォーラム一覧を取得できませんでした。Bot設定とサーバーIDを確認してください。保存済みの値はそのまま残ります。", "Could not read the Discord forum list. Check Bot settings and the server ID. Saved values are kept.")) + `</p>`)
	}

	forumOptions := make([][2]string, 0, len(forums))
	tagsByForum := map[string][]forumTagInfo{}
	for _, forum := range forums {
		forumOptions = append(forumOptions, [2]string{forum.ID, forum.Name})
		tagsByForum[forum.ID] = forum.Tags
	}

	routeByTaskType := map[string]model.ProductionForumRoute{}
	for _, route := range current.Routes {
		routeByTaskType[route.TaskTypeID] = route
	}
	var taskTypeRows strings.Builder
	seenTaskTypes := map[string]struct{}{}
	for _, taskType := range taskTypes {
		seenTaskTypes[taskType.ID] = struct{}{}
		taskTypeRows.WriteString(forumTaskTypeRow(lang, taskType.ID, taskType.Name, routeByTaskType[taskType.ID].ForumChannelID, forumOptions))
	}
	for _, route := range current.Routes { // saved but no longer listed by Kitsu
		if _, ok := seenTaskTypes[route.TaskTypeID]; !ok {
			taskTypeRows.WriteString(forumTaskTypeRow(lang, route.TaskTypeID, route.TaskTypeName, route.ForumChannelID, forumOptions))
		}
	}

	roleByStatus := map[string]string{}
	for _, row := range current.StatusRoles {
		roleByStatus[row.StatusID] = row.Role
	}
	roleOptions := make([][2]string, 0, len(forumRoleOptions))
	for _, role := range forumRoleOptions {
		roleOptions = append(roleOptions, [2]string{role, forumRoleLabel(lang, role)})
	}
	var statusRows strings.Builder
	for _, status := range statuses {
		name := strings.TrimSpace(status.Name)
		if name == "" {
			name = status.ShortName
		}
		statusRows.WriteString(`<tr><td>` + esc(name) + `<input type="hidden" name="st_id" value="` + esc(status.ID) + `"><input type="hidden" name="st_name" value="` + esc(name) + `"></td><td>` +
			forumSelect("st_role", roleByStatus[status.ID], roleOptions, t(lang, "（使わない）", "(unused)")) + `</td></tr>`)
	}

	tagByKey := map[string]string{}
	for _, tag := range current.Tags {
		tagByKey[tag.ForumChannelID+"|"+tag.Role] = tag.TagID
	}
	var tagSections strings.Builder
	shownForums := map[string]struct{}{}
	for _, route := range current.Routes {
		if _, done := shownForums[route.ForumChannelID]; done {
			continue
		}
		shownForums[route.ForumChannelID] = struct{}{}
		forumLabel := route.ForumChannelID
		tagOptions := make([][2]string, 0)
		for _, tag := range tagsByForum[route.ForumChannelID] {
			tagOptions = append(tagOptions, [2]string{tag.ID, tag.Name})
		}
		for _, forum := range forums {
			if forum.ID == route.ForumChannelID {
				forumLabel = forum.Name
			}
		}
		tagSections.WriteString(`<h3>` + esc(forumLabel) + `</h3><table class="data-table"><tbody>`)
		for _, role := range forumRoleOptions {
			tagSections.WriteString(`<tr><td>` + esc(forumRoleLabel(lang, role)) + `<input type="hidden" name="tag_forum" value="` + esc(route.ForumChannelID) + `"><input type="hidden" name="tag_role" value="` + esc(role) + `"></td><td>` +
				forumSelect("tag_id", tagByKey[route.ForumChannelID+"|"+role], tagOptions, t(lang, "（タグなし）", "(no tag)")) + `</td></tr>`)
		}
		tagSections.WriteString(`</tbody></table>`)
	}
	if tagSections.Len() == 0 {
		tagSections.WriteString(`<p class="field-help">` + esc(t(lang, "タスクタイプにフォーラムを選んで一度保存すると、ここにタグの設定が出ます。", "Pick a forum for a Task Type and save once; the tag settings appear here.")) + `</p>`)
	}

	laneChecked, forumChecked := current.Config.Mode != model.NotificationModeForum, current.Config.Mode == model.NotificationModeForum
	form := `<form method="post" action="` + esc(withLang("/bot/admin/forum", r)) + `" class="section-stack">` +
		`<input type="hidden" name="project" value="` + esc(productionID) + `">` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "通知の方式", "Notification mode")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "フォーラムにすると、このProductionの通知はレーン（チャンネル）ではなくフォーラム投稿になります。設定がそろうまでは「レーン」のままにしてください。いつでも戻せます。", "Forum mode sends this Production's notifications as forum posts instead of lane (channel) messages. Keep Lane until the settings are complete. You can switch back at any time.")) + `</p>` +
		`<label><input type="radio" name="mode" value="lane"` + checkedAttr(laneChecked) + `> ` + esc(t(lang, "レーン（現行）", "Lane (current)")) + `</label> ` +
		`<label><input type="radio" name="mode" value="forum"` + checkedAttr(forumChecked) + `> ` + esc(t(lang, "フォーラム", "Forum")) + `</label>` +
		`<label class="field-label" for="forum-guild">` + esc(t(lang, "DiscordサーバーID", "Discord server ID")) + `</label><input id="forum-guild" name="guild_id" value="` + esc(guildID) + `" autocomplete="off"></section>` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "タスクタイプ → フォーラム", "Task Type → forum")) + `</h2><table class="data-table"><tbody>` + taskTypeRows.String() + `</tbody></table></section>` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "Kitsuのステータス → 役割", "Kitsu status → role")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "確認待ち・リテイク・完了にそれぞれステータスを1つ以上割り当てます。", "Assign at least one status each to Waiting, Retake and Done.")) + `</p><table class="data-table"><tbody>` + statusRows.String() + `</tbody></table></section>` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "役割 → フォーラムのタグ", "Role → forum tag")) + `</h2>` + tagSections.String() + `</section>` +
		`<div class="button-row"><button type="submit" class="btn">` + esc(t(lang, "保存", "Save")) + `</button> <a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(productionID)+"&tab=notifications", r)) + `">` + esc(t(lang, "通知設定に戻る", "Back to notifications")) + `</a></div></form>`

	body := `<div class="section-stack"><section class="section-card glass"><h2>` + esc(project.Name) + `</h2></section>` + notices.String() + form + `</div>`
	return adminPage(lang, t(lang, "フォーラム通知の設定", "Forum notification settings"), r, body)
}

func forumTaskTypeRow(lang, id, name, selected string, forumOptions [][2]string) string {
	return `<tr><td>` + esc(name) + `<input type="hidden" name="tt_id" value="` + esc(id) + `"><input type="hidden" name="tt_name" value="` + esc(name) + `"></td><td>` +
		forumSelect("tt_forum", selected, forumOptions, t(lang, "（通知しない）", "(no forum)")) + `</td></tr>`
}

func checkedAttr(checked bool) string {
	if checked {
		return " checked"
	}
	return ""
}

// renderForumSettingsLink adds the entry to the forum settings page under the
// Production's notifications tab, with the current mode.
func renderForumSettingsLink(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	mode := t(lang, "レーン（現行）", "Lane (current)")
	if model.NotificationModeForProduction(db, p.KitsuProjectID) == model.NotificationModeForum {
		mode = t(lang, "フォーラム", "Forum")
	}
	return `<section class="section-card glass"><h2>` + esc(t(lang, "フォーラム通知", "Forum notifications")) + `</h2>` +
		`<p class="field-help">` + esc(t(lang, "現在の方式", "Current mode")) + `: <strong>` + esc(mode) + `</strong></p>` +
		`<div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/forum?project="+url.QueryEscape(p.KitsuProjectID), r)) + `">` + esc(t(lang, "フォーラム設定を開く", "Open forum settings")) + `</a></div></section>`
}

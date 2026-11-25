package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"encoding/json"
	"errors"
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
	ID    string
	Name  string
	Emoji string
}

type forumChannelInfo struct {
	ID       string
	Name     string
	ParentID string
	Tags     []forumTagInfo
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
// It goes through the shared cache refresh, so saves get fresh data while
// page rendering stays non-blocking.
func listGuildForumChannels(guildID, botToken string) ([]forumChannelInfo, error) {
	body, status, err := guildChannelListCache.refresh(guildID, botToken)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, discordBotAPIError("discord guild channel list failed", status, body)
	}
	return decodeGuildForumChannels(body)
}

// decodeGuildForumChannels reads the forum channels (with their tags) from a
// raw guild channel list body, which may come from the cache.
func decodeGuildForumChannels(body []byte) ([]forumChannelInfo, error) {
	var raw []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Type          int    `json:"type"`
		ParentID      string `json:"parent_id"`
		AvailableTags []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Emoji string `json:"emoji_name"`
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
		forum := forumChannelInfo{ID: channel.ID, Name: channel.Name, ParentID: strings.TrimSpace(channel.ParentID)}
		for _, tag := range channel.AvailableTags {
			forum.Tags = append(forum.Tags, forumTagInfo{ID: tag.ID, Name: tag.Name, Emoji: tag.Emoji})
		}
		forums = append(forums, forum)
	}
	return forums, nil
}

var forumRoleOptions = []string{model.ForumRoleWFA, model.ForumRoleRetake, model.ForumRoleDone, model.ForumRoleWIP}

// forumTagIDForRole returns the ID of the forum's tag named after the role's
// standard tag name (WFA / RETAKE / DONE / WIP, case-insensitive), or "" when
// the forum has no such tag.
func forumTagIDForRole(tags []forumTagInfo, role string) string {
	for _, want := range forumAutoTagNames {
		if want.Role != role {
			continue
		}
		for _, got := range tags {
			if strings.EqualFold(strings.TrimSpace(got.Name), want.Name) {
				return got.ID
			}
		}
	}
	return ""
}

// deriveForumTagsFromChannels builds the tag mapping for every routed forum
// from the forum's own tag names. Owner decision 2026-11-18: every forum uses
// the same four standard tags, so the mapping is derived at save time instead
// of being assigned per forum on the page.
func deriveForumTagsFromChannels(productionID string, routes []model.ProductionForumRoute, forums []forumChannelInfo) []model.ProductionForumTag {
	byID := make(map[string]forumChannelInfo, len(forums))
	for _, forum := range forums {
		byID[forum.ID] = forum
	}
	var tags []model.ProductionForumTag
	derived := map[string]struct{}{}
	for _, route := range routes {
		forumID := strings.TrimSpace(route.ForumChannelID)
		if forumID == "" {
			continue
		}
		if _, done := derived[forumID]; done {
			continue
		}
		derived[forumID] = struct{}{}
		forum, ok := byID[forumID]
		if !ok {
			continue
		}
		for _, role := range forumRoleOptions {
			if tagID := forumTagIDForRole(forum.Tags, role); tagID != "" {
				tags = append(tags, model.ProductionForumTag{ProductionID: productionID, ForumChannelID: forumID, Role: role, TagID: tagID})
			}
		}
	}
	return tags
}

func forumRoleLabel(lang, role string) string {
	switch role {
	case model.ForumRoleWFA:
		return t(lang, "確認待ち（WFA）", "Waiting for approval (WFA)")
	case model.ForumRoleRetake:
		return t(lang, "リテイク", "Retake")
	case model.ForumRoleDone:
		return t(lang, "完了", "Done")
	case model.ForumRoleWIP:
		return t(lang, "作業中（タグのみ）", "In progress (tag only)")
	}
	return role
}

// forumTemplateRoleForStatus maps a Kitsu status name to the default forum
// role. Owner decision 2026-11-18: the mapping is decided by the status name,
// so it is applied as a template instead of being typed by the operator.
func forumTemplateRoleForStatus(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "waiting for approval":
		return model.ForumRoleWFA
	case "retake":
		return model.ForumRoleRetake
	case "done", "approved":
		return model.ForumRoleDone
	case "work in progress", "wip":
		return model.ForumRoleWIP
	}
	return ""
}

// forumTemplateStatusRolesFrom derives the same template from a status list
// already fetched, so callers do not request the list a second time.
func forumTemplateStatusRolesFrom(statuses []kitsu.TaskStatus) []model.ProductionForumStatusRole {
	var rows []model.ProductionForumStatusRole
	for _, status := range statuses {
		name := strings.TrimSpace(status.Name)
		if name == "" {
			name = status.ShortName
		}
		if role := forumTemplateRoleForStatus(name); role != "" {
			rows = append(rows, model.ProductionForumStatusRole{StatusID: status.ID, StatusName: name, Role: role})
		}
	}
	return rows
}

// ForumSettingsHandler serves /admin/forum?project=<Kitsu project ID>. It
// configures only the task type -> forum routing; the status -> role mapping
// lives on the bot settings page, and comment notifications always relay
// @mentions only.
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
			if project.ValidationOnly || ValidationOnlyModeEnabled() {
				http.Error(w, "validation-only Production is read-only", http.StatusForbidden)
				return
			}
			if strings.TrimSpace(r.FormValue("action")) == "create_forums" {
				submitted := withCarriedStatusRoles(db, parseForumSettingsForm(r, projectID))
				created, err := createForumsForTaskTypes(db, projectID, botToken, fallbackGuildID, submitted)
				if err == nil {
					http.Redirect(w, r, withLang("/bot/admin/forum?project="+url.QueryEscape(projectID), r)+"&msg=forums_created", http.StatusSeeOther)
					return
				}
				// The forums may already exist, so show what was made and what is left.
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, renderForumSettingsPage(db, r, lang, *project, botToken, fallbackGuildID, &created, err))
				return
			}
			settings := withCarriedStatusRoles(db, parseForumSettingsForm(r, projectID))
			if len(settings.StatusRoles) == 0 {
				// First save of a Production whose roles were never set: the
				// name template fills them, the same way the create action
				// does, so one save still finishes the setup.
				if statuses, statusErr := forumStatusLister(db); statusErr == nil {
					settings.StatusRoles = forumTemplateStatusRolesFrom(statuses)
				}
			}
			// The tag mapping is derived from each routed forum's own tag names,
			// so the save needs the live forum list. While Discord is unreachable
			// the saved mapping is kept, and a save with nothing to derive from is
			// refused instead of storing a configuration that cannot tag posts.
			guildID := strings.TrimSpace(settings.Config.GuildID)
			if guildID == "" {
				guildID = strings.TrimSpace(project.DiscordGuildID)
			}
			if guildID == "" {
				guildID = strings.TrimSpace(fallbackGuildID)
			}
			var forums []forumChannelInfo
			forumErr := fmt.Errorf("no Discord bot token or server ID")
			if strings.TrimSpace(botToken) != "" && guildID != "" {
				forums, forumErr = forumChannelLister(guildID, botToken)
			}
			if forumErr == nil {
				settings.Tags = deriveForumTagsFromChannels(projectID, settings.Routes, forums)
			} else if len(settings.Routes) > 0 {
				routed := map[string]struct{}{}
				for _, route := range settings.Routes {
					routed[route.ForumChannelID] = struct{}{}
				}
				if saved, ok, err := model.LoadProductionForumSettings(db, projectID); err == nil && ok && strings.TrimSpace(saved.Config.GuildID) == guildID {
					for _, tag := range saved.Tags {
						if _, keep := routed[tag.ForumChannelID]; keep {
							settings.Tags = append(settings.Tags, tag)
						}
					}
				}
				if len(settings.Tags) == 0 {
					err := &model.ForumSettingsError{Problems: []model.ForumSettingsProblem{{Code: model.ProblemForumListUnavailable}}}
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, renderForumSettingsPage(db, r, lang, *project, botToken, fallbackGuildID, &settings, err))
					return
				}
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
	return settings
}

// withCarriedStatusRoles loads the saved status -> role rows for the
// Production so a forum-settings save (which replaces the whole settings
// bundle) preserves the mapping that is edited on the bot settings page.
func withCarriedStatusRoles(db *gorm.DB, settings model.ProductionForumSettings) model.ProductionForumSettings {
	if saved, ok, err := model.LoadProductionForumSettings(db, settings.Config.ProductionID); err == nil && ok {
		settings.StatusRoles = saved.StatusRoles
	}
	return settings
}

func forumSelect(name, selected string, options [][2]string, emptyLabel string) string {
	var b strings.Builder
	b.WriteString(`<select name="` + esc(name) + `"><option value="">` + esc(emptyLabel) + `</option>`)
	found := selected == ""
	for _, option := range options {
		if option[0] == selected {
			found = true
		}
		b.WriteString(`<option value="` + esc(option[0]) + `"` + selectedOptionAttr(option[0] == selected) + `>` + esc(option[1]) + `</option>`)
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
		current.Config = model.ProductionForumConfig{ProductionID: productionID}
	}
	guildID := strings.TrimSpace(current.Config.GuildID)
	if guildID == "" {
		guildID = strings.TrimSpace(project.DiscordGuildID)
	}
	if guildID == "" {
		guildID = strings.TrimSpace(fallbackGuildID)
	}

	var notices strings.Builder
	switch msg := r.FormValue("msg"); msg {
	case "forums_created":
		notices.WriteString(`<div class="notice" role="status">` + esc(t(lang, "フォーラムとタグを作成し、設定を保存しました。", "Created the forums and tags and saved the settings.")) + `</div>`)
	case "saved":
		notices.WriteString(`<div class="notice" role="status">` + esc(t(lang, "設定を保存しました。", "Settings saved.")) + `</div>`)
	}
	taskTypes := forumTaskTypeLister(db, productionID)
	_, statusErr := forumStatusLister(db)
	var forums []forumChannelInfo
	var forumErr error
	var staleNotice string
	refetchNeeded := false
	if strings.TrimSpace(botToken) != "" && guildID != "" {
		view := guildChannelListCache.view(guildID, botToken)
		if view.hasData() {
			if decoded, decodeErr := decodeGuildForumChannels(view.body); decodeErr == nil {
				forums = decoded
				if !view.fresh && view.fetchFailed {
					refetchNeeded = true
					staleNotice = guildChannelStaleNoticeText(lang, view.rateLimited)
				}
			} else {
				forumErr = decodeErr
			}
		} else {
			// No cached list yet (first view or every fetch has failed): the
			// background refresh may still land, so offer the manual refetch.
			refetchNeeded = true
			forumErr = fmt.Errorf("discord forum list unavailable")
		}
	} else {
		forumErr = fmt.Errorf("no Discord bot token or server ID")
	}
	if statusErr != nil {
		notices.WriteString(`<p class="field-help" role="status">` + esc(t(lang, "Kitsuのステータス一覧を取得できませんでした。保存済みのステータス設定はそのまま残ります。", "Could not read the Kitsu status list. Saved status settings are kept.")) + `</p>`)
	}
	if forumErr != nil {
		notices.WriteString(`<p class="field-help" role="status">` + esc(t(lang, "Discordのフォーラム一覧を取得できませんでした。Bot設定とサーバーIDを確認してください。保存済みの値はそのまま残ります。", "Could not read the Discord forum list. Check Bot settings and the server ID. Saved values are kept.")) + `</p>`)
	}
	if staleNotice != "" {
		notices.WriteString(`<p class="field-help" role="status">` + esc(staleNotice) + `</p>`)
	}
	refetchButton := ""
	if refetchNeeded {
		refetchURL := withLang("/bot/admin/refresh-discord-channels?guild="+url.QueryEscape(guildID)+"&back="+url.QueryEscape(r.URL.RequestURI()), r)
		refetchButton = ` <a class="btn-ghost" href="` + esc(refetchURL) + `">` + esc(t(lang, "チャンネル情報を再取得", "Refresh channel info")) + `</a>`
	}

	forumOptions := make([][2]string, 0, len(forums))
	for _, forum := range forums {
		forumOptions = append(forumOptions, [2]string{forum.ID, forum.Name})
	}

	forumNames := map[string]string{}
	for _, forum := range forums {
		forumNames[forum.ID] = forum.Name
	}
	errorHTML := ""
	createFailed := saveErr != nil && errors.Is(saveErr, errForumCreateAction)
	if saveErr != nil {
		if createFailed {
			// Creation errors are already written for the operator in Japanese;
			// the result panel below shows them next to the current state.
		} else {
			errorHTML = `<div class="notice notice-error" role="alert"><strong>` + esc(t(lang, "保存できませんでした", "Could not save")) + `</strong>` + forumSaveErrorMarkup(lang, saveErr, forumNames) + `</div>`
		}
	}

	routeByTaskType := map[string]model.ProductionForumRoute{}
	for _, route := range current.Routes {
		routeByTaskType[route.TaskTypeID] = route
	}
	var taskTypeRows strings.Builder
	seenTaskTypes := map[string]struct{}{}
	hasUnassignedForum := false
	for _, taskType := range taskTypes {
		seenTaskTypes[taskType.ID] = struct{}{}
		forumID := routeByTaskType[taskType.ID].ForumChannelID
		if strings.TrimSpace(forumID) == "" {
			hasUnassignedForum = true
		}
		taskTypeRows.WriteString(forumTaskTypeRow(lang, taskType.ID, taskType.Name, forumID, forumOptions))
	}
	for _, route := range current.Routes { // saved but no longer listed by Kitsu
		if _, ok := seenTaskTypes[route.TaskTypeID]; !ok {
			if strings.TrimSpace(route.ForumChannelID) == "" {
				hasUnassignedForum = true
			}
			taskTypeRows.WriteString(forumTaskTypeRow(lang, route.TaskTypeID, route.TaskTypeName, route.ForumChannelID, forumOptions))
		}
	}

	autoCheck := model.GetSetting(db, forumAutoCreateCheckKey(productionID))
	createButton := ""
	if hasUnassignedForum || autoCheck != "" {
		createButton = ` <button type="submit" name="action" value="create_forums" class="btn">` + esc(t(lang, "Botでフォーラムとタグを作成", "Create forums and tags with the Bot")) + `</button>`
	}

	taskTypeTable := `<p class="state-explanation" role="status">` + esc(t(lang, "このプロダクションに割り当て可能なタスクタイプが見つかりませんでした。Kitsu接続とタスクタイプ設定を確認してください。", "No task types were found for this Production. Check the Kitsu connection and task type settings.")) + `</p>`
	if taskTypeRows.Len() > 0 {
		taskTypeTable = `<table class="data-table forum-map-table"><tbody>` + taskTypeRows.String() + `</tbody></table>`
	}

	form := `<form method="post" action="` + esc(withLang("/bot/admin/forum", r)) + `" class="section-stack">` +
		`<input type="hidden" name="project" value="` + esc(productionID) + `">` +
		`<section class="section-card glass"><h2>` + esc(t(lang, "タスクタイプ → フォーラム", "Task Type → forum")) + `</h2>` +
		`<label class="field-label" for="forum-guild">` + esc(t(lang, "DiscordサーバーID", "Discord server ID")) + `</label><input id="forum-guild" name="guild_id" value="` + esc(guildID) + `" autocomplete="off">` +
		`<p class="field-help">` + esc(t(lang, "保存時に、フォーラムが持つ標準名のタグ（WFA / RETAKE / DONE / WIP）から役割の紐づけを自動で作ります。", "On save, the role mapping is derived automatically from each forum's standard tags (WFA / RETAKE / DONE / WIP).")) + `</p>` +
		taskTypeTable + `</section>` +
		`<div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(productionID)+"&tab=notifications", r)) + `">` + esc(t(lang, "通知設定に戻る", "Back to notifications")) + `</a>` + refetchButton + ` <button type="submit" class="btn-ghost">` + esc(t(lang, "保存", "Save")) + `</button>` + createButton + `</div></form>`

	// The result panel appears after the create button ran (done or failed) and
	// whenever the connection's automatic creation left a failure mark.
	result := ""
	if r.FormValue("msg") == "forums_created" || createFailed || autoCheck != "" {
		failure := ""
		if createFailed {
			failure = saveErr.Error()
		} else if autoCheck != "" {
			failure = autoCheck
		}
		result = renderForumResultPanel(lang, current, taskTypes, forumNames, failure, r.FormValue("msg") == "forums_created")
	}

	body := `<div class="section-stack"><section class="section-card glass"><h2>` + esc(project.Name) + `</h2></section>` + errorHTML + notices.String() + result + form + `</div>`
	return adminPage(lang, t(lang, "フォーラム通知の設定", "Forum notification settings"), r, body)
}

// renderForumResultPanel shows what the forum creation produced and the current
// state: for each Task Type the forum it posts to and which of the four role
// tags exist, plus the failure (if any) in the operator's words. failure is
// shown as is; it is already written for the operator.
func renderForumResultPanel(lang string, current model.ProductionForumSettings, taskTypes []kitsu.TaskType, forumNames map[string]string, failure string, justCreated bool) string {
	tagRoles := map[string]struct{}{}
	for _, tag := range current.Tags {
		tagRoles[tag.ForumChannelID+"|"+tag.Role] = struct{}{}
	}
	routeByTaskType := map[string]model.ProductionForumRoute{}
	for _, route := range current.Routes {
		routeByTaskType[route.TaskTypeID] = route
	}
	type stateRow struct{ id, name string }
	var rows []stateRow
	seen := map[string]struct{}{}
	for _, taskType := range taskTypes {
		rows = append(rows, stateRow{taskType.ID, taskType.Name})
		seen[taskType.ID] = struct{}{}
	}
	for _, route := range current.Routes {
		if _, ok := seen[route.TaskTypeID]; !ok {
			rows = append(rows, stateRow{route.TaskTypeID, route.TaskTypeName})
		}
	}
	pill := func(ok bool, yes, no string) string {
		if ok {
			return `<span class="status-pill success">` + esc(yes) + `</span>`
		}
		return `<span class="status-pill warning">` + esc(no) + `</span>`
	}
	var head strings.Builder
	head.WriteString(`<th scope="col">` + esc(t(lang, "タスクタイプ", "Task Type")) + `</th><th scope="col">` + esc(t(lang, "フォーラム", "Forum")) + `</th>`)
	for _, role := range forumRoleOptions {
		head.WriteString(`<th scope="col">` + esc(strings.ToUpper(role)) + `</th>`)
	}
	var body strings.Builder
	created, missing := 0, 0
	for _, row := range rows {
		route, routed := routeByTaskType[row.id]
		forumCell := pill(false, "", t(lang, "未作成", "Not created"))
		if routed && strings.TrimSpace(route.ForumChannelID) != "" {
			created++
			name := strings.TrimSpace(route.ForumChannelName)
			if name == "" {
				name = forumNames[route.ForumChannelID]
			}
			if name == "" {
				name = route.ForumChannelID
			}
			forumCell = `<strong>#` + esc(strings.TrimPrefix(name, "#")) + `</strong>`
		} else {
			missing++
		}
		body.WriteString(`<tr><th scope="row">` + esc(row.name) + `</th><td>` + forumCell + `</td>`)
		for _, role := range forumRoleOptions {
			_, has := tagRoles[route.ForumChannelID+"|"+role]
			has = has && routed
			if has {
				body.WriteString(`<td>` + pill(true, t(lang, "あり", "Yes"), "") + `</td>`)
			} else {
				body.WriteString(`<td></td>`)
			}
		}
		body.WriteString(`</tr>`)
	}
	headline := t(lang, "現在の状態", "Current state")
	summary := fmt.Sprintf(t(lang, "フォーラムが作成済みのタスクタイプ: %d、未作成: %d", "Task Types with a forum: %d, not created: %d"), created, missing)
	var failureHTML string
	switch {
	case failure != "":
		headline = t(lang, "作成結果: 失敗あり", "Result: failed")
		failureHTML = `<div class="notice notice-error" role="alert"><strong>` + esc(t(lang, "失敗した内容", "What failed")) + `</strong><p>` + esc(failure) + `</p></div>`
	case justCreated:
		headline = t(lang, "作成結果: 成功", "Result: created")
	}
	tableHTML := ""
	if created > 0 {
		tableHTML = `<div class="table-wrap"><table class="data-table"><thead><tr>` + head.String() + `</tr></thead><tbody>` + body.String() + `</tbody></table></div>`
	}
	return `<section class="section-card glass forum-result" id="forum-result"><h2>` + esc(t(lang, "作成結果と現在の状態", "Creation result and current state")) + `</h2>` +
		`<p class="field-help" role="status">` + esc(headline+" — "+summary) + `</p>` + failureHTML + tableHTML + `</section>`
}

func forumTaskTypeRow(lang, id, name, selected string, forumOptions [][2]string) string {
	return `<tr><td>` + esc(name) + `<input type="hidden" name="tt_id" value="` + esc(id) + `"><input type="hidden" name="tt_name" value="` + esc(name) + `"></td><td>` +
		forumSelect("tt_forum", selected, forumOptions, t(lang, "（通知しない）", "(no forum)")) + `</td></tr>`
}

// selectedOptionAttr returns the attribute with its leading space, so it can be
// appended right after the value attribute.
func selectedOptionAttr(selected bool) string {
	if selected {
		return " selected"
	}
	return ""
}

// renderForumSettingsLink adds the entry to the forum settings page under the
// Production's notifications tab.
func renderForumSettingsLink(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	return `<section class="section-card glass"><h2>` + esc(t(lang, "フォーラム通知", "Forum notifications")) + `</h2>` +
		`<div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/forum?project="+url.QueryEscape(p.KitsuProjectID), r)) + `">` + esc(t(lang, "フォーラム設定を開く", "Open forum settings")) + `</a></div></section>`
}

// forumSaveErrorMarkup words a rejected save in the viewer's language. Internal
// errors are not shown verbatim; the structured validation problems are.
func forumSaveErrorMarkup(lang string, err error, forumNames map[string]string) string {
	var settingsErr *model.ForumSettingsError
	if !errors.As(err, &settingsErr) {
		return `<p>` + esc(t(lang, "設定を保存できませんでした。しばらくしてからもう一度お試しください。", "The settings could not be stored. Please try again in a moment.")) + `</p>`
	}
	var b strings.Builder
	b.WriteString(`<ul>`)
	for _, problem := range settingsErr.Problems {
		b.WriteString(`<li>` + esc(forumProblemText(lang, problem, forumNames)) + `</li>`)
	}
	b.WriteString(`</ul>`)
	return b.String()
}

func forumProblemText(lang string, problem model.ForumSettingsProblem, forumNames map[string]string) string {
	// Forum arguments are channel IDs; show the forum's name when it is known.
	forumLabel := func(i int) string {
		id := ""
		if i < len(problem.Args) {
			id = problem.Args[i]
		}
		if name := forumNames[id]; name != "" {
			return name
		}
		return id
	}
	arg := func(i int) string {
		if i < len(problem.Args) {
			return problem.Args[i]
		}
		return ""
	}
	switch problem.Code {
	case model.ProblemProductionRequired:
		return t(lang, "プロダクションを特定できません。画面を開き直してください。", "The Production could not be identified. Reopen this page.")
	case model.ProblemRouteIncomplete:
		return t(lang, "タスクタイプとフォーラムの組み合わせが不完全です。", "A Task Type / forum pair is incomplete.")
	case model.ProblemTaskTypeDuplicate:
		return t(lang, "同じタスクタイプが複数回設定されています: ", "The same Task Type is set more than once: ") + arg(0)
	case model.ProblemStatusNoRole:
		return t(lang, "役割が正しくないステータスがあります: ", "A status has no valid role: ") + arg(0)
	case model.ProblemStatusDuplicate:
		return t(lang, "同じステータスが複数回設定されています: ", "The same status is set more than once: ") + arg(0)
	case model.ProblemTagUnroutedForum:
		return t(lang, "どのタスクタイプにも使われていないフォーラムのタグが設定されています: ", "Tags are set for a forum no Task Type uses: ") + forumLabel(0)
	case model.ProblemTagInvalid:
		return t(lang, "タグの設定が正しくありません（フォーラム: ", "A tag setting is not valid (forum: ") + forumLabel(0) + t(lang, "）。", ").")
	case model.ProblemTagRoleDuplicate:
		return t(lang, "同じ役割のタグが複数あります（フォーラム: ", "A role has more than one tag (forum: ") + forumLabel(0) + t(lang, "、役割: ", ", role: ") + forumRoleLabel(lang, arg(1)) + t(lang, "）。", ").")
	case model.ProblemTagReused:
		return t(lang, "1つのタグが複数の役割に使われています（フォーラム: ", "One tag is used for more than one role (forum: ") + forumLabel(0) + t(lang, "）。", ").")
	case model.ProblemGuildRequired:
		return t(lang, "フォーラム方式にするには、DiscordサーバーIDが必要です。", "Forum mode needs the Discord server ID.")
	case model.ProblemRoutesRequired:
		return t(lang, "フォーラム方式にするには、フォーラムを選んだタスクタイプが1つ以上必要です。", "Forum mode needs at least one Task Type with a forum.")
	case model.ProblemRoleStatusMissing:
		return t(lang, "役割「", "No Kitsu status is assigned to the role \"") + forumRoleLabel(lang, arg(0)) + t(lang, "」に割り当てるステータスがありません。", "\".")
	case model.ProblemForumTagMissing:
		return t(lang, "フォーラム「", "Forum \"") + forumLabel(0) + t(lang, "」に、役割「", "\" has no tag for the role \"") + forumRoleLabel(lang, arg(1)) + t(lang, "」のタグがありません。", "\".")
	case model.ProblemForumListUnavailable:
		return t(lang, "タグの名前から紐づけを作るにはDiscordのフォーラム一覧が必要です。Bot設定とサーバーIDを確認し、接続できてから保存してください。", "Deriving the tag mapping from the tag names needs the Discord forum list. Check the Bot settings and the server ID, then save once Discord is reachable.")
	}
	return t(lang, "設定に問題があります。", "There is a problem with the settings.")
}

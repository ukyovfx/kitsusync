package setup

import (
	"net/http"
	"net/url"
	"strings"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

var globalDiscordDirectoryReader = loadGlobalDiscordDirectory

func saveGlobalUserLink(db *gorm.DB, r *http.Request) (bool, string) {
	teamProjectID := strings.TrimSpace(r.FormValue("team_link_project_id"))
	project := (*model.Project)(nil)
	var validatedTeamPerson *kitsu.Person
	if teamProjectID != "" {
		project = model.FindProjectByKitsuID(db, teamProjectID)
		if project == nil {
			return false, ""
		}
		returnURL := withLang("/bot/admin/projects?project="+url.QueryEscape(teamProjectID)+"&tab=team", r)
		if model.IsValidationOnlyProject(db, teamProjectID) || project.ReadOnlyPreview || strings.TrimSpace(project.DiscordGuildID) == "" || strings.TrimSpace(r.FormValue("discord_guild_id")) != strings.TrimSpace(project.DiscordGuildID) {
			return false, returnURL
		}
		personID := strings.TrimSpace(r.FormValue("kitsu_id"))
		if personID == "" {
			return false, returnURL
		}
		team, err := reviewerProductionTeamReader(db, teamProjectID)
		if err != nil {
			return false, returnURL
		}
		found := false
		botEmail := botAccountEmail(db)
		for _, person := range team {
			if strings.TrimSpace(person.ID) != personID || !person.Active || person.Archived || person.IsBot || (botEmail != "" && strings.EqualFold(strings.TrimSpace(person.Email), botEmail)) {
				continue
			}
			validatedTeamPerson = &person
			found = true
			break
		}
		if !found {
			return false, returnURL
		}
	}

	selectedID := strings.TrimSpace(r.FormValue("discord_user_id"))
	guildID := strings.TrimSpace(r.FormValue("discord_guild_id"))
	directory, err := globalDiscordDirectoryReader(storedRuntimeDiscordBotToken(db), guildID)
	validatedGuildID := validatedDiscordGuildSelection(directory, guildID)
	if err != nil || validatedGuildID == "" || selectedID == "" {
		if project != nil {
			return false, withLang("/bot/admin/projects?project="+url.QueryEscape(teamProjectID)+"&tab=team", r)
		}
		return false, globalUserLinkingURL(r, validatedGuildID, "")
	}
	var selected globalDiscordUserOption
	for _, option := range directory.Options {
		if strings.TrimSpace(option.ID) == selectedID {
			selected = option
			break
		}
	}
	if selected.ID == "" || !isDiscordSnowflake(selected.ID) {
		if project != nil {
			return false, withLang("/bot/admin/projects?project="+url.QueryEscape(teamProjectID)+"&tab=team", r)
		}
		return false, globalUserLinkingURL(r, directory.SelectedGuild.ID, "")
	}
	kitsuID, kitsuName, kitsuEmail := r.FormValue("kitsu_id"), r.FormValue("kitsu_name"), r.FormValue("kitsu_email")
	if id := parseUint(r.FormValue("user_id")); id > 0 {
		user := model.FindUserMapByID(db, id)
		if user == nil {
			if project != nil {
				return false, withLang("/bot/admin/projects?project="+url.QueryEscape(teamProjectID)+"&tab=team", r)
			}
			return false, globalUserLinkingURL(r, directory.SelectedGuild.ID, "")
		}
		if validatedTeamPerson == nil {
			if strings.TrimSpace(user.KitsuName) != "" {
				kitsuName = user.KitsuName
			}
			if strings.TrimSpace(user.KitsuEmail) != "" {
				kitsuEmail = user.KitsuEmail
			}
		}
	}
	if validatedTeamPerson != nil {
		kitsuID = strings.TrimSpace(validatedTeamPerson.ID)
		kitsuName = kitsuPersonDisplayName(*validatedTeamPerson)
		kitsuEmail = strings.TrimSpace(validatedTeamPerson.Email)
	}
	model.UpsertUserMapWithIdentity(db, kitsuID, kitsuName, kitsuEmail, directory.SelectedGuild.ID, selected.ID, selected.Name)
	if project != nil {
		return true, withLang("/bot/admin/projects?project="+url.QueryEscape(teamProjectID)+"&tab=team&msg=saved", r)
	}
	return true, globalUserLinkingURL(r, directory.SelectedGuild.ID, "saved")
}

// saveGlobalUserLinks applies a bulk save. Each submitted row is validated with
// the same rules as a single save; rows the client did not change are not sent.
// The Discord directory is read once, and every row is resolved before any row
// is written, so a rejected row cannot leave a partial apply behind.
func saveGlobalUserLinks(db *gorm.DB, r *http.Request) string {
	_ = r.ParseForm()
	userIDs := r.PostForm["bulk_user_id"]
	kitsuIDs := r.PostForm["bulk_kitsu_id"]
	kitsuNames := r.PostForm["bulk_kitsu_name"]
	kitsuEmails := r.PostForm["bulk_kitsu_email"]
	discordIDs := r.PostForm["bulk_discord_user_id"]
	guildID := strings.TrimSpace(r.FormValue("discord_guild_id"))
	directory, err := globalDiscordDirectoryReader(storedRuntimeDiscordBotToken(db), guildID)
	validatedGuildID := validatedDiscordGuildSelection(directory, guildID)
	if err != nil || validatedGuildID == "" {
		return globalUserLinkingURL(r, validatedGuildID, "error")
	}
	type linkRow struct{ kitsuID, kitsuName, kitsuEmail, discordID, discordName string }
	rows := make([]linkRow, 0, len(discordIDs))
	for i, discordID := range discordIDs {
		if i >= len(userIDs) || i >= len(kitsuIDs) {
			return globalUserLinkingURL(r, validatedGuildID, "error")
		}
		selected := globalDiscordUserOption{}
		for _, option := range directory.Options {
			if strings.TrimSpace(option.ID) == strings.TrimSpace(discordID) {
				selected = option
				break
			}
		}
		if selected.ID == "" || !isDiscordSnowflake(selected.ID) {
			return globalUserLinkingURL(r, validatedGuildID, "error")
		}
		kitsuID, kitsuName, kitsuEmail := kitsuIDs[i], formValueAt(kitsuNames, i), formValueAt(kitsuEmails, i)
		if id := parseUint(userIDs[i]); id > 0 {
			user := model.FindUserMapByID(db, id)
			if user == nil {
				return globalUserLinkingURL(r, validatedGuildID, "error")
			}
			if strings.TrimSpace(user.KitsuName) != "" {
				kitsuName = user.KitsuName
			}
			if strings.TrimSpace(user.KitsuEmail) != "" {
				kitsuEmail = user.KitsuEmail
			}
		}
		rows = append(rows, linkRow{kitsuID: kitsuID, kitsuName: kitsuName, kitsuEmail: kitsuEmail, discordID: selected.ID, discordName: selected.Name})
	}
	if len(rows) == 0 {
		return globalUserLinkingURL(r, validatedGuildID, "")
	}
	for _, row := range rows {
		model.UpsertUserMapWithIdentity(db, row.kitsuID, row.kitsuName, row.kitsuEmail, directory.SelectedGuild.ID, row.discordID, row.discordName)
	}
	return globalUserLinkingURL(r, validatedGuildID, "saved")
}

func formValueAt(values []string, index int) string {
	if index < len(values) {
		return values[index]
	}
	return ""
}

func globalUserLinkingURL(r *http.Request, validatedGuildID, message string) string {
	path := "/bot/admin/users"
	if isDiscordSnowflake(validatedGuildID) {
		path += "?discord_guild_id=" + url.QueryEscape(validatedGuildID)
	}
	if message != "" {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		path += separator + "msg=" + url.QueryEscape(message)
	}
	return withLang(path, r)
}

// userLinkingGuildForRedirect revalidates the submitted selection through the
// Bot's live Guild directory before it is retained across a redirect. A
// member-list failure may still return the Guild selected from the trusted
// Guild list, which is sufficient to preserve page context without accepting
// an arbitrary client-supplied Guild ID.
func userLinkingGuildForRedirect(db *gorm.DB, r *http.Request) string {
	guildID := strings.TrimSpace(r.FormValue("discord_guild_id"))
	if !isDiscordSnowflake(guildID) {
		return ""
	}
	directory, _ := globalDiscordDirectoryReader(storedRuntimeDiscordBotToken(db), guildID)
	return validatedDiscordGuildSelection(directory, guildID)
}

func validatedDiscordGuildSelection(directory globalDiscordDirectory, requested string) string {
	requested = strings.TrimSpace(requested)
	if !isDiscordSnowflake(requested) || strings.TrimSpace(directory.SelectedGuild.ID) != requested {
		return ""
	}
	for _, guild := range directory.Guilds {
		if strings.TrimSpace(guild.ID) == requested && strings.TrimSpace(guild.Name) != "" {
			return requested
		}
	}
	return ""
}

// saveTeamUserLinks saves in-place Discord user linking changes submitted from
// the Production Team tab into the canonical global UserMap table.
func saveTeamUserLinks(db *gorm.DB, r *http.Request, botToken string) string {
	_ = r.ParseForm()
	projectID := strings.TrimSpace(r.FormValue("project_id"))
	baseURL := withLang("/bot/admin/projects?project="+url.QueryEscape(projectID)+"&tab=team", r)
	project := model.FindProjectByKitsuID(db, projectID)
	if project == nil || model.IsValidationOnlyProject(db, projectID) || project.ReadOnlyPreview || strings.TrimSpace(project.DiscordGuildID) == "" {
		return baseURL + "&msg=error"
	}
	if strings.TrimSpace(r.FormValue("discord_guild_id")) != strings.TrimSpace(project.DiscordGuildID) {
		return baseURL + "&msg=error"
	}
	team, err := reviewerProductionTeamReader(db, projectID)
	if err != nil {
		return baseURL + "&msg=error"
	}
	botEmail := botAccountEmail(db)
	teamByID := make(map[string]kitsu.Person, len(team))
	for _, person := range team {
		id := strings.TrimSpace(person.ID)
		if id == "" || !person.Active || person.Archived || person.IsBot || (botEmail != "" && strings.EqualFold(strings.TrimSpace(person.Email), botEmail)) {
			continue
		}
		teamByID[id] = person
	}
	clearLink := func(person kitsu.Person) {
		if user := globalUserForKitsuPerson(filterAssignableUsers(model.ListUserMap(db), botEmail), person); user != nil && user.ID > 0 {
			model.UpdateUserMap(db, user.ID, user.KitsuName, user.KitsuEmail, "")
			model.UpdateUserMapDisplayName(db, user.ID, "")
			db.Model(&model.UserMap{}).Where("id = ?", user.ID).Update("discord_guild_id", "")
		}
	}
	if unlinkID := strings.TrimSpace(r.FormValue("unlink_team_kitsu_id")); unlinkID != "" {
		person, ok := teamByID[unlinkID]
		if !ok {
			return baseURL + "&msg=error"
		}
		clearLink(person)
		return baseURL + "&msg=saved"
	}
	kitsuIDs := r.PostForm["team_kitsu_id"]
	discordIDs := r.PostForm["team_discord_user_id"]
	if len(kitsuIDs) != len(discordIDs) {
		return baseURL + "&msg=error"
	}
	if strings.TrimSpace(botToken) == "" {
		botToken = storedRuntimeDiscordBotToken(db)
	}
	directory, err := globalDiscordDirectoryReader(botToken, project.DiscordGuildID)
	if err != nil || validatedDiscordGuildSelection(directory, project.DiscordGuildID) == "" {
		return baseURL + "&msg=error"
	}
	optionsByID := make(map[string]globalDiscordUserOption, len(directory.Options))
	for _, opt := range directory.Options {
		optionsByID[strings.TrimSpace(opt.ID)] = opt
	}
	type pendingLink struct {
		person kitsu.Person
		opt    *globalDiscordUserOption
	}
	pending := make([]pendingLink, 0, len(kitsuIDs))
	for i, rawKitsuID := range kitsuIDs {
		person, ok := teamByID[strings.TrimSpace(rawKitsuID)]
		if !ok {
			return baseURL + "&msg=error"
		}
		discordID := strings.TrimSpace(discordIDs[i])
		if discordID == "" {
			pending = append(pending, pendingLink{person: person})
			continue
		}
		opt, ok := optionsByID[discordID]
		if !ok || !isDiscordSnowflake(opt.ID) {
			return baseURL + "&msg=error"
		}
		optCopy := opt
		pending = append(pending, pendingLink{person: person, opt: &optCopy})
	}
	for _, item := range pending {
		if item.opt == nil {
			clearLink(item.person)
		} else {
			model.UpsertUserMapWithIdentity(db, strings.TrimSpace(item.person.ID), kitsuPersonDisplayName(item.person), strings.TrimSpace(item.person.Email), project.DiscordGuildID, item.opt.ID, item.opt.Name)
		}
	}
	return baseURL + "&msg=saved"
}

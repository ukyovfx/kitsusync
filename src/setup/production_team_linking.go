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

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
	if err != nil || directory.SelectedGuild.ID == "" || strings.TrimSpace(directory.SelectedGuild.ID) != guildID || selectedID == "" {
		if project != nil {
			return false, withLang("/bot/admin/projects?project="+url.QueryEscape(teamProjectID)+"&tab=team", r)
		}
		return false, ""
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
		return false, ""
	}
	kitsuID, kitsuName, kitsuEmail := r.FormValue("kitsu_id"), r.FormValue("kitsu_name"), r.FormValue("kitsu_email")
	if id := parseUint(r.FormValue("user_id")); id > 0 {
		user := model.FindUserMapByID(db, id)
		if user == nil {
			if project != nil {
				return false, withLang("/bot/admin/projects?project="+url.QueryEscape(teamProjectID)+"&tab=team", r)
			}
			return false, ""
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
	return true, withLang("/bot/admin/users?msg=saved", r)
}

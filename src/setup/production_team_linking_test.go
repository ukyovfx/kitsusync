package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

func TestTeamLinkSaveUsesGlobalUserMapAndRevalidatesLiveTeam(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "team-link-production", Name: "Team Link Production", DiscordGuildID: "11111111111111111"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	oldTeamReader, oldDirectoryReader := reviewerProductionTeamReader, globalDiscordDirectoryReader
	defer func() { reviewerProductionTeamReader, globalDiscordDirectoryReader = oldTeamReader, oldDirectoryReader }()
	team := []kitsu.Person{{ID: "person-current", FullName: "Current Member", Email: "member@example.invalid", Active: true}}
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) { return team, nil }
	globalDiscordDirectoryReader = func(_, guildID string) (globalDiscordDirectory, error) {
		return globalDiscordDirectory{SelectedGuild: DiscordGuild{ID: guildID}, Options: []globalDiscordUserOption{{ID: "123456789012345678", Name: "Member"}}}, nil
	}
	values := url.Values{
		"action": {"save_global_link"}, "team_link_project_id": {project.KitsuProjectID},
		"kitsu_id": {"person-current"}, "kitsu_name": {"spoofed name"},
		"kitsu_email": {"spoofed@example.invalid"}, "discord_guild_id": {project.DiscordGuildID},
		"discord_user_id": {"123456789012345678"},
	}
	request := httptest.NewRequest("POST", "/bot/admin/users", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	UsersHandler(db, "").ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "project=team-link-production") || !strings.Contains(response.Header().Get("Location"), "tab=team") {
		t.Fatalf("valid live Team mapping did not use the global User Linking save path: status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
	userMaps := model.ListUserMap(db)
	if len(userMaps) != 1 || userMaps[0].KitsuID != "person-current" || userMaps[0].KitsuName != "Current Member" || userMaps[0].KitsuEmail != "member@example.invalid" || userMaps[0].DiscordID != "123456789012345678" || userMaps[0].DiscordGuildID != project.DiscordGuildID {
		t.Fatalf("Team link did not update the canonical global UserMap: %#v", userMaps)
	}
	if got := model.ListProjectUserMaps(db, project.ID); len(got) != 0 {
		t.Fatalf("Team link wrote Production-local mappings: %#v", got)
	}

	team = []kitsu.Person{{ID: "person-other", FullName: "Other Member", Email: "other@example.invalid", Active: true}}
	staleRequest := httptest.NewRequest("POST", "/bot/admin/users", strings.NewReader(values.Encode()))
	staleRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	staleResponse := httptest.NewRecorder()
	UsersHandler(db, "").ServeHTTP(staleResponse, staleRequest)
	if !strings.Contains(staleResponse.Header().Get("Location"), "msg=error") {
		t.Fatal("stale/non-Team person was accepted")
	}
	if got := len(model.ListUserMap(db)); got != 1 {
		t.Fatalf("rejected stale Team link changed global mappings: %d", got)
	}
}

func TestProductionTeamRendersGlobalUserLinkingModalOnlyAfterGuildRead(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "team-modal-production", Name: "Team Modal", DiscordGuildID: "11111111111111111"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	oldTeamReader, oldTaskReader, oldGuildReader := reviewerProductionTeamReader, reviewerTaskTypesForProduction, reviewerGuildMembersForGuild
	defer func() {
		reviewerProductionTeamReader, reviewerTaskTypesForProduction, reviewerGuildMembersForGuild = oldTeamReader, oldTaskReader, oldGuildReader
	}()
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) {
		return []kitsu.Person{{ID: "team-person", FullName: "Team Person", Email: "team@example.invalid", Active: true}}, nil
	}
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType { return nil }
	reviewerGuildMembersForGuild = func(string, string) ([]DiscordGuildMember, error) {
		var member DiscordGuildMember
		member.User.ID = "22222222222222222"
		member.User.Username = "candidate"
		return []DiscordGuildMember{member}, nil
	}
	request := httptest.NewRequest("GET", "/bot/admin/projects?project=team-modal-production&tab=team&lang=en", nil)
	body := renderCurrentProductionTeam(db, request, project, "en", "test-token")
	for _, want := range []string{`data-open-team-link`, `data-team-link-modal`, `name="team_link_project_id"`, `name="action" value="save_global_link"`, `Link Discord user`, `candidate`} {
		if !strings.Contains(body, want) {
			t.Fatalf("Team User Linking modal missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "ProjectUserMap") || strings.Contains(body, "add_production_user") {
		t.Fatal("Team modal exposes Production-local membership mutation")
	}
	reviewerGuildMembersForGuild = func(string, string) ([]DiscordGuildMember, error) { return nil, errors.New("Guild unavailable") }
	body = renderCurrentProductionTeam(db, request, project, "en", "test-token")
	if strings.Contains(body, `data-team-link-modal`) || strings.Contains(body, `data-open-team-link`) {
		t.Fatal("Team modal remained available without a verified Guild read")
	}
}

package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/model"
)

// One save applies every changed row; unchanged rows are never sent by the
// client, and each submitted row still goes through the single-row validation.
func TestGlobalUserLinkingBulkSaveAppliesAllChangedRows(t *testing.T) {
	db := newIAViewDB(t)
	const guildID = "11111111111111111"
	oldDirectoryReader := globalDiscordDirectoryReader
	t.Cleanup(func() { globalDiscordDirectoryReader = oldDirectoryReader })
	globalDiscordDirectoryReader = func(_, requested string) (globalDiscordDirectory, error) {
		guild := DiscordGuild{ID: requested, Name: "Fixture Guild"}
		return globalDiscordDirectory{Guilds: []DiscordGuild{guild}, SelectedGuild: guild, Options: []globalDiscordUserOption{
			{ID: "123456789012345678", Name: "First"},
			{ID: "123456789012345679", Name: "Second"},
		}}, nil
	}

	values := url.Values{
		"action":               {"save_global_links"},
		"discord_guild_id":     {guildID},
		"bulk_user_id":         {"", ""},
		"bulk_kitsu_id":        {"person-1", "person-2"},
		"bulk_kitsu_name":      {"First Person", "Second Person"},
		"bulk_kitsu_email":     {"first@example.invalid", "second@example.invalid"},
		"bulk_discord_user_id": {"123456789012345678", "123456789012345679"},
	}
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/users", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	UsersHandler(db, "").ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "msg=saved") {
		t.Fatalf("bulk save did not report success: status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
	maps := model.ListUserMap(db)
	if len(maps) != 2 {
		t.Fatalf("bulk save did not apply every changed row: %#v", maps)
	}
	byKitsu := map[string]model.UserMap{}
	for _, m := range maps {
		byKitsu[m.KitsuID] = m
	}
	if byKitsu["person-1"].DiscordID != "123456789012345678" || byKitsu["person-2"].DiscordID != "123456789012345679" {
		t.Fatalf("bulk save stored the wrong Discord identities: %#v", byKitsu)
	}
	if byKitsu["person-1"].DiscordGuildID != guildID {
		t.Fatalf("bulk save lost the validated Guild: %#v", byKitsu["person-1"])
	}
}

func TestGlobalUserLinkingBulkSaveRejectsInvalidRowWithoutWritingOthers(t *testing.T) {
	db := newIAViewDB(t)
	const guildID = "11111111111111111"
	oldDirectoryReader := globalDiscordDirectoryReader
	t.Cleanup(func() { globalDiscordDirectoryReader = oldDirectoryReader })
	globalDiscordDirectoryReader = func(_, requested string) (globalDiscordDirectory, error) {
		guild := DiscordGuild{ID: requested, Name: "Fixture Guild"}
		return globalDiscordDirectory{Guilds: []DiscordGuild{guild}, SelectedGuild: guild, Options: []globalDiscordUserOption{
			{ID: "123456789012345678", Name: "First"},
		}}, nil
	}

	values := url.Values{
		"action":               {"save_global_links"},
		"discord_guild_id":     {guildID},
		"bulk_user_id":         {"", ""},
		"bulk_kitsu_id":        {"person-1", "person-2"},
		"bulk_kitsu_name":      {"First Person", "Second Person"},
		"bulk_kitsu_email":     {"", ""},
		"bulk_discord_user_id": {"123456789012345678", "not-a-snowflake"},
	}
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/users", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	UsersHandler(db, "").ServeHTTP(response, request)

	if !strings.Contains(response.Header().Get("Location"), "msg=error") {
		t.Fatalf("bulk save did not report the rejected row: %q", response.Header().Get("Location"))
	}
	if maps := model.ListUserMap(db); len(maps) != 0 {
		t.Fatalf("a rejected row still applied part of the batch: %#v", maps)
	}
}

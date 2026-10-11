package setup

import (
	"app/src/model"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func userLinkingIdentityDB(t *testing.T, discordID, storedName string) *gorm.DB {
	t.Helper()
	db := newIAViewDB(t)
	if err := db.Create(&model.UserMap{KitsuID: "person-1", KitsuName: "Kitsu Person", DiscordID: discordID, DiscordDisplayName: storedName}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func userLinkingIdentityDirectory(options ...globalDiscordUserOption) globalDiscordDirectory {
	guild := DiscordGuild{ID: "guild-selected", Name: "Selected Guild"}
	return globalDiscordDirectory{Guilds: []DiscordGuild{guild}, SelectedGuild: guild, Options: options}
}

func userLinkingTestGuildMember(id, username, globalName, nickname string) DiscordGuildMember {
	member := DiscordGuildMember{Nick: nickname}
	member.User.ID = id
	member.User.Username = username
	member.User.GlobalName = globalName
	return member
}

func renderUserLinkingIdentity(db *gorm.DB, lang string, directory globalDiscordDirectory) string {
	return renderGlobalUserLinkingTable(db, lang, directory, []KitsuPerson{{ID: "person-1", FullName: "Kitsu Person", Active: true}})
}

func TestUserLinkingSeparatesVerifiedUsernameFromGuildNickname(t *testing.T) {
	const discordID = "123456789012345678"
	db := userLinkingIdentityDB(t, discordID, "stale cached nickname")
	body := renderUserLinkingIdentity(db, "en", userLinkingIdentityDirectory(globalDiscordUserOption{ID: discordID, Username: "global_user", Name: "Studio Nick"}))
	if !strings.Contains(body, "@global_user") || !strings.Contains(body, "Studio Nick") || strings.Contains(body, "stale cached nickname") {
		t.Fatalf("table must show live global username and dropdown live nickname, not stored display cache: %s", body)
	}
	if !strings.Contains(body, `option value="`+discordID+`"`) || !strings.Contains(body, `selected>Studio Nick</option>`) {
		t.Fatal("selection value must remain the immutable Discord User ID")
	}
}

func TestUserLinkingDropdownFallsBackFromNicknameToGlobalNameThenUsername(t *testing.T) {
	db := userLinkingIdentityDB(t, "", "")
	tests := []struct {
		name string
		user globalDiscordUserOption
		want string
	}{
		{name: "global name fallback", user: globalDiscordUserOption{ID: "id-1", Username: "account", Name: "Global Name"}, want: "Global Name"},
		{name: "username fallback", user: globalDiscordUserOption{ID: "id-2", Username: "account_name", Name: "account_name"}, want: "account_name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := renderUserLinkingIdentity(db, "en", userLinkingIdentityDirectory(tc.user))
			if !strings.Contains(body, ">"+tc.want+"</option>") {
				t.Fatalf("dropdown did not use the expected live fallback %q: %s", tc.want, body)
			}
		})
	}
}

func TestUserLinkingIdentityChangesKeepSameDiscordUserID(t *testing.T) {
	const discordID = "123456789012345678"
	db := userLinkingIdentityDB(t, discordID, "old cached name")
	first := renderUserLinkingIdentity(db, "en", userLinkingIdentityDirectory(globalDiscordUserOption{ID: discordID, Username: "before", Name: "Before Nick"}))
	second := renderUserLinkingIdentity(db, "en", userLinkingIdentityDirectory(globalDiscordUserOption{ID: discordID, Username: "after", Name: "After Nick"}))
	for _, body := range []string{first, second} {
		if !strings.Contains(body, `option value="`+discordID+`"`) || !strings.Contains(body, ` selected>`) {
			t.Fatalf("username/nickname changes must not alter the saved identity: %s", body)
		}
	}
	if !strings.Contains(first, "@before") || !strings.Contains(first, "Before Nick") || !strings.Contains(second, "@after") || !strings.Contains(second, "After Nick") {
		t.Fatal("presentation should follow current live Discord identity fields")
	}
}

func TestUserLinkingUnavailableGuildMemberDoesNotFabricateOrUnlink(t *testing.T) {
	const linkedID = "123456789012345678"
	db := userLinkingIdentityDB(t, linkedID, "cached nickname must not be trusted")
	body := renderUserLinkingIdentity(db, "en", userLinkingIdentityDirectory(globalDiscordUserOption{ID: "different-user", Username: "other", Name: "Other User"}))
	for _, want := range []string{"Unavailable in selected Discord server", `option value="` + linkedID + `" selected disabled`, `action" value="remove_global_link"`, `name="discord_user_id"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("unavailable mapping lost truthful status, preserved selection, or unlink action %q: %s", want, body)
		}
	}
	if strings.Contains(body, "cached nickname must not be trusted") || strings.Contains(body, "@cached") {
		t.Fatal("cached Discord display name was fabricated as a verified global username")
	}
}

func TestUserLinkingDuplicateGuildNicknamesAreDisambiguatedByAccount(t *testing.T) {
	members := []DiscordGuildMember{
		userLinkingTestGuildMember("123456789012345678", "first_user", "", "Same Nick"),
		userLinkingTestGuildMember("123456789012345679", "second_user", "", "Same Nick"),
	}
	options := globalDiscordOptionsFromMembers(members)
	if len(options) != 2 || options[0].Name == options[1].Name || !strings.Contains(options[0].Name+options[1].Name, "@first_user") || !strings.Contains(options[0].Name+options[1].Name, "@second_user") {
		t.Fatalf("duplicate nicknames must be minimally disambiguated while preserving user IDs: %+v", options)
	}
	duplicateUsernames := globalDiscordOptionsFromMembers([]DiscordGuildMember{
		userLinkingTestGuildMember("123456789012345678", "same_account", "", "Same Nick"),
		userLinkingTestGuildMember("123456789012345679", "same_account", "", "Same Nick"),
	})
	if len(duplicateUsernames) != 2 || duplicateUsernames[0].Name == duplicateUsernames[1].Name ||
		!strings.Contains(duplicateUsernames[0].Name+duplicateUsernames[1].Name, "ID 123456789012345678") ||
		!strings.Contains(duplicateUsernames[0].Name+duplicateUsernames[1].Name, "ID 123456789012345679") {
		t.Fatalf("duplicate nicknames and usernames must remain unambiguous by stable ID: %+v", duplicateUsernames)
	}
}

func TestUserLinkingLiveDirectoryUsesNicknameThenGlobalNameThenUsername(t *testing.T) {
	options := globalDiscordOptionsFromMembers([]DiscordGuildMember{
		userLinkingTestGuildMember("123456789012345678", "first_account", "Global Fallback", "Server Nick"),
		userLinkingTestGuildMember("123456789012345679", "second_account", "Global Fallback", ""),
		userLinkingTestGuildMember("123456789012345680", "third_account", "", ""),
	})
	byID := make(map[string]string)
	for _, option := range options {
		byID[option.ID] = option.Name
	}
	if byID["123456789012345678"] != "Server Nick" || byID["123456789012345679"] != "Global Fallback" || byID["123456789012345680"] != "third_account" {
		t.Fatalf("live Guild display name priority changed: %#v", byID)
	}
}

func TestUserLinkingJapaneseUnavailableStateAndEscaping(t *testing.T) {
	const linkedID = "123456789012345678"
	db := userLinkingIdentityDB(t, linkedID, "<script>cached()</script>")
	option := globalDiscordUserOption{ID: linkedID, Username: `<img src=x onerror=alert(1)>`, Name: `<script>alert(1)</script>`}
	available := renderUserLinkingIdentity(db, "ja", userLinkingIdentityDirectory(option))
	if !strings.Contains(available, `@&lt;img src=x onerror=alert(1)&gt;`) || !strings.Contains(available, `&lt;script&gt;alert(1)&lt;/script&gt;`) || strings.Contains(available, `<script>`) || strings.Contains(available, `<img`) {
		t.Fatalf("Discord identity fields were not escaped: %s", available)
	}
	unavailable := renderUserLinkingIdentity(db, "ja", userLinkingIdentityDirectory())
	for _, want := range []string{"選択中のDiscordサーバーで利用できません", "要確認", `option value="` + linkedID + `" selected disabled`} {
		if !strings.Contains(unavailable, want) {
			t.Fatalf("Japanese unavailable state is missing %q: %s", want, unavailable)
		}
	}
	if strings.Contains(unavailable, "cached()") {
		t.Fatal("unavailable linked user showed unverified cached identity")
	}
}

func TestUserLinkingLongDiscordNicknameUsesConstrainedIdentityCell(t *testing.T) {
	longNick := "Long" + strings.Repeat("Nickname", 30)
	db := userLinkingIdentityDB(t, "", "")
	body := renderUserLinkingIdentity(db, "en", userLinkingIdentityDirectory(globalDiscordUserOption{ID: "new-id", Username: "account", Name: longNick}))
	if !strings.Contains(body, `title="`+longNick+`">`+longNick+`</option>`) || !strings.Contains(adminThemeCSS, `.user-link-actions .user-link-form select{min-width:0;width:100%}`) {
		t.Fatal("long Discord nickname should retain a full native option label in a width-constrained selection control")
	}
}

func TestUserLinkingExistingSaveAndUnlinkControlsRemain(t *testing.T) {
	db := userLinkingIdentityDB(t, "123456789012345678", "cached")
	body := renderUserLinkingIdentity(db, "en", userLinkingIdentityDirectory(globalDiscordUserOption{ID: "123456789012345678", Username: "global_user", Name: "Guild Nick"}))
	if strings.Count(body, `type="submit" disabled`) != 1 || !strings.Contains(body, `action" value="save_global_link"`) || !strings.Contains(body, `action" value="remove_global_link"`) {
		t.Fatal("User Linking save/unlink actions or disabled initial Save behavior changed")
	}
}

func TestUserLinkingTableLabelsRemainLocalized(t *testing.T) {
	db := userLinkingIdentityDB(t, "", "")
	directory := userLinkingIdentityDirectory(globalDiscordUserOption{ID: "123456789012345678", Username: "global_user", Name: "Guild Nick"})
	for _, tc := range []struct{ lang, userLabel, actionLabel string }{{"ja", "Discordユーザー", "操作"}, {"en", "Discord user", "Action"}} {
		body := renderUserLinkingIdentity(db, tc.lang, directory)
		if !strings.Contains(body, tc.userLabel) || !strings.Contains(body, tc.actionLabel) {
			t.Errorf("%s User Linking table lost localized column labels: %s", tc.lang, body)
		}
	}
}

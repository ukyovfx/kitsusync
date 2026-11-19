package setup

import (
	"strings"
	"testing"
	"time"
)

// warmGuildDirectoryCache injects the member and role list bodies as fresh
// snapshot entries, so the resolution reads them without any network effort.
// The injection is cleaned up afterwards.
func warmGuildDirectoryCache(t *testing.T, guildID, membersBody, rolesBody string) {
	t.Helper()
	fetchedAt := time.Now()
	guildMemberListCache.mu.Lock()
	guildMemberListCache.entries[guildID] = &guildDirectoryCacheEntry{body: []byte(membersBody), fetchedAt: fetchedAt}
	guildMemberListCache.mu.Unlock()
	guildRoleListCache.mu.Lock()
	guildRoleListCache.entries[guildID] = &guildDirectoryCacheEntry{body: []byte(rolesBody), fetchedAt: fetchedAt}
	guildRoleListCache.mu.Unlock()
	t.Cleanup(func() { invalidateGuildDirectory(guildID) })
}

// TestGuildAdministratorDiscordIDsFilteringAndDegradation filters the Guild's
// administrators out of the cached directory snapshots, so the fallback notify
// resolves through them.
func TestGuildAdministratorDiscordIDsFilteringAndDegradation(t *testing.T) {
	guildID := "123456789012345678"
	linkAdmin := "123456789012345601"
	linkMember := "123456789012345602"
	unknownHolder := "123456789012345603"
	rolelessMember := "123456789012345604"
	memberBody := `[
		{"user":{"id":"` + linkAdmin + `"},"roles":["123456789012345701"]},
		{"user":{"id":"` + linkMember + `"},"roles":["123456789012345702"]},
		{"user":{"id":"` + unknownHolder + `"},"roles":["123456789012345703"]},
		{"user":{"id":"` + rolelessMember + `"},"roles":[]},
		{"user":{"id":"guild-1"},"roles":["123456789012345701"]},
		{"user":{"id":"` + linkAdmin + `"},"roles":["123456789012345701"]}
	]`

	for _, tc := range []struct {
		name        string
		rolesBody   string
		want        string
		description string
	}{
		{
			name:        "only members holding an Administrator role resolve through",
			rolesBody:   `[{"id":"123456789012345701","permissions":"8"},{"id":"123456789012345702","permissions":"1024"}]`,
			want:        linkAdmin,
			description: "linkMember and the unknown-role holder fall off, and duplicate members deduplicate",
		},
		{
			name:        "unreadable permission bits degrade to not judgeable",
			rolesBody:   `[{"id":"123456789012345701","permissions":"8"},{"id":"123456789012345702","permissions":"not-a-number"}]`,
			want:        linkAdmin,
			description: "the unreadable role grants no provable admin, so its holders fall off",
		},
		{
			name:        "Administrator on the Everyone role resolves every linked member",
			rolesBody:   `[{"id":"` + guildID + `","permissions":"8"}]`,
			want:        strings.Join([]string{linkAdmin, linkMember, unknownHolder, rolelessMember}, ","),
			description: "the Everyone role shares the Guild's ID, and the synthetic member ID falls off the snowflake check",
		},
		{
			name:        "zero administrators resolve to no destination without an error",
			rolesBody:   `[{"id":"123456789012345701","permissions":"0"},{"id":"` + guildID + `","permissions":"0"}]`,
			want:        "",
			description: "role-less members judge against the Everyone bits and resolve to no destination",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			warmGuildDirectoryCache(t, guildID, memberBody, tc.rolesBody)

			ids := GuildAdministratorDiscordIDs(guildID, "synthetic-token")
			if got := strings.Join(ids, ","); got != tc.want {
				t.Fatalf("%s: got [%s], want [%s]", tc.description, got, tc.want)
			}
		})
	}
}

package setup

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// handoffDirectoryMember is the slice of a cached Guild member list body the
// admin fallback needs: the Discord user ID and that member's role IDs.
type handoffDirectoryMember struct {
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Roles []string `json:"roles"`
}

// handoffDirectoryRole is the slice of a cached Guild role list body the admin
// fallback needs: the role ID and its permission bitfield.
type handoffDirectoryRole struct {
	ID          string `json:"id"`
	Permissions string `json:"permissions"`
}

// GuildAdministratorDiscordIDs resolves the Guild members whose roles carry
// the Discord Administrator permission, so the handoff admin fallback only
// reaches people who can actually run the server. It reads the cached member
// and role snapshots and never waits on the network: a cold cache resolves no
// one for this poll and settles on the next one, and zero administrators
// resolves to no destination instead of an error.
func GuildAdministratorDiscordIDs(guildID, botToken string) []string {
	guildID = strings.TrimSpace(guildID)
	token := strings.TrimSpace(botToken)
	if !isDiscordSnowflake(guildID) {
		return nil
	}
	members := guildMemberListCache.view(guildID, token)
	roles := guildRoleListCache.view(guildID, token)
	if !members.hasData() || !roles.hasData() {
		return nil
	}
	var directoryMembers []handoffDirectoryMember
	if err := json.Unmarshal(members.body, &directoryMembers); err != nil {
		return nil
	}
	var directoryRoles []handoffDirectoryRole
	if err := json.Unmarshal(roles.body, &directoryRoles); err != nil {
		return nil
	}
	// Unreadable permission bits degrade to "not judgeable": the role grants
	// no provable admin, so members holding only it fall off.
	permissionBits := make(map[string]uint64, len(directoryRoles))
	for _, role := range directoryRoles {
		id := strings.TrimSpace(role.ID)
		if !isDiscordSnowflake(id) {
			continue
		}
		bits, err := strconv.ParseUint(strings.TrimSpace(role.Permissions), 10, 64)
		if err != nil {
			continue
		}
		permissionBits[id] = bits
	}
	// The @everyone role shares the Guild's ID, so its bits apply to every
	// member before any role check.
	base := permissionBits[guildID]
	seen := make(map[string]struct{}, len(directoryMembers))
	admins := make([]string, 0, len(directoryMembers))
	for _, member := range directoryMembers {
		id := strings.TrimSpace(member.User.ID)
		if !isDiscordSnowflake(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if base&discordPermissionAdministrator != 0 || memberHoldsAdministrator(member.Roles, permissionBits) {
			seen[id] = struct{}{}
			admins = append(admins, id)
		}
	}
	sort.Strings(admins)
	return admins
}

// memberHoldsAdministrator reports whether one of the member's known roles
// carries the Administrator bit; unknown role IDs (e.g. deleted since the
// snapshot) prove nothing and fall off.
func memberHoldsAdministrator(roleIDs []string, permissionBits map[string]uint64) bool {
	for _, roleID := range roleIDs {
		if permissionBits[roleID]&discordPermissionAdministrator != 0 {
			return true
		}
	}
	return false
}

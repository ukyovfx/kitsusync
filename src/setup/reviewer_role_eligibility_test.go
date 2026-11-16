package setup

import (
	"testing"

	"app/src/model"
)

func TestForumRoleEligibilityUsesMentionableOrVerifiedBotPermission(t *testing.T) {
	const guildID = "123456789012345600"
	const channelID = "123456789012345601"
	verified := reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, BotID: "123456789012345603", PermissionsKnown: true, CanMentionRoles: true}
	cases := []struct {
		name     string
		role     DiscordGuildRole
		evidence reviewerRoleSendEvidence
		want     bool
	}{
		{name: "mentionable role needs no mention permission bit", role: DiscordGuildRole{ID: "123456789012345604", Mentionable: true}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, BotID: "123456789012345603", PermissionsKnown: true}, want: true},
		{name: "unknown permissions deny even a mentionable role", role: DiscordGuildRole{ID: "123456789012345604", Mentionable: true}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, BotID: "123456789012345603"}, want: false},
		{name: "stale channel identity fails closed", role: DiscordGuildRole{ID: "123456789012345604", Mentionable: true}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: "123456789012345609", BotID: "123456789012345603", PermissionsKnown: true}, want: false},
		{name: "nonmentionable role allowed with verified channel permission", role: DiscordGuildRole{ID: "123456789012345604"}, evidence: verified, want: true},
		{name: "nonmentionable role denied without permission", role: DiscordGuildRole{ID: "123456789012345604"}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, BotID: "123456789012345603", PermissionsKnown: true}, want: false},
		{name: "unknown permission fails closed", role: DiscordGuildRole{ID: "123456789012345604"}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, BotID: "123456789012345603", CanMentionRoles: true}, want: false},
		{name: "wrong guild fails closed", role: DiscordGuildRole{ID: "123456789012345604", Mentionable: true}, evidence: reviewerRoleSendEvidence{GuildID: "123456789012345699", ChannelID: channelID, BotID: "123456789012345603", PermissionsKnown: true}, want: false},
		{name: "everyone is never eligible", role: DiscordGuildRole{ID: guildID, Mentionable: true}, evidence: verified, want: false},
		{name: "invalid role id is never eligible", role: DiscordGuildRole{ID: "@everyone", Mentionable: true}, evidence: verified, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := forumRoleCanBeMentioned(guildID, channelID, tc.role, tc.evidence); got != tc.want {
				t.Fatalf("eligible=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyReviewerRoleEligibilityUsesForumDestinationAndCurrentBotEvidence(t *testing.T) {
	db, project := reviewerResolutionTestDB(t)
	seedReviewerForumRoute(t, db, project, "task-1")
	oldRoles, oldEvidence := reviewerGuildRolesReader, forumReviewerRoleEvidenceReader
	permissions := false
	reviewerGuildRolesReader = func(_, _ string) ([]DiscordGuildRole, error) {
		return []DiscordGuildRole{{ID: "123456789012345604", Name: "Quiet", Mentionable: false}}, nil
	}
	forumReviewerRoleEvidenceReader = func(guildID, channelID, _ string) (reviewerRoleSendEvidence, error) {
		return reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, BotID: "123456789012345603", PermissionsKnown: true, CanMentionRoles: permissions}, nil
	}
	t.Cleanup(func() { reviewerGuildRolesReader, forumReviewerRoleEvidenceReader = oldRoles, oldEvidence })
	change := []model.ProductionReviewerDelta{{TaskTypeID: "task-1", AddRoleIDs: []string{"123456789012345604"}}}
	if err := validateProductionReviewerDeltas(db, project, change, "bot-token"); err == nil {
		t.Fatal("non-mentionable Role without destination permission was accepted")
	}
	permissions = true
	if err := validateProductionReviewerDeltas(db, project, change, "bot-token"); err != nil {
		t.Fatalf("verified Bot permission did not allow non-mentionable Role: %v", err)
	}
	change[0].AddRoleIDs = []string{"123456789012345699"}
	if err := validateProductionReviewerDeltas(db, project, change, "bot-token"); err == nil {
		t.Fatal("Role absent from the linked Guild was accepted")
	}
}

func TestEffectiveDiscordChannelPermissionsApplyRoleAndMemberOverwrites(t *testing.T) {
	const guildID = "123456789012345600"
	const botID = "123456789012345603"
	const roleID = "123456789012345604"
	roles := []DiscordGuildRoleWithPermissions{
		{ID: guildID, Permissions: "0"},
		{ID: roleID, Permissions: "134144"}, // VIEW_CHANNEL | SEND_MESSAGES | MENTION_EVERYONE
	}
	member := DiscordPermissionMember{ID: botID, RoleIDs: []string{roleID}}
	channel := DiscordPermissionChannel{GuildID: guildID, ID: "123456789012345601", Overwrites: []DiscordPermissionOverwrite{
		{ID: roleID, Type: 0, Allow: "0", Deny: "131072"},
		{ID: botID, Type: 1, Allow: "131072", Deny: "0"},
	}}
	got, err := effectiveDiscordChannelPermissions(guildID, roles, member, channel)
	if err != nil {
		t.Fatal(err)
	}
	if !got.MentionEveryone || !got.SendMessages {
		t.Fatalf("member overwrite did not apply after role deny: %+v", got)
	}

	channel.Overwrites[1] = DiscordPermissionOverwrite{ID: botID, Type: 1, Allow: "0", Deny: "131072"}
	got, err = effectiveDiscordChannelPermissions(guildID, roles, member, channel)
	if err != nil {
		t.Fatal(err)
	}
	if got.MentionEveryone {
		t.Fatalf("member deny did not override role permission: %+v", got)
	}
	channel.Overwrites[1] = DiscordPermissionOverwrite{ID: botID, Type: 1, Allow: "131072", Deny: "1024"}
	got, err = effectiveDiscordChannelPermissions(guildID, roles, member, channel)
	if err != nil {
		t.Fatal(err)
	}
	if got.SendMessages || got.MentionEveryone {
		t.Fatalf("missing VIEW_CHANNEL did not deny send/mention permissions: %+v", got)
	}
}

func TestEffectiveDiscordChannelPermissionsFailsClosedOnMalformedEvidence(t *testing.T) {
	const guildID = "123456789012345600"
	cases := []struct {
		name    string
		roles   []DiscordGuildRoleWithPermissions
		member  DiscordPermissionMember
		channel DiscordPermissionChannel
	}{
		{name: "guild mismatch", roles: []DiscordGuildRoleWithPermissions{{ID: guildID, Permissions: "0"}}, member: DiscordPermissionMember{ID: "123456789012345603"}, channel: DiscordPermissionChannel{GuildID: "123456789012345699", ID: "123456789012345601"}},
		{name: "missing everyone role", roles: []DiscordGuildRoleWithPermissions{{ID: "123456789012345604", Permissions: "131072"}}, member: DiscordPermissionMember{ID: "123456789012345603", RoleIDs: []string{"123456789012345604"}}, channel: DiscordPermissionChannel{GuildID: guildID, ID: "123456789012345601"}},
		{name: "malformed permission string", roles: []DiscordGuildRoleWithPermissions{{ID: guildID, Permissions: "unknown"}}, member: DiscordPermissionMember{ID: "123456789012345603"}, channel: DiscordPermissionChannel{GuildID: guildID, ID: "123456789012345601"}},
		{name: "unknown overwrite type", roles: []DiscordGuildRoleWithPermissions{{ID: guildID, Permissions: "0"}}, member: DiscordPermissionMember{ID: "123456789012345603"}, channel: DiscordPermissionChannel{GuildID: guildID, ID: "123456789012345601", Overwrites: []DiscordPermissionOverwrite{{ID: "123456789012345604", Type: 9, Allow: "131072", Deny: "0"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := effectiveDiscordChannelPermissions(guildID, tc.roles, tc.member, tc.channel); err == nil {
				t.Fatal("malformed Discord permission evidence must fail closed")
			}
		})
	}
}

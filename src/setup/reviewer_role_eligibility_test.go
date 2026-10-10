package setup

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"app/src/model"
)

func TestReviewerRoleEligibilityUsesMentionableOrVerifiedBotPermission(t *testing.T) {
	const guildID = "123456789012345600"
	const channelID = "123456789012345601"
	const webhookURL = "https://discord.com/api/webhooks/123456789012345602/synthetic-token"
	evidence := reviewerRoleSendEvidence{
		GuildID: guildID, ChannelID: channelID, WebhookID: "123456789012345602",
		BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true,
		PermissionsKnown: true, CanMentionRoles: true,
	}
	cases := []struct {
		name     string
		role     DiscordGuildRole
		evidence reviewerRoleSendEvidence
		err      error
		want     bool
	}{
		{name: "mentionable role needs no mention permission bit", role: DiscordGuildRole{ID: "123456789012345604", Mentionable: true}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, WebhookID: "123456789012345602", BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true, PermissionsKnown: true}, want: true},
		{name: "unknown permissions deny even a mentionable role", role: DiscordGuildRole{ID: "123456789012345604", Mentionable: true}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, WebhookID: "123456789012345602", BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true}, want: false},
		{name: "API error denies a mentionable role", role: DiscordGuildRole{ID: "123456789012345604", Mentionable: true}, evidence: evidence, err: errors.New("Discord unavailable"), want: false},
		{name: "stale channel identity fails closed", role: DiscordGuildRole{ID: "123456789012345604", Mentionable: true}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: "123456789012345609", WebhookID: "123456789012345602", BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true, PermissionsKnown: true}, want: false},
		{name: "stale webhook identity fails closed", role: DiscordGuildRole{ID: "123456789012345604", Mentionable: true}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, WebhookID: "123456789012345609", BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true, PermissionsKnown: true}, want: false},
		{name: "nonmentionable role allowed with verified channel permission", role: DiscordGuildRole{ID: "123456789012345604"}, evidence: evidence, want: true},
		{name: "nonmentionable role denied without permission", role: DiscordGuildRole{ID: "123456789012345604"}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: "123456789012345601", WebhookID: "123456789012345602", BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true, PermissionsKnown: true}, want: false},
		{name: "unknown permission fails closed", role: DiscordGuildRole{ID: "123456789012345604"}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: "123456789012345601", WebhookID: "123456789012345602", BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true, CanMentionRoles: true}, want: false},
		{name: "unknown webhook ownership fails closed", role: DiscordGuildRole{ID: "123456789012345604"}, evidence: reviewerRoleSendEvidence{GuildID: guildID, ChannelID: "123456789012345601", WebhookID: "123456789012345602", BotID: "123456789012345603", PermissionsKnown: true, CanMentionRoles: true}, want: false},
		{name: "wrong guild fails closed", role: DiscordGuildRole{ID: "123456789012345604", Mentionable: true}, evidence: reviewerRoleSendEvidence{GuildID: "123456789012345699", ChannelID: "123456789012345601", WebhookID: "123456789012345602", BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true}, want: false},
		{name: "everyone is never eligible", role: DiscordGuildRole{ID: guildID, Mentionable: true}, evidence: evidence, want: false},
		{name: "invalid role id is never eligible", role: DiscordGuildRole{ID: "@everyone", Mentionable: true}, evidence: evidence, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reviewerRoleCanBeMentioned(guildID, channelID, webhookURL, tc.role, tc.evidence, tc.err); got != tc.want {
				t.Fatalf("eligible=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyReviewerRoleEligibilityUsesTaskTypeDestinationAndCurrentBotEvidence(t *testing.T) {
	db, project := reviewerResolutionTestDB(t)
	senderURL := reviewerTestRoute(t, db, project, "task-1")
	routes := model.ListProductionNotificationRoutes(db, project.KitsuProjectID)
	oldRoles, oldEvidence := reviewerGuildRolesReader, reviewerRoleSendEvidenceReader
	permissions := false
	reviewerGuildRolesReader = func(_, _ string) ([]DiscordGuildRole, error) {
		return []DiscordGuildRole{{ID: "123456789012345604", Name: "Quiet", Mentionable: false}}, nil
	}
	reviewerRoleSendEvidenceReader = func(guildID, channelID, webhookURL, _ string) (reviewerRoleSendEvidence, error) {
		webhookID, err := discordWebhookID(webhookURL)
		if err != nil {
			return reviewerRoleSendEvidence{}, err
		}
		return reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, WebhookID: webhookID, BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true, PermissionsKnown: true, CanMentionRoles: permissions}, nil
	}
	t.Cleanup(func() { reviewerGuildRolesReader, reviewerRoleSendEvidenceReader = oldRoles, oldEvidence })
	change := []model.ProductionReviewerDelta{{TaskTypeID: "task-1", AddRoleIDs: []string{"123456789012345604"}}}
	if err := validateProductionReviewerDeltas(db, project, change, routes, nil, "bot-token"); err == nil {
		t.Fatal("non-mentionable Role without destination permission was accepted")
	}
	permissions = true
	if err := validateProductionReviewerDeltas(db, project, change, routes, nil, "bot-token"); err != nil {
		t.Fatalf("verified Bot permission did not allow non-mentionable Role: %v", err)
	}
	change[0].AddRoleIDs = []string{"123456789012345699"}
	if err := validateProductionReviewerDeltas(db, project, change, routes, nil, "bot-token"); err == nil {
		t.Fatal("Role absent from the linked Guild was accepted")
	}
	change[0].AddRoleIDs = []string{"123456789012345604"}
	created := &model.ProjectWebhook{KitsuProjectID: project.KitsuProjectID, TaskType: "task-1", WebhookURL: senderURL, DiscordChannelID: "123456789012345608"}
	newRoute := []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-1", DestinationWebhookID: 0}}
	if err := validateProductionReviewerDeltas(db, project, change, newRoute, []*model.ProjectWebhook{created}, "bot-token"); err != nil {
		t.Fatalf("verified newly created route destination was not accepted: %v", err)
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

func TestResolveReviewerRoleSendEvidenceVerifiesWebhookAndChannelPermission(t *testing.T) {
	const guildID = "123456789012345600"
	const channelID = "123456789012345601"
	const webhookID = "123456789012345602"
	const botID = "123456789012345603"
	oldTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, "{}"
		switch request.URL.Path {
		case "/api/v10/users/@me":
			body = `{"id":"` + botID + `"}`
		case "/api/v10/webhooks/" + webhookID:
			body = `{"id":"` + webhookID + `","type":1,"guild_id":"` + guildID + `","channel_id":"` + channelID + `","user":{"id":"` + botID + `"}}`
		case "/api/v10/guilds/" + guildID + "/roles":
			body = `[{"id":"` + guildID + `","permissions":"0"},{"id":"123456789012345604","permissions":"134144"}]`
		case "/api/v10/guilds/" + guildID + "/members/" + botID:
			body = `{"user":{"id":"` + botID + `"},"roles":["123456789012345604"]}`
		case "/api/v10/guilds/" + guildID + "/channels":
			body = `[{"id":"` + channelID + `","guild_id":"` + guildID + `","permission_overwrites":[]}]`
		default:
			return nil, fmt.Errorf("unexpected Discord read %s", request.URL.Path)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	webhookURL := "https://discord.com/api/webhooks/" + webhookID + "/not-a-real-secret"
	evidence, err := resolveReviewerRoleSendEvidence(guildID, channelID, webhookURL, "synthetic-bot-token")
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.OwnershipVerified || !evidence.PermissionsKnown || !evidence.CanMentionRoles || evidence.WebhookOwnerID != botID {
		t.Fatalf("expected positively verified Bot-owned webhook and role permission, got %+v", evidence)
	}
}

func TestResolveReviewerRoleSendEvidenceRejectsWrongWebhookOwner(t *testing.T) {
	const guildID = "123456789012345600"
	const channelID = "123456789012345601"
	const webhookID = "123456789012345602"
	oldTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"id":"123456789012345603"}`
		if request.URL.Path == "/api/v10/webhooks/"+webhookID {
			body = `{"id":"` + webhookID + `","type":1,"guild_id":"` + guildID + `","channel_id":"` + channelID + `","user":{"id":"123456789012345699"}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if _, err := resolveReviewerRoleSendEvidence(guildID, channelID, "https://discord.com/api/webhooks/"+webhookID+"/redacted", "synthetic-bot-token"); err == nil {
		t.Fatal("a webhook created by another user must not be accepted as this Bot's sender")
	}
}

func TestResolveReviewerRoleSendEvidenceFailsClosedOnDiscord403And429(t *testing.T) {
	const guildID = "123456789012345600"
	const channelID = "123456789012345601"
	const webhookID = "123456789012345602"
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			oldTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = oldTransport })
			http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				responseStatus, body := http.StatusOK, `{}`
				switch request.URL.Path {
				case "/api/v10/users/@me":
					body = `{"id":"123456789012345603"}`
				case "/api/v10/webhooks/" + webhookID:
					body = `{"id":"` + webhookID + `","type":1,"guild_id":"` + guildID + `","channel_id":"` + channelID + `","user":{"id":"123456789012345603"}}`
				case "/api/v10/guilds/" + guildID + "/roles":
					responseStatus = status
				}
				header := make(http.Header)
				if status == http.StatusTooManyRequests && request.URL.Path == "/api/v10/guilds/"+guildID+"/roles" {
					header.Set("Retry-After", "0.001")
				}
				return &http.Response{StatusCode: responseStatus, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			if _, err := resolveReviewerRoleSendEvidence(guildID, channelID, "https://discord.com/api/webhooks/"+webhookID+"/redacted", "synthetic-bot-token"); err == nil {
				t.Fatalf("HTTP %d must leave role send permission unavailable", status)
			}
		})
	}
}

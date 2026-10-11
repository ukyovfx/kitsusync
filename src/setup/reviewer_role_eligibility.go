package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"app/src/model"
	"gorm.io/gorm"
)

type reviewerRoleSendEvidence struct {
	GuildID           string
	ChannelID         string
	WebhookID         string
	BotID             string
	WebhookOwnerID    string
	OwnershipVerified bool
	PermissionsKnown  bool
	CanMentionRoles   bool
}

type reviewerRoleSender struct {
	WebhookURL string
	ChannelID  string
}

type DiscordGuildRoleWithPermissions struct {
	ID          string `json:"id"`
	Permissions string `json:"permissions"`
}

type DiscordPermissionMember struct {
	ID      string
	RoleIDs []string
}

type DiscordPermissionOverwrite struct {
	ID    string `json:"id"`
	Type  int    `json:"type"`
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

type DiscordPermissionChannel struct {
	GuildID    string
	ID         string
	Type       int
	Overwrites []DiscordPermissionOverwrite
}

type effectiveDiscordChannelPermissionSet struct {
	SendMessages    bool
	MentionEveryone bool
}

var reviewerRoleSendEvidenceReader = resolveReviewerRoleSendEvidence
var errReviewerEligibilityUnavailable = errors.New("Reviewer eligibility could not be verified")

func verifiedReviewerRolesForTaskType(db *gorm.DB, project model.Project, taskTypeID, botToken, senderWebhookURL string) (map[string]struct{}, error) {
	if db == nil || !isDiscordSnowflake(project.DiscordGuildID) || strings.TrimSpace(senderWebhookURL) == "" {
		return nil, errors.New("Reviewer sender context is unavailable")
	}
	var destination *model.ProjectWebhook
	for _, route := range model.ListProductionNotificationRoutes(db, project.KitsuProjectID) {
		if strings.TrimSpace(route.TaskTypeID) != strings.TrimSpace(taskTypeID) {
			continue
		}
		webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID)
		if webhook == nil || strings.TrimSpace(webhook.KitsuProjectID) != strings.TrimSpace(project.KitsuProjectID) ||
			strings.TrimSpace(webhook.WebhookURL) != strings.TrimSpace(senderWebhookURL) || strings.TrimSpace(webhook.DiscordChannelID) == "" {
			return nil, errors.New("Reviewer sender does not match the current Task Type destination")
		}
		destination = webhook
		break
	}
	if destination == nil {
		return nil, errors.New("Reviewer Task Type has no current notification route")
	}
	return eligibleReviewerRolesForSender(project.DiscordGuildID, destination.DiscordChannelID, destination.WebhookURL, botToken)
}

func currentReviewerRoleEligibility(db *gorm.DB, project model.Project, taskTypeID, botToken string) (map[string]struct{}, error) {
	for _, route := range model.ListProductionNotificationRoutes(db, project.KitsuProjectID) {
		if strings.TrimSpace(route.TaskTypeID) != strings.TrimSpace(taskTypeID) {
			continue
		}
		webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID)
		if webhook == nil || strings.TrimSpace(webhook.KitsuProjectID) != strings.TrimSpace(project.KitsuProjectID) {
			return nil, errors.New("Reviewer route destination is unavailable")
		}
		return verifiedReviewerRolesForTaskType(db, project, taskTypeID, botToken, webhook.WebhookURL)
	}
	return nil, errors.New("Reviewer Task Type has no current notification route")
}

func eligibleReviewerRolesForSender(guildID, channelID, webhookURL, botToken string) (map[string]struct{}, error) {
	evidence, err := reviewerRoleSendEvidenceReader(guildID, channelID, webhookURL, botToken)
	if err != nil {
		return nil, fmt.Errorf("verify Reviewer sender permissions: %w", err)
	}
	roles, err := reviewerGuildRolesReader(guildID, botToken)
	if err != nil {
		return nil, fmt.Errorf("verify Reviewer Guild roles: %w", err)
	}
	eligible := make(map[string]struct{})
	for _, role := range reviewerRoleCatalog(guildID, roles) {
		if reviewerRoleCanBeMentioned(guildID, channelID, webhookURL, role, evidence, nil) {
			eligible[strings.TrimSpace(role.ID)] = struct{}{}
		}
	}
	return eligible, nil
}

const (
	discordPermissionAdministrator = uint64(1 << 3)
	discordPermissionViewChannel   = uint64(1 << 10)
	discordPermissionSendMessages  = uint64(1 << 11)
	discordPermissionMentionAll    = uint64(1 << 17)
)

// reviewerRoleCanBeMentioned is the one role eligibility rule shared by the
// UI, Apply validation, and delivery-time resolution. Evidence is always tied
// to the exact linked Guild, destination channel, and Bot-owned webhook.
func reviewerRoleCanBeMentioned(guildID, channelID, webhookURL string, role DiscordGuildRole, evidence reviewerRoleSendEvidence, evidenceErr error) bool {
	guildID = strings.TrimSpace(guildID)
	roleID := strings.TrimSpace(role.ID)
	webhookID, webhookErr := discordWebhookID(webhookURL)
	if !isDiscordSnowflake(guildID) || !isDiscordSnowflake(roleID) || roleID == guildID {
		return false
	}
	if webhookErr != nil || evidenceErr != nil || !evidence.PermissionsKnown || strings.TrimSpace(evidence.GuildID) != guildID ||
		strings.TrimSpace(evidence.ChannelID) != strings.TrimSpace(channelID) || strings.TrimSpace(evidence.WebhookID) != webhookID ||
		!isDiscordSnowflake(evidence.BotID) || evidence.WebhookOwnerID != evidence.BotID || !evidence.OwnershipVerified {
		return false
	}
	if role.Mentionable {
		return true
	}
	return evidence.CanMentionRoles
}

func effectiveDiscordChannelPermissions(guildID string, roles []DiscordGuildRoleWithPermissions, member DiscordPermissionMember, channel DiscordPermissionChannel) (effectiveDiscordChannelPermissionSet, error) {
	guildID = strings.TrimSpace(guildID)
	if !isDiscordSnowflake(guildID) || strings.TrimSpace(channel.GuildID) != guildID || !isDiscordSnowflake(channel.ID) || !isDiscordSnowflake(member.ID) {
		return effectiveDiscordChannelPermissionSet{}, errors.New("Discord permission context identity is incomplete")
	}
	rolePermissions := make(map[string]uint64, len(roles))
	for _, role := range roles {
		id := strings.TrimSpace(role.ID)
		if !isDiscordSnowflake(id) {
			return effectiveDiscordChannelPermissionSet{}, errors.New("Discord role identity is invalid")
		}
		permissions, err := strconv.ParseUint(strings.TrimSpace(role.Permissions), 10, 64)
		if err != nil {
			return effectiveDiscordChannelPermissionSet{}, errors.New("Discord role permissions are invalid")
		}
		if _, duplicate := rolePermissions[id]; duplicate {
			return effectiveDiscordChannelPermissionSet{}, errors.New("Discord role identity is duplicated")
		}
		rolePermissions[id] = permissions
	}
	everyone, ok := rolePermissions[guildID]
	if !ok {
		return effectiveDiscordChannelPermissionSet{}, errors.New("Discord @everyone role permission is unavailable")
	}
	memberRoleIDs := make(map[string]bool, len(member.RoleIDs))
	base := everyone
	for _, rawID := range member.RoleIDs {
		id := strings.TrimSpace(rawID)
		permissions, found := rolePermissions[id]
		if !isDiscordSnowflake(id) || id == guildID || !found || memberRoleIDs[id] {
			return effectiveDiscordChannelPermissionSet{}, errors.New("Discord Bot role membership is incomplete")
		}
		memberRoleIDs[id] = true
		base |= permissions
	}
	if base&discordPermissionAdministrator != 0 {
		base = ^uint64(0)
		return permissionSetFromBits(base), nil
	}
	seenOverwrites := make(map[string]bool, len(channel.Overwrites))
	var everyoneOverwrite *DiscordPermissionOverwrite
	var roleDeny, roleAllow uint64
	var memberOverwrite *DiscordPermissionOverwrite
	for i := range channel.Overwrites {
		overwrite := &channel.Overwrites[i]
		id := strings.TrimSpace(overwrite.ID)
		if !isDiscordSnowflake(id) || (overwrite.Type != 0 && overwrite.Type != 1) || seenOverwrites[strconv.Itoa(overwrite.Type)+":"+id] {
			return effectiveDiscordChannelPermissionSet{}, errors.New("Discord channel permission overwrite is invalid")
		}
		seenOverwrites[strconv.Itoa(overwrite.Type)+":"+id] = true
		if _, err := strconv.ParseUint(strings.TrimSpace(overwrite.Allow), 10, 64); err != nil {
			return effectiveDiscordChannelPermissionSet{}, errors.New("Discord channel permission allow value is invalid")
		}
		if _, err := strconv.ParseUint(strings.TrimSpace(overwrite.Deny), 10, 64); err != nil {
			return effectiveDiscordChannelPermissionSet{}, errors.New("Discord channel permission deny value is invalid")
		}
		if overwrite.Type == 0 && id != guildID {
			if _, exists := rolePermissions[id]; !exists {
				return effectiveDiscordChannelPermissionSet{}, errors.New("Discord channel overwrite role is stale")
			}
		}
		if overwrite.Type == 0 && id == guildID {
			everyoneOverwrite = overwrite
		} else if overwrite.Type == 0 && memberRoleIDs[id] {
			allow, _ := strconv.ParseUint(overwrite.Allow, 10, 64)
			deny, _ := strconv.ParseUint(overwrite.Deny, 10, 64)
			roleAllow |= allow
			roleDeny |= deny
		} else if overwrite.Type == 1 && id == member.ID {
			memberOverwrite = overwrite
		}
	}
	if everyoneOverwrite != nil {
		allow, _ := strconv.ParseUint(everyoneOverwrite.Allow, 10, 64)
		deny, _ := strconv.ParseUint(everyoneOverwrite.Deny, 10, 64)
		base = (base &^ deny) | allow
	}
	base = (base &^ roleDeny) | roleAllow
	if memberOverwrite != nil {
		allow, _ := strconv.ParseUint(memberOverwrite.Allow, 10, 64)
		deny, _ := strconv.ParseUint(memberOverwrite.Deny, 10, 64)
		base = (base &^ deny) | allow
	}
	return permissionSetFromBits(base), nil
}

func permissionSetFromBits(bits uint64) effectiveDiscordChannelPermissionSet {
	if bits&discordPermissionViewChannel == 0 {
		bits &^= discordPermissionSendMessages | discordPermissionMentionAll
	} else if bits&discordPermissionSendMessages == 0 {
		bits &^= discordPermissionMentionAll
	}
	return effectiveDiscordChannelPermissionSet{
		SendMessages:    bits&discordPermissionSendMessages != 0,
		MentionEveryone: bits&discordPermissionMentionAll != 0,
	}
}

func resolveReviewerRoleSendEvidence(guildID, channelID, webhookURL, botToken string) (reviewerRoleSendEvidence, error) {
	evidence := reviewerRoleSendEvidence{GuildID: strings.TrimSpace(guildID), ChannelID: strings.TrimSpace(channelID)}
	webhookID, err := discordWebhookID(webhookURL)
	if err != nil || !isDiscordSnowflake(evidence.GuildID) || !isDiscordSnowflake(evidence.ChannelID) || strings.TrimSpace(botToken) == "" {
		return evidence, errors.New("Discord Reviewer destination could not be verified")
	}
	evidence.WebhookID = webhookID
	botBody, status, err := botDo(http.MethodGet, discordAPI+"/users/@me", nil, botToken)
	if err != nil || status != http.StatusOK {
		return evidence, errors.New("Discord Bot identity could not be verified")
	}
	var bot struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(botBody, &bot) != nil || !isDiscordSnowflake(bot.ID) {
		return evidence, errors.New("Discord Bot identity response was invalid")
	}
	evidence.BotID = strings.TrimSpace(bot.ID)
	webhookBody, status, err := botDo(http.MethodGet, discordAPI+"/webhooks/"+webhookID, nil, botToken)
	if err != nil || status != http.StatusOK {
		return evidence, errors.New("Discord webhook ownership could not be verified")
	}
	var webhook struct {
		ID        string `json:"id"`
		Type      int    `json:"type"`
		GuildID   string `json:"guild_id"`
		ChannelID string `json:"channel_id"`
		User      struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if json.Unmarshal(webhookBody, &webhook) != nil || webhook.Type != 1 || strings.TrimSpace(webhook.ID) != webhookID ||
		strings.TrimSpace(webhook.GuildID) != evidence.GuildID || strings.TrimSpace(webhook.ChannelID) != evidence.ChannelID ||
		strings.TrimSpace(webhook.User.ID) != evidence.BotID {
		return evidence, errors.New("Discord webhook is not verified as owned by this Bot and destination")
	}
	evidence.WebhookOwnerID = strings.TrimSpace(webhook.User.ID)
	evidence.OwnershipVerified = true

	roleBody, status, err := botDo(http.MethodGet, discordAPI+"/guilds/"+evidence.GuildID+"/roles", nil, botToken)
	if err != nil || status != http.StatusOK {
		return evidence, errors.New("Discord Bot roles could not be verified")
	}
	var roles []DiscordGuildRoleWithPermissions
	if json.Unmarshal(roleBody, &roles) != nil {
		return evidence, errors.New("Discord Bot roles response was invalid")
	}
	memberBody, status, err := botDo(http.MethodGet, discordAPI+"/guilds/"+evidence.GuildID+"/members/"+evidence.BotID, nil, botToken)
	if err != nil || status != http.StatusOK {
		return evidence, errors.New("Discord Bot Guild membership could not be verified")
	}
	var member struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Roles []string `json:"roles"`
	}
	if json.Unmarshal(memberBody, &member) != nil || strings.TrimSpace(member.User.ID) != evidence.BotID {
		return evidence, errors.New("Discord Bot Guild membership response was invalid")
	}
	channelBody, status, err := botDo(http.MethodGet, discordAPI+"/guilds/"+evidence.GuildID+"/channels", nil, botToken)
	if err != nil || status != http.StatusOK {
		return evidence, errors.New("Discord destination channel permissions could not be verified")
	}
	var channels []struct {
		ID                   string                        `json:"id"`
		GuildID              string                        `json:"guild_id"`
		Type                 int                           `json:"type"`
		PermissionOverwrites *[]DiscordPermissionOverwrite `json:"permission_overwrites"`
	}
	if json.Unmarshal(channelBody, &channels) != nil {
		return evidence, errors.New("Discord channel response was invalid")
	}
	var channel *DiscordPermissionChannel
	for _, candidate := range channels {
		if strings.TrimSpace(candidate.ID) != evidence.ChannelID {
			continue
		}
		if candidate.PermissionOverwrites == nil {
			return evidence, errors.New("Discord channel permission overwrites are unavailable")
		}
		channel = &DiscordPermissionChannel{GuildID: strings.TrimSpace(candidate.GuildID), ID: strings.TrimSpace(candidate.ID), Type: candidate.Type, Overwrites: *candidate.PermissionOverwrites}
		break
	}
	if channel == nil || channel.Type != 0 {
		return evidence, errors.New("Discord destination channel is unavailable")
	}
	permissions, err := effectiveDiscordChannelPermissions(evidence.GuildID, roles, DiscordPermissionMember{ID: evidence.BotID, RoleIDs: member.Roles}, *channel)
	if err != nil {
		return evidence, fmt.Errorf("Discord destination permissions could not be calculated: %w", err)
	}
	evidence.PermissionsKnown = true
	evidence.CanMentionRoles = permissions.SendMessages && permissions.MentionEveryone
	return evidence, nil
}

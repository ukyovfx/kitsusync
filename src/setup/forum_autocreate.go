package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"app/src/api/kitsu"
	"app/src/forum"
	"app/src/model"
)

// Owner decision 2026-11-15: the Bot creates the forum and its tags so a
// beginner never has to hand-build them in Discord. The forum is named after
// the Task Type, the same way the lane path names its channels.

// forumAutoTagNames are the tags the Bot creates, in creation order.
var forumAutoTagNames = []struct {
	Role string
	Name string
}{
	{model.ForumRoleWFA, "WFA"},
	{model.ForumRoleRetake, "RETAKE"},
	{model.ForumRoleDone, "DONE"},
	{model.ForumRoleWIP, "WIP"},
}

// forumAutoTag is one tag to create or patch, with the emoji that colors it.
type forumAutoTag struct {
	Name  string
	Emoji string
}

// Owner decision 2026-11-19: Discord forum tags have no color property, so the
// tag emoji carries the color. Each role tag gets the colored circle emoji
// closest to the color of the Kitsu status the saved status_roles maps to it.
var forumTagEmojiCandidates = []struct {
	Emoji   string
	R, G, B int
}{
	{"🔴", 221, 46, 68},
	{"🟠", 244, 144, 12},
	{"🟡", 255, 196, 12},
	{"🟢", 119, 178, 85},
	{"🔵", 85, 172, 238},
	{"🟣", 170, 141, 216},
	{"🟤", 165, 121, 57},
	{"⚫", 0, 0, 0},
	{"⚪", 255, 255, 255},
}

// nearestForumTagEmoji maps a Kitsu status hex color to the closest colored
// circle emoji by RGB distance. It returns "" when the color is missing or
// unreadable, so the tag keeps the no-emoji look.
func nearestForumTagEmoji(color string) string {
	hex := strings.TrimPrefix(strings.TrimSpace(color), "#")
	if len(hex) != 6 {
		return ""
	}
	var rgb [3]int
	for i := range rgb {
		value, err := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
		if err != nil {
			return ""
		}
		rgb[i] = int(value)
	}
	best, bestDistance := "", -1
	for _, candidate := range forumTagEmojiCandidates {
		distance := (candidate.R-rgb[0])*(candidate.R-rgb[0]) + (candidate.G-rgb[1])*(candidate.G-rgb[1]) + (candidate.B-rgb[2])*(candidate.B-rgb[2])
		if bestDistance < 0 || distance < bestDistance {
			best, bestDistance = candidate.Emoji, distance
		}
	}
	return best
}

// forumRoleTagEmojis derives each role's tag emoji from the color of the Kitsu
// status mapped to the role in the saved status_roles (role ← status name is
// what the mapping means) and the Kitsu status list's colors. Roles without a
// mapped status, or whose status color is unreadable, get no emoji.
func forumRoleTagEmojis(statusRoles []model.ProductionForumStatusRole, statuses []kitsu.TaskStatus) map[string]string {
	colors := make(map[string]string, len(statuses))
	for _, status := range statuses {
		for _, name := range []string{status.Name, status.ShortName} {
			if key := strings.ToLower(strings.TrimSpace(name)); key != "" {
				colors[key] = status.Color
			}
		}
	}
	emojis := make(map[string]string, len(statusRoles))
	for _, row := range statusRoles {
		if emoji := nearestForumTagEmoji(colors[strings.ToLower(strings.TrimSpace(row.StatusName))]); emoji != "" {
			if _, dup := emojis[row.Role]; !dup {
				emojis[row.Role] = emoji
			}
		}
	}
	return emojis
}

// forumRoleTags builds the tags to create or patch: the standard names in
// creation order, each with its color emoji when one is known.
func forumRoleTags(emojis map[string]string) []forumAutoTag {
	tags := make([]forumAutoTag, 0, len(forumAutoTagNames))
	for _, tag := range forumAutoTagNames {
		tags = append(tags, forumAutoTag{Name: tag.Name, Emoji: emojis[tag.Role]})
	}
	return tags
}

// forumTagPatchList renders the available_tags entries both the create and the
// patch request send: existing tags keep their ID and their exact name (the ID
// is what Discord preserves, so posts already wearing the tag keep it), every
// desired tag carries its color emoji, and new tags arrive with the name only.
func forumTagPatchList(existing []forumTagInfo, desired []forumAutoTag) []map[string]any {
	available := make([]map[string]any, 0, len(existing)+len(desired))
	matched := make([]bool, len(existing))
	for _, want := range desired {
		entry := map[string]any{"name": want.Name}
		for i, tag := range existing {
			if matched[i] || !strings.EqualFold(strings.TrimSpace(tag.Name), want.Name) {
				continue
			}
			entry["id"] = tag.ID
			entry["name"] = tag.Name
			matched[i] = true
			break
		}
		if want.Emoji != "" {
			entry["emoji_name"] = want.Emoji
		}
		available = append(available, entry)
	}
	for i, tag := range existing {
		if !matched[i] {
			available = append(available, map[string]any{"id": tag.ID, "name": tag.Name})
		}
	}
	return available
}

// forumCreateChannel is the Bot API call that creates a forum channel. It is a
// variable so tests can answer without Discord.
var forumCreateChannel = createGuildForumChannel

// forumCategoryCreator is the Bot API call that creates a category channel.
// It is a variable so tests can answer without Discord.
var forumCategoryCreator = CreateCategory

func createGuildForumChannel(guildID, parentID, name string, tags []forumAutoTag, botToken string) (forumChannelInfo, error) {
	payload := map[string]any{"name": name, "type": discordChannelTypeForum}
	if len(tags) > 0 {
		payload["available_tags"] = forumTagPatchList(nil, tags)
	}
	if strings.TrimSpace(parentID) != "" {
		payload["parent_id"] = parentID
	}
	body, status, err := botDo(http.MethodPost, fmt.Sprintf("%s/guilds/%s/channels", discordAPI, url.PathEscape(strings.TrimSpace(guildID))), payload, botToken)
	if err != nil {
		return forumChannelInfo{}, err
	}
	if status >= 400 {
		return forumChannelInfo{}, discordBotAPIError("discord forum channel create failed", status, body)
	}
	var raw struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		AvailableTags []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"available_tags"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || raw.ID == "" {
		return forumChannelInfo{}, fmt.Errorf("discord forum channel create response was invalid")
	}
	forum := forumChannelInfo{ID: raw.ID, Name: raw.Name}
	for _, tag := range raw.AvailableTags {
		forum.Tags = append(forum.Tags, forumTagInfo{ID: tag.ID, Name: tag.Name})
	}
	return forum, nil
}

// forumTagUpdater adds missing tags to an existing forum channel. It is a
// variable so tests can answer without Discord.
var forumTagUpdater = addForumTags

// addForumTags PATCHes the forum's available_tags with its existing tags kept
// (Discord preserves their IDs) plus the missing ones, and refreshes the color
// emoji on the role tags it carries, then returns the tag list Discord reports
// afterwards, so the tags' IDs can be mapped.
func addForumTags(channelID string, existing []forumTagInfo, desired []forumAutoTag, botToken string) ([]forumTagInfo, error) {
	body, status, err := botDo(http.MethodPatch, fmt.Sprintf("%s/channels/%s", discordAPI, url.PathEscape(strings.TrimSpace(channelID))), map[string]any{"available_tags": forumTagPatchList(existing, desired)}, botToken)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, discordBotAPIError("discord forum tag update failed", status, body)
	}
	var raw struct {
		AvailableTags []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"available_tags"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return nil, fmt.Errorf("discord forum tag update response was invalid")
	}
	var tags []forumTagInfo
	for _, tag := range raw.AvailableTags {
		tags = append(tags, forumTagInfo{ID: tag.ID, Name: tag.Name})
	}
	return tags, nil
}

// forumTagsNeedUpdate reports whether a forum is missing one of the desired
// role tags, or carries one without the color emoji it should have.
func forumTagsNeedUpdate(existing []forumTagInfo, desired []forumAutoTag) bool {
	for _, want := range desired {
		found := false
		for _, tag := range existing {
			if !strings.EqualFold(strings.TrimSpace(tag.Name), want.Name) {
				continue
			}
			found = true
			if want.Emoji != "" && tag.Emoji != want.Emoji {
				return true
			}
			break
		}
		if !found {
			return true
		}
	}
	return false
}

// forumChannelNameKey normalizes a channel name the way Discord does, so an
// earlier run's forum is found again instead of duplicated.
func forumChannelNameKey(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "-")
}

// errForumCreateAction marks a failure of the "create forums" action whose
// message is already written for the operator.
var errForumCreateAction = errors.New("forum create action failed")

// ForumChannelLister adapts the admin page's forum lookup for the startup
// validator.
func ForumChannelLister() func(guildID, botToken string) ([]forum.ForumChannel, error) {
	return func(guildID, botToken string) ([]forum.ForumChannel, error) {
		channels, err := forumChannelLister(guildID, botToken)
		if err != nil {
			return nil, err
		}
		out := make([]forum.ForumChannel, 0, len(channels))
		for _, channel := range channels {
			tags := make([]forum.ForumTag, 0, len(channel.Tags))
			for _, tag := range channel.Tags {
				tags = append(tags, forum.ForumTag{ID: tag.ID, Name: tag.Name})
			}
			out = append(out, forum.ForumChannel{ID: channel.ID, Name: channel.Name, Tags: tags})
		}
		return out, nil
	}
}

// CheckForumPermissions reports missing Bot permissions for forum mode.
func CheckForumPermissions(settings model.ProductionForumSettings, botToken string) []string {
	return checkForumPermissions(settings, botToken)
}

// createForumsForTaskTypes creates one forum per routed Task Type and fills the
// tag mapping, then saves the settings. Task Types that already point at a
// forum are left alone, so running it twice does not create duplicates.
// submitted carries the form the operator just pressed the button on, so the
// mapping they typed is what gets created and saved.
func createForumsForTaskTypes(db *gorm.DB, productionID, botToken, fallbackGuildID string, submitted model.ProductionForumSettings) (model.ProductionForumSettings, error) {
	settings, ok, err := model.LoadProductionForumSettings(db, productionID)
	if err != nil {
		return settings, err
	}
	if !ok {
		// First run: seed the routes from the Kitsu Task Type list so the Bot has
		// something to create forums for. Roles stay unset until the operator
		// maps statuses, so nothing is routed yet.
		settings.Config = model.ProductionForumConfig{ProductionID: productionID}
		for _, taskType := range forumTaskTypeLister(db, productionID) {
			settings.Routes = append(settings.Routes, model.ProductionForumRoute{ProductionID: productionID, TaskTypeID: taskType.ID, TaskTypeName: taskType.Name})
		}
	}
	if len(submitted.Routes) > 0 {
		settings.Routes = submitted.Routes
	}
	if len(submitted.StatusRoles) > 0 {
		settings.StatusRoles = submitted.StatusRoles
	}
	if strings.TrimSpace(submitted.Config.GuildID) != "" {
		settings.Config.GuildID = submitted.Config.GuildID
	}
	if strings.TrimSpace(botToken) == "" {
		return settings, fmt.Errorf("%w: DiscordのBotトークンが設定されていません。Bot設定を保存してから実行してください。", errForumCreateAction)
	}
	project := model.FindProjectByKitsuID(db, productionID)
	guildID := strings.TrimSpace(settings.Config.GuildID)
	if guildID == "" && project != nil {
		guildID = strings.TrimSpace(project.DiscordGuildID)
	}
	if guildID == "" {
		guildID = strings.TrimSpace(fallbackGuildID)
	}
	if guildID == "" {
		return settings, fmt.Errorf("%w: DiscordサーバーIDが未設定です。サーバーIDを入力して保存してから実行してください。", errForumCreateAction)
	}
	settings.Config.GuildID = guildID
	// Forums are created under the Production's own Discord category.
	parentID := ""
	projectName := ""
	if project != nil {
		parentID = strings.TrimSpace(project.DiscordCategoryID)
		projectName = project.Name
	}

	// The status → role mapping is decided from the Kitsu status names, so the
	// template fills it in before the tags are created and one press finishes
	// the whole setup — the color emojis included. A failed status list keeps
	// the saved mapping and leaves the tags without emojis.
	statuses, statusErr := forumStatusLister(db)
	if len(settings.StatusRoles) == 0 && statusErr == nil {
		settings.StatusRoles = forumTemplateStatusRolesFrom(statuses)
	}
	tagPlan := forumRoleTags(forumRoleTagEmojis(settings.StatusRoles, statuses))
	routes := make([]model.ProductionForumRoute, 0, len(settings.Routes))
	tags := make([]model.ProductionForumTag, 0, len(settings.Routes)*len(forumAutoTagNames))
	// Keep the forums already created in the returned settings even when a later
	// one fails, so the operator's retry does not orphan them.
	keepCreated := func() (model.ProductionForumSettings, error) {
		settings.Routes = routes
		settings.Tags = tags
		return settings, nil
	}
	var existingForums map[string]forumChannelInfo
	existingLoaded := false
	// Forums already routed (an earlier save) are never offered for reuse
	// again, and each match is consumed after one route, so Task Types whose
	// normalized names collide do not share one forum.
	usedForums := map[string]struct{}{}
	for _, route := range settings.Routes {
		if id := strings.TrimSpace(route.ForumChannelID); id != "" {
			usedForums[id] = struct{}{}
		}
	}
	for _, route := range settings.Routes {
		if strings.TrimSpace(route.ForumChannelID) == "" {
			if !existingLoaded {
				existingLoaded = true
				// A forum of the same name under the same category is reused, so
				// retrying from a partial state never duplicates channels.
				existing, err := forumChannelLister(guildID, botToken)
				if err != nil {
					return settings, fmt.Errorf("%w: Discordのフォーラム一覧を取得できませんでした: %w", errForumCreateAction, err)
				}
				categoryValid := false
				if parentID != "" {
					if body, ok := guildChannelListCache.cachedBody(guildID); ok {
						if channels, err := decodeGuildChannelsBody(body); err == nil {
							categoryValid = discordCategoryExists(channels, parentID)
						}
					}
				}
				if !categoryValid {
					newCategoryID, err := forumCategoryCreator(guildID, KitsuSyncCategoryName(projectName), botToken)
					if err != nil {
						return settings, fmt.Errorf("%w: Discordカテゴリを作成できませんでした: %w", errForumCreateAction, err)
					}
					if err := model.UpdateProjectCategoryID(db, productionID, newCategoryID); err != nil {
						return settings, fmt.Errorf("%w: DiscordカテゴリIDを保存できませんでした: %w", errForumCreateAction, err)
					}
					parentID = newCategoryID
				}
				existingForums = map[string]forumChannelInfo{}
				for _, forum := range existing {
					if forum.ParentID != parentID {
						continue
					}
					if _, used := usedForums[forum.ID]; used {
						continue
					}
					existingForums[forumChannelNameKey(forum.Name)] = forum
				}
			}
			nameKey := forumChannelNameKey(route.TaskTypeName)
			forum, found := existingForums[nameKey]
			if !found {
				var err error
				forum, err = forumCreateChannel(guildID, parentID, route.TaskTypeName, tagPlan, botToken)
				if err != nil {
					created, _ := keepCreated()
					return created, fmt.Errorf("%w: フォーラム「%s」を作成できませんでした: %w", errForumCreateAction, route.TaskTypeName, err)
				}
				guildChannelListCache.invalidate(guildID)
			} else {
				delete(existingForums, nameKey)
				// A reused forum may be missing the role tags a save requires,
				// or carry them without their color emoji; refresh them while
				// keeping its existing tags and their IDs.
				if forumTagsNeedUpdate(forum.Tags, tagPlan) {
					updated, err := forumTagUpdater(forum.ID, forum.Tags, tagPlan, botToken)
					if err != nil {
						created, _ := keepCreated()
						return created, fmt.Errorf("%w: フォーラム「%s」に足りないタグを追加できませんでした: %w", errForumCreateAction, route.TaskTypeName, err)
					}
					guildChannelListCache.invalidate(guildID)
					forum.Tags = updated
				}
			}
			route.ForumChannelID = forum.ID
			route.ForumChannelName = forum.Name
			for _, role := range forumRoleOptions {
				if tagID := forumTagIDForRole(forum.Tags, role); tagID != "" {
					tags = append(tags, model.ProductionForumTag{ProductionID: productionID, ForumChannelID: forum.ID, Role: role, TagID: tagID})
				}
			}
		}
		routes = append(routes, route)
	}
	if len(routes) == 0 {
		return settings, fmt.Errorf("%w: タスクタイプが1つも選ばれていません。先にタスクタイプ → フォーラムの行を保存してください。", errForumCreateAction)
	}
	// Tags the operator already mapped for an existing forum are kept.
	kept := map[string]struct{}{}
	for _, tag := range tags {
		kept[tag.ForumChannelID+"|"+tag.Role] = struct{}{}
	}
	for _, tag := range settings.Tags {
		if _, done := kept[tag.ForumChannelID+"|"+tag.Role]; done {
			continue
		}
		tags = append(tags, tag)
	}
	settings.Routes = routes
	settings.Tags = tags
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		return settings, err
	}
	model.DeleteSetting(db, forumAutoCreateCheckKey(productionID))
	return settings, nil
}

// forumPermissionProblems reports each missing Bot permission per routed forum.
// An unknown permission set is reported as a warning, not an error.
var forumRequiredPermissions = []struct {
	Bit  int64
	Name string
	JA   string
}{
	{1 << 10, "View Channel", "チャンネルを見る"},
	{1 << 11, "Send Messages", "メッセージを送信"},
	{1 << 38, "Send Messages in Threads", "スレッドでメッセージを送信"},
	{1 << 34, "Manage Threads", "スレッドを管理"},
	{1 << 14, "Embed Links", "埋め込みリンク"},
	{1 << 16, "Read Message History", "メッセージ履歴を読む"},
}

// forumPermissionGetter returns the Bot's effective permission bitfield in a
// channel. It is a variable so tests can answer without Discord.
var forumPermissionGetter = getBotChannelPermissions

// getBotChannelPermissions returns the Bot's effective permission bits in a
// channel. Discord has no endpoint that reports them directly, so they are
// computed from the Bot's roles and the channel's permission overwrites.
func getBotChannelPermissions(channelID, botToken string) (int64, error) {
	channelID = strings.TrimSpace(channelID)
	if !isDiscordSnowflake(channelID) || strings.TrimSpace(botToken) == "" {
		return 0, errors.New("Discord channel permission context is incomplete")
	}
	botBody, status, err := botDo(http.MethodGet, discordAPI+"/users/@me", nil, botToken)
	if err != nil || status != http.StatusOK {
		return 0, errors.New("Discord Bot identity could not be verified")
	}
	var bot struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(botBody, &bot) != nil || !isDiscordSnowflake(bot.ID) {
		return 0, errors.New("Discord Bot identity response was invalid")
	}
	botID := strings.TrimSpace(bot.ID)
	guildBody, status, err := botDo(http.MethodGet, discordAPI+"/users/@me/guilds", nil, botToken)
	if err != nil || status != http.StatusOK {
		return 0, errors.New("Discord Bot Guilds could not be verified")
	}
	var guilds []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(guildBody, &guilds) != nil {
		return 0, errors.New("Discord Bot Guilds response was invalid")
	}
	for _, guild := range guilds {
		guildID := strings.TrimSpace(guild.ID)
		if !isDiscordSnowflake(guildID) {
			continue
		}
		bits, found, err := botChannelPermissionsInGuild(guildID, channelID, botID, botToken)
		if err != nil {
			return 0, err
		}
		if found {
			return bits, nil
		}
	}
	return 0, errors.New("Discord channel is not in a Guild this Bot is in")
}

func botChannelPermissionsInGuild(guildID, channelID, botID, botToken string) (int64, bool, error) {
	roleBody, status, err := botDo(http.MethodGet, discordAPI+"/guilds/"+guildID+"/roles", nil, botToken)
	if err != nil || status != http.StatusOK {
		return 0, false, errors.New("Discord Bot roles could not be verified")
	}
	var roles []DiscordGuildRoleWithPermissions
	if json.Unmarshal(roleBody, &roles) != nil {
		return 0, false, errors.New("Discord Bot roles response was invalid")
	}
	memberBody, status, err := botDo(http.MethodGet, discordAPI+"/guilds/"+guildID+"/members/"+botID, nil, botToken)
	if err != nil || status == http.StatusNotFound {
		return 0, false, nil
	}
	if status != http.StatusOK {
		return 0, false, errors.New("Discord Bot Guild membership could not be verified")
	}
	var member struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Roles []string `json:"roles"`
	}
	if json.Unmarshal(memberBody, &member) != nil || strings.TrimSpace(member.User.ID) != botID {
		return 0, false, errors.New("Discord Bot Guild membership response was invalid")
	}
	channelBody, status, err := botDo(http.MethodGet, discordAPI+"/guilds/"+guildID+"/channels", nil, botToken)
	if err != nil || status != http.StatusOK {
		return 0, false, errors.New("Discord channels could not be verified")
	}
	var channels []struct {
		ID                   string                        `json:"id"`
		GuildID              string                        `json:"guild_id"`
		Type                 int                           `json:"type"`
		PermissionOverwrites *[]DiscordPermissionOverwrite `json:"permission_overwrites"`
	}
	if json.Unmarshal(channelBody, &channels) != nil {
		return 0, false, errors.New("Discord channel response was invalid")
	}
	for _, candidate := range channels {
		if strings.TrimSpace(candidate.ID) != channelID {
			continue
		}
		overwrites := []DiscordPermissionOverwrite{}
		if candidate.PermissionOverwrites != nil {
			overwrites = *candidate.PermissionOverwrites
		}
		channel := DiscordPermissionChannel{GuildID: strings.TrimSpace(candidate.GuildID), ID: strings.TrimSpace(candidate.ID), Type: candidate.Type, Overwrites: overwrites}
		bits, err := effectiveDiscordChannelPermissionBits(guildID, roles, DiscordPermissionMember{ID: botID, RoleIDs: member.Roles}, channel)
		if err != nil {
			return 0, false, fmt.Errorf("Discord channel permissions could not be calculated: %w", err)
		}
		return int64(bits), true, nil
	}
	return 0, false, nil
}

// checkForumPermissions returns Japanese problems for the routed forums.
func checkForumPermissions(settings model.ProductionForumSettings, botToken string) []string {
	if strings.TrimSpace(botToken) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var problems []string
	for _, route := range settings.Routes {
		if _, done := seen[route.ForumChannelID]; done {
			continue
		}
		seen[route.ForumChannelID] = struct{}{}
		bits, err := forumPermissionGetter(route.ForumChannelID, botToken)
		if err != nil {
			problems = append(problems, fmt.Sprintf("フォーラム %s の権限を確認できませんでした: %v", route.TaskTypeName, err))
			continue
		}
		var missing []string
		for _, permission := range forumRequiredPermissions {
			if bits&permission.Bit == 0 {
				missing = append(missing, permission.JA)
			}
		}
		if len(missing) > 0 {
			problems = append(problems, fmt.Sprintf("フォーラム %s でBotに権限が足りません: %s", route.TaskTypeName, strings.Join(missing, "、")))
		}
	}
	return problems
}

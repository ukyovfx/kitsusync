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

// forumCreateChannel is the Bot API call that creates a forum channel. It is a
// variable so tests can answer without Discord.
var forumCreateChannel = createGuildForumChannel

func createGuildForumChannel(guildID, name string, tags []string, botToken string) (forumChannelInfo, error) {
	payload := map[string]any{"name": name, "type": discordChannelTypeForum}
	if len(tags) > 0 {
		available := make([]map[string]any, 0, len(tags))
		for _, tag := range tags {
			available = append(available, map[string]any{"name": tag})
		}
		payload["available_tags"] = available
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

// errForumsCreatedNeedsRoles is not a save failure: the forums exist, only the
// status mapping is still missing.
var errForumsCreatedNeedsRoles = errors.New("フォーラムとタグは作成しました。続けて「Kitsuのステータス → 役割」を設定して保存してください。")

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
		settings.Config = model.ProductionForumConfig{ProductionID: productionID, Mode: model.NotificationModeLane}
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
	if submitted.Config.Mode != "" {
		settings.Config.Mode = submitted.Config.Mode
	}
	if strings.TrimSpace(submitted.Config.GuildID) != "" {
		settings.Config.GuildID = submitted.Config.GuildID
	}
	if strings.TrimSpace(botToken) == "" {
		return settings, fmt.Errorf("%w: DiscordのBotトークンが設定されていません。Bot設定を保存してから実行してください。", errForumCreateAction)
	}
	guildID := strings.TrimSpace(settings.Config.GuildID)
	if guildID == "" {
		if project := model.FindProjectByKitsuID(db, productionID); project != nil {
			guildID = strings.TrimSpace(project.DiscordGuildID)
		}
	}
	if guildID == "" {
		guildID = strings.TrimSpace(fallbackGuildID)
	}
	if guildID == "" {
		return settings, fmt.Errorf("%w: DiscordサーバーIDが未設定です。サーバーIDを入力して保存してから実行してください。", errForumCreateAction)
	}
	settings.Config.GuildID = guildID

	tagNames := make([]string, 0, len(forumAutoTagNames))
	for _, tag := range forumAutoTagNames {
		tagNames = append(tagNames, tag.Name)
	}
	routes := make([]model.ProductionForumRoute, 0, len(settings.Routes))
	tags := make([]model.ProductionForumTag, 0, len(settings.Routes)*len(forumAutoTagNames))
	for _, route := range settings.Routes {
		if strings.TrimSpace(route.ForumChannelID) == "" {
			forum, err := forumCreateChannel(guildID, route.TaskTypeName, tagNames, botToken)
			if err != nil {
				return settings, fmt.Errorf("%w: フォーラム「%s」を作成できませんでした: %w", errForumCreateAction, route.TaskTypeName, err)
			}
			route.ForumChannelID = forum.ID
			route.ForumChannelName = forum.Name
			for _, want := range forumAutoTagNames {
				for _, got := range forum.Tags {
					if strings.EqualFold(strings.TrimSpace(got.Name), want.Name) {
						tags = append(tags, model.ProductionForumTag{ProductionID: productionID, ForumChannelID: forum.ID, Role: want.Role, TagID: got.ID})
						break
					}
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
	if len(settings.StatusRoles) == 0 {
		// The operator still has to map Kitsu statuses to roles; saving now would
		// fail validation and throw away the forums just created, so show the
		// created mapping unsaved and say what is left.
		return settings, errForumsCreatedNeedsRoles
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		return settings, fmt.Errorf("%w: %w", errForumCreateAction, err)
	}
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

func getBotChannelPermissions(channelID, botToken string) (int64, error) {
	body, status, err := botDo(http.MethodGet, fmt.Sprintf("%s/channels/%s", discordAPI, url.PathEscape(channelID)), nil, botToken)
	if err != nil {
		return 0, err
	}
	if status >= 400 {
		return 0, discordBotAPIError("discord channel permission read failed", status, body)
	}
	var raw struct {
		Permissions string `json:"permissions"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return 0, fmt.Errorf("discord channel permission response was invalid")
	}
	bits, err := strconv.ParseInt(strings.TrimSpace(raw.Permissions), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("discord channel permission response was invalid")
	}
	return bits, nil
}

// checkForumPermissions returns Japanese problems for the routed forums.
func checkForumPermissions(settings model.ProductionForumSettings, botToken string) []string {
	if settings.Config.Mode != model.NotificationModeForum || strings.TrimSpace(botToken) == "" {
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

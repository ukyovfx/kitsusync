package setup

import (
	"app/src/model"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// globalUserLinkingPeopleWithError keeps successful empty Kitsu data distinct
// from a failed live lookup while retaining local-map compatibility when no
// persisted/runtime Kitsu source is configured.
func globalUserLinkingPeopleWithError(db *gorm.DB) ([]KitsuPerson, string, error) {
	if baseURL, token, liveReady := runtimeKitsuDataSource(db); liveReady {
		people, err := ListKitsuPersonsWithCredentials(baseURL, token)
		if err != nil {
			return nil, "live_kitsu_api", err
		}
		return filterAssignablePersons(people, botAccountEmail(db)), "live_kitsu_api", nil
	}

	people := make([]KitsuPerson, 0)
	for _, user := range filterAssignableUsers(model.ListUserMap(db), botAccountEmail(db)) {
		if strings.TrimSpace(user.KitsuName) == "" {
			continue
		}
		people = append(people, KitsuPerson{
			ID:       user.KitsuID,
			FullName: user.KitsuName,
			Email:    user.KitsuEmail,
			Active:   true,
		})
	}
	sort.Slice(people, func(i, j int) bool {
		return strings.ToLower(people[i].FullName) < strings.ToLower(people[j].FullName)
	})
	return people, "local_user_map", nil
}

func kitsuLookupDiagnostic(lookupErr error) string {
	var failure *KitsuLookupError
	if errors.As(lookupErr, &failure) {
		detail := failure.Endpoint + " lookup: " + failure.Class
		if failure.StatusCode > 0 {
			detail += fmt.Sprintf(" (HTTP %d)", failure.StatusCode)
		}
		return detail
	}
	return "Kitsu lookup failed (request)"
}

func globalKitsuLookupMessage(lang string, lookupErr error) string {
	return `<div class="notice notice-warning" role="status"><strong>` +
		esc(t(lang, "Kitsuユーザーを確認できませんでした", "Kitsu users could not be checked")) +
		`</strong><p>` +
		esc(t(lang, "Kitsuからユーザー一覧を取得できません。接続設定を確認して再読み込みしてください。", "The Kitsu user list could not be retrieved. Check the connection settings and reload.")) +
		`</p><div class="button-row"><a class="btn-ghost" href="` + esc(appendLang("/bot/admin/bot", lang)) + `">` +
		esc(t(lang, "Kitsu接続を確認", "Check Kitsu connection")) +
		`</a></div><details class="advanced-details"><summary>` +
		esc(t(lang, "診断の詳細", "Diagnostic details")) +
		`</summary><p class="field-help">` + esc(kitsuLookupDiagnostic(lookupErr)) + `</p></details></div>`
}

// renderUserLinkingSetupNotice shows only the prerequisite blocking User
// Linking. It intentionally avoids rendering the guild selector, mapping table
// or diagnostic details while configuration is incomplete.
func renderUserLinkingSetupNotice(lang string, kitsuConfigured, discordConfigured bool) string {
	message := t(lang, "Kitsuを設定するとKitsuユーザーを利用できます。", "Configure Kitsu to load Kitsu users.")
	if !kitsuConfigured && !discordConfigured {
		message = t(lang, "KitsuとDiscord Botを設定すると、サーバーとユーザーを取得できます。", "Configure Kitsu and the Discord Bot to load servers and users.")
	} else if !discordConfigured {
		message = t(lang, "Discord Botを設定すると、Discordサーバーとユーザーを取得できます。", "Configure the Discord Bot to load servers and users.")
	}
	action := `<a class="btn-ghost" href="` + esc(appendLang("/bot/admin/bot", lang)) + `">` +
		esc(t(lang, "接続設定", "Connection settings")) + `</a>`
	return `<div class="notice notice-info user-linking-readiness-notice" role="status"><p>` + esc(message) +
		`</p><div class="button-row">` + action + `</div></div>`
}

func renderUserLinkingGuildSelector(lang string, directory globalDiscordDirectory) string {
	var options strings.Builder
	if len(directory.Guilds) > 0 {
		options.WriteString(`<option value="">` + esc(t(lang, "Discordサーバーを選択", "Select a Discord server")) + `</option>`)
		for _, guild := range directory.Guilds {
			selected := ""
			if strings.TrimSpace(guild.ID) == strings.TrimSpace(directory.SelectedGuild.ID) {
				selected = " selected"
			}
			options.WriteString(`<option value="` + esc(guild.ID) + `"` + selected + `>` + esc(guild.Name) + `</option>`)
		}
	}
	return `<form method="GET" class="form-action-row" aria-label="` + esc(t(lang, "Discordサーバーの選択", "Discord server selection")) + `">` +
		`<input type="hidden" name="lang" value="` + esc(lang) + `">` +
		`<label for="global-discord-guild">` + esc(tr(lang, "ia.discord_server")) + `</label>` +
		`<select id="global-discord-guild" name="discord_guild_id" onchange="this.form.submit()">` + options.String() + `</select>` +
		`<noscript><button class="btn-ghost" type="submit">` + esc(t(lang, "表示", "Show")) + `</button></noscript></form>`
}

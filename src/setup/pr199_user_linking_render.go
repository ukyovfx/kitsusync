package setup

import (
	"app/src/model"
	"fmt"
	"net/http"
	"strings"

	"gorm.io/gorm"
)

func renderGlobalUserLinking(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	if lang != "en" {
		lang = "ja"
	}

	_, _, kitsuConfigured := runtimeKitsuDataSource(db)
	botToken := storedRuntimeDiscordBotToken(db)
	discordConfigured := strings.TrimSpace(botToken) != ""
	if !kitsuConfigured || !discordConfigured {
		body := userLinkingPage(lang, `<section class="section-card glass">`+
			renderUserLinkingSetupNotice(lang, kitsuConfigured, discordConfigured)+`</section>`)
		fmt.Fprint(w, adminPage(lang, "", r, body))
		return
	}

	people, _, peopleErr := globalUserLinkingPeopleWithError(db)
	if peopleErr != nil {
		body := userLinkingPage(lang, `<section class="section-card glass">`+
			globalKitsuLookupMessage(lang, peopleErr)+`</section>`)
		fmt.Fprint(w, adminPage(lang, "", r, body))
		return
	}
	if len(people) == 0 {
		body := userLinkingPage(lang, userLinkingEmptyState(
			lang,
			t(lang, "Kitsuユーザーが見つかりません", "No Kitsu users were returned"),
			t(lang, "Kitsuにリンク可能な人間ユーザーが追加されると、ここからDiscordユーザーを選択できます。", "When Kitsu returns linkable human users, you can select their Discord identities here."),
		))
		fmt.Fprint(w, adminPage(lang, "", r, body))
		return
	}

	directory, loadErr := loadGlobalDiscordDirectory(botToken, canonicalDiscordGuildQuery(r))
	if loadErr != nil {
		content := globalDiscordMemberLoadMessage(lang, loadErr)
		if len(directory.Guilds) > 0 {
			content = renderUserLinkingGuildSelector(lang, directory) + content
		}
		body := userLinkingPage(lang, `<section class="section-card glass">`+content+`</section>`)
		fmt.Fprint(w, adminPage(lang, "", r, body))
		return
	}
	if len(directory.Guilds) == 0 {
		body := userLinkingPage(lang, userLinkingEmptyState(
			lang,
			t(lang, "Discordサーバーがありません", "No Discord servers are available"),
			t(lang, "Botが参加しているDiscordサーバーが見つかると、ここからユーザーを取得できます。", "A Discord server joined by the Bot is required before users can be loaded."),
		))
		fmt.Fprint(w, adminPage(lang, "", r, body))
		return
	}
	if len(directory.Guilds) > 1 && strings.TrimSpace(directory.SelectedGuild.ID) == "" {
		body := userLinkingPage(lang, `<section class="section-card glass user-linking-server-select">`+
			renderUserLinkingGuildSelector(lang, directory)+
			`<p class="user-linking-select-hint" role="status">`+
			esc(t(lang, "Discordサーバーを選択してください。", "Select a Discord server first."))+
			`</p></section>`)
		fmt.Fprint(w, adminPage(lang, "", r, body))
		return
	}
	if len(directory.Options) == 0 {
		body := userLinkingPage(lang, `<section class="section-card glass user-linking-directory">`+
			renderUserLinkingGuildSelector(lang, directory)+
			`<div class="empty-state user-linking-empty" role="status"><strong>`+
			esc(t(lang, "選択できるDiscordユーザーがいません", "No selectable Discord users"))+
			`</strong><span class="field-help">`+
			esc(t(lang, "このDiscordサーバーにはリンク可能な人間ユーザーがいません。", "This Discord server has no selectable human users."))+
			`</span></div></section>`)
		fmt.Fprint(w, adminPage(lang, "", r, body))
		return
	}

	body := userLinkingPage(lang, `<section class="section-card glass user-linking-directory">`+
		renderUserLinkingGuildSelector(lang, directory)+
		`</section>`+
		renderGlobalUserLinkingTable(db, lang, directory, people))
	fmt.Fprint(w, adminPage(lang, "", r, body))
}

func userLinkingPage(lang, content string) string {
	return `<section class="section-stack user-linking-page"><h1>` + esc(tr(lang, "ia.user_mapping")) + `</h1>` + content + `</section>`
}

func userLinkingEmptyState(lang, title, detail string) string {
	return `<section class="section-card glass"><div class="empty-state user-linking-empty" role="status"><strong>` +
		esc(title) + `</strong><span class="field-help">` + esc(detail) + `</span></div></section>`
}

func renderGlobalUserLinkingTable(db *gorm.DB, lang string, directory globalDiscordDirectory, people []KitsuPerson) string {
	localMaps := model.ListUserMap(db)
	liveDiscordUsers := make(map[string]globalDiscordUserOption, len(directory.Options))
	for _, option := range directory.Options {
		id := strings.TrimSpace(option.ID)
		if id != "" {
			liveDiscordUsers[id] = option
		}
	}
	findMap := func(person KitsuPerson) *model.UserMap {
		for i := range localMaps {
			m := &localMaps[i]
			if strings.TrimSpace(person.ID) != "" && strings.TrimSpace(m.KitsuID) == strings.TrimSpace(person.ID) {
				return m
			}
			if strings.TrimSpace(person.Email) != "" && strings.EqualFold(strings.TrimSpace(m.KitsuEmail), strings.TrimSpace(person.Email)) {
				return m
			}
			if strings.EqualFold(strings.TrimSpace(m.KitsuName), strings.TrimSpace(person.FullName)) {
				return m
			}
		}
		return nil
	}
	memberOptions := func(current string) string {
		var b strings.Builder
		b.WriteString(`<option value="">` + esc(t(lang, "未設定", "Not set")) + `</option>`)
		if current != "" {
			if _, found := liveDiscordUsers[current]; !found {
				b.WriteString(`<option value="` + esc(current) + `" selected disabled>` + esc(t(lang, "選択中のDiscordサーバーで利用できません", "Unavailable in selected Discord server")) + `</option>`)
			}
		}
		for _, option := range directory.Options {
			selected := ""
			if strings.TrimSpace(option.ID) == strings.TrimSpace(current) {
				selected = " selected"
			}
			b.WriteString(`<option value="` + esc(option.ID) + `" title="` + esc(option.Name) + `"` + selected + `>` + esc(option.Name) + `</option>`)
		}
		return b.String()
	}

	var rows strings.Builder
	for _, person := range people {
		if strings.TrimSpace(person.FullName) == "" {
			continue
		}
		mapped := findMap(person)
		identity, state, class := t(lang, "未設定", "Not set"), t(lang, "未設定", "Not set"), "blocked"
		currentDiscordID := ""
		userID := ""
		if mapped != nil {
			userID = fmt.Sprint(mapped.ID)
			currentDiscordID = strings.TrimSpace(mapped.DiscordID)
			switch {
			case isSyntheticDiscordID(currentDiscordID):
				identity, state, class = t(lang, "検証用データ", "Fixture data"), t(lang, "検証用データ", "Fixture data"), "neutral"
				currentDiscordID = ""
			case currentDiscordID != "":
				live, found := liveDiscordUsers[currentDiscordID]
				switch {
				case !found:
					identity, state, class = t(lang, "選択中のDiscordサーバーで利用できません", "Unavailable in selected Discord server"), t(lang, "要確認", "Needs verification"), "warning"
				case strings.TrimSpace(live.Username) == "":
					identity, state, class = t(lang, "Discordユーザー名を確認できません", "Discord username unavailable"), t(lang, "要確認", "Needs verification"), "warning"
				default:
					identity, state, class = "@"+strings.TrimSpace(live.Username), t(lang, "紐づけ済み", "Linked"), "success"
				}
			}
		}

		initialIndex := 0
		if currentDiscordID != "" {
			for i, option := range directory.Options {
				if strings.TrimSpace(option.ID) == currentDiscordID {
					initialIndex = i + 1
					break
				}
			}
			if initialIndex == 0 {
				initialIndex = 1
			}
		}
		form := `<form method="POST" class="inline-form user-link-form">` +
			`<input type="hidden" name="action" value="save_global_link">` +
			`<input type="hidden" name="user_id" value="` + esc(userID) + `">` +
			`<input type="hidden" name="kitsu_id" value="` + esc(person.ID) + `">` +
			`<input type="hidden" name="kitsu_name" value="` + esc(person.FullName) + `">` +
			`<input type="hidden" name="kitsu_email" value="` + esc(person.Email) + `">` +
			`<input type="hidden" name="discord_guild_id" value="` + esc(directory.SelectedGuild.ID) + `">` +
			`<select data-initial-index="` + fmt.Sprint(initialIndex) + `" name="discord_user_id" onchange="this.form.querySelector('button[type=submit]').disabled = this.value === '' || this.selectedIndex === Number(this.dataset.initialIndex)" aria-label="` +
			esc(person.FullName+" - "+t(lang, "Discordユーザー", "Discord user")) + `">` +
			memberOptions(currentDiscordID) + `</select>` +
			`<button class="btn" type="submit" disabled>` + esc(t(lang, "保存", "Save")) + `</button></form>`
		if mapped != nil && mapped.ID > 0 {
			form += `<form method="POST" class="inline-form delete-form" data-confirm="` +
				esc(t(lang, "Kitsuユーザー「"+person.FullName+"」とDiscordユーザー「"+identity+"」の紐づけを解除します。", "Unlink the Kitsu user \""+person.FullName+"\" from Discord user \""+identity+"\".")) +
				`"><input type="hidden" name="action" value="remove_global_link"><input type="hidden" name="discord_guild_id" value="` + esc(directory.SelectedGuild.ID) + `"><input type="hidden" name="user_id" value="` +
				esc(fmt.Sprint(mapped.ID)) + `"><button class="btn-ghost" type="submit">` + esc(t(lang, "解除", "Unlink")) + `</button></form>`
		}
		rows.WriteString(`<tr class="user-link-grid-row"><td data-label="` + esc(t(lang, "Kitsuユーザー", "Kitsu user")) + `">` +
			esc(person.FullName) + `</td><td data-label="` + esc(t(lang, "Discordユーザー", "Discord user")) + `"><span class="user-link-discord-identity" title="` + esc(identity) + `">` +
			esc(identity) + `</span></td><td data-label="` + esc(t(lang, "状態", "Status")) + `"><span class="status-badge status-badge-` +
			class + `" role="status">` + esc(state) + `</span></td><td data-label="` + esc(t(lang, "操作", "Actions")) +
			`"><div class="user-link-actions">` + form + `</div></td></tr>`)
	}
	return `<div class="table-wrap user-linking-table"><table><thead><tr><th>` +
		esc(t(lang, "Kitsuユーザー", "Kitsu user")) + `</th><th>` +
		esc(t(lang, "Discordユーザー", "Discord user")) + `</th><th>` +
		esc(t(lang, "状態", "Status")) + `</th><th>` +
		esc(t(lang, "操作", "Action")) + `</th></tr></thead><tbody>` +
		rows.String() + `</tbody></table></div>`
}

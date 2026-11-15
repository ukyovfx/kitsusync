package setup

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"app/src/model"
	"gorm.io/gorm"
)

type reviewerRoleAvailability string

const (
	reviewerRolesNoGuild        reviewerRoleAvailability = "no_guild"
	reviewerRolesNoBot          reviewerRoleAvailability = "no_bot"
	reviewerRolesUnavailable    reviewerRoleAvailability = "unavailable"
	reviewerRolesEmpty          reviewerRoleAvailability = "empty"
	reviewerRolesNonMentionable reviewerRoleAvailability = "non_mentionable"
	reviewerRolesAvailable      reviewerRoleAvailability = "available"
)

func productionNotificationRoleAvailability(project model.Project, view productionNotificationReviewerView) reviewerRoleAvailability {
	if !isDiscordSnowflake(project.DiscordGuildID) {
		return reviewerRolesNoGuild
	}
	if strings.TrimSpace(view.BotToken) == "" {
		return reviewerRolesNoBot
	}
	if !view.RolesReady || view.RolesErr != nil {
		return reviewerRolesUnavailable
	}
	if len(view.RoleCatalog) == 0 {
		return reviewerRolesEmpty
	}
	if view.RoleSendEvidenceRequested || len(view.RoleSendEvidenceByTaskType) > 0 || len(view.RoleSendErrorsByTaskType) > 0 {
		evidenceUnavailable := false
		for _, evidenceErr := range view.RoleSendErrorsByTaskType {
			if evidenceErr != nil {
				evidenceUnavailable = true
			}
		}
		for taskTypeID, evidence := range view.RoleSendEvidenceByTaskType {
			if view.RoleSendErrorsByTaskType[taskTypeID] != nil {
				continue
			}
			sender := view.RoleSendersByTaskType[taskTypeID]
			for _, role := range view.RoleCatalog {
				if forumRoleCanBeMentioned(project.DiscordGuildID, sender.ChannelID, role, evidence) {
					return reviewerRolesAvailable
				}
			}
		}
		if len(view.RoleSendEvidenceByTaskType) == 0 && !evidenceUnavailable {
			return reviewerRolesUnavailable
		}
		if evidenceUnavailable {
			return reviewerRolesUnavailable
		}
		return reviewerRolesNonMentionable
	}
	if len(view.Roles) == 0 {
		return reviewerRolesNonMentionable
	}
	return reviewerRolesAvailable
}

func reviewerRoleAvailabilityMessage(lang string, project model.Project, view productionNotificationReviewerView) string {
	var message string
	switch productionNotificationRoleAvailability(project, view) {
	case reviewerRolesNoGuild:
		message = t(lang, "Discordサーバーが設定されていません。", "No Discord server is configured.")
	case reviewerRolesNoBot:
		message = t(lang, "Discord Botが接続されていません。", "Discord Bot is not configured.")
	case reviewerRolesUnavailable:
		var apiErr *discordGuildRoleListError
		if errors.As(view.RolesErr, &apiErr) && apiErr.StatusCode > 0 {
			switch apiErr.StatusCode {
			case http.StatusForbidden:
				message = t(lang, "Discordロールを取得できません（HTTP 403）。Bot接続とアクセス権を確認してください。", "Discord roles could not be read (HTTP 403). Check the Bot connection and access.")
			case http.StatusTooManyRequests:
				message = t(lang, "Discordロールの確認は一時的に制限されています（HTTP 429）。後でもう一度お試しください。", "Discord role lookup is rate limited (HTTP 429). Try again shortly.")
			default:
				message = t(lang, fmt.Sprintf("Discordロールを確認できません（HTTP %d）。", apiErr.StatusCode), fmt.Sprintf("Discord roles could not be verified (HTTP %d).", apiErr.StatusCode))
			}
		} else {
			message = t(lang, "Discordロールと送信先の権限を確認できません。", "Discord roles and destination permissions could not be verified.")
		}
	case reviewerRolesEmpty:
		message = t(lang, "このDiscordサーバーにカスタムロールはありません。", "No custom Discord roles are configured.")
	case reviewerRolesNonMentionable:
		message = t(lang, "確認済みの送信先で利用できるロールはありません。", "No roles are eligible for a verified destination.")
	}
	if message == "" {
		return ""
	}
	return `<p class="field-help recipient-empty-state" role="status">` + esc(message) + `</p>`
}

func renderCurrentIARoutingEditorSetupStyle(db *gorm.DB, r *http.Request, p model.Project, lang, readTable string, botTokens ...string) string {
	view := loadProductionNotificationReviewerView(db, p, true, true, botTokens...)
	return renderCurrentIARoutingEditorSetupStyleWithData(db, r, p, lang, readTable, view)
}

type reviewerTargetView struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

func renderCurrentIARoutingEditorSetupStyleWithData(db *gorm.DB, r *http.Request, p model.Project, lang, readTable string, view productionNotificationReviewerView) string {
	settings, _, _ := model.LoadProductionForumSettings(db, p.KitsuProjectID)
	eligibleUsers := map[string]string{}
	if view.TeamErr == nil && view.GuildErr == nil {
		eligibleUsers = currentProductionLinkedHumanDiscordIDs(view.Team, view.Users, view.GuildUsers)
	}
	roleLabels := map[string]DiscordGuildRole{}
	for _, role := range view.RoleCatalog {
		roleLabels[role.ID] = role
	}
	var userOptions, roleOptions strings.Builder
	userIDs := make([]string, 0, len(eligibleUsers))
	for id := range eligibleUsers {
		userIDs = append(userIDs, id)
	}
	sort.Strings(userIDs)
	var userListboxOptions strings.Builder
	for _, id := range userIDs {
		userOptions.WriteString(`<option value="` + esc(id) + `">` + esc(eligibleUsers[id]) + `</option>`)
		userListboxOptions.WriteString(`<button type="button" role="option" tabindex="-1" aria-selected="false" data-wfa-option-value="` + esc(id) + `">` + esc(eligibleUsers[id]) + `</button>`)
	}
	roleCatalog := reviewerRoleCatalog(p.DiscordGuildID, view.RoleCatalog)
	sort.Slice(roleCatalog, func(i, j int) bool { return roleCatalog[i].Name < roleCatalog[j].Name })
	var roleListboxOptions strings.Builder
	for _, role := range roleCatalog {
		label := "@" + role.Name
		disabledLabel := t(lang, "送信先を確認できません", "Destination could not be verified")
		if !role.Mentionable {
			disabledLabel = t(lang, "メンション不可・Bot権限を確認できません", "Not mentionable; Bot permission is unavailable")
		}
		roleOptions.WriteString(`<option value="` + esc(role.ID) + `" disabled>@` + esc(role.Name) + `</option>`)
		roleListboxOptions.WriteString(`<button type="button" role="option" tabindex="-1" aria-selected="false" aria-disabled="true" class="recipient-option-disabled" data-role-mentionable="` + strconv.FormatBool(role.Mentionable) + `" data-role-name="` + esc(label) + `" data-role-disabled-label="` + esc(disabledLabel) + `" data-wfa-option-value="` + esc(role.ID) + `">` + esc(label+" — "+disabledLabel) + `</button>`)
	}
	userAvailability, roleAvailability := "", ""
	if view.TeamErr != nil {
		userAvailability = `<p class="field-help" role="status">` + esc(t(lang, "Production Teamを読み込めないため、ユーザーを確認できません。", "Production Team could not be verified, so User targets are unavailable.")) + `</p>`
	} else if view.GuildErr != nil || view.BotToken == "" {
		userAvailability = `<p class="field-help" role="status">` + esc(t(lang, "Discordメンバーを確認できないため、ユーザーを選択できません。", "Discord membership could not be verified, so User targets are unavailable.")) + `</p>`
	} else if len(userIDs) == 0 {
		userAvailability = `<p class="field-help" role="status">` + esc(t(lang, "現在リンク可能なDiscordユーザーはいません。", "No linked Production Team users are currently available.")) + `</p>`
	}
	roleAvailability = reviewerRoleAvailabilityMessage(lang, p, view)
	formAction := withLang("/bot/admin/projects", r) + "&project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=notifications&edit_routing=1"
	successURL := withLang("/bot/admin/projects", r) + "&project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=notifications"
	revision, _ := model.ProductionNotificationRevisionWithError(db, p.ID, p.KitsuProjectID)
	var rows strings.Builder
	for _, route := range settings.Routes {
		targets, _, _ := model.ListProjectReviewerTargetsForTaskType(db, p.ID, route.TaskTypeID)
		evidence := view.RoleSendEvidenceByTaskType[route.TaskTypeID]
		evidenceErr := view.RoleSendErrorsByTaskType[route.TaskTypeID]
		targetViews := make([]reviewerTargetView, 0, len(targets))
		for _, target := range targets {
			label := target.DiscordID
			if target.TargetKind == model.ReviewerTargetUser {
				if name := eligibleUsers[target.DiscordID]; name != "" {
					label = name
				} else {
					label = t(lang, "リンクまたはメンバーを確認できません", "User is no longer verified")
				}
			} else if role, ok := roleLabels[target.DiscordID]; ok {
				if evidenceErr == nil && forumRoleCanBeMentioned(p.DiscordGuildID, route.ForumChannelID, role, evidence) {
					label = "@" + role.Name
				} else {
					label = t(lang, "ロール利用不可", "Role unavailable")
				}
			} else {
				label = t(lang, "ロールを確認できません", "Role is no longer verified")
			}
			targetViews = append(targetViews, reviewerTargetView{target.TargetKind, target.DiscordID, label})
		}
		encodedTargets, _ := json.Marshal(targetViews)
		targetsAttribute := base64.StdEncoding.EncodeToString(encodedTargets)
		eligibleRoleIDs := make([]string, 0, len(roleCatalog))
		for _, role := range roleCatalog {
			if evidenceErr == nil && forumRoleCanBeMentioned(p.DiscordGuildID, route.ForumChannelID, role, evidence) {
				eligibleRoleIDs = append(eligibleRoleIDs, role.ID)
			}
		}
		evidenceVerified := evidenceErr == nil && evidence.PermissionsKnown
		rows.WriteString(`<tr data-routing-row data-task-type="` + esc(route.TaskTypeID) + `" data-task-type-name="` + esc(route.TaskTypeName) + `" data-existing-targets="` + targetsAttribute + `" data-role-sender-verified="` + strconv.FormatBool(evidenceVerified) + `" data-bot-can-mention-roles="` + strconv.FormatBool(evidence.CanMentionRoles) + `" data-role-eligible-ids="` + esc(strings.Join(eligibleRoleIDs, ",")) + `" tabindex="0" aria-controls="production-wfa-detail-panel" aria-selected="false" aria-expanded="false"><th scope="row"><button type="button" class="routing-select-task" data-select-task aria-pressed="false" aria-expanded="false"><span class="routing-task-type-name">` + esc(route.TaskTypeName) + `</span><span class="routing-expand-indicator" aria-hidden="true"></span></button></th><td>#` + esc(strings.TrimPrefix(route.ForumChannelName, "#")) + `</td></tr>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr class="production-notification-empty"><td colspan="2">` + esc(t(lang, "フォーラムがまだ設定されていません。先にフォーラム設定を完了してください。", "No forums are configured yet. Finish the forum settings first.")) + `</td></tr>`)
	}
	userSelectLabel := t(lang, "選択", "Select")
	userPicker := userAvailability
	if len(userIDs) > 0 {
		userPicker = `<label class="recipient-picker-label">` + esc(t(lang, "Discordユーザー", "Discord user")) + `<span class="recipient-picker" data-recipient-picker><button type="button" class="recipient-combobox" role="combobox" aria-haspopup="listbox" aria-expanded="false" aria-controls="wfa-user-listbox" aria-label="` + esc(t(lang, "Discordユーザー", "Discord user")) + `" data-wfa-user-combobox><span data-recipient-combobox-value>` + esc(userSelectLabel) + `</span><span aria-hidden="true" class="recipient-combobox-indicator">▾</span></button><div id="wfa-user-listbox" class="recipient-listbox" role="listbox" aria-label="` + esc(t(lang, "Discordユーザー", "Discord user")) + `" data-wfa-user-listbox hidden>` + userListboxOptions.String() + `</div></span></label>`
	} else if userPicker == "" {
		userPicker = `<p class="field-help recipient-empty-state" role="status">` + esc(t(lang, "現在追加できるDiscordユーザーはいません", "No eligible Discord users available")) + `</p>`
	}
	rolePicker := roleAvailability
	if view.RolesReady && len(roleCatalog) > 0 {
		rolePicker = roleAvailability + `<label class="recipient-picker-label">` + esc(t(lang, "Discordロール", "Discord role")) + `<span class="recipient-picker" data-recipient-picker><button type="button" class="recipient-combobox" role="combobox" aria-haspopup="listbox" aria-expanded="false" aria-controls="wfa-role-listbox" aria-label="` + esc(t(lang, "Discordロール", "Discord role")) + `" data-wfa-role-combobox><span data-recipient-combobox-value>` + esc(userSelectLabel) + `</span><span aria-hidden="true" class="recipient-combobox-indicator">▾</span></button><div id="wfa-role-listbox" class="recipient-listbox" role="listbox" aria-label="` + esc(t(lang, "Discordロール", "Discord role")) + `" data-wfa-role-listbox hidden>` + roleListboxOptions.String() + `</div></span></label>`
	}
	addModal := `<dialog data-wfa-add-modal data-has-role-options="false" data-role-unverified-label="` + esc(t(lang, "送信先を確認できません", "Destination could not be verified")) + `" data-role-permission-label="` + esc(t(lang, "メンション不可・Bot権限を確認できません", "Not mentionable; Bot permission is unavailable")) + `" data-route-required-label="` + esc(t(lang, "先にTask Typeを選択してください。", "Select a Task Type before adding a recipient.")) + `" data-target-required-label="` + esc(t(lang, "追加するDiscordユーザーまたはロールを選択してください。", "Select a Discord user or role to add.")) + `" data-recipient-mode="user" aria-labelledby="wfa-add-modal-title"><h4 id="wfa-add-modal-title">` + esc(t(lang, "通知先を追加", "Add recipient")) + `</h4><div class="recipient-mode-tabs" role="tablist" aria-label="` + esc(t(lang, "通知先の種類", "Recipient type")) + `"><button type="button" role="tab" id="wfa-user-tab" aria-selected="true" aria-controls="wfa-user-panel" data-recipient-mode="user">` + esc(t(lang, "ユーザー", "Users")) + `</button><button type="button" role="tab" id="wfa-role-tab" aria-selected="false" aria-controls="wfa-role-panel" tabindex="-1" data-recipient-mode="role">` + esc(t(lang, "ロール", "Roles")) + `</button></div><div id="wfa-user-panel" role="tabpanel" aria-labelledby="wfa-user-tab" data-recipient-panel="user">` + userPicker + `<select data-wfa-user-id data-recipient-select="user" hidden aria-hidden="true" tabindex="-1"><option value=""></option>` + userOptions.String() + `</select></div><div id="wfa-role-panel" role="tabpanel" aria-labelledby="wfa-role-tab" data-recipient-panel="role" hidden>` + rolePicker + `<select data-wfa-role-id data-recipient-select="role" hidden aria-hidden="true" tabindex="-1"><option value=""></option>` + roleOptions.String() + `</select></div><p class="field-help recipient-add-message" data-wfa-add-message role="status" aria-live="polite" hidden></p><div class="button-row"><button type="button" class="btn-ghost" data-wfa-add-cancel>` + esc(t(lang, "キャンセル", "Cancel")) + `</button><button type="button" class="btn-primary" data-wfa-add-confirm disabled>` + esc(t(lang, "追加", "Add")) + `</button></div></dialog>`
	panel := `<section id="production-wfa-detail-panel" class="production-wfa-edit-panel" data-wfa-detail-panel aria-live="polite" hidden><div class="production-wfa-edit-heading"><h4 data-wfa-title></h4><span>` + esc(t(lang, "WFA通知先", "WFA recipients")) + `</span></div><div class="production-wfa-recipient-row" data-wfa-automatic><span class="production-wfa-recipient-kind">` + esc(t(lang, "Supervisor", "Supervisor")) + `</span><span data-wfa-automatic-value>` + esc(t(lang, "確認中", "Checking")) + `</span></div><div class="production-wfa-recipient-row" data-wfa-additional><span class="production-wfa-recipient-kind">` + esc(t(lang, "追加通知先", "Additional recipients")) + `</span><ul data-wfa-target-list></ul><button type="button" class="btn-ghost production-wfa-add-target" data-wfa-add-target>+ ` + esc(t(lang, "追加", "Add")) + `</button></div></section>`
	html := `<section class="production-routing-editor" aria-labelledby="production-routing-editor-title"><div class="page-heading"><div><h3 id="production-routing-editor-title">` + esc(t(lang, "WFA通知先", "WFA recipients")) + `</h3></div><span class="status-pill ` + esc(normalizeStatusClass(iaStatusClass(db, p))) + `">` + esc(iaStatusLabel(db, p, lang)) + `</span></div>` + readTable + `<form method="post" action="` + esc(formAction) + `" data-current-routing-form lang="` + esc(lang) + `" data-async-notification-apply data-apply-endpoint="/bot/admin/projects?apply_notifications=1" data-project-id="` + esc(p.KitsuProjectID) + `" data-success-url="` + esc(successURL) + `" data-no-pending="` + esc(t(lang, "変更はありません", "No pending changes")) + `" data-pending-one="` + esc(t(lang, "件の未適用の変更", "pending change")) + `" data-pending-many="` + esc(t(lang, "件の未適用の変更", "pending changes")) + `" data-pending-instruction="` + esc(t(lang, "変更はまだ保存されていません。下の「適用」を押してください。", "Changes are pending; click Apply below to save.")) + `" data-stale-message="` + esc(t(lang, "別の編集が保存されました。入力内容は保持されています。内容を確認してから、もう一度変更を適用してください。", "Another edit was saved. Your pending changes are preserved. Review them, then Apply again.")) + `" data-error-message="` + esc(t(lang, "変更を適用できませんでした。入力内容は保持されています。", "Changes could not be applied. Your pending edits are preserved.")) + `" data-error-production-not-writable="` + esc(t(lang, "このProductionは読み取り専用のため保存できません。書き込み可能なProductionを選択してください。", "This Production is read-only, so changes were not saved. Select a writable Production.")) + `" data-error-reviewer-target="` + esc(t(lang, "選択したユーザーまたはロールを現在の送信先で確認できません。利用可能な対象を選び直してください。", "The selected User or Role could not be verified for the current destination. Choose an eligible target and try again.")) + `" data-error-reviewer-unavailable="` + esc(t(lang, "チーム所属またはDiscord権限を確認できませんでした。接続状態を確認してから再試行してください。", "Team membership or Discord permissions could not be verified. Check the connections and try again.")) + `" data-error-route-removed="` + esc(t(lang, "対象のTask Typeのフォーラムが削除されています。画面を再読み込みしてください。", "The forum for that Task Type no longer exists. Reload the page.")) + `" data-remove-label="` + esc(t(lang, "削除", "Remove")) + `" data-empty-wfa="` + esc(t(lang, "該当するSupervisorはいません。", "No matching Supervisor.")) + `"><input type="hidden" name="expected_revision" value="` + esc(revision) + `"><div class="table-wrap wizard-plan-table"><table><thead><tr><th>` + esc(t(lang, "Kitsu Task Type", "Kitsu Task Type")) + `</th><th>` + esc(t(lang, "フォーラム", "Forum")) + `</th></tr></thead><tbody data-current-routing-sort>` + rows.String() + `</tbody></table></div>` + panel + `<div class="button-row production-routing-editor-actions"><span data-pending-status aria-live="polite"></span><button type="submit" class="btn-primary" data-apply-submit disabled>` + esc(t(lang, "適用", "Apply")) + `</button><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications", r)) + `">` + esc(t(lang, "キャンセル", "Cancel")) + `</a></div><p data-apply-message role="alert"></p></form>` + addModal + `</section>`
	return html + currentRoutingEditorScript() + currentRecipientComboboxScript()
}

func renderCurrentIARoutingSummaryWithStatus(db *gorm.DB, r *http.Request, p model.Project, lang, class, label string) string {
	channelLabel := t(lang, "フォーラム", "Forum")
	var rows strings.Builder
	settings, _, _ := model.LoadProductionForumSettings(db, p.KitsuProjectID)
	for _, route := range settings.Routes {
		channel := t(lang, "未設定", "Not configured")
		if name := strings.TrimSpace(route.ForumChannelName); name != "" {
			channel = "#" + strings.TrimPrefix(name, "#")
		}
		rows.WriteString(`<div class="production-routing-summary-row"><strong>` + esc(route.TaskTypeName) + `</strong><span aria-hidden="true">&#8594;</span><span>` + esc(channel) + `</span></div>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<p class="field-help">` + esc(t(lang, "フォーラムはまだ設定されていません。", "No forums are configured yet.")) + `</p>`)
	}
	editURL := withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications&edit_routing=1", r)
	return `<section class="production-settings-section production-routing-summary"><div class="page-heading"><div><h3>` + esc(t(lang, "WFA通知先", "WFA recipients")) + `</h3><p class="field-help">` + esc(t(lang, "タスクタイプごとのフォーラムと通知先の設定です。", "Per-Task-Type forums and their recipients.")) + `</p></div><span class="status-pill ` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(label) + `</span><a class="btn-ghost" href="` + esc(editURL) + `">` + esc(t(lang, "編集", "Edit")) + `</a></div><div class="production-routing-summary-head"><strong>Kitsu Task Type</strong><strong>` + esc(channelLabel) + `</strong></div><div class="production-routing-summary-list">` + rows.String() + `</div></section>`
}

func iaStatusClass(db *gorm.DB, p model.Project) string {
	class, _, _ := iaStatus(db, p, "en")
	return class
}
func iaStatusLabel(db *gorm.DB, p model.Project, lang string) string {
	_, label, _ := iaStatus(db, p, lang)
	return label
}

func currentRecipientComboboxScript() string {
	return `<script>
(function(){
  var modal=document.querySelector('[data-wfa-add-modal]');
  if(!modal)return;
  var pickers=Array.prototype.map.call(modal.querySelectorAll('[data-recipient-picker]'),function(picker){
    var combo=picker.querySelector('[role="combobox"]'),list=picker.querySelector('[role="listbox"]'),panel=picker.closest('[data-recipient-panel]'),select=panel&&panel.querySelector('select[data-recipient-select]'),valueNode=combo.querySelector('[data-recipient-combobox-value]'),empty=valueNode.textContent;
    if(!select)return null;
    function options(){return Array.prototype.slice.call(list.querySelectorAll('[role="option"]'))}
    function enabled(){return options().filter(function(option){return option.getAttribute('aria-disabled')!=='true'})}
    function sync(){var selected=select.selectedOptions&&select.selectedOptions[0];valueNode.textContent=selected&&selected.value?selected.textContent:empty;options().forEach(function(option){option.setAttribute('aria-selected',String(!!selected&&selected.value===option.dataset.wfaOptionValue))})}
    function close(){list.hidden=true;combo.setAttribute('aria-expanded','false');list.style.left='';list.style.top='';list.style.width='';list.style.maxHeight=''}
    function open(last){list.hidden=false;combo.setAttribute('aria-expanded','true');var anchor=combo.getBoundingClientRect(),dialog=picker.closest('dialog[open]'),bounds=dialog?dialog.getBoundingClientRect():{left:0,right:innerWidth,top:0,bottom:innerHeight},width=Math.min(anchor.width,innerWidth-16,bounds.right-bounds.left-16),left=Math.max(bounds.left+8,Math.min(anchor.left,bounds.right-width-8)),contentHeight=list.scrollHeight||options().length*36+8,natural=Math.min(contentHeight,220,Math.round(innerHeight*.4)),below=Math.max(0,bounds.bottom-anchor.bottom-8),above=Math.max(0,anchor.top-bounds.top-8),height=natural,top;if(height<=below){top=anchor.bottom+4}else if(height<=above){top=anchor.top-height-4}else if(above>=below){height=above;top=anchor.top-height-4}else{height=below;top=anchor.bottom+4}list.style.left=left+'px';list.style.width=width+'px';list.style.maxHeight=height+'px';list.style.top=top+'px';var choices=enabled(),target=last?choices[choices.length-1]:choices[0];if(target){try{target.focus({preventScroll:true})}catch(_){target.focus()}}}
    function choose(option){if(!option||option.getAttribute('aria-disabled')==='true')return;select.value=option.dataset.wfaOptionValue;select.dispatchEvent(new Event('change',{bubbles:true}));sync();close();combo.focus()}
    combo.addEventListener('click',function(){if(combo.dataset.keyboardOpen==='true'){combo.dataset.keyboardOpen='';return}if(list.hidden)open(false);else close()});
    combo.addEventListener('keydown',function(event){if(event.key==='ArrowDown'||event.key==='ArrowUp'||event.key==='Enter'||event.key===' '){event.preventDefault();combo.dataset.keyboardOpen='true';open(event.key==='ArrowUp')}});
    list.addEventListener('click',function(event){var option=event.target.closest('[role="option"]');if(option)choose(option)});
    list.addEventListener('keydown',function(event){var choices=enabled(),index=choices.indexOf(document.activeElement),target=null;if(event.key==='ArrowDown'){event.preventDefault();target=choices[Math.min(index+1,choices.length-1)]}else if(event.key==='ArrowUp'){event.preventDefault();target=choices[Math.max(index-1,0)]}else if(event.key==='Home'){event.preventDefault();target=choices[0]}else if(event.key==='End'){event.preventDefault();target=choices[choices.length-1]}else if(event.key==='Enter'||event.key===' '){event.preventDefault();choose(document.activeElement)}else if(event.key==='Escape'){event.preventDefault();close();combo.focus()}else if(event.key==='Tab'){close()}if(target)target.focus()});
    select.addEventListener('change',sync);
    return{picker:picker,close:close,sync:sync}
  });
  pickers=pickers.filter(Boolean);
  document.addEventListener('pointerdown',function(event){pickers.forEach(function(item){if(!item.picker.contains(event.target))item.close()})});
  modal.addEventListener('cancel',function(){pickers.forEach(function(item){item.close()})});
  modal.addEventListener('close',function(){if(modal.open)return;pickers.forEach(function(item){item.close();item.sync()})});
  window.addEventListener('resize',function(){pickers.forEach(function(item){item.close()})});
  window.addEventListener('scroll',function(event){var scrolled=event.target;pickers.forEach(function(item){var list=item.picker.querySelector('[role="listbox"]');if(list.hidden||list.contains(scrolled))return;item.close()})},true)
})();
</script>`
}

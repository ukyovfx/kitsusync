package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

func prepareReviewerApplyTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&model.Setting{}, &model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
}

func installReviewerApplySyntheticReaders(t *testing.T, db *gorm.DB, project model.Project, webhook model.ProjectWebhook, team []kitsu.Person, teamErr error, guildMembers []DiscordGuildMember, guildErr error, roles []DiscordGuildRole, rolesErr error) {
	t.Helper()
	oldTaskTypes, oldDiscordCheck, oldChannels, oldInspect := reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook
	oldTeam, oldMembers, oldRoles, oldEvidence := reviewerProductionTeamReader, reviewerGuildMembersForGuild, reviewerGuildRolesReader, reviewerRoleSendEvidenceReader
	t.Cleanup(func() {
		reviewerTaskTypesForProduction, currentRoutingDiscordCheck, currentRoutingListChannels, currentRoutingInspectWebhook = oldTaskTypes, oldDiscordCheck, oldChannels, oldInspect
		reviewerProductionTeamReader, reviewerGuildMembersForGuild, reviewerGuildRolesReader, reviewerRoleSendEvidenceReader = oldTeam, oldMembers, oldRoles, oldEvidence
	})
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType { return []kitsu.TaskType{{ID: "task-1", Name: "Compositing"}} }
	currentRoutingDiscordCheck = func(string, string) DiscordStatusInfo {
		return DiscordStatusInfo{BotValid: true, GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true, ManageWebhooks: true}}
	}
	currentRoutingListChannels = func(guildID, _ string) ([]DiscordGuildChannel, error) {
		return []DiscordGuildChannel{{ID: webhook.DiscordChannelID, Name: webhook.ChannelName, Type: 0, ParentID: project.DiscordCategoryID}}, nil
	}
	currentRoutingInspectWebhook = func(raw string) (routingWebhookIdentity, bool, error) {
		id, err := discordWebhookID(webhook.WebhookURL)
		if err != nil || raw != webhook.WebhookURL {
			return routingWebhookIdentity{}, false, errors.New("synthetic webhook identity mismatch")
		}
		return routingWebhookIdentity{ID: id, Type: 1, GuildID: project.DiscordGuildID, ChannelID: webhook.DiscordChannelID}, false, nil
	}
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) { return team, teamErr }
	reviewerGuildMembersForGuild = func(guildID, _ string) ([]DiscordGuildMember, error) {
		if guildID != project.DiscordGuildID {
			return nil, errors.New("synthetic Guild mismatch")
		}
		return guildMembers, guildErr
	}
	reviewerGuildRolesReader = func(guildID, _ string) ([]DiscordGuildRole, error) {
		if guildID != project.DiscordGuildID {
			return nil, errors.New("synthetic Guild mismatch")
		}
		return roles, rolesErr
	}
	reviewerRoleSendEvidenceReader = func(guildID, channelID, raw, _ string) (reviewerRoleSendEvidence, error) {
		webhookID, err := discordWebhookID(webhook.WebhookURL)
		if err != nil || guildID != project.DiscordGuildID || channelID != webhook.DiscordChannelID || raw != webhook.WebhookURL {
			return reviewerRoleSendEvidence{}, errors.New("synthetic sender identity mismatch")
		}
		return reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, WebhookID: webhookID, BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true, PermissionsKnown: true}, nil
	}
}

func TestProductionNotificationApplyPersistsUserAndRoleReviewerDeltas(t *testing.T) {
	db, project := reviewerResolutionTestDB(t)
	prepareReviewerApplyTestDB(t, db)
	project.DiscordCategoryID = "123456789012345610"
	if err := db.Save(&project).Error; err != nil {
		t.Fatal(err)
	}
	const userID = "123456789012345611"
	const roleID = "123456789012345612"
	if err := db.Create(&model.UserMap{KitsuID: "person-1", KitsuName: "Linked teammate", DiscordID: userID}).Error; err != nil {
		t.Fatal(err)
	}
	reviewerTestRoute(t, db, project, "task-1")
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	webhook.ChannelName = "compositing"
	webhook.DiscordChannelID = "123456789012345608"
	if err := db.Save(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
	installReviewerApplySyntheticReaders(t, db, project, webhook,
		[]kitsu.Person{{ID: "person-1", FullName: "Linked teammate", Email: "linked@example.test", Active: true}}, nil,
		[]DiscordGuildMember{reviewerTestGuildMember(userID, "linked", "Linked", "Linked teammate")}, nil,
		[]DiscordGuildRole{{ID: roleID, Name: "Compositing Reviewers", Mentionable: true}}, nil)
	topologyWrites := 0
	oldCreateWebhook, oldDeleteWebhook := currentRoutingCreateWebhook, currentRoutingDeleteWebhook
	oldCreateChannel, oldDeleteChannel, oldSetPositions := currentRoutingCreateChannel, currentRoutingDeleteChannel, currentRoutingSetPositions
	currentRoutingCreateWebhook = func(string, string, string) (string, error) { topologyWrites++; return "", nil }
	currentRoutingDeleteWebhook = func(string, string) error { topologyWrites++; return nil }
	currentRoutingCreateChannel = func(string, string, string, string) (string, error) { topologyWrites++; return "", nil }
	currentRoutingDeleteChannel = func(string, string) error { topologyWrites++; return nil }
	currentRoutingSetPositions = func(string, []DiscordChannelPosition, string) error { topologyWrites++; return nil }
	t.Cleanup(func() {
		currentRoutingCreateWebhook, currentRoutingDeleteWebhook = oldCreateWebhook, oldDeleteWebhook
		currentRoutingCreateChannel, currentRoutingDeleteChannel, currentRoutingSetPositions = oldCreateChannel, oldDeleteChannel, oldSetPositions
	})
	revision := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	payload := `{"project_id":"` + project.KitsuProjectID + `","expected_revision":"` + revision + `","routes":[{"task_type_id":"task-1","destination_webhook_id":` + strconv.FormatUint(uint64(webhook.ID), 10) + `}],"reviewer_changes":[{"task_type_id":"task-1","add_user_ids":["` + userID + `"],"add_role_ids":["` + roleID + `"]}]}`
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	if !handleProductionNotificationApply(response, request, "en", db, "synthetic-token") {
		t.Fatal("the Notifications Apply endpoint did not handle the synthetic reviewer request")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("synthetic Apply returned status=%d body=%s", response.Code, response.Body.String())
	}
	got, _, err := model.ListProjectReviewerTargetsForTaskType(db, project.ID, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].DiscordID != userID && got[1].DiscordID != userID || got[0].DiscordID != roleID && got[1].DiscordID != roleID {
		t.Fatalf("Apply succeeded without persisting both User and Role targets: %#v", got)
	}
	if topologyWrites != 0 {
		t.Fatalf("recipient-only Apply changed Discord topology %d times", topologyWrites)
	}
}

func TestRecipientOnlyApplyNeverRepairsMissingManagedWebhook(t *testing.T) {
	db, project := reviewerResolutionTestDB(t)
	prepareReviewerApplyTestDB(t, db)
	project.DiscordCategoryID = "123456789012345610"
	if err := db.Save(&project).Error; err != nil {
		t.Fatal(err)
	}
	const userID = "123456789012345611"
	if err := db.Create(&model.UserMap{KitsuID: "person-1", KitsuName: "Linked teammate", DiscordID: userID}).Error; err != nil {
		t.Fatal(err)
	}
	reviewerTestRoute(t, db, project, "task-1")
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	webhook.ChannelName = "compositing"
	webhook.DiscordChannelID = "123456789012345608"
	if err := db.Save(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
	installReviewerApplySyntheticReaders(t, db, project, webhook,
		[]kitsu.Person{{ID: "person-1", FullName: "Linked teammate", Email: "linked@example.test", Active: true}}, nil,
		[]DiscordGuildMember{reviewerTestGuildMember(userID, "linked", "Linked", "Linked teammate")}, nil,
		nil, nil)
	oldInspect := currentRoutingInspectWebhook
	currentRoutingInspectWebhook = func(raw string) (routingWebhookIdentity, bool, error) {
		if raw == webhook.WebhookURL {
			return routingWebhookIdentity{}, true, nil
		}
		if raw == "https://discord.com/api/webhooks/123456789012345699/synthetic-repair" {
			return routingWebhookIdentity{ID: "123456789012345699", Type: 1, GuildID: project.DiscordGuildID, ChannelID: webhook.DiscordChannelID}, false, nil
		}
		t.Fatalf("unexpected webhook verification URL %q", raw)
		return routingWebhookIdentity{}, false, nil
	}
	t.Cleanup(func() { currentRoutingInspectWebhook = oldInspect })
	createWebhookCalls, deleteWebhookCalls, createChannelCalls, deleteChannelCalls, reorderCalls := 0, 0, 0, 0, 0
	oldCreateWebhook, oldDeleteWebhook := currentRoutingCreateWebhook, currentRoutingDeleteWebhook
	oldCreateChannel, oldDeleteChannel, oldSetPositions := currentRoutingCreateChannel, currentRoutingDeleteChannel, currentRoutingSetPositions
	currentRoutingCreateWebhook = func(string, string, string) (string, error) {
		createWebhookCalls++
		return "https://discord.com/api/webhooks/123456789012345699/synthetic-repair", nil
	}
	currentRoutingDeleteWebhook = func(string, string) error { deleteWebhookCalls++; return nil }
	currentRoutingCreateChannel = func(string, string, string, string) (string, error) { createChannelCalls++; return "", nil }
	currentRoutingDeleteChannel = func(string, string) error { deleteChannelCalls++; return nil }
	currentRoutingSetPositions = func(string, []DiscordChannelPosition, string) error { reorderCalls++; return nil }
	t.Cleanup(func() {
		currentRoutingCreateWebhook, currentRoutingDeleteWebhook = oldCreateWebhook, oldDeleteWebhook
		currentRoutingCreateChannel, currentRoutingDeleteChannel, currentRoutingSetPositions = oldCreateChannel, oldDeleteChannel, oldSetPositions
	})
	revision := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
	payload := `{"project_id":"` + project.KitsuProjectID + `","expected_revision":"` + revision + `","routes":[{"task_type_id":"task-1","destination_webhook_id":` + strconv.FormatUint(uint64(webhook.ID), 10) + `}],"reviewer_changes":[{"task_type_id":"task-1","add_user_ids":["` + userID + `"]}]}`
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	if !handleProductionNotificationApply(response, request, "en", db, "synthetic-token") {
		t.Fatal("the Notifications Apply endpoint did not handle the synthetic recipient-only request")
	}
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"webhook_repair_required"`) {
		t.Fatalf("recipient-only Apply with an unverified managed webhook must fail closed without repairing it: status=%d body=%s", response.Code, response.Body.String())
	}
	if createWebhookCalls != 0 || deleteWebhookCalls != 0 || createChannelCalls != 0 || deleteChannelCalls != 0 || reorderCalls != 0 {
		t.Fatalf("recipient-only Apply changed Discord topology: createWebhook=%d deleteWebhook=%d createChannel=%d deleteChannel=%d reorder=%d", createWebhookCalls, deleteWebhookCalls, createChannelCalls, deleteChannelCalls, reorderCalls)
	}
	if got := model.ListProjectReviewerTargets(db, project.ID); len(got) != 0 {
		t.Fatalf("rejected recipient-only Apply persisted Reviewer targets: %#v", got)
	}
}

func TestProductionNotificationApplyRejectsUnsafeReviewerWritesWithoutPersistence(t *testing.T) {
	for _, tc := range []struct {
		name           string
		validationOnly bool
		team           []kitsu.Person
		teamErr        error
		roleID         string
		roles          []DiscordGuildRole
		rolesErr       error
		wantStatus     int
		wantError      string
	}{
		{name: "validation-only Production is read-only", validationOnly: true, wantStatus: http.StatusForbidden, wantError: "production_not_writable"},
		{name: "user no longer eligible", wantStatus: http.StatusBadRequest, wantError: "reviewer_target_not_eligible"},
		{name: "team eligibility unavailable", teamErr: errors.New("synthetic Kitsu outage"), wantStatus: http.StatusBadGateway, wantError: "reviewer_eligibility_unavailable"},
		{name: "role no longer eligible", roleID: "123456789012345612", wantStatus: http.StatusBadRequest, wantError: "reviewer_target_not_eligible"},
		{name: "role eligibility unavailable", roleID: "123456789012345612", rolesErr: errors.New("synthetic Discord outage"), wantStatus: http.StatusBadGateway, wantError: "reviewer_eligibility_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, project := reviewerResolutionTestDB(t)
			prepareReviewerApplyTestDB(t, db)
			if tc.validationOnly {
				project.ValidationOnly = true
				if err := db.Save(&project).Error; err != nil {
					t.Fatal(err)
				}
			}
			reviewerTestRoute(t, db, project, "task-1")
			webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
			model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-token")
			installReviewerApplySyntheticReaders(t, db, project, webhook, tc.team, tc.teamErr, nil, nil, tc.roles, tc.rolesErr)
			revision := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID)
			delta := `"add_user_ids":["123456789012345611"]`
			if tc.roleID != "" {
				delta = `"add_role_ids":["` + tc.roleID + `"]`
			}
			payload := `{"project_id":"` + project.KitsuProjectID + `","expected_revision":"` + revision + `","routes":[{"task_type_id":"task-1","destination_webhook_id":` + strconv.FormatUint(uint64(webhook.ID), 10) + `}],"reviewer_changes":[{"task_type_id":"task-1",` + delta + `}]}`
			request := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?apply_notifications=1", strings.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			if !handleProductionNotificationApply(response, request, "ja", db, "synthetic-token") {
				t.Fatal("the Notifications Apply endpoint did not handle the synthetic rejection")
			}
			if response.Code != tc.wantStatus || !strings.Contains(response.Body.String(), `"error":"`+tc.wantError+`"`) {
				t.Fatalf("Apply rejection status=%d body=%s, want %d / %s", response.Code, response.Body.String(), tc.wantStatus, tc.wantError)
			}
			if got := model.ListProjectReviewerTargets(db, project.ID); len(got) != 0 {
				t.Fatalf("rejected Apply persisted Reviewer targets: %#v", got)
			}
		})
	}
}

func TestCurrentRoutingEditorExplainsPendingSaveAndSafeApplyErrors(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "routing-pending-guidance", Name: "Routing Pending Guidance"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=routing-pending-guidance&tab=notifications&lang=ja", nil)
	for _, tc := range []struct {
		lang     string
		expected []string
	}{
		{lang: "ja", expected: []string{"変更はまだ保存されていません", "変更を適用", "通知先Webhookを確認できないため、受信者だけの変更は保存できません", "このProductionは読み取り専用", "ユーザーまたはロールを現在の送信先で確認できません", "チーム所属またはDiscord権限を確認できませんでした", "Discord送信先を確認できませんでした"}},
		{lang: "en", expected: []string{"Changes are pending; click Apply below to save.", "This Production is read-only", "The destination webhook could not be verified, so a recipients-only change cannot be saved", "The selected User or Role could not be verified", "Team membership or Discord permissions could not be verified", "The Discord destination could not be verified"}},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			body := renderCurrentIARoutingEditorSetupStyleWithData(db, request, project, tc.lang, "", productionNotificationReviewerView{})
			for _, text := range tc.expected {
				if !strings.Contains(body, text) {
					t.Errorf("%s Apply guidance missing %q", tc.lang, text)
				}
			}
			for _, required := range []string{"safeApplyErrors[code]", "webhook_repair_required:form.dataset.errorWebhookRepairRequired", "await response.json()", "data-pending-instruction", "form.dataset.pendingInstruction"} {
				if !strings.Contains(body, required) {
					t.Errorf("safe Apply feedback flow missing %q", required)
				}
			}
		})
	}
}

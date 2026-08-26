package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

func firstTimeTestOps(createChannel func(string) (string, error)) firstTimeConnectionOps {
	return firstTimeConnectionOps{
		Projects: func(string) []KitsuProject {
			return []KitsuProject{{ID: "p1", Name: "Test Production"}}
		},
		TaskTypes: func(string) []kitsu.TaskType {
			return []kitsu.TaskType{{ID: "tt1", Name: "Concept"}, {ID: "tt2", Name: "Modeling"}}
		},
		DiscordCheck: func(string, string) firstTimeDiscordCheck {
			return firstTimeDiscordCheck{BotValid: true, GuildValid: true, ManageChannels: true, ManageWebhooks: true}
		},
		ListChannels:   func(string, string) ([]DiscordGuildChannel, error) { return nil, nil },
		CreateCategory: func(string, string, string) (string, error) { return "cat1", nil },
		CreateChannel:  func(_, _, name, _ string) (string, error) { return createChannel(name) },
		CreateWebhook:  func(channel, _, _ string) (string, error) { return "https://discord.invalid/" + channel, nil },
		SetPosition:    func(string, int, string) error { return nil },
		DeleteChannel:  func(string, string) error { return nil },
	}
}

func firstTimeTestPlan() TaskTypeChannelPlan {
	types := []kitsu.TaskType{{ID: "tt1", Name: "Concept"}, {ID: "tt2", Name: "Modeling"}}
	return BuildTaskTypeChannelPlanWithOverrides("p1", "g1", types, nil, nil)
}

func firstTimeTestRequest(action string, plan TaskTypeChannelPlan) *http.Request {
	values := url.Values{
		"action":           {action},
		"confirm_plan":     {"yes"},
		"project_id":       {plan.ProductionID},
		"guild_id":         {plan.GuildID},
		"plan_fingerprint": {plan.Fingerprint()},
	}
	for _, entry := range plan.Entries {
		values.Add("included_task_type_id", entry.TaskTypeID)
		values.Set("channel_name_"+entry.TaskTypeID, entry.ChannelName)
		values.Set("channel_order_"+entry.TaskTypeID, string(rune('0'+entry.Order)))
	}
	r := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestFirstTimePrepareAllowsMissingProjectWithoutWrites(t *testing.T) {
	db := newIAViewDB(t)
	previous := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(string) (string, error) { return "channel", nil })
	defer func() { firstTimeOps = previous }()

	plan := firstTimeTestPlan()
	w := httptest.NewRecorder()
	if !handleFirstTimeConnectionAction(w, firstTimeTestRequest("prepare_production_connection", plan), "en", "https://kitsu.invalid/", "bot-token", db) {
		t.Fatal("first-time preparation was not handled")
	}
	body := w.Body.String()
	if !strings.Contains(body, `name="action" value="execute_production_connection"`) || strings.Contains(body, "production_missing") {
		t.Fatal("preparation did not render the execution gate")
	}
	if model.FindProjectByKitsuID(db, "p1") != nil {
		t.Fatal("preparation created a local Production")
	}
}

func TestFirstTimeExecutionUsesWizardShellAndSelectedGuildName(t *testing.T) {
	plan := firstTimeConnectionPlan{
		Project:   KitsuProject{ID: "p1", Name: "Test Production"},
		GuildID:   "g1",
		GuildName: "Test Guild",
		Plan:      firstTimeTestPlan(),
	}
	body := renderWizardFrame("en", 6, renderWizardExecutionCard("en", httptest.NewRequest(http.MethodGet, "/bot/setup?lang=en", nil), plan))
	if !strings.Contains(body, `class="setup-step active"`) || !strings.Contains(body, "Test Guild") {
		t.Fatal("Step 6 did not render the wizard shell or selected guild name")
	}
	if strings.Contains(body, `type="checkbox"`) || strings.Contains(body, "wizard-confirm") {
		t.Fatal("Step 6 duplicated the confirmation UI")
	}
}

func TestFirstTimeCompletionUsesWizardShellAndProductionSummary(t *testing.T) {
	db := newIAViewDB(t)
	plan := firstTimeConnectionPlan{
		Project:   KitsuProject{ID: "p1", Name: "Test Production"},
		GuildID:   "g1",
		GuildName: "Test Guild",
		Plan:      firstTimeTestPlan(),
	}
	body := renderWizardFrame("en", 7, renderWizardComplete("en", httptest.NewRequest(http.MethodGet, "/bot/setup?lang=en", nil), db, plan))
	if !strings.Contains(body, `class="setup-step active"`) || !strings.Contains(body, "Connection setup complete") || !strings.Contains(body, "Test Production") || !strings.Contains(body, "Test Guild") {
		t.Fatal("Step 7 did not render the wizard shell and completion summary")
	}
	if !strings.Contains(body, "Open Production") || strings.Contains(body, "<html") {
		t.Fatal("Step 7 did not render the styled completion CTA")
	}
}

func TestFirstTimeExecuteCreatesProjectAndRoutes(t *testing.T) {
	db := newIAViewDB(t)
	createdChannels := []string{}
	previous := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(name string) (string, error) {
		createdChannels = append(createdChannels, name)
		return "channel-" + name, nil
	})
	defer func() { firstTimeOps = previous }()

	plan := firstTimeTestPlan()
	w := httptest.NewRecorder()
	if !handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", plan), "en", "https://kitsu.invalid/", "bot-token", db) {
		t.Fatal("first-time execution was not handled")
	}
	if !strings.Contains(w.Body.String(), "Connection setup complete") {
		t.Fatal("successful execution did not render completion")
	}
	if model.FindProjectByKitsuID(db, "p1") == nil || len(model.ListProductionChannelMappings(db, "p1")) != 2 || len(model.ListProductionNotificationRoutes(db, "p1")) != 2 {
		t.Fatal("first-time execution did not activate all routes")
	}
	if len(createdChannels) != 2 || createdChannels[0] != "concept" || createdChannels[1] != "modeling" {
		t.Fatalf("unexpected created channels: %#v", createdChannels)
	}
}

func TestFirstTimeSecondExecutionIsRejectedAfterCompletion(t *testing.T) {
	db := newIAViewDB(t)
	previous := firstTimeOps
	categoryCreates := 0
	firstTimeOps = firstTimeTestOps(func(name string) (string, error) { return "channel-" + name, nil })
	firstTimeOps.CreateCategory = func(string, string, string) (string, error) {
		categoryCreates++
		return "cat1", nil
	}
	defer func() { firstTimeOps = previous }()

	plan := firstTimeTestPlan()
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", plan), "en", "https://kitsu.invalid/", "bot-token", db)
		if i == 1 && !strings.Contains(w.Body.String(), "already connected") {
			t.Fatal("second first-time execution was not rejected")
		}
	}
	if categoryCreates != 1 {
		t.Fatalf("duplicate execution created %d categories", categoryCreates)
	}
}

func TestFirstTimeExecuteRejectsStalePlanBeforeWrites(t *testing.T) {
	db := newIAViewDB(t)
	createCount := 0
	previous := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(string) (string, error) {
		createCount++
		return "channel", nil
	})
	defer func() { firstTimeOps = previous }()

	plan := firstTimeTestPlan()
	// Alter only the signed fingerprint; the submitted plan remains otherwise valid.
	values := url.Values{
		"action": {"execute_production_connection"}, "confirm_plan": {"yes"}, "project_id": {"p1"}, "guild_id": {"g1"}, "plan_fingerprint": {"stale"},
	}
	for _, entry := range plan.Entries {
		values.Add("included_task_type_id", entry.TaskTypeID)
		values.Set("channel_name_"+entry.TaskTypeID, entry.ChannelName)
		values.Set("channel_order_"+entry.TaskTypeID, string(rune('0'+entry.Order)))
	}
	r := httptest.NewRequest(http.MethodPost, "/bot/setup", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleFirstTimeConnectionAction(w, r, "en", "https://kitsu.invalid/", "bot-token", db)
	if !strings.Contains(w.Body.String(), "stale") || createCount != 0 || model.FindProjectByKitsuID(db, "p1") != nil {
		t.Fatal("stale plan was not rejected before writes")
	}
}

func TestFirstTimeCategoryFailureLeavesNoLocalState(t *testing.T) {
	db := newIAViewDB(t)
	previous := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(string) (string, error) { return "unused", nil })
	firstTimeOps.CreateCategory = func(string, string, string) (string, error) { return "", errors.New("simulated category failure") }
	defer func() { firstTimeOps = previous }()

	w := httptest.NewRecorder()
	handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", firstTimeTestPlan()), "en", "https://kitsu.invalid/", "bot-token", db)
	assertFirstTimeStateEmpty(t, db, "p1")
}

func TestFirstTimeWebhookFailureRollsBackPersistedMapping(t *testing.T) {
	db := newIAViewDB(t)
	previous := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(name string) (string, error) { return "channel-" + name, nil })
	firstTimeOps.CreateWebhook = func(string, string, string) (string, error) { return "", errors.New("simulated webhook failure") }
	deleted := []string{}
	firstTimeOps.DeleteChannel = func(id, _ string) error { deleted = append(deleted, id); return nil }
	defer func() { firstTimeOps = previous }()

	w := httptest.NewRecorder()
	handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", firstTimeTestPlan()), "en", "https://kitsu.invalid/", "bot-token", db)
	assertFirstTimeStateEmpty(t, db, "p1")
	if len(deleted) != 2 {
		t.Fatalf("expected created channel and category cleanup, got %v", deleted)
	}
}

func TestFirstTimePartialFailureCleansOwnedState(t *testing.T) {
	db := newIAViewDB(t)
	count := 0
	deleted := []string{}
	previous := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(name string) (string, error) {
		count++
		if count == 2 {
			return "", errors.New("simulated channel failure")
		}
		return "channel-" + name, nil
	})
	firstTimeOps.DeleteChannel = func(id, _ string) error {
		deleted = append(deleted, id)
		return nil
	}
	defer func() { firstTimeOps = previous }()

	plan := firstTimeTestPlan()
	w := httptest.NewRecorder()
	handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", plan), "en", "https://kitsu.invalid/", "bot-token", db)
	if strings.Contains(w.Body.String(), "Connection setup complete") {
		t.Fatal("partial failure reported success")
	}
	if model.FindProjectByKitsuID(db, "p1") != nil || len(model.ListProjectWebhooks(db, "p1")) != 0 {
		t.Fatal("failed first-time setup left connection records")
	}
	if len(model.ListProductionChannelMappings(db, "p1")) != 0 || len(model.ListProductionNotificationRoutes(db, "p1")) != 0 || model.FindProductionNotificationConfig(db, "p1") != nil {
		t.Fatal("failed first-time setup left operational state")
	}
	if len(deleted) != 2 {
		t.Fatalf("expected created channel and category cleanup, got %v", deleted)
	}
}

func TestFirstTimeCleanupRemovesOperationalStateAfterActivation(t *testing.T) {
	db := newIAViewDB(t)
	previous := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(name string) (string, error) { return "channel-" + name, nil })
	defer func() { firstTimeOps = previous }()

	plan := firstTimeTestPlan()
	w := httptest.NewRecorder()
	handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", plan), "en", "https://kitsu.invalid/", "bot-token", db)
	if model.FindProjectByKitsuID(db, "p1") == nil || len(model.ListProductionChannelMappings(db, "p1")) != 2 || len(model.ListProductionNotificationRoutes(db, "p1")) != 2 || model.FindProductionNotificationConfig(db, "p1") == nil {
		t.Fatal("setup did not reach the activated state needed for the regression")
	}
	if err := rollbackFirstTimeConnection(db, "bot-token", firstTimeOps, firstTimeOwnedResources{
		ProductionID: "p1", OperationID: "first-time-p1", CreatedProject: true, CreatedCategory: true,
		CategoryID: "cat1", ChannelIDs: []string{"channel-concept", "channel-modeling"},
	}); err != nil {
		t.Fatalf("first-time rollback failed: %v", err)
	}
	if model.FindProjectByKitsuID(db, "p1") != nil || len(model.ListProjectWebhooks(db, "p1")) != 0 || len(model.ListProductionChannelMappings(db, "p1")) != 0 || len(model.ListProductionNotificationRoutes(db, "p1")) != 0 || model.FindProductionNotificationConfig(db, "p1") != nil {
		t.Fatal("cleanup left first-time operational state behind")
	}
}

func TestFirstTimeActivationFailureRollsBackAllOwnedState(t *testing.T) {
	db := newIAViewDB(t)
	previousOps := firstTimeOps
	previousActivate := firstTimeActivateRouting
	firstTimeOps = firstTimeTestOps(func(name string) (string, error) { return "channel-" + name, nil })
	deleted := []string{}
	firstTimeOps.DeleteChannel = func(id, _ string) error { deleted = append(deleted, id); return nil }
	firstTimeActivateRouting = func(*gorm.DB, string, string, []model.ProductionChannelMapping) error {
		return errors.New("simulated activation failure")
	}
	defer func() {
		firstTimeOps = previousOps
		firstTimeActivateRouting = previousActivate
	}()

	plan := firstTimeTestPlan()
	w := httptest.NewRecorder()
	handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", plan), "en", "https://kitsu.invalid/", "bot-token", db)
	if model.FindProjectByKitsuID(db, "p1") != nil || len(model.ListProjectWebhooks(db, "p1")) != 0 || len(model.ListProductionChannelMappings(db, "p1")) != 0 || len(model.ListProductionNotificationRoutes(db, "p1")) != 0 || model.FindProductionNotificationConfig(db, "p1") != nil {
		t.Fatal("activation failure left local setup state behind")
	}
	if len(deleted) != 3 {
		t.Fatalf("expected two channels and one category to be deleted, got %v", deleted)
	}
}

func TestFirstTimeCleanupFailureRetainsRecoverableOwnership(t *testing.T) {
	db := newIAViewDB(t)
	previous := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(name string) (string, error) { return "channel-" + name, nil })
	defer func() { firstTimeOps = previous }()
	w := httptest.NewRecorder()
	handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", firstTimeTestPlan()), "en", "https://kitsu.invalid/", "bot-token", db)
	firstTimeOps.DeleteChannel = func(string, string) error { return errors.New("simulated cleanup failure") }
	if err := rollbackFirstTimeConnection(db, "bot-token", firstTimeOps, firstTimeOwnedResources{ProductionID: "p1", OperationID: "first-time-p1", CreatedProject: true, CreatedCategory: true, CategoryID: "cat1", ChannelIDs: []string{"channel-concept", "channel-modeling"}}); err == nil {
		t.Fatal("expected cleanup failure")
	}
	if model.FindProjectByKitsuID(db, "p1") == nil || len(model.ListProjectWebhooks(db, "p1")) == 0 {
		t.Fatal("cleanup failure did not preserve ownership evidence")
	}
	for _, row := range model.ListProductionChannelMappings(db, "p1") {
		if row.Active || row.State != model.ChannelMappingStateReviewRequired {
			t.Fatalf("mapping was not made recoverable: %#v", row)
		}
	}
	if config := model.FindProductionNotificationConfig(db, "p1"); config == nil || config.Enabled {
		t.Fatal("cleanup failure left notification routing enabled")
	}
}

func assertFirstTimeStateEmpty(t *testing.T, db *gorm.DB, productionID string) {
	t.Helper()
	if model.FindProjectByKitsuID(db, productionID) != nil || len(model.ListProjectWebhooks(db, productionID)) != 0 || len(model.ListProductionChannelMappings(db, productionID)) != 0 || len(model.ListProductionNotificationRoutes(db, productionID)) != 0 || model.FindProductionNotificationConfig(db, productionID) != nil {
		t.Fatalf("first-time state was not cleaned for %s", productionID)
	}
}

func TestExistingManagementStillRequiresConnectedProject(t *testing.T) {
	db := newIAViewDB(t)
	values := url.Values{"action": {"confirm_task_type_channels"}, "confirm_plan": {"yes"}, "project_id": {"missing"}, "guild_id": {"g1"}}
	r := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	if !handleTaskTypeChannelPlanMutation(w, r, "en", "", db) || !strings.Contains(w.Body.String(), "no longer connected") {
		t.Fatal("existing connected-Production guard was bypassed")
	}
}

func TestFirstTimeValidationRejectsInvalidSelectionAndPermissions(t *testing.T) {
	db := newIAViewDB(t)
	previous := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(string) (string, error) { return "channel", nil })
	defer func() { firstTimeOps = previous }()

	plan := firstTimeTestPlan()
	invalid := firstTimeTestRequest("execute_production_connection", plan)
	invalid.FormValue("project_id")
	invalid.Form.Set("included_task_type_id", "unknown")
	if _, err := validateFirstTimeConnectionRequest(invalid, "https://kitsu.invalid/", "bot-token", db); err == nil {
		t.Fatal("invalid Task Type selection was accepted")
	}
	firstTimeOps = firstTimeTestOps(func(string) (string, error) { return "channel", nil })
	firstTimeOps.DiscordCheck = func(string, string) firstTimeDiscordCheck {
		return firstTimeDiscordCheck{BotValid: true, GuildValid: true, Reason: "permission denied"}
	}
	if _, err := validateFirstTimeConnectionRequest(firstTimeTestRequest("execute_production_connection", plan), "https://kitsu.invalid/", "bot-token", db); err == nil || !strings.Contains(err.Error(), "required Discord permissions") {
		t.Fatal("permission failure was not surfaced before execution")
	}
}

func TestFirstTimeValidationBlocksOrphanOperationalStateBeforeRetry(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.ProductionChannelMapping{ProductionID: "p1", GuildID: "g1", TaskTypeID: "tt1", ChannelID: "channel-1", Active: true, State: model.ChannelMappingStateCurrent, MigrationState: model.ChannelMappingStateCurrent})
	previous := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(string) (string, error) { return "unexpected", nil })
	defer func() { firstTimeOps = previous }()

	if _, err := validateFirstTimeConnectionRequest(firstTimeTestRequest("execute_production_connection", firstTimeTestPlan()), "https://kitsu.invalid/", "bot-token", db); err == nil || !strings.Contains(err.Error(), "incomplete setup state") {
		t.Fatalf("orphan state was not blocked before retry: %v", err)
	}
}

func TestFirstTimeCatalogCopyIsNaturalUTF8(t *testing.T) {
	for _, key := range []string{"channel_plan.result_title", "channel_plan.production_missing", "channel_plan.review"} {
		value := tr("ja", key)
		if value == "" || strings.ContainsAny(value, string([]rune{0x7ab6, 0x8b41, 0x7e67, 0xfffd})) {
			t.Fatalf("catalog entry %q is malformed: %q", key, value)
		}
	}
}

func TestFirstTimePermissionErrorListsOnlyMissingPermissions(t *testing.T) {
	db := newIAViewDB(t)
	previous := firstTimeOps
	defer func() { firstTimeOps = previous }()
	for _, tc := range []struct {
		name       string
		channels   bool
		webhooks   bool
		wantJP     []string
		wantEN     []string
		wantAbsent string
	}{
		{name: "channels", channels: false, webhooks: true, wantJP: []string{"Discord Botの権限が不足しています", "チャンネルの管理"}, wantEN: []string{"The Discord Bot is missing required permissions.", "Manage Channels"}, wantAbsent: "Webhookの管理"},
		{name: "webhooks", channels: true, webhooks: false, wantJP: []string{"Webhookの管理"}, wantEN: []string{"Manage Webhooks"}, wantAbsent: "チャンネルの管理"},
		{name: "both", channels: false, webhooks: false, wantJP: []string{"チャンネルの管理", "Webhookの管理"}, wantEN: []string{"Manage Channels", "Manage Webhooks"}, wantAbsent: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			firstTimeOps = firstTimeTestOps(func(string) (string, error) {
				t.Fatal("Discord write started before permission failure")
				return "", nil
			})
			firstTimeOps.DiscordCheck = func(string, string) firstTimeDiscordCheck {
				return firstTimeDiscordCheck{BotValid: true, GuildValid: true, ManageChannels: tc.channels, ManageWebhooks: tc.webhooks}
			}
			plan := firstTimeTestPlan()
			for _, lang := range []string{"ja", "en"} {
				w := httptest.NewRecorder()
				handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", plan), lang, "https://kitsu.invalid/", "bot-token", db)
				body := w.Body.String()
				for _, want := range append(tc.wantJP, tc.wantEN...) {
					if lang == "ja" && !strings.Contains(body, want) && containsAny(tc.wantJP, want) {
						t.Fatalf("JP permission error missing %q", want)
					}
					if lang == "en" && !strings.Contains(body, want) && containsAny(tc.wantEN, want) {
						t.Fatalf("EN permission error missing %q", want)
					}
				}
				if tc.wantAbsent != "" && strings.Contains(body, tc.wantAbsent) {
					t.Fatalf("permission error exposed a permission that was present: %q", tc.wantAbsent)
				}
			}
		})
	}
}

func containsAny(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func TestDiscordAdministratorSatisfiesRequiredPermissions(t *testing.T) {
	permissions := discordPermissionInfoFromBits(1 << 3)
	if !permissions.ManageChannels || !permissions.ManageWebhooks {
		t.Fatal("Administrator should satisfy the required effective permissions")
	}
}

func TestDiscordRolePermissionsAreCombinedWithEveryoneRole(t *testing.T) {
	perms := discordPermissionBitsFromRoles(
		"guild-1",
		[]string{"role-channels", "role-webhooks"},
		map[string]string{
			"guild-1":       "0",
			"role-channels": "16",
			"role-webhooks": "536870912",
		},
		"",
	)
	info := discordPermissionInfoFromBits(perms)
	if !info.ManageChannels || !info.ManageWebhooks {
		t.Fatalf("role permissions were not combined: %#v", info)
	}
}

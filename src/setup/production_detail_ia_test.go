package setup

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

func TestProductionTeamUsesGuildDisplayNameAndFailsClosedWhenGuildReadFails(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "team-display-production", Name: "Team Display", DiscordGuildID: "11111111111111111"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-team-display", KitsuName: "Linked Person", DiscordID: "22222222222222222", DiscordDisplayName: "Stored Discord Name"}).Error; err != nil {
		t.Fatal(err)
	}
	oldTeam, oldMembers := reviewerProductionTeamReader, reviewerGuildMembersForGuild
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) {
		return []kitsu.Person{{ID: "person-team-display", FullName: "Linked Person", Role: "artist"}}, nil
	}
	reviewerGuildMembersForGuild = func(guildID, botToken string) ([]DiscordGuildMember, error) {
		if guildID != project.DiscordGuildID || botToken != "synthetic-token" {
			t.Fatalf("Guild member request used guild/token %q/%q", guildID, botToken)
		}
		var member DiscordGuildMember
		member.User.ID = "22222222222222222"
		member.User.GlobalName = "Global Name"
		member.User.Username = "username"
		member.Nick = "Guild Nick"
		return []DiscordGuildMember{member}, nil
	}
	t.Cleanup(func() { reviewerProductionTeamReader, reviewerGuildMembersForGuild = oldTeam, oldMembers })

	request := httptest.NewRequest("GET", "/bot/admin/projects?project=team-display-production&tab=team&lang=en", nil)
	body := renderCurrentProductionTeam(db, request, project, "en", "synthetic-token")
	if !strings.Contains(body, "@Guild Nick") || strings.Contains(body, "@Stored Discord Name") {
		t.Fatalf("Team did not prefer the live Production Guild nickname: %s", body)
	}

	reviewerGuildMembersForGuild = func(string, string) ([]DiscordGuildMember, error) { return nil, errors.New("synthetic read failure") }
	body = renderCurrentProductionTeam(db, request, project, "en", "synthetic-token")
	if !strings.Contains(body, "Discord membership needs review") || strings.Contains(body, "@Stored Discord Name") {
		t.Fatalf("Team treated an unverified Discord membership as linked: %s", body)
	}
}

func TestProductionTeamCompactRows(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "compact-team-production", Name: "Compact Team"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	longDepartment := "Long Department Name for Composite Visual Effects"
	longScope := longDepartment + ": Comp, Paint, Roto"
	oldTeam, oldTaskTypes := reviewerProductionTeamReader, reviewerTaskTypesForProduction
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) {
		return []kitsu.Person{
			{ID: "supervisor-one", FullName: "Supervisor One", Role: "supervisor", Active: true, Departments: []string{"department-long"}},
			{ID: "supervisor-two", FullName: "Supervisor Two", Role: "supervisor", Active: true, Departments: []string{"department-long"}},
		}, nil
	}
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{
			{ID: "task-comp", Name: "Comp", DepartmentID: "department-long", DepartmentName: longDepartment},
			{ID: "task-roto", Name: "Roto", DepartmentID: "department-long", DepartmentName: longDepartment},
			{ID: "task-paint", Name: "Paint", DepartmentID: "department-long", DepartmentName: longDepartment},
		}
	}
	t.Cleanup(func() { reviewerProductionTeamReader, reviewerTaskTypesForProduction = oldTeam, oldTaskTypes })

	request := httptest.NewRequest("GET", "/bot/admin/projects?project=compact-team-production&tab=team&lang=en", nil)
	body := renderCurrentProductionTeam(db, request, project, "en")
	if strings.Count(body, `class="production-team-row"`) != 2 || !strings.Contains(body, longDepartment) || !strings.Contains(body, longScope) {
		t.Fatalf("Team did not render comparative member rows with Department and derived Supervisor scope: %s", body)
	}
	for _, marker := range []string{`class="production-team-table"`, `<th scope="col">Member</th>`, `<th scope="col">Kitsu role</th>`, `<th scope="col">Department</th>`, `<th scope="col">Discord</th>`, `<th scope="col">Status / action</th>`, `href="/bot/admin/users?lang=en"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("Team is missing the Gemini-derived table/navigation structure %q: %s", marker, body)
		}
	}
	if !strings.Contains(body, `<colgroup><col class="production-team-col-member"><col class="production-team-col-role"><col class="production-team-col-department"><col class="production-team-col-discord"><col class="production-team-col-status">`) {
		t.Fatal("Team table is missing its stable five-column width contract")
	}
	if strings.Contains(body, `class="section-card`) || strings.Contains(body, `class="glass`) {
		t.Fatal("Production Team regressed to per-person cards")
	}
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) { return nil, nil }
	if empty := renderCurrentProductionTeam(db, request, project, "en"); !strings.Contains(empty, "The Kitsu Production Team is empty") {
		t.Fatal("successful empty Team read was not rendered as empty")
	}
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) { return nil, errors.New("synthetic read failure") }
	if failed := renderCurrentProductionTeam(db, request, project, "en"); !strings.Contains(failed, "Could not load the Kitsu Production Team") {
		t.Fatal("failed Team read was not rendered distinctly from empty")
	}
}

func TestProductionTeamCompactRowStyles(t *testing.T) {
	for _, expected := range []string{
		`.production-team-table{width:100%;table-layout:fixed;`,
		`.production-team-col-member{width:24%}`,
		`.production-team-col-status{width:20%}`,
		`.production-notification-col-task{width:26%}`,
		`.production-wfa-kind{display:inline-flex;align-items:center;`,
		`.production-team-row>th,.production-team-row>td{`,
		`.production-team-row:first-child>th,.production-team-row:first-child>td{border-top:0}`,
		`.production-team-table thead{position:absolute;`,
	} {
		if !strings.Contains(adminThemeCSS, expected) {
			t.Errorf("Production Team is missing compact row style %q", expected)
		}
	}
}

func TestProductionNotificationsAndTeamLinkDialogsStayWithinMobileViewport(t *testing.T) {
	for _, rule := range []string{
		`.production-wfa-edit-panel{display:grid;gap:14px;min-width:0;padding:16px;`,
		`.production-routing-editor{padding:0;border:0;border-radius:0;background:transparent;box-shadow:none}`,
		`.production-routing-editor .routing-select-task{display:inline-flex;align-items:center;max-width:100%;min-height:34px;padding:5px 8px;border:1px solid transparent;border-radius:7px;background:transparent;color:var(--text);font:inherit;text-align:start;cursor:pointer;appearance:none}`,
		`.production-routing-editor [data-routing-row].selected{background:rgba(255,255,255,.045);box-shadow:inset 3px 0 0 var(--accent-2)}`,
		`.production-routing-editor .routing-select-task[aria-pressed="true"]{text-decoration:underline;`,
		`.editorial-workbench .production-context dialog,.editorial-workbench .production-team-page dialog{color-scheme:dark;`,
		`.production-wfa-edit-panel dialog select,.production-team-page dialog select{width:100%;min-width:0}`,
		`.editorial-workbench .production-context dialog select,.editorial-workbench .production-team-page dialog select{color-scheme:dark;`,
		`.editorial-workbench .production-context .production-routing-editor .wizard-plan-table [data-routing-row]{display:grid;`,
		`.editorial-workbench .production-context .production-notification-row>td[data-label]::before{`,
	} {
		if !strings.Contains(adminThemeCSS, rule) {
			t.Errorf("Production detail modal/layout is missing constrained style %q", rule)
		}
	}
}

func TestProductionSettingsVerticalSections(t *testing.T) {
	for _, expected := range []string{
		`.editorial-workbench .production-context .production-settings-list{display:grid;grid-template-columns:minmax(0,1fr);gap:0;`,
		`.editorial-workbench .production-context #panel-settings>.production-settings-list>.production-settings-section{border:0;border-top:1px solid var(--divider-color);`,
		`.editorial-workbench .production-context .production-settings-disclosure-row{min-width:0;padding:0;border:0;border-top:1px solid var(--divider-color);`,
		`.editorial-workbench .production-context .production-settings-disclosure-row>summary{min-height:48px;`,
	} {
		if !strings.Contains(adminThemeCSS, expected) {
			t.Errorf("Production Settings is missing its vertical section treatment %q", expected)
		}
	}
}

func TestProductionWFATargetChipsStayCompactAndSummarizeOverflow(t *testing.T) {
	chips := renderProductionWFATargetChips([]string{"Supervisor A", "Supervisor B", "Supervisor C"}, "No Supervisor", "Unavailable", true)
	if !strings.Contains(chips, `class="production-wfa-target-chip">Supervisor A</span>`) || !strings.Contains(chips, `class="production-wfa-more-chip">+1</span>`) || !strings.Contains(chips, `aria-label="Supervisor A, Supervisor B, Supervisor C"`) {
		t.Fatalf("automatic recipients should use compact chips with accessible full summary: %s", chips)
	}
	if got := renderProductionWFATargetChips(nil, "Supervisorなし", "確認不可", true); got != `<span class="production-wfa-state-chip">Supervisorなし</span>` {
		t.Fatalf("empty automatic state should render as a subdued state chip: %s", got)
	}
	if got := renderProductionWFATargetChips(nil, "追加なし", "", false); got != `<span class="production-wfa-state-chip">追加なし</span>` {
		t.Fatalf("empty additional state should render as a subdued state chip: %s", got)
	}
}

func TestSelectedProductionTabNormalizesLegacyDestinations(t *testing.T) {
	for _, tc := range []struct{ legacy, want string }{
		{"", "overview"},
		{"overview", "overview"},
		{"notifications", "notifications"},
		{"users", "team"},
		{"user-settings", "team"},
		{"reviewers", "notifications"},
		{"storage-settings", "settings"},
		{"activity", "overview"},
		{"troubleshooting", "settings"},
		{"advanced", "settings"},
		{"danger-zone", "settings"},
		{"settings", "settings"},
		{"invalid", "overview"},
	} {
		if got := selectedProductionTab(tc.legacy); got != tc.want {
			t.Errorf("selectedProductionTab(%q) = %q, want %q", tc.legacy, got, tc.want)
		}
	}
}

func TestProductionDetailUsesFourSectionsAndMapsLegacyTabs(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "detail-production", Name: "Detail Production", DiscordGuildID: "guild", DiscordCategoryID: "category"}
	db.Create(&project)
	model.WriteAuditLog(db, model.AuditLog{ProjectID: project.KitsuProjectID, ProjectName: project.Name, EntityName: "Storyboard", TaskType: "Storyboard", Success: true, CreatedAt: time.Now()})
	model.WriteAuditLog(db, model.AuditLog{ProjectID: "another-production", ProjectName: project.Name, EntityName: "Must not leak", Success: true, CreatedAt: time.Now()})

	render := func(tab string) string {
		t.Helper()
		path := "/bot/admin/projects?project=detail-production&lang=en"
		if tab != "" {
			path += "&tab=" + tab
		}
		w := httptest.NewRecorder()
		renderIASelectedProduction(w, httptest.NewRequest("GET", path, nil), db, project, "")
		return w.Body.String()
	}

	defaultBody := render("")
	if strings.Count(defaultBody, `role="tab" aria-selected=`) != 4 {
		t.Fatalf("expected four primary Production sections, got %d", strings.Count(defaultBody, `role="tab" aria-selected=`))
	}
	for _, marker := range []string{`id="tab-overview"`, `id="tab-notifications"`, `id="tab-team"`, `id="tab-settings"`, "Overview", "Notifications", "Team", "Settings"} {
		if !strings.Contains(defaultBody, marker) {
			t.Fatalf("default Production detail missing %q", marker)
		}
	}
	for _, forbidden := range []string{`id="tab-users"`, `id="tab-storage-settings"`, `id="tab-activity"`, `id="tab-troubleshooting"`, `id="tab-advanced"`, `id="tab-danger-zone"`, "Selected Production"} {
		if strings.Contains(defaultBody, forbidden) {
			t.Fatalf("default Production detail retained obsolete primary UI %q", forbidden)
		}
	}
	if strings.Contains(defaultBody, "production-summary-grid") || !strings.Contains(defaultBody, `class="status-list production-status-list"`) {
		t.Fatal("Overview should use a compact status list, not a metric-card grid")
	}
	if !strings.Contains(defaultBody, `id="recent-activity"`) || !strings.Contains(defaultBody, "Storyboard") || strings.Contains(defaultBody, "Must not leak") {
		t.Fatal("Overview did not show only safely Production-scoped recent activity")
	}
	if !strings.Contains(defaultBody, "Configuration changed") {
		t.Fatal("recent activity omitted the existing action summary")
	}

	for _, tc := range []struct {
		legacy, tab, section string
		expand               bool
	}{
		{"team", "team", "production-team", false},
		{"notifications", "notifications", "wfa-recipients", false},
		{"users", "team", "production-team", false},
		{"user-settings", "team", "production-team", false},
		{"reviewers", "notifications", "wfa-recipients", false},
		{"storage-settings", "settings", `id="storage"`, false},
		{"activity", "overview", `id="recent-activity"`, false},
		{"troubleshooting", "settings", `id="diagnostics"`, true},
		{"advanced", "settings", `id="technical-details"`, true},
		{"danger-zone", "settings", `id="danger-zone"`, true},
	} {
		body := render(tc.legacy)
		if !strings.Contains(body, `id="panel-`+tc.tab+`"`) {
			t.Errorf("legacy tab %q did not map to %q", tc.legacy, tc.tab)
		}
		if !strings.Contains(body, tc.section) {
			t.Errorf("legacy tab %q did not focus its destination %q", tc.legacy, tc.section)
		}
		if tc.tab == "team" && strings.Contains(body, `class="production-reviewer-manager"`) {
			t.Errorf("Team route %q included WFA recipient controls", tc.legacy)
		}
		if tc.legacy == "activity" && !strings.Contains(body, "panel-overview") {
			t.Error("legacy Activity should fall back to Overview when the activity section is omitted")
		}
		if tc.expand {
			at := strings.Index(body, tc.section)
			if at < 0 {
				t.Errorf("legacy tab %q destination %q is missing", tc.legacy, tc.section)
				continue
			}
			end := strings.Index(body[at:], ">")
			open := strings.Index(body[at:], ` open>`)
			if end < 0 || open < 0 || open > end {
				t.Errorf("legacy tab %q did not open its destination", tc.legacy)
			}
		}
	}
	if body := render("notifications"); strings.Contains(body, `id="tab-reviewers"`) || !strings.Contains(body, `id="wfa-recipients"`) {
		t.Fatal("Notifications did not own WFA recipients without reintroducing a Reviewers tab")
	}
	if body := render("reviewers"); !strings.Contains(body, `id="wfa-recipients" tabindex="-1"`) {
		t.Fatal("legacy Reviewer destination is not keyboard-focusable under Notifications")
	}
}

func TestProductionSettingsGroupsExistingSectionsWithoutChangingForms(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "settings-production", Name: "Settings Production", StorageURL: "https://storage.example"}
	db.Create(&project)
	w := httptest.NewRecorder()
	renderIASelectedProduction(w, httptest.NewRequest("GET", "/bot/admin/projects?project=settings-production&tab=settings&lang=en", nil), db, project, "")
	body := w.Body.String()
	markers := []string{`id="storage"`, `id="technical-details"`, `id="diagnostics"`, `id="danger-zone"`}
	positions := make([]int, len(markers))
	for i, marker := range markers {
		positions[i] = strings.Index(body, marker)
		if positions[i] < 0 {
			t.Fatalf("Settings missing %q", marker)
		}
	}
	for i := 1; i < len(positions); i++ {
		if positions[i] <= positions[i-1] {
			t.Fatalf("Settings sections are out of order: %v", positions)
		}
	}
	if !strings.Contains(body, `class="form-stack drive-storage-form"`) || !strings.Contains(body, `name="storage_url"`) || !strings.Contains(body, "https://storage.example") {
		t.Fatal("Storage form no longer preserves the existing saved value and form contract")
	}
	for _, id := range []string{"technical-details", "diagnostics", "danger-zone"} {
		start := strings.Index(body, `<details id="`+id+`"`)
		if start < 0 {
			t.Fatalf("Settings disclosure %q is missing", id)
		}
		end := strings.Index(body[start:], ">")
		if end < 0 {
			t.Fatalf("Settings disclosure %q has an incomplete opening tag", id)
		}
		if strings.Contains(body[start:start+end], " open") {
			t.Errorf("normal Settings view should keep %q collapsed", id)
		}
	}
	if !strings.Contains(body, `data-drive-save disabled>`) || !strings.Contains(body, `button.disabled=input.value===original`) || !strings.Contains(body, `);sync();`) {
		t.Fatal("Storage Save should start disabled until the value changes, with no-JavaScript submission still available")
	}
	if !strings.Contains(body, `data-require-text="DISCONNECT"`) || !strings.Contains(body, `data-require-text="DELETE"`) {
		t.Fatal("Danger Zone lost its existing destructive confirmations")
	}
}

func TestProductionNotificationsHasWFARecipientsAndNoPreview(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "preview-production", Name: "Preview Production", Language: "en"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	const webhookSecret = "synthetic-webhook-secret-must-not-render"
	if err := model.CreateProjectWebhook(db, p.KitsuProjectID, "compositing", "", webhookSecret, "channel-comp"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, p.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: p.KitsuProjectID, ProductionName: p.Name, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: p.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=preview-production&tab=notifications&lang=en", nil)
	body := renderSelectedProductionNotifications(db, r, p, "en", "success", "Healthy", "")
	for _, expected := range []string{"WFA recipients", "Automatic", "Additional", "Compositing", "#compositing"} {
		if !strings.Contains(body, expected) {
			t.Errorf("Notifications missing %q", expected)
		}
	}
	if strings.Contains(body, "Notification routing") {
		t.Fatal("Notifications read mode should not repeat the tab's routing heading")
	}
	for _, forbidden := range []string{"Notification preview", "Example task", "Please review this task.", `id="notification-preview"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Notifications retained removed preview content %q", forbidden)
		}
	}
	if strings.Contains(body, webhookSecret) || strings.Contains(body, "channel-comp") {
		t.Fatal("Notifications exposed a webhook secret or internal channel ID")
	}
}

func TestProductionNotificationsReadTableSummarizesRecipientsPerStableTaskType(t *testing.T) {
	db := newIAViewDB(t)
	t.Setenv("KitsuJWTToken", "synthetic-kitsu-token")
	t.Setenv("KITSU_API_BASE_URL", "https://kitsu.example.test/api")
	project := model.Project{KitsuProjectID: "notifications-read-table", Name: "Notifications Read Table", DiscordGuildID: "123456789012345678", DiscordCategoryID: "123456789012345679"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ taskID, taskName, channel string }{{"task-comp", "Compositing", "comp"}, {"task-anim", "Animation", "anim"}} {
		if err := model.CreateProjectWebhook(db, project.KitsuProjectID, route.channel, route.taskName, "synthetic-webhook-secret", "channel-"+route.channel); err != nil {
			t.Fatal(err)
		}
		webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[len(model.ListProjectWebhooks(db, project.KitsuProjectID))-1]
		if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, append(model.ListProductionNotificationRoutes(db, project.KitsuProjectID), model.ProductionNotificationRoute{ProductionID: project.KitsuProjectID, TaskTypeID: route.taskID, TaskTypeName: route.taskName, DestinationWebhookID: webhook.ID})); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.UserMap{KitsuID: "artist-person", KitsuName: "Linked Artist", DiscordID: "123456789012345680", DiscordDisplayName: "linked-artist"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, user := range []model.UserMap{
		{KitsuID: "comp-supervisor", KitsuName: "Comp Supervisor", DiscordID: "123456789012345682", DiscordDisplayName: "comp-supervisor"},
		{KitsuID: "anim-supervisor", KitsuName: "Animation Supervisor", DiscordID: "123456789012345683", DiscordDisplayName: "anim-supervisor"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := model.UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", model.ReviewerTargetUser, "123456789012345680"); err != nil {
		t.Fatal(err)
	}
	if err := model.UpsertProjectReviewerTarget(db, project.ID, "task-anim", "Animation", model.ReviewerTargetRole, "123456789012345681"); err != nil {
		t.Fatal(err)
	}
	oldTasks, oldTeam, oldMembers, oldRoles, oldSupervisors := reviewerTaskTypesForProduction, reviewerProductionTeamReader, reviewerGuildMembersForGuild, reviewerDiscordRolesForGuild, reviewerDepartmentSupervisorsForTeam
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "task-comp", Name: "Compositing", DepartmentID: "dept-comp", DepartmentName: "Comp"}, {ID: "task-anim", Name: "Animation", DepartmentID: "dept-anim", DepartmentName: "Animation"}}
	}
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) {
		return []kitsu.Person{{ID: "comp-supervisor", FullName: "Comp Supervisor", Role: "supervisor", Active: true}, {ID: "anim-supervisor", FullName: "Animation Supervisor", Role: "supervisor", Active: true}, {ID: "artist-person", FullName: "Linked Artist", Role: "artist", Active: true}}, nil
	}
	reviewerGuildMembersForGuild = func(string, string) ([]DiscordGuildMember, error) {
		return []DiscordGuildMember{reviewerTestGuildMember("123456789012345680", "linked-artist", "Linked Artist", "Artist Nick"), reviewerTestGuildMember("123456789012345682", "comp-supervisor", "Comp Supervisor", ""), reviewerTestGuildMember("123456789012345683", "anim-supervisor", "Animation Supervisor", "")}, nil
	}
	reviewerDiscordRolesForGuild = func(string, string) ([]DiscordGuildRole, error) {
		return []DiscordGuildRole{{ID: "123456789012345681", Name: "Leads", Mentionable: true}}, nil
	}
	reviewerDepartmentSupervisorsForTeam = func(_, _, departmentID string, _ []kitsu.Person) ([]kitsu.Person, error) {
		if departmentID == "dept-comp" {
			return []kitsu.Person{{ID: "comp-supervisor", FullName: "Comp Supervisor"}}, nil
		}
		if departmentID == "dept-anim" {
			return []kitsu.Person{{ID: "anim-supervisor", FullName: "Animation Supervisor"}}, nil
		}
		return nil, nil
	}
	t.Cleanup(func() {
		reviewerTaskTypesForProduction, reviewerProductionTeamReader, reviewerGuildMembersForGuild, reviewerDiscordRolesForGuild, reviewerDepartmentSupervisorsForTeam = oldTasks, oldTeam, oldMembers, oldRoles, oldSupervisors
	})
	body := renderSelectedProductionNotifications(db, httptest.NewRequest("GET", "/bot/admin/projects?project=notifications-read-table&tab=notifications&lang=en", nil), project, "en", "success", "Healthy", "", "synthetic-discord-token")
	for _, want := range []string{"Kitsu Task Type", "Discord Channel", "WFA recipients", "Comp Supervisor", "Animation Supervisor", "@Artist Nick", "@Leads", "task-comp", "task-anim"} {
		if !strings.Contains(body, want) {
			t.Errorf("Notifications read table missing %q: %s", want, body)
		}
	}
	if got := strings.Count(body, `class="production-wfa-summary-line"`); got != 2 {
		t.Fatalf("each Task Type should show Automatic and Additional in one compact summary row; got %d summary groups: %s", got, body)
	}
	if strings.Contains(body, "No matching Supervisor.") || strings.Contains(body, "該当するSupervisorはいません。") {
		t.Fatal("read table should use a concise no-Supervisor label, not explanatory prose")
	}
	if strings.Contains(body, `name="reviewer_task_type"`) || strings.Contains(body, `class="reviewer-target-form"`) || strings.Contains(body, `action="add_production_reviewer_target"`) {
		t.Fatalf("Notifications read mode exposed a standalone WFA editor: %s", body)
	}
}

func TestProductionNotificationsReadTableLocalizesEmptyAndTeamFailureStates(t *testing.T) {
	for _, tc := range []struct{ lang, empty, teamFailure string }{
		{"en", "No notification routing is configured.", "Unavailable"},
		{"ja", "通知ルーティングはまだ設定されていません。", "利用不可"},
	} {
		db := newIAViewDB(t)
		project := model.Project{KitsuProjectID: "notification-state-" + tc.lang, Name: "Notification State", DiscordGuildID: "123456789012345678"}
		if err := db.Create(&project).Error; err != nil {
			t.Fatal(err)
		}
		empty := renderSelectedProductionNotifications(db, httptest.NewRequest("GET", "/bot/admin/projects?project="+project.KitsuProjectID+"&tab=notifications&lang="+tc.lang, nil), project, tc.lang, "ok", "Healthy", "")
		if !strings.Contains(empty, tc.empty) {
			t.Errorf("%s empty route state missing %q: %s", tc.lang, tc.empty, empty)
		}
		if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "comp", "Compositing", "synthetic-webhook", "channel-comp"); err != nil {
			t.Fatal(err)
		}
		webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
		if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}}); err != nil {
			t.Fatal(err)
		}
		oldTeam, oldMembers, oldRoles := reviewerProductionTeamReader, reviewerGuildMembersForGuild, reviewerDiscordRolesForGuild
		reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) { return nil, errors.New("synthetic team read failure") }
		reviewerGuildMembersForGuild = func(string, string) ([]DiscordGuildMember, error) { return nil, nil }
		reviewerDiscordRolesForGuild = func(string, string) ([]DiscordGuildRole, error) {
			return nil, errors.New("synthetic roles read failure")
		}
		body := renderSelectedProductionNotifications(db, httptest.NewRequest("GET", "/bot/admin/projects?project="+project.KitsuProjectID+"&tab=notifications&lang="+tc.lang, nil), project, tc.lang, "ok", "Healthy", "", "synthetic-discord-token")
		reviewerProductionTeamReader, reviewerGuildMembersForGuild, reviewerDiscordRolesForGuild = oldTeam, oldMembers, oldRoles
		if !strings.Contains(body, tc.teamFailure) {
			t.Errorf("%s Team read failure state missing %q: %s", tc.lang, tc.teamFailure, body)
		}
	}
}

func TestProductionRoutingEditStagesRoutingAndWFAInOneAsyncApply(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "pending-apply-production", Name: "Pending Apply"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "comp", "Compositing", "synthetic-webhook", "channel-comp"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := model.UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", model.ReviewerTargetUser, "123456789012345680"); err != nil {
		t.Fatal(err)
	}
	body := renderCurrentIARoutingEditorSetupStyle(db, httptest.NewRequest("GET", "/bot/admin/projects?project=pending-apply-production&tab=notifications&edit_routing=1&lang=en", nil), project, "en", "")
	for _, required := range []string{"data-async-notification-apply", "expected_revision", "reviewer_changes", "data-wfa-detail-panel", "data-wfa-add-target", "data-routing-remove", "data-routing-undo", "fetch(", "response.status===409", "live.has(id)", "data-stale-message", `data-wfa-channel-control`, `data-wfa-automatic-value`, `production-routing-editor-footer"`} {
		if !strings.Contains(body, required) {
			t.Errorf("unified pending Apply editor missing %q", required)
		}
	}
	if strings.Count(body, `data-async-notification-apply`) != 1 || strings.Contains(body, `action="save_current_production_routing"`) {
		t.Fatalf("Notifications edit must expose one async Apply and no independent route save: %s", body)
	}
	if !strings.Contains(body, `<section class="production-routing-editor"`) || strings.Contains(body, `class="section-card glass production-routing-editor"`) {
		t.Fatal("routing list must stay a flat selector around one contained selected-task editor")
	}
	if strings.Contains(body, `method="post"`) && !strings.Contains(body, `event.preventDefault()`) {
		t.Fatal("Apply form must not submit as a full-page request")
	}
	if !strings.Contains(body, `data-route-channel`) || !strings.Contains(body, `class="btn-ghost production-wfa-add-target" data-wfa-add-target`) {
		t.Fatal("selected Task Type editor must preserve route summary and secondary Add recipient control")
	}
	attribute := `data-existing-targets="`
	start := strings.Index(body, attribute)
	if start < 0 {
		t.Fatal("existing User/Role targets are missing from the selected route")
	}
	start += len(attribute)
	end := strings.IndexByte(body[start:], '"')
	if end < 0 {
		t.Fatal("existing User/Role target attribute is not closed")
	}
	encoded := body[start : start+end]
	if strings.Contains(encoded, `"`) {
		t.Fatal("existing target JSON must be encoded before insertion into an HTML attribute")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !strings.Contains(string(decoded), "123456789012345680") {
		t.Fatalf("encoded existing target could not be safely restored: %s (err=%v)", decoded, err)
	}
}

func TestProductionRoutingEditorProvidesAutomaticSummaryForUnroutedTaskTypes(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "pending-new-task-type", Name: "Pending New Task Type"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, project.KitsuProjectID, "existing", "Existing", "synthetic-webhook", "channel-existing"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, project.KitsuProjectID)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: project.KitsuProjectID, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: project.KitsuProjectID, TaskTypeID: "task-existing", TaskTypeName: "Existing", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	view := productionNotificationReviewerView{
		TaskTypes: []kitsu.TaskType{{ID: "task-existing", Name: "Existing"}, {ID: "task-new", Name: "New Task"}},
		BotToken:  "synthetic-discord-token",
	}
	request := httptest.NewRequest("GET", "/bot/admin/projects?project=pending-new-task-type&tab=notifications&edit_routing=1&lang=en", nil)
	readTable := renderProductionNotificationsReadTableWithData(db, request, project, "en", "success", "Healthy", view)
	body := renderCurrentIARoutingEditorSetupStyleWithData(db, request, project, "en", readTable, view)
	source := strings.Index(body, `data-wfa-pending-source`)
	if source < 0 {
		t.Fatal("pending Task Type must have a WFA summary source before Apply")
	}
	end := strings.Index(body[source:], `</div><p class="field-help routing-destructive-note">`)
	if end < 0 {
		t.Fatal("pending WFA source boundary is missing")
	}
	pendingSource := body[source : source+end]
	newTask := strings.Index(pendingSource, `data-task-type-id="task-new"`)
	if newTask < 0 {
		t.Fatal("unrouted Task Type must have an Automatic summary available")
	}
	if strings.Contains(pendingSource, `data-task-type-id="task-existing"`) {
		t.Fatal("already-routed Task Type WFA summary should not be duplicated in pending source")
	}
	if !strings.Contains(pendingSource[newTask:], "Unavailable") {
		t.Fatal("unrouted Task Type must show a concise truthful Automatic-recipient state")
	}
}

func TestPendingAutomaticReviewerSourceOmitsRoutedTaskTypes(t *testing.T) {
	source := renderPendingAutomaticReviewerSource(
		[]kitsu.TaskType{{ID: "task-routed", Name: "Routed"}, {ID: "task-pending", Name: "Pending"}},
		map[string]bool{"task-routed": true},
		productionNotificationReviewerView{BotToken: "synthetic-discord-token"},
		"en",
	)
	if strings.Contains(source, `data-task-type-id="task-routed"`) {
		t.Fatal("routed Task Type summary must not be duplicated in pending source")
	}
	if !strings.Contains(source, `data-task-type-id="task-pending"`) || !strings.Contains(source, "Unavailable") {
		t.Fatalf("pending Task Type must expose its concise truthful Automatic summary: %s", source)
	}
}

func TestProductionOverviewAndNotificationsSectionHierarchy(t *testing.T) {
	if !strings.Contains(adminThemeCSS, `.production-context .production-tabs{overflow-y:hidden}`) {
		t.Fatal("Production tabs must not show a vertical scrollbar alongside the horizontally scrollable tab row")
	}
	if !strings.Contains(adminThemeCSS, `.production-context #panel-notifications{grid-template-columns:minmax(0,1fr)}`) {
		t.Fatal("Production Notifications grid track must stay within the panel width")
	}
	if !strings.Contains(adminThemeCSS, `.production-context #panel-notifications>.production-notifications{min-width:0}`) {
		t.Fatal("Production Notifications grid item must shrink around its horizontally scrollable routing editor")
	}
	if !strings.Contains(adminThemeCSS, `.production-routing-editor{display:grid;grid-template-columns:minmax(0,1fr);min-width:0;gap:16px}`) {
		t.Fatal("Production routing editor must constrain its grid track so the table can scroll without widening the page")
	}
	for _, rule := range []string{`.production-notification-table table{width:100%;min-width:760px;table-layout:fixed}`, `.production-notification-col-wfa{width:50%}`, `.production-wfa-summary-line{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);`} {
		if !strings.Contains(adminThemeCSS, rule) {
			t.Errorf("Production Notifications is missing sparse-table geometry rule %q", rule)
		}
	}
	for _, rule := range []string{
		`.production-notification-table.production-detail-surface{padding:0;overflow-x:auto}`,
		`.production-notification-table thead th{padding:12px 12px;background:rgba(255,255,255,.045);`,
		`.production-notification-table tbody th,.production-notification-table tbody td{padding:14px 12px;vertical-align:middle;border-bottom:1px solid rgba(255,255,255,.07)}`,
	} {
		if !strings.Contains(adminThemeCSS, rule) {
			t.Errorf("Production Notifications is missing the contained table treatment %q", rule)
		}
	}
	if strings.Contains(adminThemeCSS, `.production-context #panel-notifications>.section-card>.section-card`) {
		t.Fatal("Notifications still targets an obsolete card DOM hierarchy")
	}
}

func TestProductionOverviewPreservesEmptyCurrentIssuesAndActivitySections(t *testing.T) {
	db := newIAViewDB(t)
	project := createHealthyOverviewProject(t, db, "overview-empty-production")
	w := httptest.NewRecorder()
	renderIASelectedProduction(w, httptest.NewRequest("GET", "/bot/admin/projects?project=overview-empty-production&lang=en", nil), db, project, "")
	body := w.Body.String()
	if !strings.Contains(body, `id="recent-activity"`) || !strings.Contains(body, "No recent activity") || strings.Contains(body, "participants") || strings.Contains(body, "参加者") {
		t.Fatal("Overview must retain a compact empty Recent Activity section without inventing a participant metric")
	}
	if !strings.Contains(body, `production-current-issues"`) || !strings.Contains(body, "No current issues.") {
		t.Fatal("healthy Overview must retain Current Issues with a compact empty state")
	}
	for _, marker := range []string{`production-detail-surface production-overview-status`, `production-issue-empty`, `production-detail-surface production-current-issues`, `production-detail-surface production-activity-section`, `production-activity-empty`} {
		if !strings.Contains(body, marker) {
			t.Errorf("sparse Overview must keep structured content inside visual blocks: missing %q", marker)
		}
	}
	if strings.Index(body, `production-overview-status"`) > strings.Index(body, `production-current-issues"`) || strings.Index(body, `production-current-issues"`) > strings.Index(body, `id="recent-activity"`) {
		t.Fatal("Overview status, Current Issues, and Recent Activity sections must remain in canonical order")
	}
}

func TestProductionDetailTabsShareOpenCurrentIAGrammar(t *testing.T) {
	for _, rule := range []string{
		`.editorial-workbench .production-context .production-detail-surface{min-width:0;padding:0;border:0;border-radius:0;background:transparent;box-shadow:none}`,
		`.editorial-workbench .production-context #panel-overview>.production-overview>.production-settings-section,`,
		`#panel-settings>.production-settings-list>.production-settings-section{margin:0;padding:20px 0;border:0;border-top:1px solid var(--divider-color);`,
		`.production-detail-section-title{margin:0 0 10px;font-size:1rem;line-height:1.35;`,
		`.editorial-workbench .production-context .production-detail-state-row{min-height:42px;padding:10px 12px;border:0;border-left:2px solid var(--line);border-radius:0;background:var(--surface-subtle);`,
		`.editorial-workbench .production-context .production-notification-table thead th{padding:0 10px 10px;background:transparent;`,
		`.editorial-workbench .production-context .production-settings-disclosure-row>summary{min-height:48px;padding:0 8px;`,
		`.editorial-workbench .production-context .production-settings-list{display:grid;grid-template-columns:minmax(0,1fr);gap:0;`,
		`.editorial-workbench .production-context .production-team-table-wrap{padding:0;border:0;border-radius:0;background:transparent}`,
		`.editorial-workbench .production-context .production-wfa-edit-panel{padding:14px;border:1px solid var(--border-default);border-radius:var(--radius-md);background:var(--surface-subtle)}`,
	} {
		if !strings.Contains(adminThemeCSS, rule) {
			t.Errorf("Production detail visual grammar is missing %q", rule)
		}
	}

	db := newIAViewDB(t)
	project := createHealthyOverviewProject(t, db, "detail-visual-grammar")
	request := httptest.NewRequest("GET", "/bot/admin/projects?project=detail-visual-grammar&tab=settings&lang=en", nil)
	settings := renderCurrentProductionSettings(db, request, project, "en")
	for _, marker := range []string{`production-settings-section production-detail-surface`, `production-settings-disclosure-row`} {
		if !strings.Contains(settings, marker) {
			t.Errorf("Settings must retain semantic sections and disclosure rows: missing %q", marker)
		}
	}

	routes := renderProductionNotificationsReadTableWithData(db, request, project, "en", "success", "Healthy", productionNotificationReviewerView{})
	if !strings.Contains(routes, `production-notification-table production-detail-surface`) || !strings.Contains(routes, `data-label="Discord Channel"`) || !strings.Contains(routes, `data-label="WFA recipients"`) || strings.Contains(routes, `<div class="production-notifications"><h2>Notifications</h2>`) {
		t.Fatal("Notifications read mode must keep the shared responsive table semantics without a duplicate tab heading")
	}
}

func TestProductionSparseDetailRetainsStableSectionAndTableStructure(t *testing.T) {
	db := newIAViewDB(t)
	project := createHealthyOverviewProject(t, db, "sparse-detail-production")
	for _, lang := range []string{"en", "ja"} {
		body := renderCurrentProductionOverview(db, httptest.NewRequest("GET", "/bot/admin/projects?project=sparse-detail-production&lang="+lang, nil), project, lang, "success", "Connected", "")
		for _, marker := range []string{`production-overview-status"`, `production-current-issues"`, `id="recent-activity"`} {
			if !strings.Contains(body, marker) {
				t.Errorf("%s sparse Overview is missing required block %s", lang, marker)
			}
		}
		if !strings.Contains(body, "No current issues") && lang == "en" || !strings.Contains(body, "問題はありません") && lang == "ja" {
			t.Errorf("%s sparse Overview is missing its compact Current Issues empty state", lang)
		}
		if !strings.Contains(body, "No recent activity") && lang == "en" || !strings.Contains(body, "最近のアクティビティはありません") && lang == "ja" {
			t.Errorf("%s sparse Overview is missing its compact Recent Activity empty state", lang)
		}
	}
}

func TestProductionOverviewAggregatesRepeatedIssuesAndCapsVisibleRows(t *testing.T) {
	db := newIAViewDB(t)
	project := createHealthyOverviewProject(t, db, "overview-issue-cap")
	baseTime := time.Now()
	for i, detail := range []string{"Cause E", "Cause D", "Cause C", "Cause B", "Repeated cause", "Repeated cause", "Repeated cause"} {
		model.RecordNotificationRoutingDiagnosis(db, model.NotificationRoutingDiagnosis{ProductionID: project.KitsuProjectID, Reason: "notification skipped", Detail: detail, CreatedAt: baseTime.Add(time.Duration(i) * time.Second)})
	}
	body := renderCurrentProductionOverview(db, httptest.NewRequest("GET", "/bot/admin/projects?project=overview-issue-cap&lang=en", nil), project, "en", "success", "Connected", "")
	if got := strings.Count(body, `class="production-issue-row"`); got != 3 {
		t.Fatalf("Current Issues should render at most three aggregated rows, got %d: %s", got, body)
	}
	if !strings.Contains(body, "Repeated cause") || !strings.Contains(body, "3 occurrences") {
		t.Fatalf("repeated issue cause was not aggregated: %s", body)
	}
	if !strings.Contains(body, "2 other issues") || strings.Contains(body, "Cause D") || strings.Contains(body, "Cause E") {
		t.Fatalf("Current Issues did not summarize overflow with localized other-count copy: %s", body)
	}
	if !strings.Contains(body, `project=overview-issue-cap&amp;tab=notifications`) {
		t.Fatalf("routing issue CTA should stay in-window and point to Notifications: %s", body)
	}
	if strings.Contains(body, `target="_blank"`) {
		t.Fatal("Overview issue actions must stay in the current window")
	}
	ja := renderCurrentProductionOverview(db, httptest.NewRequest("GET", "/bot/admin/projects?project=overview-issue-cap&lang=ja", nil), project, "ja", "success", "Connected", "")
	if !strings.Contains(ja, "他 2 件") {
		t.Fatalf("Japanese overflow summary is missing equivalent copy: %s", ja)
	}
}

func TestProductionOverviewRoutesDiscordResourceIssueToSettings(t *testing.T) {
	db := newIAViewDB(t)
	project := createHealthyOverviewProject(t, db, "overview-discord-resource")
	project.DiscordGuildID = ""
	body := renderCurrentProductionOverview(db, httptest.NewRequest("GET", "/bot/admin/projects?project=overview-discord-resource&lang=ja", nil), project, "ja", "success", "Connected", "")
	if !strings.Contains(body, `project=overview-discord-resource&amp;tab=settings`) || strings.Contains(body, `href="/bot/setup`) {
		t.Fatalf("Discord resource issue should point to this Production's Settings without inventing a setup destination: %s", body)
	}
}

func TestProductionOverviewDoesNotClassifyUnlinkedArtistAsIssue(t *testing.T) {
	db := newIAViewDB(t)
	project := createHealthyOverviewProject(t, db, "overview-unlinked-artist")
	oldTeam, oldTaskTypes := reviewerProductionTeamReader, reviewerTaskTypesForProduction
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) {
		return []kitsu.Person{{ID: "ordinary-artist", FullName: "Ordinary Artist", Role: "artist"}}, nil
	}
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType { return nil }
	t.Cleanup(func() { reviewerProductionTeamReader, reviewerTaskTypesForProduction = oldTeam, oldTaskTypes })
	body := renderCurrentProductionOverview(db, httptest.NewRequest("GET", "/bot/admin/projects?project=overview-unlinked-artist&lang=en", nil), project, "en", "success", "Connected", "")
	if strings.Contains(body, "Ordinary Artist") || strings.Contains(body, "User Linking") || strings.Contains(body, "ユーザー紐づけ") || !strings.Contains(body, `production-current-issues"`) || !strings.Contains(body, "No current issues.") {
		t.Fatalf("ordinary unlinked Artist became an Overview issue: %s", body)
	}
}

func TestProductionOverviewShowsOnlyFiveNewestExactProductionActivities(t *testing.T) {
	db := newIAViewDB(t)
	project := createHealthyOverviewProject(t, db, "overview-five-activity")
	for i := 0; i < 7; i++ {
		model.WriteAuditLog(db, model.AuditLog{ProjectID: project.KitsuProjectID, ProjectName: project.Name, EntityName: fmt.Sprintf("Activity %d", i), Success: true, CreatedAt: time.Now().Add(-time.Duration(i) * 24 * time.Hour)})
	}
	model.WriteAuditLog(db, model.AuditLog{ProjectID: "another-production", ProjectName: project.Name, EntityName: "Other Production Activity", Success: true, CreatedAt: time.Now()})
	body := renderCurrentProductionOverview(db, httptest.NewRequest("GET", "/bot/admin/projects?project=overview-five-activity&lang=en", nil), project, "en", "success", "Connected", "")
	if got := strings.Count(body, `class="activity-row"`); got != 5 {
		t.Fatalf("Recent Activity rows = %d, want 5", got)
	}
	if strings.Index(body, "Activity 0") > strings.Index(body, "Activity 4") || strings.Contains(body, "Activity 5") || strings.Contains(body, "Activity 6") || strings.Contains(body, "Other Production Activity") {
		t.Fatalf("Recent Activity order/scope/limit is incorrect: %s", body)
	}
}

func TestProductionOverviewExcludesSuccessfulNotificationAndShowsFailure(t *testing.T) {
	db := newIAViewDB(t)
	project := createHealthyOverviewProject(t, db, "overview-activity-filter")
	model.WriteAuditLog(db, model.AuditLog{ProjectID: project.KitsuProjectID, TaskID: "successful-task", EntityName: "Successful notification task", DiscordMsgID: "message-id", Success: true, CreatedAt: time.Now()})
	model.WriteAuditLog(db, model.AuditLog{ProjectID: project.KitsuProjectID, TaskID: "failed-task", EntityName: "Failed notification task", ErrorMessage: "delivery unavailable", Success: false, CreatedAt: time.Now().Add(-time.Second)})
	body := renderCurrentProductionOverview(db, httptest.NewRequest("GET", "/bot/admin/projects?project=overview-activity-filter&lang=en", nil), project, "en", "success", "Connected", "")
	if strings.Contains(body, "Successful notification task") || strings.Count(body, `class="activity-row"`) != 1 || !strings.Contains(body, "Failed notification task") {
		t.Fatalf("Recent Activity should omit successful notification records and keep real failures: %s", body)
	}
}

func createHealthyOverviewProject(t *testing.T, db *gorm.DB, id string) model.Project {
	t.Helper()
	project := model.Project{KitsuProjectID: id, Name: "Healthy Overview", DiscordGuildID: "synthetic-guild"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, id, "channel", "concept", "synthetic-webhook-secret", "synthetic-channel"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, id)[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: id, ProductionName: project.Name, Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: id, TaskTypeID: "concept", TaskTypeName: "Concept", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	return project
}

func TestProductionOverviewDoesNotCallUnconfiguredRoutingHealthy(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "overview-unconfigured-production", Name: "Overview Unconfigured", DiscordGuildID: "synthetic-guild"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	statusClass, statusLabel, statusHint := iaStatus(db, project, "en")
	body := renderCurrentProductionOverview(db, httptest.NewRequest("GET", "/bot/admin/projects?project=overview-unconfigured-production&lang=en", nil), project, "en", statusClass, statusLabel, statusHint)
	if strings.Contains(body, "No current issues") || !strings.Contains(body, "No notification route is configured.") {
		t.Fatal("Overview reported no issues while notification routing is unconfigured")
	}
	if !strings.Contains(body, "tab=notifications") {
		t.Fatal("Overview omitted the actionable route to notification settings")
	}
}

func TestProductionDiagnosticsAuditCountsAreProductionScoped(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "diagnostics-scoped", Name: "Shared Production Name"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	model.WriteAuditLog(db, model.AuditLog{ProjectID: project.KitsuProjectID, ProjectName: project.Name, Success: true, CreatedAt: time.Now()})
	model.WriteAuditLog(db, model.AuditLog{ProjectID: "other-production", ProjectName: project.Name, Success: false, CreatedAt: time.Now()})
	body := renderCurrentProductionTroubleshooting(db, project, "en", false)
	if !strings.Contains(body, "1 recent records, 0 failures.") {
		t.Fatalf("Diagnostics included same-name audit records from another Production: %s", body)
	}
}

func TestLegacyActivityDeepLinkFocusesEmptyRecentActivity(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "empty-activity-project", Name: "Empty Activity"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/bot/admin/projects?project=empty-activity-project&tab=activity&lang=en", nil)
	renderIASelectedProduction(w, request, db, project, "")
	body := w.Body.String()
	if !strings.Contains(body, `id="recent-activity"`) || !strings.Contains(body, `id="panel-overview"`) || !strings.Contains(body, `getElementById('recent-activity')||document.getElementById('panel-overview')`) {
		t.Fatal("Activity deep link should focus the retained empty Recent Activity section")
	}
}

func TestProductionTabsHaveEquivalentJapaneseLabels(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "jp-tabs-production", Name: "JP Tabs"}
	db.Create(&project)
	w := httptest.NewRecorder()
	renderIASelectedProduction(w, httptest.NewRequest("GET", "/bot/admin/projects?project=jp-tabs-production&lang=ja", nil), db, project, "")
	body := w.Body.String()
	if strings.Count(body, `role="tab" aria-selected=`) != 4 {
		t.Fatalf("Japanese Production detail should have four primary sections")
	}
	for _, label := range []string{"概要", "通知", "チーム", "設定"} {
		if !strings.Contains(body, ">"+label+"</a>") {
			t.Errorf("Japanese Production navigation missing %q", label)
		}
	}
}

func TestProductionHeaderAndKitsuTabs(t *testing.T) {
	request := httptest.NewRequest("GET", "/bot/admin/projects?project=header-production&tab=overview&lang=en", nil)
	project := model.Project{KitsuProjectID: "header-production", Name: "Header Production"}
	body := adminPage("en", "", request, renderProductionContext(project, "en", request, "overview", "success", "Connected"))

	for _, expected := range []string{`class="production-identity"`, `class="eyebrow">Production</div>`, `<h1>Header Production</h1>`, `role="status">Connected</span>`, `class="section-nav production-tabs"`} {
		if !strings.Contains(body, expected) {
			t.Errorf("Production identity header is missing %q", expected)
		}
	}
	if got := strings.Count(body, `role="tab" aria-selected=`); got != 4 {
		t.Fatalf("Production detail should render four primary tabs, got %d", got)
	}
	for _, label := range []string{"Overview", "Notifications", "Team", "Settings"} {
		if !strings.Contains(body, ">"+label+"</a>") {
			t.Errorf("Production tabs are missing %q", label)
		}
	}
	for _, expected := range []string{`.production-context .production-tabs .section-link{`, `background:transparent`, `border-radius:0`, `.production-context .production-tabs .section-link.active::after{`, `bottom:-7px;height:2px;background:var(--accent-2)`} {
		if !strings.Contains(body, expected) {
			t.Errorf("Kitsu tab treatment is missing %q", expected)
		}
	}
}

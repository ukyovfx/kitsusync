package setup

import (
	"errors"
	"reflect"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func reviewerResolutionTestDB(t *testing.T) (*gorm.DB, model.Project) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.UserMap{}, &model.ProjectUserMap{}, &model.ProjectReviewerTarget{}, &model.ProjectWebhook{}, &model.ProductionNotificationRoute{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationWriteLock{}); err != nil {
		t.Fatal(err)
	}
	project := model.Project{KitsuProjectID: "production-1", DiscordGuildID: "123456789012345600"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	return db, project
}

func reviewerTestGuildMember(id, username, globalName, nickname string) DiscordGuildMember {
	member := DiscordGuildMember{Nick: nickname}
	member.User.ID = id
	member.User.Username = username
	member.User.GlobalName = globalName
	return member
}

func reviewerTestRoute(t *testing.T, db *gorm.DB, project model.Project, taskTypeID string) string {
	t.Helper()
	webhookID := "123456789012345607"
	webhookURL := "https://discord.com/api/webhooks/" + webhookID + "/synthetic-token"
	webhook := model.ProjectWebhook{KitsuProjectID: project.KitsuProjectID, TaskType: taskTypeID, ChannelName: "review", WebhookURL: webhookURL, DiscordChannelID: "123456789012345608"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	route := model.ProductionNotificationRoute{ProductionID: project.KitsuProjectID, TaskTypeID: taskTypeID, TaskTypeName: "Compositing", DestinationWebhookID: webhook.ID}
	if err := db.Create(&route).Error; err != nil {
		t.Fatal(err)
	}
	return webhookURL
}

func installReviewerResolutionReaders(t *testing.T, resolution kitsu.ProjectTaskTypeSupervisorResolution, resolutionErr error, members []DiscordGuildMember, memberErr error, roles []DiscordGuildRole, roleErr error) {
	t.Helper()
	if resolutionErr == nil && resolution.TaskType.ID == "" {
		resolution.TaskType = kitsu.TaskType{ID: "task-1", DepartmentID: "department-1"}
	}
	oldResolution := reviewerProductionResolutionReader
	oldMembers := reviewerGuildMembersReader
	oldRoles := reviewerGuildRolesReader
	oldEvidence := reviewerRoleSendEvidenceReader
	reviewerProductionResolutionReader = func(_, _, _, _ string) (kitsu.ProjectTaskTypeSupervisorResolution, error) {
		return resolution, resolutionErr
	}
	reviewerGuildMembersReader = func(_, _ string) ([]DiscordGuildMember, error) { return members, memberErr }
	reviewerGuildRolesReader = func(_, _ string) ([]DiscordGuildRole, error) { return roles, roleErr }
	reviewerRoleSendEvidenceReader = func(guildID, channelID, webhookURL, _ string) (reviewerRoleSendEvidence, error) {
		webhookID, err := discordWebhookID(webhookURL)
		if err != nil {
			return reviewerRoleSendEvidence{}, err
		}
		return reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, WebhookID: webhookID, BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true, PermissionsKnown: true}, nil
	}
	t.Cleanup(func() {
		reviewerProductionResolutionReader = oldResolution
		reviewerGuildMembersReader = oldMembers
		reviewerGuildRolesReader = oldRoles
		reviewerRoleSendEvidenceReader = oldEvidence
	})
}

func TestResolveProductionWFAReviewerTargetsUnionsCurrentAutomaticAndOverrides(t *testing.T) {
	db, project := reviewerResolutionTestDB(t)
	senderURL := reviewerTestRoute(t, db, project, "task-1")
	team := []kitsu.Person{
		{ID: "person-auto", Active: true, Role: "artist", ProjectRole: "supervisor", FirstName: "Auto"},
		{ID: "person-override", Active: true, Role: "artist", ProjectRole: "artist", FirstName: "Override"},
		{ID: "person-stale-guild", Active: true, Role: "artist", ProjectRole: "artist", FirstName: "Stale"},
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-auto", KitsuName: "Auto", DiscordID: "123456789012345601"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-override", KitsuName: "Override", DiscordID: "123456789012345602"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-stale-guild", KitsuName: "Stale", DiscordID: "123456789012345603"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, target := range []model.ProjectReviewerTarget{
		{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetUser, DiscordID: "123456789012345602"},
		{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetUser, DiscordID: "123456789012345603"},
		{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetRole, DiscordID: "123456789012345604"},
		{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetRole, DiscordID: project.DiscordGuildID},
		{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetRole, DiscordID: "123456789012345605"},
	} {
		if err := db.Create(&target).Error; err != nil {
			t.Fatal(err)
		}
	}
	resolution := kitsu.ProjectTaskTypeSupervisorResolution{Team: team, Supervisors: []kitsu.Person{{ID: "person-auto", Departments: []string{"department-1"}}}}
	members := []DiscordGuildMember{
		reviewerTestGuildMember("123456789012345601", "", "", ""),
		reviewerTestGuildMember("123456789012345602", "", "", ""),
	}
	installReviewerResolutionReaders(t, resolution, nil, members, nil, []DiscordGuildRole{
		{ID: "123456789012345604", Name: "Reviewers", Mentionable: true},
		{ID: "123456789012345605", Name: "Quiet", Mentionable: false},
	}, nil)
	got, err := ResolveProductionWFAReviewerTargets(db, "https://kitsu.example.test", "kitsu-token", "discord-token", project.KitsuProjectID, "task-1", senderURL)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.ProjectReviewerTarget{
		{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetRole, DiscordID: "123456789012345604"},
		{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetUser, DiscordID: "123456789012345601"},
		{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetUser, DiscordID: "123456789012345602"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targets=%+v want=%+v", got, want)
	}
}

func TestResolveProductionWFAReviewerTargetsAllowsNonmentionableRoleOnlyForCurrentVerifiedSender(t *testing.T) {
	db, project := reviewerResolutionTestDB(t)
	senderURL := reviewerTestRoute(t, db, project, "task-1")
	if err := db.Create(&model.ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetRole, DiscordID: "123456789012345604"}).Error; err != nil {
		t.Fatal(err)
	}
	installReviewerResolutionReaders(t, kitsu.ProjectTaskTypeSupervisorResolution{}, nil, nil, nil, []DiscordGuildRole{{ID: "123456789012345604", Name: "Quiet", Mentionable: false}}, nil)
	oldEvidence := reviewerRoleSendEvidenceReader
	canMentionRoles := true
	reviewerRoleSendEvidenceReader = func(guildID, channelID, webhookURL, _ string) (reviewerRoleSendEvidence, error) {
		webhookID, _ := discordWebhookID(webhookURL)
		return reviewerRoleSendEvidence{GuildID: guildID, ChannelID: channelID, WebhookID: webhookID, BotID: "123456789012345603", WebhookOwnerID: "123456789012345603", OwnershipVerified: true, PermissionsKnown: true, CanMentionRoles: canMentionRoles}, nil
	}
	t.Cleanup(func() { reviewerRoleSendEvidenceReader = oldEvidence })
	got, err := ResolveProductionWFAReviewerTargets(db, "https://kitsu.example.test", "kitsu-token", "discord-token", project.KitsuProjectID, "task-1", senderURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TargetKind != model.ReviewerTargetRole || got[0].DiscordID != "123456789012345604" {
		t.Fatalf("verified non-mentionable Role did not resolve: %+v", got)
	}
	canMentionRoles = false
	got, err = ResolveProductionWFAReviewerTargets(db, "https://kitsu.example.test", "kitsu-token", "discord-token", project.KitsuProjectID, "task-1", senderURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Role that became ineligible after a permission change was still resolved: %+v", got)
	}
	if _, err := ResolveProductionWFAReviewerTargets(db, "https://kitsu.example.test", "kitsu-token", "discord-token", project.KitsuProjectID, "task-1", "https://discord.com/api/webhooks/123456789012345699/not-the-route"); err == nil {
		t.Fatal("sender URL that does not match the current route must fail closed")
	}
}

func TestResolveProductionWFAReviewerTargetsRequiresGlobalLinkAndCurrentGuildMembership(t *testing.T) {
	db, project := reviewerResolutionTestDB(t)
	if err := db.Create(&model.ProjectUserMap{ProjectID: project.ID, KitsuName: "Legacy", DiscordUserID: "123456789012345601"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "person-not-member", KitsuName: "Stale Guild", DiscordID: "123456789012345602"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetUser, DiscordID: "123456789012345601"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetUser, DiscordID: "123456789012345602"}).Error; err != nil {
		t.Fatal(err)
	}
	team := []kitsu.Person{{ID: "person-legacy", Active: true, FirstName: "Legacy"}}
	resolution := kitsu.ProjectTaskTypeSupervisorResolution{Team: team}
	members := []DiscordGuildMember{
		reviewerTestGuildMember("123456789012345601", "", "", ""),
		reviewerTestGuildMember("123456789012345602", "", "", ""),
	}
	installReviewerResolutionReaders(t, resolution, nil, members, nil, nil, nil)
	got, err := ResolveProductionWFAReviewerTargets(db, "https://kitsu.example.test", "kitsu-token", "discord-token", project.KitsuProjectID, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("legacy project mapping or stale Guild user was accepted: %+v", got)
	}
}

func TestDepartmentlessTaskTypeKeepsOnlyValidatedExplicitReviewerTargets(t *testing.T) {
	db, project := reviewerResolutionTestDB(t)
	senderURL := reviewerTestRoute(t, db, project, "task-1")
	if err := db.Create(&model.ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetRole, DiscordID: "123456789012345604"}).Error; err != nil {
		t.Fatal(err)
	}
	resolution := kitsu.ProjectTaskTypeSupervisorResolution{
		TaskType: kitsu.TaskType{ID: "task-1"},
		Team:     []kitsu.Person{{ID: "person", Active: true}},
		Supervisors: []kitsu.Person{{
			ID: "person", Departments: []string{"department-1"},
		}},
	}
	members := []DiscordGuildMember{reviewerTestGuildMember("123456789012345601", "", "", "")}
	installReviewerResolutionReaders(t, resolution, nil, members, nil, []DiscordGuildRole{{ID: "123456789012345604", Name: "Reviewers", Mentionable: true}}, nil)
	got, err := ResolveProductionWFAReviewerTargets(db, "https://kitsu.example.test", "kitsu-token", "discord-token", project.KitsuProjectID, "task-1", senderURL)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.ProjectReviewerTarget{{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetRole, DiscordID: "123456789012345604"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Departmentless Task Type targets=%+v want only explicit Role %+v", got, want)
	}
}

func TestResolveProductionWFAReviewerTargetsFailsClosedOnLookupErrors(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		resolutionErr, memberErr, roleErr error
	}{
		{name: "Kitsu", resolutionErr: errors.New("Kitsu unavailable")},
		{name: "Guild members", memberErr: errors.New("Discord members unavailable")},
		{name: "Guild roles", roleErr: errors.New("Discord roles unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, project := reviewerResolutionTestDB(t)
			if tc.roleErr != nil {
				senderURL := reviewerTestRoute(t, db, project, "task-1")
				if err := db.Create(&model.ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-1", TargetKind: model.ReviewerTargetRole, DiscordID: "123456789012345604"}).Error; err != nil {
					t.Fatal(err)
				}
				installReviewerResolutionReaders(t, kitsu.ProjectTaskTypeSupervisorResolution{}, tc.resolutionErr, nil, tc.memberErr, []DiscordGuildRole{{ID: "123456789012345604", Name: "Reviewers", Mentionable: true}}, tc.roleErr)
				got, err := ResolveProductionWFAReviewerTargets(db, "https://kitsu.example.test", "kitsu-token", "discord-token", project.KitsuProjectID, "task-1", senderURL)
				if err == nil || len(got) != 0 {
					t.Fatalf("lookup error did not fail closed: targets=%+v err=%v", got, err)
				}
				return
			}
			installReviewerResolutionReaders(t, kitsu.ProjectTaskTypeSupervisorResolution{}, tc.resolutionErr, nil, tc.memberErr, nil, tc.roleErr)
			got, err := ResolveProductionWFAReviewerTargets(db, "https://kitsu.example.test", "kitsu-token", "discord-token", project.KitsuProjectID, "task-1")
			if err == nil || len(got) != 0 {
				t.Fatalf("lookup error did not fail closed: targets=%+v err=%v", got, err)
			}
		})
	}
}

package main

import (
	"testing"
	"time"

	"app/src/api/discord"
	"app/src/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// handoffNotifyDB opens an isolated database with the handoff tables and one
// Production whose only admin maps to a Kitsu person.
func handoffNotifyDB(t *testing.T, timing string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.Setting{}, &model.UserMap{}, &model.AuditLog{}, &model.ProjectReviewerTarget{}, &model.ProductionForumConfig{}, &model.ProductionForumRoute{}, &model.ProductionForumStatusRole{}, &model.ProductionForumTag{}, &model.ForumBaseline{}, &model.ForumTask{}, &model.ForumTransition{}, &model.ForumOutbox{}, &model.ForumFlapState{}, &model.ProductionHandoffConfig{}, &model.ProductionHandoffProductType{}, &model.ProductionHandoffRoute{}, &model.HandoffEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p1", "Production", "", "guild-1", "category-1", "ja"); err != nil {
		t.Fatal(err)
	}
	// The digest only serves a Production with forum settings.
	if err := model.SaveProductionForumSettings(db, model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "p1", GuildID: "guild-1"},
		Routes: []model.ProductionForumRoute{{ProductionID: "p1", TaskTypeID: "tt-comp", TaskTypeName: "Compositing", ForumChannelID: "forum-1", ForumChannelName: "Compositing"}},
		StatusRoles: []model.ProductionForumStatusRole{
			{ProductionID: "p1", StatusID: "s-wfa", StatusName: "WFA", Role: model.ForumRoleWFA},
			{ProductionID: "p1", StatusID: "s-retake", StatusName: "Retake", Role: model.ForumRoleRetake},
			{ProductionID: "p1", StatusID: "s-done", StatusName: "Done", Role: model.ForumRoleDone},
		},
		Tags: []model.ProductionForumTag{
			{ProductionID: "p1", ForumChannelID: "forum-1", Role: model.ForumRoleWFA, TagID: "tag-wfa"},
			{ProductionID: "p1", ForumChannelID: "forum-1", Role: model.ForumRoleRetake, TagID: "tag-retake"},
			{ProductionID: "p1", ForumChannelID: "forum-1", Role: model.ForumRoleDone, TagID: "tag-done"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	seedHandoffTiming(t, db, timing)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func seedHandoffTiming(t *testing.T, db *gorm.DB, timing string) {
	t.Helper()
	settings := model.ProductionHandoffSettings{
		Config:       model.ProductionHandoffConfig{ProductionID: "p1", NotifyTiming: timing, NoAssignee: model.HandoffFallbackAdmin},
		ProductTypes: []model.ProductionHandoffProductType{{ProductionID: "p1", Name: "matchmove"}},
		Routes: []model.ProductionHandoffRoute{{
			ProductionID: "p1", TaskTypeID: "tt-lay", TaskTypeName: "Layout", ProductType: "matchmove",
			Flow: model.HandoffFlowHand, NotifyTaskTypeIDs: "tt-comp", NotifyTaskTypeName: "Compositing",
		}},
	}
	if err := model.SaveProductionHandoffSettings(db, settings); err != nil {
		t.Fatal(err)
	}
}

// handoffEvent builds one pending event; personIDs empty plus fallback "admin"
// is the no-assignee handoff K2 records.
func handoffEvent(t *testing.T, db *gorm.DB, commentID, fallback string, personIDs ...string) model.HandoffEvent {
	t.Helper()
	event := model.HandoffEvent{
		CommentID:          commentID,
		ProductionID:       "p1",
		SourceTaskID:       "task-lay-1",
		SourceTaskTypeID:   "tt-lay",
		SourceTaskTypeName: "Layout",
		SourceAuthorID:     "person-author",
		SourceAuthorName:   "Layout Artist",
		EntityID:           "shot-1",
		EntityKind:         model.ForumEntityKindShot,
		EntityName:         "sq010",
		ProductType:        "matchmove",
		ProductVersion:     "v003",
		ProductName:        "camera",
		NotifyTaskID:       "task-comp-1",
		NotifyTaskTypeID:   "tt-comp",
		NotifyTaskTypeName: "Compositing",
		NotifyPersonIDs:    model.JoinHandoffNotifyIDs(personIDs),
		FallbackUsed:       fallback,
	}
	if _, err := model.RecordHandoffEvent(db, event); err != nil {
		t.Fatal(err)
	}
	return event
}

// stubHandoffIdentity maps the test Kitsu persons to Discord users.
func stubHandoffIdentity(t *testing.T, mapping map[string]string) {
	t.Helper()
	old := discord.UserMapResolver
	discord.UserMapResolver = func(projectID, kitsuPersonID, kitsuName, kitsuEmail string) string {
		return mapping[kitsuPersonID]
	}
	t.Cleanup(func() { discord.UserMapResolver = old })
}

func handoffState(t *testing.T, db *gorm.DB, commentID string) string {
	t.Helper()
	event, err := model.FindHandoffEvent(db, commentID)
	if err != nil {
		t.Fatal(err)
	}
	return event.State
}

// A handoff whose receiving task has an assignee is sent immediately and is
// listed again in the morning digest while nobody has answered it.
func TestHandoffAssignedIsSentImmediatelyOnce(t *testing.T) {
	db := handoffNotifyDB(t, model.HandoffTimingMixed)
	fixture := newDMFixture(t)
	stubHandoffIdentity(t, map[string]string{"person-comp": "222222222222222222"})
	handoffEvent(t, db, "comment-1", model.HandoffFallbackNone, "person-comp")

	sent, err := sendImmediateHandoffs(digestTestConfig(), db)
	if err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	if fixture.sendsFor("222222222222222222") != 1 {
		t.Fatalf("sends = %d", fixture.sendsFor("222222222222222222"))
	}
	if state := handoffState(t, db, "comment-1"); state != model.HandoffEventNotified {
		t.Fatalf("state = %q", state)
	}
	// The next morning the same unanswered handoff is listed again.
	if _, err := sendForumMorningDigests(digestTestConfig(), db, time.Date(2026, 11, 17, 9, 0, 0, 0, jst), "2026-11-17"); err != nil {
		t.Fatal(err)
	}
	if fixture.sendsFor("222222222222222222") != 2 {
		t.Fatalf("unanswered handoff not repeated: sends = %d", fixture.sendsFor("222222222222222222"))
	}
	if sent, err := sendImmediateHandoffs(digestTestConfig(), db); err != nil || sent != 0 {
		t.Fatalf("resent: sent=%d err=%v", sent, err)
	}
}

// The admin fallback is not sent now under the mixed rule; it waits for the
// morning digest and stays listed there while it is unanswered.
func TestHandoffFallbackWaitsForTheMorningDigest(t *testing.T) {
	db := handoffNotifyDB(t, model.HandoffTimingMixed)
	fixture := newDMFixture(t)
	stubHandoffIdentity(t, map[string]string{"person-admin": "111111111111111111"})
	// The fallback destination narrows to the Guild's administrators (2026-11-19
	// owner decision), so the resolver seam feeds the waiting admin directly.
	old := resolveHandoffAdminKitsuPersonIDs
	resolveHandoffAdminKitsuPersonIDs = func(db *gorm.DB, productionID string) []string {
		return []string{"person-admin"}
	}
	t.Cleanup(func() { resolveHandoffAdminKitsuPersonIDs = old })
	if err := db.Create(&model.UserMap{KitsuID: "person-admin", DiscordGuildID: "guild-1", DiscordID: "111111111111111111"}).Error; err != nil {
		t.Fatal(err)
	}
	handoffEvent(t, db, "comment-1", model.HandoffFallbackAdmin)

	if sent, err := sendImmediateHandoffs(digestTestConfig(), db); err != nil || sent != 0 {
		t.Fatalf("fallback sent immediately: sent=%d err=%v", sent, err)
	}
	if sent, err := sendForumMorningDigests(digestTestConfig(), db, time.Date(2026, 11, 16, 9, 0, 0, 0, jst), "2026-11-16"); err != nil || sent != 1 {
		t.Fatalf("digest sent=%d err=%v", sent, err)
	}
	if fixture.sendsFor("111111111111111111") != 1 {
		t.Fatalf("admin digest sends = %d", fixture.sendsFor("111111111111111111"))
	}
	// The next morning lists the same unanswered handoff again.
	if _, err := sendForumMorningDigests(digestTestConfig(), db, time.Date(2026, 11, 17, 9, 0, 0, 0, jst), "2026-11-17"); err != nil {
		t.Fatal(err)
	}
	if fixture.sendsFor("111111111111111111") != 2 {
		t.Fatalf("unanswered handoff not repeated: sends = %d", fixture.sendsFor("111111111111111111"))
	}
	if state := handoffState(t, db, "comment-1"); state != model.HandoffEventPending {
		t.Fatalf("state = %q, want pending until answered", state)
	}
}

// The timing setting biases the whole rule: immediate sends the fallback now,
// digest keeps even an assigned handoff for the morning.
func TestHandoffTimingSettingBiasesTheRule(t *testing.T) {
	for _, tc := range []struct {
		name   string
		timing string
		fallb  string
		ids    []string
		sent   int
	}{
		{"immediate-fallback", model.HandoffTimingImmediate, model.HandoffFallbackAdmin, nil, 1},
		{"digest-assigned", model.HandoffTimingDigest, model.HandoffFallbackNone, []string{"person-comp"}, 0},
		{"mixed-assigned", model.HandoffTimingMixed, model.HandoffFallbackNone, []string{"person-comp"}, 1},
		{"mixed-fallback", model.HandoffTimingMixed, model.HandoffFallbackAdmin, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := handoffNotifyDB(t, tc.timing)
			newDMFixture(t)
			stubHandoffIdentity(t, map[string]string{"person-comp": "222222222222222222", "person-admin": "111111111111111111"})
			// Subtests that exercise the fallback only depend on the resolver
			// seam; assigned personIDs never reach it.
			old := resolveHandoffAdminKitsuPersonIDs
			resolveHandoffAdminKitsuPersonIDs = func(db *gorm.DB, productionID string) []string {
				return []string{"person-admin"}
			}
			t.Cleanup(func() { resolveHandoffAdminKitsuPersonIDs = old })
			if err := db.Create(&model.UserMap{KitsuID: "person-admin", DiscordGuildID: "guild-1", DiscordID: "111111111111111111"}).Error; err != nil {
				t.Fatal(err)
			}
			handoffEvent(t, db, "comment-1", tc.fallb, tc.ids...)
			sent, err := sendImmediateHandoffs(digestTestConfig(), db)
			if err != nil || sent != tc.sent {
				t.Fatalf("sent=%d err=%v, want %d", sent, err, tc.sent)
			}
		})
	}
}

// The fallback destination narrows to the Guild's members holding a role with
// the Discord Administrator permission (2026-11-19 owner decision): the
// non-admin linked user falls off, and every linked administrator resolves.
func TestHandoffAdminFallbackReachesOnlyGuildAdministrators(t *testing.T) {
	db := handoffNotifyDB(t, model.HandoffTimingImmediate)
	fixture := newDMFixture(t)
	stubHandoffIdentity(t, map[string]string{"person-admin": "111111111111111111"})
	// person-comp stays a linked UserMap member but holds no Administrator role.
	for _, user := range []model.UserMap{
		{KitsuID: "person-admin", DiscordGuildID: "guild-1", DiscordID: "111111111111111111"},
		{KitsuID: "person-comp", DiscordGuildID: "guild-1", DiscordID: "222222222222222222"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	// The OWNER decision of 2026-11-19 narrows the fallback to the Guild's
	// administrators, so the resolver seam feeds the waiting admin directly.
	old := resolveHandoffAdminKitsuPersonIDs
	resolveHandoffAdminKitsuPersonIDs = func(db *gorm.DB, productionID string) []string {
		return []string{"person-admin"}
	}
	t.Cleanup(func() { resolveHandoffAdminKitsuPersonIDs = old })

	handoffEvent(t, db, "comment-1", model.HandoffFallbackAdmin)
	sent, err := sendImmediateHandoffs(digestTestConfig(), db)
	if err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	if fixture.sendsFor("111111111111111111") != 1 {
		t.Fatalf("administrator sends = %d", fixture.sendsFor("111111111111111111"))
	}
	if fixture.sendsFor("222222222222222222") != 0 {
		t.Fatalf("non-admin linked user fell through: sends = %d", fixture.sendsFor("222222222222222222"))
	}
	if state := handoffState(t, db, "comment-1"); state != model.HandoffEventNotified {
		t.Fatalf("state = %q", state)
	}
}

// Zero administrators resolve to no destination: the poll ends normally
// without an error instead of falling back to every linked user.
func TestHandoffNoAdministratorResolvesToNoDestination(t *testing.T) {
	db := handoffNotifyDB(t, model.HandoffTimingImmediate)
	fixture := newDMFixture(t)
	stubHandoffIdentity(t, map[string]string{"person-comp": "222222222222222222"})
	if err := db.Create(&model.UserMap{KitsuID: "person-comp", DiscordGuildID: "guild-1", DiscordID: "222222222222222222"}).Error; err != nil {
		t.Fatal(err)
	}
	old := resolveHandoffAdminKitsuPersonIDs
	resolveHandoffAdminKitsuPersonIDs = func(db *gorm.DB, productionID string) []string {
		return nil
	}
	t.Cleanup(func() { resolveHandoffAdminKitsuPersonIDs = old })

	handoffEvent(t, db, "comment-1", model.HandoffFallbackAdmin)
	sent, err := sendImmediateHandoffs(digestTestConfig(), db)
	if err != nil || sent != 0 {
		t.Fatalf("zero administrators must end normally: sent=%d err=%v", sent, err)
	}
	if sent, err := sendForumMorningDigests(digestTestConfig(), db, time.Date(2026, 11, 19, 9, 0, 0, 0, jst), "2026-11-19"); err != nil || sent != 0 {
		t.Fatalf("digest sent=%d err=%v", sent, err)
	}
	if fixture.sendsFor("222222222222222222") != 0 {
		t.Fatalf("non-admin notified: sends = %d", fixture.sendsFor("222222222222222222"))
	}
}

// A digest-timing handoff of an assigned person reaches them in the morning.
func TestHandoffDigestTimingListsAssignedHandoff(t *testing.T) {
	db := handoffNotifyDB(t, model.HandoffTimingDigest)
	fixture := newDMFixture(t)
	stubHandoffIdentity(t, map[string]string{"person-comp": "222222222222222222"})
	handoffEvent(t, db, "comment-1", model.HandoffFallbackNone, "person-comp")

	if sent, err := sendForumMorningDigests(digestTestConfig(), db, time.Date(2026, 11, 16, 9, 0, 0, 0, jst), "2026-11-16"); err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	if fixture.sendsFor("222222222222222222") != 1 {
		t.Fatalf("sends = %d", fixture.sendsFor("222222222222222222"))
	}
}

func TestHandoffImmediateRule(t *testing.T) {
	ids := []string{"9"}
	cases := []struct {
		timing  string
		fallb   string
		ids     []string
		want    bool
		comment string
	}{
		{model.HandoffTimingMixed, model.HandoffFallbackNone, ids, true, "mixed assigned without a fallback flag"},
		{model.HandoffTimingMixed, model.HandoffFallbackAdmin, ids, false, "mixed fallback"},
		{model.HandoffTimingMixed, model.HandoffFallbackUsers, ids, false, "mixed chosen user"},
		{model.HandoffTimingImmediate, model.HandoffFallbackAdmin, ids, true, "immediate biases everything"},
		{model.HandoffTimingDigest, model.HandoffFallbackNone, ids, false, "digest biases everything"},
		{model.HandoffTimingMixed, "", nil, false, "nobody resolved"},
	}
	for _, tc := range cases {
		if got := handoffImmediate(tc.timing, tc.fallb, tc.ids); got != tc.want {
			t.Fatalf("%s: got %v", tc.comment, got)
		}
	}
}

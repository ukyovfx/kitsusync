package setup

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

func TestNearestForumTagEmoji(t *testing.T) {
	cases := []struct{ color, want string }{
		{"#FF0000", "🔴"}, {"#ff3860", "🔴"}, {"#8B0000", "🔴"},
		{"#f5a623", "🟠"}, {"#FFA500", "🟠"},
		{"#FFD700", "🟡"}, {"#ffcc4d", "🟡"}, {"#FFFF00", "🟡"},
		{"#27ae60", "🟢"}, {"#00C853", "🟢"},
		{"#3273dc", "🔵"}, {"#2196F3", "🔵"},
		{"#9b59b6", "🟣"},
		{"#8B4513", "🟤"}, {"#a0522d", "🟤"},
		{"#FFFFFF", "⚪"}, {"#f5f5f5", "⚪"},
		{"#000000", "⚫"}, {"#333333", "⚫"},
		// Missing or unreadable colors keep the tag without an emoji.
		{"", ""}, {"   ", ""}, {"nope", ""}, {"#12345", ""}, {"#G60708", ""}, {"#", ""},
	}
	for _, c := range cases {
		if got := nearestForumTagEmoji(c.color); got != c.want {
			t.Errorf("nearestForumTagEmoji(%q) = %q, want %q", c.color, got, c.want)
		}
	}
}

func TestForumRoleTagEmojisFollowStatusColors(t *testing.T) {
	statuses := []kitsu.TaskStatus{
		{ID: "s1", Name: "Waiting For Approval", Color: "#f5a623"},
		{ID: "s2", Name: "Retake", Color: "#ff3860"},
		{ID: "s3", Name: "Done", Color: "#27ae60"},
		{ID: "s4", Name: "Work In Progress", ShortName: "WIP", Color: "#3273dc"},
		{ID: "s5", Name: "Colorless"},
	}
	roles := []model.ProductionForumStatusRole{
		{StatusName: "waiting for approval", Role: model.ForumRoleWFA},
		{StatusName: "Retake", Role: model.ForumRoleRetake},
		{StatusName: "done", Role: model.ForumRoleDone},
		{StatusName: "WIP", Role: model.ForumRoleWIP},
		{StatusName: "Colorless", Role: "review"},
		{StatusName: "Unknown", Role: "extra"},
	}
	got := forumRoleTagEmojis(roles, statuses)
	want := map[string]string{
		model.ForumRoleWFA:    "🟠",
		model.ForumRoleRetake: "🔴",
		model.ForumRoleDone:   "🟢",
		model.ForumRoleWIP:    "🔵",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("role emojis = %v, want %v", got, want)
	}
}

func TestForumTagPatchListKeepsIDsAndAddsEmoji(t *testing.T) {
	existing := []forumTagInfo{
		{ID: "t2", Name: "Retake"},
		{ID: "t4", Name: "WIP"},
		{ID: "t9", Name: "Reference"},
	}
	desired := []forumAutoTag{
		{Name: "WFA", Emoji: "🟠"},
		{Name: "RETAKE", Emoji: "🔴"},
		{Name: "DONE", Emoji: "🟢"},
		{Name: "WIP", Emoji: "⚫"},
	}
	available := forumTagPatchList(existing, desired)
	type tagEntry struct {
		ID        string `json:"id,omitempty"`
		Name      string `json:"name"`
		EmojiName string `json:"emoji_name,omitempty"`
	}
	want := []tagEntry{
		{Name: "WFA", EmojiName: "🟠"},
		// The hand-built spelling and the tag ID are kept, so Discord
		// preserves the tag and the posts already wearing it.
		{ID: "t2", Name: "Retake", EmojiName: "🔴"},
		{Name: "DONE", EmojiName: "🟢"},
		{ID: "t4", Name: "WIP", EmojiName: "⚫"},
		{ID: "t9", Name: "Reference"},
	}
	body, err := json.Marshal(map[string]any{"available_tags": available})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		AvailableTags []tagEntry `json:"available_tags"`
	}
	if json.Unmarshal(body, &got) != nil {
		t.Fatalf("payload was not valid tag JSON: %s", body)
	}
	if !reflect.DeepEqual(got.AvailableTags, want) {
		t.Fatalf("available_tags payload = %s, want %+v", body, want)
	}
}

// stubColoredStatuses gives the stubbed Kitsu status list the production-like
// colors the emoji decision is derived from.
func stubColoredStatuses(t *testing.T) {
	t.Helper()
	oldStatuses := forumStatusLister
	t.Cleanup(func() { forumStatusLister = oldStatuses })
	forumStatusLister = func(*gorm.DB) ([]kitsu.TaskStatus, error) {
		return []kitsu.TaskStatus{
			{ID: "s-wfa", Name: "Waiting For Approval", Color: "#f5a623"},
			{ID: "s-retake", Name: "Retake", Color: "#ff3860"},
			{ID: "s-done", Name: "Done", Color: "#27ae60"},
			{ID: "s-wip", Name: "Work In Progress", ShortName: "WIP", Color: "#3273dc"},
		}, nil
	}
}

func TestForumAutoCreateSendsColoredTags(t *testing.T) {
	stubForumLookups(t, nil)
	stubNoExistingForums(t)
	stubColoredStatuses(t)
	var createdTags []forumAutoTag
	old := forumCreateChannel
	t.Cleanup(func() { forumCreateChannel = old })
	forumCreateChannel = func(guildID, parentID, name string, tags []forumAutoTag, botToken string) (forumChannelInfo, error) {
		createdTags = tags
		forum := forumChannelInfo{ID: "new-" + name, Name: name}
		for _, tag := range tags {
			forum.Tags = append(forum.Tags, forumTagInfo{ID: "newtag-" + tag.Name, Name: tag.Name})
		}
		return forum, nil
	}
	db := newForumPageDB(t)

	form := url.Values{"project": {"p1"}, "mode": {"forum"}, "guild_id": {"g1"}, "action": {"create_forums"}}
	form["tt_id"], form["tt_name"], form["tt_forum"] = []string{"tt-anim"}, []string{"Animation"}, []string{""}
	form["st_id"] = []string{"s-wfa", "s-retake", "s-done", "s-wip"}
	form["st_name"] = []string{"Waiting For Approval", "Retake", "Done", "Work In Progress"}
	form["st_role"] = []string{"wfa", "retake", "done", "wip"}
	w := forumPost(t, db, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	want := map[string]string{"WFA": "🟠", "RETAKE": "🔴", "DONE": "🟢", "WIP": "🔵"}
	if len(createdTags) != 4 {
		t.Fatalf("created tags = %+v", createdTags)
	}
	for _, tag := range createdTags {
		if want[tag.Name] != tag.Emoji {
			t.Fatalf("tag %s emoji = %q, want %q", tag.Name, tag.Emoji, want[tag.Name])
		}
	}
}

func TestForumAutoCreateColorsReusedForumTags(t *testing.T) {
	stubForumLookups(t, nil)
	stubColoredStatuses(t)
	// Discord already has the Animation forum with colorless role tags (an
	// earlier run) and the Compositing forum already colored. Only Animation
	// must be patched, with every existing tag ID kept.
	oldLister := forumChannelLister
	t.Cleanup(func() { forumChannelLister = oldLister })
	forumChannelLister = func(string, string) ([]forumChannelInfo, error) {
		colorless := []forumTagInfo{{ID: "t1", Name: "WFA"}, {ID: "t2", Name: "RETAKE"}, {ID: "t3", Name: "DONE"}, {ID: "t4", Name: "WIP"}}
		colored := []forumTagInfo{{ID: "t1", Name: "WFA", Emoji: "🟠"}, {ID: "t2", Name: "RETAKE", Emoji: "🔴"}, {ID: "t3", Name: "DONE", Emoji: "🟢"}, {ID: "t4", Name: "WIP", Emoji: "🔵"}}
		return []forumChannelInfo{
			{ID: "f-anim", Name: "Animation", ParentID: "cat-1", Tags: colorless},
			{ID: "f-comp", Name: "Compositing", ParentID: "cat-1", Tags: colored},
		}, nil
	}
	var createdForums, patchedForums int
	oldCreate := forumCreateChannel
	t.Cleanup(func() { forumCreateChannel = oldCreate })
	forumCreateChannel = func(guildID, parentID, name string, tags []forumAutoTag, botToken string) (forumChannelInfo, error) {
		createdForums++
		return forumChannelInfo{ID: "new-" + name, Name: name}, nil
	}
	var gotExisting []forumTagInfo
	var gotDesired []forumAutoTag
	oldUpdater := forumTagUpdater
	t.Cleanup(func() { forumTagUpdater = oldUpdater })
	forumTagUpdater = func(channelID string, existing []forumTagInfo, desired []forumAutoTag, botToken string) ([]forumTagInfo, error) {
		patchedForums++
		gotExisting = existing
		gotDesired = desired
		return existing, nil
	}
	db := newForumPageDB(t)
	guildChannelListCache.entries["g1"] = &guildChannelCacheEntry{
		body: []byte(`[{"id":"cat-1","name":"Category","type":4},{"id":"f-anim","name":"Animation","type":15,"parent_id":"cat-1"},{"id":"f-comp","name":"Compositing","type":15,"parent_id":"cat-1"}]`),
	}
	if err := db.Model(&model.Project{}).Where("kitsu_project_id = ?", "p1").Update("discord_category_id", "cat-1").Error; err != nil {
		t.Fatal(err)
	}

	form := url.Values{"project": {"p1"}, "guild_id": {"g1"}, "action": {"create_forums"}}
	w := forumPost(t, db, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	if createdForums != 0 {
		t.Fatalf("created forums = %d, want reuse only", createdForums)
	}
	if patchedForums != 1 {
		t.Fatalf("patched forums = %d, want only the colorless one", patchedForums)
	}
	if len(gotExisting) != 4 {
		t.Fatalf("updater received existing tags = %+v, want the forum's four tags", gotExisting)
	}
	for _, tag := range gotDesired {
		if tag.Emoji == "" {
			t.Fatalf("desired tag %s lost its emoji", tag.Name)
		}
	}
	// The emoji set follows the saved status colors, not a fixed scheme.
	if gotDesired[0].Emoji != "🟠" || gotDesired[1].Emoji != "🔴" || gotDesired[2].Emoji != "🟢" || gotDesired[3].Emoji != "🔵" {
		t.Fatalf("desired emojis = %+v", gotDesired)
	}
	loaded, ok, err := model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	got := map[string]string{}
	for _, tag := range loaded.Tags {
		got[tag.ForumChannelID+"|"+tag.Role] = tag.TagID
	}
	if got["f-anim|wfa"] != "t1" || got["f-anim|retake"] != "t2" || got["f-anim|done"] != "t3" || got["f-anim|wip"] != "t4" {
		t.Fatalf("tag mapping = %v, want the existing IDs kept", got)
	}
}

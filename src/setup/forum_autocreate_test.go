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

func stubForumCreate(t *testing.T, created *[]string) {
	t.Helper()
	old := forumCreateChannel
	t.Cleanup(func() { forumCreateChannel = old })
	forumCreateChannel = func(guildID, parentID, name string, tags []string, botToken string) (forumChannelInfo, error) {
		*created = append(*created, name)
		forum := forumChannelInfo{ID: "new-" + name, Name: name}
		for _, tag := range tags {
			forum.Tags = append(forum.Tags, forumTagInfo{ID: "newtag-" + tag, Name: tag})
		}
		return forum, nil
	}
}

// stubNoExistingForums empties the Discord forum list, so the auto-create
// action creates every forum instead of reusing one that is already there.
func stubNoExistingForums(t *testing.T) {
	t.Helper()
	old := forumChannelLister
	t.Cleanup(func() { forumChannelLister = old })
	forumChannelLister = func(string, string) ([]forumChannelInfo, error) { return nil, nil }
}

func TestForumAutoCreateMakesForumAndTagsPerTaskType(t *testing.T) {
	stubForumLookups(t, nil)
	stubNoExistingForums(t)
	var created []string
	stubForumCreate(t, &created)
	db := newForumPageDB(t)

	form := url.Values{"project": {"p1"}, "mode": {"forum"}, "guild_id": {"g1"}, "action": {"create_forums"}}
	form["tt_id"], form["tt_name"], form["tt_forum"] = []string{"tt-anim", "tt-comp"}, []string{"Animation", "Compositing"}, []string{"", ""}
	form["st_id"], form["st_name"], form["st_role"] = []string{"s-wfa", "s-retake", "s-done"}, []string{"WFA", "Retake", "Done"}, []string{"wfa", "retake", "done"}
	for _, forum := range []string{"f-anim", "f-comp"} {
		for i, role := range []string{"wfa", "retake", "done"} {
			form.Add("tag_forum", forum)
			form.Add("tag_role", role)
			form.Add("tag_id", []string{"t1", "t2", "t3"}[i])
		}
	}
	w := forumPost(t, db, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	if strings.Join(created, ",") != "Animation,Compositing" {
		t.Fatalf("created forums = %v", created)
	}
	loaded, ok, err := model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if loaded.Routes[0].ForumChannelID != "new-Animation" || loaded.Routes[1].ForumChannelID != "new-Compositing" {
		t.Fatalf("routes = %+v", loaded.Routes)
	}
	want := map[string]string{"new-Animation|wfa": "newtag-WFA", "new-Animation|retake": "newtag-RETAKE", "new-Animation|done": "newtag-DONE"}
	for _, tag := range loaded.Tags {
		if key, ok := want[tag.ForumChannelID+"|"+tag.Role]; ok && tag.TagID != key {
			t.Fatalf("tag %s = %s, want %s", tag.ForumChannelID+"|"+tag.Role, tag.TagID, key)
		}
	}
	if len(loaded.Tags) != 8 {
		t.Fatalf("tags = %+v", loaded.Tags)
	}
}

func TestForumAutoCreateDoesNotDuplicateExistingForums(t *testing.T) {
	stubForumLookups(t, nil)
	var created []string
	stubForumCreate(t, &created)
	db := newForumPageDB(t)
	if w := forumPost(t, db, completeForumForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("seed save status = %d", w.Code)
	}
	form := url.Values{"project": {"p1"}, "mode": {"forum"}, "guild_id": {"g1"}, "action": {"create_forums"}}
	form["tt_id"], form["tt_name"], form["tt_forum"] = []string{"tt-anim", "tt-comp"}, []string{"Animation", "Compositing"}, []string{"f-anim", "f-comp"}
	if w := forumPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", w.Code)
	}
	if len(created) != 0 {
		t.Fatalf("created forums = %v, want none", created)
	}
}

func TestForumAutoCreateWithoutStatusRolesAppliesTemplateRoles(t *testing.T) {
	stubForumLookups(t, nil)
	stubNoExistingForums(t)
	var created []string
	stubForumCreate(t, &created)
	db := newForumPageDB(t)

	// One press with nothing mapped: the Bot seeds the routes from the Kitsu
	// Task Type list, creates the forums and tags, applies the template status
	// roles and saves — no dead end asking for roles.
	form := url.Values{"project": {"p1"}, "guild_id": {"g1"}, "action": {"create_forums"}}
	w := forumPost(t, db, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	if strings.Join(created, ",") != "Animation,Compositing" {
		t.Fatalf("created forums = %v", created)
	}
	loaded, ok, err := model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	roles := map[string]string{}
	for _, row := range loaded.StatusRoles {
		roles[row.StatusID] = row.Role
	}
	if len(roles) != 3 || roles["s-wfa"] != "wfa" || roles["s-retake"] != "retake" || roles["s-done"] != "done" {
		t.Fatalf("template status roles = %v", roles)
	}
	if len(loaded.Tags) != 8 {
		t.Fatalf("tags = %+v", loaded.Tags)
	}
}

func TestForumAutoCreateFromBrowserFormShape(t *testing.T) {
	// The zero-touch browser POST carries the whole rendered form: Task Type
	// rows with no forum chosen yet, and the template status roles pre-filled.
	stubForumLookups(t, nil)
	stubNoExistingForums(t)
	var created []string
	stubForumCreate(t, &created)
	db := newForumPageDB(t)

	form := url.Values{
		"project":  {"p1"},
		"guild_id": {""},
		"action":   {"create_forums"},
		"tt_id":    {"tt-anim", "tt-comp"},
		"tt_name":  {"Animation", "Compositing"},
		"tt_forum": {"", ""},
		"st_id":    {"s-wfa", "s-retake", "s-done", "s-wip"},
		"st_name":  {"Waiting For Approval", "Retake", "Done", "WIP"},
		"st_role":  {"wfa", "retake", "done", ""},
	}
	w := forumPost(t, db, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	if strings.Join(created, ",") != "Animation,Compositing" {
		t.Fatalf("created forums = %v", created)
	}
	loaded, ok, err := model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if len(loaded.Routes) != 2 || len(loaded.Tags) != 8 {
		t.Fatalf("routes = %+v tags = %+v", loaded.Routes, loaded.Tags)
	}
}

func TestForumAutoCreateResumesFromCreatedButUnsavedForums(t *testing.T) {
	stubForumLookups(t, nil)
	var created []string
	stubForumCreate(t, &created)
	// Discord already has both forums (an earlier run was interrupted before the
	// save), so the retry must reuse them instead of creating duplicates.
	oldLister := forumChannelLister
	t.Cleanup(func() { forumChannelLister = oldLister })
	forumChannelLister = func(string, string) ([]forumChannelInfo, error) {
		tags := []forumTagInfo{{ID: "t1", Name: "WFA"}, {ID: "t2", Name: "RETAKE"}, {ID: "t3", Name: "DONE"}, {ID: "t4", Name: "WIP"}}
		return []forumChannelInfo{
			{ID: "f-anim", Name: "animation", ParentID: "cat-1", Tags: tags},
			{ID: "f-comp", Name: "compositing", ParentID: "cat-1", Tags: tags},
		}, nil
	}
	db := newForumPageDB(t)
	if err := db.Model(&model.Project{}).Where("kitsu_project_id = ?", "p1").Update("discord_category_id", "cat-1").Error; err != nil {
		t.Fatal(err)
	}

	form := url.Values{"project": {"p1"}, "guild_id": {"g1"}, "action": {"create_forums"}}
	w := forumPost(t, db, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	if len(created) != 0 {
		t.Fatalf("retry created = %v, want no duplicates", created)
	}
	loaded, ok, err := model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if len(loaded.Routes) != 2 || loaded.Routes[0].ForumChannelID != "f-anim" || loaded.Routes[1].ForumChannelID != "f-comp" {
		t.Fatalf("routes = %+v", loaded.Routes)
	}
	if len(loaded.Tags) != 8 {
		t.Fatalf("tags = %+v", loaded.Tags)
	}
}

func TestForumAutoCreateNeedsBotToken(t *testing.T) {
	stubForumLookups(t, nil)
	var created []string
	stubForumCreate(t, &created)
	db := newForumPageDB(t)
	handler := ForumSettingsHandler(db, func() (string, string) { return "", "g1" })
	form := url.Values{"project": {"p1"}, "action": {"create_forums"}}
	r := httptest.NewRequest(http.MethodPost, "/bot/admin/forum?lang=ja", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusBadRequest || len(created) != 0 {
		t.Fatalf("status = %d created = %v", w.Code, created)
	}
	if !strings.Contains(w.Body.String(), "Botトークン") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestForumAutoCreateKeepsCreatedForumsWhenALaterCreateFails(t *testing.T) {
	stubForumLookups(t, nil)
	stubNoExistingForums(t)
	var created []string
	old := forumCreateChannel
	t.Cleanup(func() { forumCreateChannel = old })
	forumCreateChannel = func(guildID, parentID, name string, tags []string, botToken string) (forumChannelInfo, error) {
		created = append(created, name)
		if name == "Compositing" {
			return forumChannelInfo{}, errors.New("discord unavailable")
		}
		forum := forumChannelInfo{ID: "new-" + name, Name: name}
		for _, tag := range tags {
			forum.Tags = append(forum.Tags, forumTagInfo{ID: "newtag-" + tag, Name: tag})
		}
		return forum, nil
	}
	db := newForumPageDB(t)

	form := url.Values{"project": {"p1"}, "mode": {"forum"}, "guild_id": {"g1"}, "action": {"create_forums"}}
	form["tt_id"], form["tt_name"], form["tt_forum"] = []string{"tt-anim", "tt-comp"}, []string{"Animation", "Compositing"}, []string{"", ""}
	form["st_id"], form["st_name"], form["st_role"] = []string{"s-wfa"}, []string{"WFA"}, []string{"wfa"}
	w := forumPost(t, db, form)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.Join(created, ",") != "Animation,Compositing" {
		t.Fatalf("created forums = %v", created)
	}
	// The forum that was created before the failure must survive in the page,
	// otherwise the retry creates a second Animation forum.
	if !strings.Contains(w.Body.String(), "new-Animation") {
		t.Fatalf("page dropped the created forum: %s", w.Body.String())
	}
}

func TestForumAutoCreateCreatesForumUnderProductionCategory(t *testing.T) {
	stubForumLookups(t, nil)
	stubNoExistingForums(t)
	// Auto-create runs over the seeded Task Type list (empty tt_forum rows are
	// dropped from the form), so keep the list at one type for one create call.
	oldTypes := forumTaskTypeLister
	t.Cleanup(func() { forumTaskTypeLister = oldTypes })
	forumTaskTypeLister = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "tt-anim", Name: "Animation"}}
	}
	var parents []string
	old := forumCreateChannel
	t.Cleanup(func() { forumCreateChannel = old })
	forumCreateChannel = func(guildID, parentID, name string, tags []string, botToken string) (forumChannelInfo, error) {
		parents = append(parents, parentID)
		forum := forumChannelInfo{ID: "new-" + name, Name: name}
		for _, tag := range tags {
			forum.Tags = append(forum.Tags, forumTagInfo{ID: "newtag-" + tag, Name: tag})
		}
		return forum, nil
	}
	db := newForumPageDB(t)
	if err := db.Model(&model.Project{}).Where("kitsu_project_id = ?", "p1").Update("discord_category_id", "cat-1").Error; err != nil {
		t.Fatal(err)
	}

	form := url.Values{"project": {"p1"}, "mode": {"forum"}, "guild_id": {"g1"}, "action": {"create_forums"}}
	form["tt_id"], form["tt_name"], form["tt_forum"] = []string{"tt-anim"}, []string{"Animation"}, []string{""}
	form["st_id"], form["st_name"], form["st_role"] = []string{"s-wfa", "s-retake", "s-done"}, []string{"WFA", "Retake", "Done"}, []string{"wfa", "retake", "done"}
	w := forumPost(t, db, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	if strings.Join(parents, ",") != "cat-1" {
		t.Fatalf("created forums under parents %v, want cat-1", parents)
	}
}

func TestForumPermissionCheckListsMissingPermissions(t *testing.T) {
	old := forumPermissionGetter
	t.Cleanup(func() { forumPermissionGetter = old })
	// View Channel + Send Messages only.
	forumPermissionGetter = func(string, string) (int64, error) { return (1 << 10) | (1 << 11), nil }
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{GuildID: "guild-1"},
		Routes: []model.ProductionForumRoute{{TaskTypeName: "Compositing", ForumChannelID: "f1"}},
	}
	problems := checkForumPermissions(settings, "token")
	if len(problems) != 1 {
		t.Fatalf("problems = %v", problems)
	}
	for _, want := range []string{"Compositing", "スレッドでメッセージを送信", "スレッドを管理", "埋め込みリンク", "メッセージ履歴を読む"} {
		if !strings.Contains(problems[0], want) {
			t.Fatalf("problem %q is missing %q", problems[0], want)
		}
	}
	missing := problems[0][strings.Index(problems[0], ": ")+2:]
	for _, granted := range []string{"チャンネルを見る", "メッセージを送信"} {
		for _, name := range strings.Split(missing, "、") {
			if name == granted {
				t.Fatalf("reported a permission the Bot has: %q", problems[0])
			}
		}
	}
}

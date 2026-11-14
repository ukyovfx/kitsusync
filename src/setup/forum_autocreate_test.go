package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/model"
)

func stubForumCreate(t *testing.T, created *[]string) {
	t.Helper()
	old := forumCreateChannel
	t.Cleanup(func() { forumCreateChannel = old })
	forumCreateChannel = func(guildID, name string, tags []string, botToken string) (forumChannelInfo, error) {
		*created = append(*created, name)
		forum := forumChannelInfo{ID: "new-" + name, Name: name}
		for _, tag := range tags {
			forum.Tags = append(forum.Tags, forumTagInfo{ID: "newtag-" + tag, Name: tag})
		}
		return forum, nil
	}
}

func TestForumAutoCreateMakesForumAndTagsPerTaskType(t *testing.T) {
	stubForumLookups(t, nil)
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

func TestForumAutoCreateWithoutStatusRolesKeepsCreatedForums(t *testing.T) {
	stubForumLookups(t, nil)
	var created []string
	stubForumCreate(t, &created)
	db := newForumPageDB(t)

	form := url.Values{"project": {"p1"}, "mode": {"forum"}, "guild_id": {"g1"}, "action": {"create_forums"}}
	form["tt_id"], form["tt_name"], form["tt_forum"] = []string{"tt-anim"}, []string{"Animation"}, []string{""}
	w := forumPost(t, db, form)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "new-Animation") {
		t.Fatalf("page does not show the created forum: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Kitsuのステータス") {
		t.Fatalf("page does not say what is left to do: %s", w.Body.String())
	}
	if len(created) != 2 {
		t.Fatalf("created = %v", created)
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

func TestForumPermissionCheckListsMissingPermissions(t *testing.T) {
	old := forumPermissionGetter
	t.Cleanup(func() { forumPermissionGetter = old })
	// View Channel + Send Messages only.
	forumPermissionGetter = func(string, string) (int64, error) { return (1 << 10) | (1 << 11), nil }
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{Mode: model.NotificationModeForum},
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

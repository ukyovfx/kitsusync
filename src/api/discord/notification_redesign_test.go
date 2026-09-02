package discord

import (
	"app/src/api/kitsu"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"app/src/utils/config"
)

func TestParseKitsuStatusColorUsesNeutralFallback(t *testing.T) {
	if got := parseKitsuStatusColor("#12abEF"); got != 0x12abef {
		t.Fatalf("valid status color = %#x, want %#x", got, 0x12abef)
	}
	if got := parseKitsuStatusColor("not-a-color"); got != neutralEmbedColor {
		t.Fatalf("invalid status color = %#x, want %#x", got, neutralEmbedColor)
	}
	if got := parseKitsuStatusColor(""); got != neutralEmbedColor {
		t.Fatalf("empty status color = %#x, want %#x", got, neutralEmbedColor)
	}
}

func TestNotificationRecipientPolicy(t *testing.T) {
	conf := testMentionConfig()
	if got := notificationRecipientCandidates("WFA", false, []string{"101"}, []string{"202"}, conf); strings.Join(got, ",") != "202" {
		t.Fatalf("WFA recipients = %v, want checker", got)
	}
	if got := notificationRecipientCandidates("RETAKE", false, []string{"101", "101"}, []string{"202"}, conf); strings.Join(got, ",") != "101" {
		t.Fatalf("RETAKE recipients = %v, want deduplicated assignee", got)
	}
	if got := notificationRecipientCandidates("DONE", false, []string{"101"}, []string{"202"}, conf); strings.Join(got, ",") != "101" {
		t.Fatalf("DONE recipients = %v, want assignee", got)
	}
	if got := notificationRecipientCandidates("WFA", false, []string{"101"}, []string{"bad", "202", "202", "<@303>"}, conf); strings.Join(got, ",") != "202" {
		t.Fatalf("recipient validation = %v", got)
	}
}

func TestStatusMentionRoutingSerializesOnlyExactRecipients(t *testing.T) {
	conf := config.Config{}
	for _, tc := range []struct {
		status string
		want   []string
		input  []string
	}{
		{status: "WFA", want: []string{"202"}, input: []string{"101"}},
		{status: "RETAKE", want: []string{"101"}, input: []string{"101", "<@everyone>", "bad"}},
		{status: "DONE", want: []string{"101"}, input: []string{"101"}},
	} {
		ids := notificationRecipientCandidates(tc.status, false, tc.input, []string{"202"}, conf)
		payload := RenderNotificationPayload(Template{
			StatusUpper: tc.status, StatusEmoji: "•", StatusMessage: "Review",
			MentionContent: mentionContent(ids), AllowedUserIDs: ids,
		}, "rich")
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if payload.Content != mentionContent(tc.want) || len(payload.AllowedMentions.Users) != len(tc.want) {
			t.Fatalf("%s mention payload = %s, want only %v", tc.status, raw, tc.want)
		}
		if strings.Contains(string(raw), "everyone") || strings.Contains(string(raw), "<@&") {
			t.Fatalf("%s unsafe mention leaked: %s", tc.status, raw)
		}
	}
}

func TestNotificationRecipientBoundAndMentionContentMatch(t *testing.T) {
	ids := make([]string, maxNotificationRecipients+2)
	for i := range ids {
		ids[i] = "10" + strings.Repeat("0", i+1)
	}
	got := uniqueDiscordIDs(ids)
	if len(got) != maxNotificationRecipients {
		t.Fatalf("recipient count = %d, want %d", len(got), maxNotificationRecipients)
	}
	content := mentionContent(got)
	for _, id := range got {
		if !strings.Contains(content, "<@"+id+">") {
			t.Fatalf("mention content does not match allowed ID %s: %q", id, content)
		}
	}
	if strings.Contains(content, "everyone") || strings.Contains(content, "here") || strings.Contains(content, "<@&") {
		t.Fatalf("unsafe mention content: %q", content)
	}
}

func TestFinalNotificationCardHasNoDuplicateStatusOrTaskTypeEmoji(t *testing.T) {
	useRepositoryRootForTemplates(t)
	data := Template{
		TaskType:                "Compositing",
		GroupName:               "Shot",
		ParentName:              "SC02",
		TaskName:                "cut009",
		StatusUpper:             "RETAKE",
		StatusEmoji:             "🔄",
		StatusMessage:           "A revision is needed",
		StatusTransitionMessage: "A revision is required after review",
		PreviousStatus:          "WFA",
		AssigneeLabel:           "Assignee",
		AssigneesStr:            "UKYO M",
		NotificationLanguage:    "en",
		TaskURL:                 "https://kitsu.example/tasks/cut009",
		GoogleDriveURL:          "https://drive.example/cut009",
	}
	payload := RenderNotificationPayload(data, "rich")
	if len(payload.Embeds) != 1 {
		t.Fatalf("embed count = %d, want 1", len(payload.Embeds))
	}
	embed := payload.Embeds[0]
	if embed.Title != "" || !strings.Contains(embed.Description, "## Shot / SC02 - cut009") {
		t.Fatalf("task context must be a description heading: title=%q description=%q", embed.Title, embed.Description)
	}
	if embed.Url != "" {
		t.Fatalf("task title must not be a hyperlink: %q", embed.Url)
	}
	if embed.Author.Name != "Compositing" {
		t.Fatalf("task type author = %q", embed.Author.Name)
	}
	if strings.Contains(embed.Description, "<@") {
		t.Fatalf("mention leaked into embed: %q", embed.Description)
	}
	if !strings.Contains(embed.Description, "**📊 Status**　　　**👤 Assignee**\nwfa → retake　　　　UKYO M") {
		t.Fatalf("metadata is missing from the description: %q", embed.Description)
	}
	if len(embed.Fields) != 0 || !strings.Contains(embed.Description, "[🦊 Kitsu](https://kitsu.example/tasks/cut009)　　[📁 Drive](https://drive.example/cut009)") {
		t.Fatalf("links must be a single Kitsu-first description row: fields=%+v description=%q", embed.Fields, embed.Description)
	}
	if strings.HasPrefix(embed.Author.Name, "🧩") {
		t.Fatalf("decorative task type emoji leaked into card: %q", embed.Author.Name)
	}
}

func TestNotificationStatusColorUsesStableSemanticAccents(t *testing.T) {
	if got := notificationStatusColor("WFA"); got != 0xD4A72C {
		t.Fatalf("WFA color = %#x", got)
	}
	if got := notificationStatusColor("RETAKE"); got != 0xC45656 {
		t.Fatalf("RETAKE color = %#x", got)
	}
	if got := notificationStatusColor("DONE"); got != 0x4FAF78 {
		t.Fatalf("DONE color = %#x", got)
	}
	if got := notificationStatusColor("UNKNOWN"); got != neutralEmbedColor {
		t.Fatalf("unknown color = %#x", got)
	}
}

func TestNotificationCardUsesDiscordMarkdownHierarchy(t *testing.T) {
	useRepositoryRootForTemplates(t)
	for _, status := range []string{"WFA", "RETAKE", "DONE"} {
		payload := RenderNotificationPayload(Template{
			EntityType: "Shot", TaskName: "cut001", TaskType: "Animation",
			StatusUpper: status, StatusEmoji: "•", StatusMessage: "Review body",
			NotificationLanguage: "en", Color: notificationStatusColor(status),
		}, "rich")
		description := payload.Embeds[0].Description
		if !strings.Contains(description, "### • "+status) {
			t.Fatalf("%s status is not a level-three heading: %q", status, description)
		}
		if strings.Contains(description, "-#") {
			t.Fatalf("%s body hierarchy changed unexpectedly: %q", status, description)
		}
		if !strings.Contains(description, "### • "+status+"\nReview body") {
			t.Fatalf("%s status/body spacing is not compact: %q", status, description)
		}
	}
}

func TestAssigneeDisplayNeverRendersOrAllowsAssigneeMentions(t *testing.T) {
	data := Template{
		EntityType: "Shot", TaskName: "cut001", StatusUpper: "WFA", StatusEmoji: "•",
		StatusMessage: "Please review", AssigneesStr: "Artist A",
		NotificationLanguage: "en",
	}
	payload := RenderNotificationPayload(data, "rich")
	if strings.Contains(payload.Embeds[0].Description, "<@123456789012345678>") || !strings.Contains(payload.Embeds[0].Description, "**👤 Assignee**\n") || !strings.Contains(payload.Embeds[0].Description, "Artist A") {
		t.Fatalf("assignee metadata should contain only the plain Kitsu name: %+v", payload.Embeds)
	}
	if len(payload.AllowedMentions.Users) != 0 {
		t.Fatalf("assignee mention was allowed: %v", payload.AllowedMentions.Users)
	}
	if len(payload.AllowedMentions.Parse) != 0 || len(payload.AllowedMentions.Roles) != 0 {
		t.Fatalf("broad mention parsing enabled: %+v", payload.AllowedMentions)
	}
}

func TestKitsuPersonDisplayNameUsesStructuredJapaneseOrderSafely(t *testing.T) {
	person := kitsu.Person{FirstName: "侑恭", LastName: "松尾", FullName: "侑恭 松尾"}
	if got := kitsuPersonDisplayName(person, "ja"); got != "松尾 侑恭" {
		t.Fatalf("Japanese structured name = %q", got)
	}
	if got := kitsuPersonDisplayName(person, "en"); got != "侑恭 松尾" {
		t.Fatalf("English display-name convention changed = %q", got)
	}
	if got := kitsuPersonDisplayName(kitsu.Person{FullName: "Unknown order"}, "ja"); got != "Unknown order" {
		t.Fatalf("unstructured name was guessed = %q", got)
	}
}

func TestNotificationCardUsesReferenceHierarchyInJapanese(t *testing.T) {
	useRepositoryRootForTemplates(t)
	payload := RenderNotificationPayload(Template{
		ProjectName:          "Escape",
		EntityType:           "Shot",
		ParentName:           "SC02",
		TaskName:             "cut012",
		TaskType:             "Color Grading",
		StatusUpper:          "DONE",
		StatusEmoji:          "✅",
		StatusMessage:        "完了しました。必要に応じてご確認ください。",
		CommentLabel:         "コメント",
		CommentContent:       "確認をお願いします。",
		CommentAuthorLabel:   "コメント投稿者",
		CommentAuthor:        "USER A",
		AssigneeLabel:        "担当",
		AssigneesStr:         "KOTARO MITA",
		LinksLabel:           "リンク",
		TaskURL:              "https://kitsu.example/tasks/cut012",
		GoogleDriveURL:       "https://drive.example/cut012",
		NotificationLanguage: "ja",
		CardContext:          "テスト通知",
		Color:                0x4FAF78,
	}, "rich")
	embed := payload.Embeds[0]
	if embed.Title != "" || !strings.Contains(embed.Description, "## Shot / SC02 - cut012") || embed.Author.Name != "Color Grading" {
		t.Fatalf("unexpected title/task type hierarchy: %+v", embed)
	}
	if embed.Footer.Text != "テスト通知" {
		t.Fatalf("test marker is not footer-only: %q", embed.Footer.Text)
	}
	if !strings.Contains(embed.Description, "完了しました。必要に応じてご確認ください。\n\n**コメント**\n> 確認をお願いします。\nby USER A") {
		t.Fatalf("comment block is not grouped correctly: %q", embed.Description)
	}
	if embed.Url != "" {
		t.Fatalf("task title must not be a hyperlink: %q", embed.Url)
	}
	if len(embed.Fields) != 0 || !strings.Contains(embed.Description, "[🦊 Kitsu](https://kitsu.example/tasks/cut012)　　[📁 Drive](https://drive.example/cut012)") || !strings.Contains(embed.Description, "**📊 ステータス**　　　**👤 担当**\ndone　　　　KOTARO MITA") {
		t.Fatalf("links or metadata are not grouped in description: fields=%+v description=%q", embed.Fields, embed.Description)
	}
}

func TestNotificationCardNativeHierarchyIsExplicit(t *testing.T) {
	useRepositoryRootForTemplates(t)
	payload := RenderNotificationPayload(Template{
		EntityType: "Shot", ParentName: "SC02", TaskName: "cut012", TaskType: "Color Grading",
		StatusUpper: "WFA", StatusEmoji: "👀", StatusMessage: "チェックをお願いします",
		PreviousStatus: "TODO", NotificationLanguage: "ja", CardContext: "テスト通知",
		AssigneeLabel: "担当", AssigneesStr: "未割り当て", Color: 0xD4A72C,
	}, "rich")
	if len(payload.Embeds) != 1 {
		t.Fatalf("embed count = %d, want 1", len(payload.Embeds))
	}
	embed := payload.Embeds[0]
	if embed.Author.Name != "Color Grading" {
		t.Fatalf("author must contain only Task Type, got %q", embed.Author.Name)
	}
	if embed.Title != "" {
		t.Fatalf("title must be empty; shot context belongs in description, got %q", embed.Title)
	}
	if embed.Description != "## Shot / SC02 - cut012\n\n### 👀 WFA\nチェックをお願いします\n\n**📊 ステータス**　　　**👤 担当**\ntodo → wfa　　　　未割り当て" {
		t.Fatalf("description must contain the shot/status/body hierarchy, got %q", embed.Description)
	}
	if embed.Footer.Text != "テスト通知" {
		t.Fatalf("test marker must be footer-only, got %q", embed.Footer.Text)
	}
	if len(embed.Fields) != 0 || !strings.Contains(embed.Description, "**📊 ステータス**　　　**👤 担当**\ntodo → wfa　　　　未割り当て") {
		t.Fatalf("metadata row must be in the description: %+v", embed)
	}
}

func TestNotificationCardSerializedPayloadMatchesFinalDiscordContract(t *testing.T) {
	useRepositoryRootForTemplates(t)
	payload := RenderNotificationPayload(Template{
		EntityType:           "Shot",
		ParentName:           "sc001",
		TaskName:             "sh001",
		TaskType:             "Compositing",
		StatusUpper:          "RETAKE",
		StatusEmoji:          "🔄",
		StatusMessage:        "修正をお願いします",
		PreviousStatus:       "WFA",
		AssigneeLabel:        "担当",
		AssigneesStr:         "松尾 侑恭",
		TaskURL:              "https://kitsu.example.com/productions/p/shots/s/tasks/t",
		GoogleDriveURL:       "https://drive.example.com/folder",
		NotificationLanguage: "ja",
		Color:                notificationStatusColor("RETAKE"),
	}, "rich")
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Embeds []struct {
			Author      EmbedAuthor  `json:"author"`
			Title       string       `json:"title"`
			URL         string       `json:"url"`
			Description string       `json:"description"`
			Footer      EmbedFooter  `json:"footer"`
			Fields      []EmbedField `json:"fields"`
		} `json:"embeds"`
		AllowedMentions AllowedMentions `json:"allowed_mentions"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Embeds) != 1 {
		t.Fatalf("serialized embed count = %d", len(got.Embeds))
	}
	embed := got.Embeds[0]
	if embed.Author.Name != "Compositing" || embed.Title != "" || embed.URL != "" {
		t.Fatalf("serialized author/title contract mismatch: %+v", embed)
	}
	if embed.Description != "## Shot / sc001 - sh001\n\n### 🔄 RETAKE\n修正をお願いします\n\n[🦊 Kitsu](https://kitsu.example.com/productions/p/shots/s/tasks/t)　　[📁 Drive](https://drive.example.com/folder)\n\n**📊 ステータス**　　　**👤 担当**\nwfa → retake　　　　松尾 侑恭" {
		t.Fatalf("serialized status/body contract mismatch: %q", embed.Description)
	}
	if embed.Footer.Text != "" {
		t.Fatalf("serialized Production footer must be absent: %q", embed.Footer.Text)
	}
	if len(embed.Fields) != 0 || !strings.Contains(embed.Description, "[🦊 Kitsu](") || !strings.Contains(embed.Description, "[📁 Drive](") {
		t.Fatalf("serialized fields contract mismatch: %+v", embed.Fields)
	}
	if !strings.Contains(string(raw), "[📁 Drive](") || strings.Contains(string(raw), "Open") {
		t.Fatalf("serialized payload is missing the required Drive folder label: %s", raw)
	}
	if strings.Contains(string(raw), "<@") || len(got.AllowedMentions.Users) != 0 || len(got.AllowedMentions.Parse) != 0 || len(got.AllowedMentions.Roles) != 0 {
		t.Fatalf("serialized payload permits an assignee mention: %s", raw)
	}
}

func TestStatusChangeDeliverySerializesTheCanonicalCard(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	oldPublicURLResolver := KitsuPublicURLResolver
	oldDriveURLResolver := GoogleDriveURLResolver
	oldUserMapResolver := UserMapResolver
	oldCheckerResolver := CheckerResolver
	KitsuPublicURLResolver = func() string { return "https://kitsu.example.com" }
	GoogleDriveURLResolver = nil
	UserMapResolver = func(_, _, _ string) string { return "101" }
	CheckerResolver = func(_, _ string) []string { return []string{"202"} }
	t.Cleanup(func() {
		KitsuPublicURLResolver = oldPublicURLResolver
		GoogleDriveURLResolver = oldDriveURLResolver
		UserMapResolver = oldUserMapResolver
		CheckerResolver = oldCheckerResolver
	})

	var sent Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("delivery method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"message-1"}`))
	}))
	defer server.Close()

	var event kitsu.MessagePayload
	event.Project.Project = kitsu.Project{ID: "production-1", Name: "KitsuSync Local Test"}
	event.Entity.Entity = kitsu.Entity{ID: "shot-1", Name: "sh001"}
	event.EntityType.EntityType = kitsu.EntityType{ID: "shot", Name: "Shot"}
	event.Parent.Entity = kitsu.Entity{ID: "sequence-1", Name: "sc001"}
	event.Task.Task = kitsu.Task{ID: "task-1"}
	event.TaskType.TaskType = kitsu.TaskType{ID: "compositing", Name: "Compositing"}
	event.TaskStatus.TaskStatus = kitsu.TaskStatus{ID: "retake", ShortName: "RETAKE"}
	event.PreviousStatusName = "WFA"
	event.Assignees = []kitsu.Person{{FirstName: "侑恭", LastName: "松尾", FullName: "侑恭 松尾"}}

	conf := config.Config{TplPreset: "legacy"}
	conf.GoogleDrive.URL = "https://drive.example.com/folder"
	results := SendMessageBunch(conf, []kitsu.MessagePayload{event}, server.URL, nil, nil, nil, map[string]string{"production-1": "en"}, nil)
	if results["task-1"].MessageID != "message-1" {
		t.Fatalf("status-change delivery did not complete: %+v", results)
	}
	if len(sent.Embeds) != 1 {
		t.Fatalf("sent embed count = %d", len(sent.Embeds))
	}
	embed := sent.Embeds[0]
	if embed.Author.Name != "Compositing" || embed.Title != "" || embed.Url != "" {
		t.Fatalf("status-change delivery used the wrong author/title path: %+v", embed)
	}
	if !strings.Contains(embed.Description, "## Shot / sc001 - sh001\n\n### 🔄 RETAKE\nA revision is needed") {
		t.Fatalf("status-change delivery did not use the canonical description: %q", embed.Description)
	}
	if embed.Footer.Text != "" {
		t.Fatalf("status-change delivery kept a Production footer: %q", embed.Footer.Text)
	}
	if len(embed.Fields) != 0 || !strings.Contains(embed.Description, "[🦊 Kitsu](") || !strings.Contains(embed.Description, "[📁 Drive](") {
		t.Fatalf("status-change delivery fields mismatch: %+v", embed.Fields)
	}
	if !strings.Contains(sent.Content, "<@101>") || len(sent.AllowedMentions.Users) != 1 || sent.AllowedMentions.Users[0] != "101" || len(sent.AllowedMentions.Parse) != 0 || len(sent.AllowedMentions.Roles) != 0 {
		t.Fatalf("status-change delivery did not allow only the mapped assignee: %+v", sent.AllowedMentions)
	}
	if strings.Contains(embed.Description, "<@") {
		t.Fatalf("status-change delivery put a mention in the assignee metadata: %q", embed.Description)
	}
}

func TestNotificationCardOmitsProductionFooterWithoutTestContext(t *testing.T) {
	useRepositoryRootForTemplates(t)
	payload := RenderNotificationPayload(Template{
		ProjectName: "KitsuSync Local Test", EntityType: "Shot", TaskName: "cut001",
		TaskType: "Animation", StatusUpper: "DONE", StatusEmoji: "✅", StatusMessage: "Completed",
		TaskURL: "https://kitsu.example.com/tasks/cut001", NotificationLanguage: "en",
	}, "rich")
	if payload.Embeds[0].Footer.Text != "" {
		t.Fatalf("Production footer was not omitted: %q", payload.Embeds[0].Footer.Text)
	}
	if !strings.Contains(payload.Embeds[0].Description, "[🦊 Kitsu](") {
		t.Fatalf("configured Public Kitsu link was not rendered: %+v", payload.Embeds[0].Fields)
	}
}

func TestNotificationCardFixturesCoverStatusCommentsLinksAndPreview(t *testing.T) {
	useRepositoryRootForTemplates(t)
	cases := []struct {
		name, status, message string
		commentOnly           bool
		drive, preview        bool
	}{
		{"WFA", "WFA", "Please review", false, true, true},
		{"RETAKE", "RETAKE", "A revision is needed", false, false, false},
		{"DONE", "DONE", "Completed. Please check if needed.", false, true, false},
		{"comment-only-WFA", "WFA", "Please review", true, false, false},
		{"comment-only-RETAKE", "RETAKE", "A revision is needed", true, false, false},
		{"comment-only-DONE", "DONE", "Completed. Please check if needed.", true, false, false},
	}
	for _, tc := range cases {
		data := Template{
			TaskType:                "Compositing",
			StatusUpper:             tc.status,
			StatusMessage:           tc.message,
			StatusEmoji:             "•",
			CommentOnlyMessage:      "Revision details were updated",
			IsCommentOnly:           tc.commentOnly,
			StatusTransitionMessage: "A transition",
			PreviousStatus:          "WFA",
			CommentLabel:            "Comment",
			CommentContent:          "Please adjust this area",
			AssigneeLabel:           "Assignee",
			AssigneesStr:            "Artist A, Artist B",
			NotificationLanguage:    "en",
		}
		if tc.drive {
			data.GoogleDriveURL = "https://drive.example/folder"
		}
		if tc.preview {
			data.PreviewImageURL = "https://kitsu.example/preview.png"
		}
		payload := RenderNotificationPayload(data, "rich")
		embed := payload.Embeds[0]
		if tc.commentOnly && strings.Contains(embed.Description, "A transition") {
			t.Fatalf("%s fixture contains a fake transition: %q", tc.name, embed.Description)
		}
		if strings.Contains(embed.Description, "A transition") {
			t.Fatalf("%s fixture contains legacy transition text in description: %q", tc.name, embed.Description)
		}
		if (embed.Image != nil) != tc.preview {
			t.Fatalf("%s preview presence = %v, want %v", tc.name, embed.Image != nil, tc.preview)
		}
		if tc.drive != strings.Contains(embed.Description, "[📁 Drive](") {
			t.Fatalf("%s drive presence mismatch: %q", tc.name, embed.Description)
		}
	}
}

func TestNotificationCardLinksRequireValidURLsAndOmitUnavailableLinks(t *testing.T) {
	useRepositoryRootForTemplates(t)
	cases := []struct {
		name, drive, kitsu, want string
	}{
		{"both", "https://drive.example/folder", "https://kitsu.example/tasks/1", "Drive"},
		{"drive-only", "https://drive.example/folder", "", "Drive"},
		{"kitsu-only", "", "https://kitsu.example/tasks/1", "Kitsu"},
		{"neither", "", "", ""},
		{"malformed-drive", "not a url", "https://kitsu.example/tasks/1", "Kitsu"},
		{"unreliable-kitsu", "https://drive.example/folder", "", "Drive"},
	}
	for _, tc := range cases {
		payload := RenderNotificationPayload(Template{
			TaskType: "Animation", StatusUpper: "WFA", StatusEmoji: "👀", StatusMessage: "Please review",
			AssigneeLabel: "Assignee", AssigneesStr: "Unassigned", NotificationLanguage: "en",
			GoogleDriveURL: tc.drive, TaskURL: tc.kitsu,
		}, "rich")
		linkText := payload.Embeds[0].Description
		if tc.want == "" {
			if strings.Contains(linkText, "[🦊 Kitsu](") || strings.Contains(linkText, "[📁 Drive](") {
				t.Fatalf("%s links = %q, want omitted", tc.name, linkText)
			}
			continue
		}
		if !strings.Contains(linkText, tc.want) {
			t.Fatalf("%s links = %q, want %q", tc.name, linkText, tc.want)
		}
	}
}

func TestKitsuTaskURLUsesVerifiedFrontendRoute(t *testing.T) {
	if got := KitsuTaskURL("https://kitsu.example.com/", "production-1", "Shot", "task-1"); got != "https://kitsu.example.com/productions/production-1/shots/tasks/task-1" {
		t.Fatalf("shot task URL = %q", got)
	}
	if got := KitsuTaskURL("https://kitsu.example.com/team/kitsu/", "production-1", "Shot", "task-1"); got != "https://kitsu.example.com/team/kitsu/productions/production-1/shots/tasks/task-1" {
		t.Fatalf("subpath shot task URL = %q", got)
	}
	if got := KitsuTaskURL("https://kitsu.example.com", "production-1", "Asset", "task-1"); got != "https://kitsu.example.com/productions/production-1/assets/tasks/task-1" {
		t.Fatalf("asset task URL = %q", got)
	}
	if got := KitsuTaskURL("https://kitsu.example.com", "production-1", "Sequence", "task-1"); got != "" {
		t.Fatalf("unsupported entity type URL = %q, want omitted", got)
	}
	if got := KitsuTaskURL("not-a-url", "production-1", "Shot", "task-1"); got != "" {
		t.Fatalf("malformed base URL = %q, want omitted", got)
	}
	for _, local := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://192.168.1.20:8080", "http://kitsu.lan:8080"} {
		if got := KitsuTaskURL(local, "production-1", "Shot", "task-1"); got == "" {
			t.Fatalf("explicit human-facing fallback URL was rejected: %q", local)
		}
	}
	if got := KitsuTaskURL("http://host.docker.internal:8080", "production-1", "Shot", "task-1"); got != "" {
		t.Fatalf("runtime-generated host leaked into card: %q", got)
	}
	if got := KitsuTaskURL("https://kitsu.example.com", "production-1", "Shot", ""); got != "" {
		t.Fatalf("missing task ID URL = %q, want omitted", got)
	}
}

func TestRenderNotificationPayloadFitsDiscordEmbedTextLimit(t *testing.T) {
	useRepositoryRootForTemplates(t)
	payload := RenderNotificationPayload(Template{
		TaskType:             "Animation",
		StatusUpper:          "WFA",
		StatusEmoji:          "👀",
		StatusMessage:        strings.Repeat("x", 4096),
		CommentLabel:         "Comment",
		CommentContent:       strings.Repeat("y", 4096),
		AssigneeLabel:        "Assignee",
		AssigneesStr:         strings.Repeat("z", 1024),
		NotificationLanguage: "en",
	}, "rich")
	if got := embedTextLength(payload.Embeds[0]); got > maxDiscordEmbedText {
		t.Fatalf("embed text length = %d, exceeds %d", got, maxDiscordEmbedText)
	}
}

func testMentionConfig() config.Config {
	return config.Config{}
}

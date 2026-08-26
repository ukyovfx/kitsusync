package discord

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"text/template"
)

func renderRichTemplate(t *testing.T, name string, data Template) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("failed to resolve current test file")
	}
	packageDir := filepath.Dir(currentFile)
	repoRoot := filepath.Clean(filepath.Join(packageDir, "..", "..", ".."))
	tplPath := filepath.Join(repoRoot, "tpl", "rich", name)

	content, err := os.ReadFile(tplPath)
	if err != nil {
		t.Fatalf("read %s: %v", tplPath, err)
	}

	tpl, err := template.New(name).Funcs(template.FuncMap{
		"json": func(value string) string {
			encoded, _ := json.Marshal(value)
			return strings.Trim(string(encoded), "\"")
		},
	}).Parse(string(content))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		t.Fatalf("execute %s: %v", name, err)
	}

	return strings.TrimSpace(out.String())
}

func TestLocalizedStatusTransitionMessage_CompactJA(t *testing.T) {
	cases := []struct {
		prev, current, want string
	}{
		{"WIP", "WFA", "📩 チェック依頼が送られました"},
		{"WFA", "RETAKE", "レビュー結果により修正が必要です"},
		{"WFA", "DONE", "レビューが完了しました"},
		{"WFA", "WIP", "🛠 修正作業に戻りました"},
		{"RETAKE", "WIP", "🛠 リテイク対応を開始しました"},
		{"WIP", "RETAKE", "🔁 リテイクが入りました"},
		{"TODO", "WIP", "🚀 作業を開始しました"},
		{"NONE", "WIP", "🚀 作業を開始しました"},
		{"READY", "DONE", "✅ 作業完了・承認されました"},
	}

	for _, tc := range cases {
		got := localizedStatusTransitionMessage(tc.prev, tc.current, "ja")
		if got != tc.want {
			t.Fatalf("%s->%s: want %q, got %q", tc.prev, tc.current, tc.want, got)
		}
	}
}

func TestRichDescription_CompactStatusAndActionLines(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		prev       string
		statusLine string
		actionLine string
		transLine  string
	}{
		{
			name:       "wfa",
			status:     "WFA",
			prev:       "WIP",
			statusLine: "👀 WFA",
			actionLine: "チェックをお願いします",
			transLine:  "📩 チェック依頼が送られました",
		},
		{
			name:       "retake",
			status:     "RETAKE",
			prev:       "WFA",
			statusLine: "🔄 RETAKE",
			actionLine: "修正をお願いします",
			transLine:  "レビュー結果により修正が必要です",
		},
		{
			name:       "done",
			status:     "DONE",
			prev:       "WFA",
			statusLine: "✅ DONE",
			actionLine: "完了しました。必要に応じてご確認ください。",
			transLine:  "レビューが完了しました",
		},
	}

	for _, tc := range cases {
		data := Template{
			StatusEmoji:             strings.Split(tc.statusLine, " ")[0],
			StatusUpper:             tc.status,
			StatusMessage:           localizedStatusMessageInfoOrFail(t, tc.status),
			StatusTransitionMessage: localizedStatusTransitionMessage(tc.prev, tc.status, "ja"),
			CommentAuthor:           "山田太郎",
			CommentContent:          "確認をお願いします",
			CommentLabel:            "コメント",
			CommentAuthorLabel:      "コメント投稿者",
			LinksLabel:              "リンク",
			TaskURL:                 "https://kitsu.example.com/task/1",
			GoogleDriveURL:          "https://drive.example.com/folder/1",
		}
		rendered := renderRichTemplate(t, "description.tpl", data)
		mustContain(t, rendered, tc.statusLine)
		mustContain(t, rendered, tc.actionLine)
		mustContain(t, rendered, tc.transLine)
		mustContain(t, rendered, "コメント投稿者: 山田太郎")
		mustContain(t, rendered, "[🦊 KITSU](https://kitsu.example.com/task/1)")
		mustContain(t, rendered, "[📁 Drive](https://drive.example.com/folder/1)")
	}
}

func TestRichDescription_DriveAndCommentAreConditional(t *testing.T) {
	data := Template{
		StatusEmoji:             "👀",
		StatusUpper:             "WFA",
		StatusMessage:           "チェックをお願いします",
		StatusTransitionMessage: "📩 チェック依頼が送られました",
		CommentAuthor:           "田中花子",
		CommentContent:          "",
		TaskURL:                 "https://kitsu.example.com/task/2",
		GoogleDriveURL:          "",
	}

	rendered := renderRichTemplate(t, "description.tpl", data)
	mustContain(t, rendered, "[🦊 KITSU](https://kitsu.example.com/task/2)")
	if strings.Contains(rendered, "📁 Drive") {
		t.Fatalf("drive link should not be rendered when storage URL is empty: %q", rendered)
	}
	if strings.Contains(rendered, "💬 ") {
		t.Fatalf("comment block should not be rendered when comment is empty: %q", rendered)
	}
}

func TestRichFields_RemainValidJSONAndMinimal(t *testing.T) {
	data := Template{
		PreviousStatus: "WIP",
		CurrentStatus:  "WFA",
		AssigneesStr:   `A "quoted", B`,
		AssigneeLabel:  "担当",
		GoogleDriveURL: "https://drive.example.com/folder/1",
	}
	rendered := renderRichTemplate(t, "fields.tpl", data)

	var fields []EmbedField
	if err := json.Unmarshal([]byte(rendered), &fields); err != nil {
		t.Fatalf("fields JSON must be valid: %v\nraw=%s", err, rendered)
	}
	if len(fields) != 1 {
		t.Fatalf("expected 1 compact field, got %d", len(fields))
	}
	if fields[0].Name != "担当" {
		t.Fatalf("unexpected field: %+v", fields[0])
	}
}

func localizedStatusMessageInfoOrFail(t *testing.T, status string) string {
	t.Helper()
	msg, _ := localizedStatusMessageInfo(status, "ja")
	if msg == "" {
		t.Fatalf("expected localized status message for %s", status)
	}
	return msg
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q in rendered output:\n%s", want, got)
	}
}

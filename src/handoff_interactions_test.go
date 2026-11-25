package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"app/src/api/discord"
	"app/src/model"
	"app/src/setup"
	"app/src/utils/request"

	"gorm.io/gorm"
)

// interactionResponseForTest decodes the endpoint's JSON answers.
type interactionResponseForTest struct {
	Type int `json:"type"`
	Data struct {
		Content    string              `json:"content"`
		Flags      int                 `json:"flags"`
		CustomID   string              `json:"custom_id"`
		Components []discord.ActionRow `json:"components"`
	} `json:"data"`
}

// configureInteractionKey generates a signing key pair and stores the
// public half where production stores the Discord application key.
func configureInteractionKey(t *testing.T, db *gorm.DB) ed25519.PrivateKey {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, setup.DiscordApplicationPublicKeySettingKey, hex.EncodeToString(public))
	return private
}

// serveInteraction signs the body the way Discord does and runs the handler.
func serveInteraction(t *testing.T, db *gorm.DB, private ed25519.PrivateKey, body []byte) (int, interactionResponseForTest) {
	t.Helper()
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	signed := append([]byte(timestamp), body...)
	req := httptest.NewRequest(http.MethodPost, "/interactions", bytes.NewReader(body))
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, signed)))
	req.Header.Set("X-Signature-Timestamp", timestamp)
	rec := httptest.NewRecorder()
	handoffInteractionsHandler(db).ServeHTTP(rec, req)
	var decoded interactionResponseForTest
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("response is not interaction JSON: %v (%s)", err, rec.Body.String())
		}
	}
	return rec.Code, decoded
}

// interactionUserJSON is one Discord user object of a test interaction.
func interactionUserJSON(id string) map[string]interface{} {
	return map[string]interface{}{"id": id, "username": "comp", "global_name": "Comp Artist"}
}

// componentInteractionBody builds a button-press callback on a message
// carrying the given rows.
func componentInteractionBody(t *testing.T, customID, userID string, rows []discord.ActionRow) []byte {
	t.Helper()
	payload := map[string]interface{}{
		"type":     interactionTypeMessageComponent,
		"guild_id": "guild-1",
		"data":     map[string]interface{}{"custom_id": customID, "component_type": 2},
		"member":   map[string]interface{}{"user": interactionUserJSON(userID)},
		"message":  map[string]interface{}{"id": "msg-1", "content": "", "components": rows},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// capturedReplyWrite records what the handler tried to ghostwrite.
type capturedReplyWrite struct {
	calls  int
	taskID string
	text   string
	err    error
}

func stubReplyWrite(t *testing.T) *capturedReplyWrite {
	t.Helper()
	captured := &capturedReplyWrite{}
	old := writeHandoffReplyComment
	writeHandoffReplyComment = func(_ context.Context, event model.HandoffEvent, text string) error {
		captured.calls++
		captured.taskID = event.SourceTaskID
		captured.text = text
		return captured.err
	}
	t.Cleanup(func() { writeHandoffReplyComment = old })
	return captured
}

// interactionEventDB seeds one notified handoff for person-comp and the
// message rows its buttons ride on.
func interactionEventDB(t *testing.T) (*gorm.DB, ed25519.PrivateKey, []discord.ActionRow) {
	t.Helper()
	db := handoffReplyDB(t, model.HandoffTimingMixed)
	handoffEvent(t, db, "comment-1", model.HandoffFallbackNone, "person-comp")
	event, err := model.FindHandoffEvent(db, "comment-1")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := model.MarkHandoffNotified(db, event.ID); err != nil || !ok {
		t.Fatalf("notify: ok=%v err=%v", ok, err)
	}
	private := configureInteractionKey(t, db)
	event, err = model.FindHandoffEvent(db, "comment-1")
	if err != nil {
		t.Fatal(err)
	}
	rows := handoffReplyRows(db, []model.HandoffEvent{event}, "ja")
	if rows[0].Components[0].Disabled {
		t.Fatal("seeded rows must be wired once the key is configured")
	}
	return db, private, rows
}

// okCustomID is the seeded event's button ID, built the way production
// builds it.
func okCustomID(rows []discord.ActionRow) string {
	return rows[0].Components[0].CustomID
}

func TestHandoffInteractionsAnswerPing(t *testing.T) {
	db := handoffReplyDB(t, model.HandoffTimingMixed)
	private := configureInteractionKey(t, db)
	before := time.Now().UTC().Unix()
	code, response := serveInteraction(t, db, private, []byte(`{"type":1}`))
	if code != http.StatusOK || response.Type != interactionResponsePong {
		t.Fatalf("code=%d response=%+v", code, response)
	}
	after := time.Now().UTC().Unix()
	raw := model.GetSetting(db, setup.DiscordInteractionsLastPingAtSettingKey)
	got, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || got < before || got > after {
		t.Fatalf("expected recorded ping timestamp between %d and %d, got %q (err=%v)", before, after, raw, err)
	}
}

// A callback that does not verify — wrong key, tampered body, or no key
// configured at all — is rejected before anything is read from it.
func TestHandoffInteractionsRejectUnverifiableCallbacks(t *testing.T) {
	db := handoffReplyDB(t, model.HandoffTimingMixed)
	private := configureInteractionKey(t, db)

	_, otherPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := serveInteraction(t, db, otherPrivate, []byte(`{"type":1}`)); code != http.StatusUnauthorized {
		t.Fatalf("wrong key: code=%d", code)
	}
	if got := model.GetSetting(db, setup.DiscordInteractionsLastPingAtSettingKey); got != "" {
		t.Fatalf("invalid signature must not record ping timestamp, got %q", got)
	}

	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	body := []byte(`{"type":1}`)
	req := httptest.NewRequest(http.MethodPost, "/interactions", bytes.NewReader([]byte(`{"type":2}`)))
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, append([]byte(timestamp), body...))))
	req.Header.Set("X-Signature-Timestamp", timestamp)
	rec := httptest.NewRecorder()
	handoffInteractionsHandler(db).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered body: code=%d", rec.Code)
	}
	if got := model.GetSetting(db, setup.DiscordInteractionsLastPingAtSettingKey); got != "" {
		t.Fatalf("tampered body must not record ping timestamp, got %q", got)
	}

	model.DeleteSetting(db, setup.DiscordApplicationPublicKeySettingKey)
	if code, _ := serveInteraction(t, db, private, []byte(`{"type":1}`)); code != http.StatusUnauthorized {
		t.Fatalf("no key configured: code=%d", code)
	}
	if got := model.GetSetting(db, setup.DiscordInteractionsLastPingAtSettingKey); got != "" {
		t.Fatalf("missing key must not record ping timestamp, got %q", got)
	}
}

func TestHandoffInteractionsTimestampReplayWindow(t *testing.T) {
	db := handoffReplyDB(t, model.HandoffTimingMixed)
	private := configureInteractionKey(t, db)
	body := []byte(`{"type":1}`)

	cases := []struct {
		name      string
		timestamp string
		wantCode  int
	}{
		{
			name:      "fresh",
			timestamp: strconv.FormatInt(time.Now().UTC().Unix(), 10),
			wantCode:  http.StatusOK,
		},
		{
			name:      "too old",
			timestamp: strconv.FormatInt(time.Now().UTC().Add(-6*time.Minute).Unix(), 10),
			wantCode:  http.StatusUnauthorized,
		},
		{
			name:      "too far in future",
			timestamp: strconv.FormatInt(time.Now().UTC().Add(6*time.Minute).Unix(), 10),
			wantCode:  http.StatusUnauthorized,
		},
		{
			name:      "missing",
			timestamp: "",
			wantCode:  http.StatusUnauthorized,
		},
		{
			name:      "unparseable",
			timestamp: "not-a-timestamp",
			wantCode:  http.StatusUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/interactions", bytes.NewReader(body))
			signed := append([]byte(tc.timestamp), body...)
			req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, signed)))
			if tc.timestamp != "" {
				req.Header.Set("X-Signature-Timestamp", tc.timestamp)
			}
			rec := httptest.NewRecorder()
			handoffInteractionsHandler(db).ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("%s: code=%d, want %d (body=%s)", tc.name, rec.Code, tc.wantCode, rec.Body.String())
			}
		})
	}
}

// The OK press of the addressed receiver writes the ghostwritten comment on
// the source task, moves the event to answered, and returns the message
// with the handoff's row disabled and marked.
func TestHandoffInteractionsOKWritesTheReply(t *testing.T) {
	db, private, rows := interactionEventDB(t)
	captured := stubReplyWrite(t)

	body := componentInteractionBody(t, okCustomID(rows), "222222222222222222", rows)
	code, response := serveInteraction(t, db, private, body)
	if code != http.StatusOK || response.Type != interactionResponseUpdateMessage {
		t.Fatalf("code=%d response=%+v", code, response)
	}
	if captured.calls != 1 || captured.taskID != "task-lay-1" {
		t.Fatalf("write = %+v", captured)
	}
	if !strings.Contains(captured.text, "OK（受け取りを確認しました）") ||
		!strings.Contains(captured.text, "kitsusync-replier: Comp Artist") {
		t.Fatalf("comment = %q", captured.text)
	}
	if got := handoffState(t, db, "comment-1"); got != model.HandoffEventAnswered {
		t.Fatalf("state = %q", got)
	}
	if len(response.Data.Components) != 1 || len(response.Data.Components[0].Components) != 1 {
		t.Fatalf("components = %+v", response.Data.Components)
	}
	button := response.Data.Components[0].Components[0]
	if !button.Disabled {
		t.Fatalf("answered button must be disabled: %+v", button)
	}
	if button.Label != "OK（回答済み）" {
		t.Fatalf("ok label = %q", button.Label)
	}
}

// A press from anyone else is refused ephemerally and changes nothing.
func TestHandoffInteractionsRefuseNonRecipients(t *testing.T) {
	db, private, rows := interactionEventDB(t)
	captured := stubReplyWrite(t)

	body := componentInteractionBody(t, okCustomID(rows), "333333333333333333", rows)
	code, response := serveInteraction(t, db, private, body)
	if code != http.StatusOK || response.Type != interactionResponseMessage || response.Data.Flags != interactionEphemeralFlag {
		t.Fatalf("code=%d response=%+v", code, response)
	}
	if !strings.Contains(response.Data.Content, "宛先ではありません") {
		t.Fatalf("content = %q", response.Data.Content)
	}
	if captured.calls != 0 {
		t.Fatalf("write calls = %d", captured.calls)
	}
	if got := handoffState(t, db, "comment-1"); got != model.HandoffEventNotified {
		t.Fatalf("state = %q", got)
	}
}

// Ruling I: there is no Retake button and no modal. A retake-shaped press
// (an old message, a forged one) is an unknown component, and a modal
// submission is an unsupported interaction type; neither moves anything.
func TestHandoffInteractionsRefuseRetakeAndModal(t *testing.T) {
	db, private, rows := interactionEventDB(t)
	captured := stubReplyWrite(t)

	retakeID := "handoff:retake:" + okCustomID(rows)[len("handoff:ok:"):]
	body := componentInteractionBody(t, retakeID, "222222222222222222", rows)
	if code, _ := serveInteraction(t, db, private, body); code != http.StatusBadRequest {
		t.Fatalf("retake press: code=%d", code)
	}

	modalBody, err := json.Marshal(map[string]interface{}{
		"type":     5,
		"guild_id": "guild-1",
		"data":     map[string]interface{}{"custom_id": "handoff:modal:1"},
		"member":   map[string]interface{}{"user": interactionUserJSON("222222222222222222")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := serveInteraction(t, db, private, modalBody); code != http.StatusBadRequest {
		t.Fatalf("modal submit: code=%d", code)
	}
	if captured.calls != 0 {
		t.Fatalf("write calls = %d", captured.calls)
	}
	if got := handoffState(t, db, "comment-1"); got != model.HandoffEventNotified {
		t.Fatalf("state = %q", got)
	}
}

// A second press after the answer is refused and writes nothing more.
func TestHandoffInteractionsSecondPressWritesNothing(t *testing.T) {
	db, private, rows := interactionEventDB(t)
	captured := stubReplyWrite(t)

	body := componentInteractionBody(t, okCustomID(rows), "222222222222222222", rows)
	if code, _ := serveInteraction(t, db, private, body); code != http.StatusOK {
		t.Fatalf("first press code=%d", code)
	}
	code, response := serveInteraction(t, db, private, body)
	if code != http.StatusOK || response.Type != interactionResponseMessage || response.Data.Flags != interactionEphemeralFlag {
		t.Fatalf("second press: code=%d response=%+v", code, response)
	}
	if !strings.Contains(response.Data.Content, "すでに回答済み") {
		t.Fatalf("content = %q", response.Data.Content)
	}
	if captured.calls != 1 {
		t.Fatalf("write calls = %d", captured.calls)
	}
}

// When the Kitsu write fails, the claim is handed back: the event waits
// again and the next press can complete the reply.
func TestHandoffInteractionsWriteFailureReturnsTheClaim(t *testing.T) {
	db, private, rows := interactionEventDB(t)
	captured := stubReplyWrite(t)
	captured.err = errors.New("kitsu unreachable")

	body := componentInteractionBody(t, okCustomID(rows), "222222222222222222", rows)
	code, response := serveInteraction(t, db, private, body)
	if code != http.StatusOK || response.Data.Flags != interactionEphemeralFlag {
		t.Fatalf("code=%d response=%+v", code, response)
	}
	if !strings.Contains(response.Data.Content, "失敗しました") {
		t.Fatalf("content = %q", response.Data.Content)
	}
	if got := handoffState(t, db, "comment-1"); got != model.HandoffEventNotified {
		t.Fatalf("state after failure = %q", got)
	}

	captured.err = nil
	if code, response := serveInteraction(t, db, private, body); code != http.StatusOK || response.Type != interactionResponseUpdateMessage {
		t.Fatalf("retry: code=%d response=%+v", code, response)
	}
	if got := handoffState(t, db, "comment-1"); got != model.HandoffEventAnswered {
		t.Fatalf("state after retry = %q", got)
	}
}

// The real write path, against a fake Kitsu: the reply lands on the source
// task's comments through the actions endpoint (Zou 1.1.1 rejects comment
// writes on the data route with 405), carrying the task's current status
// so the comment alone never moves the task.
func TestHandoffReplyWritePostsToTheSourceTaskWithItsStatus(t *testing.T) {
	var postedPath string
	var postedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/data/tasks/task-lay-1":
			_, _ = w.Write([]byte(`{"id":"task-lay-1","task_status_id":"status-current"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/actions/tasks/task-lay-1/comment":
			postedPath = r.URL.Path
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &postedBody); err != nil {
				t.Errorf("posted body: %v", err)
			}
			_, _ = w.Write([]byte(`{"id":"comment-reply"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("KITSU_API_BASE_URL", server.URL+"/api/")
	t.Setenv("KITSU_HOSTNAME", "")
	t.Setenv("KitsuJWTToken", "reply-token")
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}

	event := model.HandoffEvent{CommentID: "comment-1", SourceTaskID: "task-lay-1"}
	if err := writeHandoffReplyComment(context.Background(), event, "OK（受け取りを確認しました）"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if postedPath != "/api/actions/tasks/task-lay-1/comment" {
		t.Fatalf("posted path = %q", postedPath)
	}
	if postedBody["task_status_id"] != "status-current" || postedBody["comment"] != "OK（受け取りを確認しました）" {
		t.Fatalf("posted body = %+v", postedBody)
	}
}

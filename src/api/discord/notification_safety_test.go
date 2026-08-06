package discord

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscordHTTPErrorCategories(t *testing.T) {
	cases := map[int]string{
		400: "invalid_payload",
		401: "invalid_token",
		403: "missing_permission",
		404: "missing_destination",
		429: "rate_limited",
		500: "discord_server_error",
		418: "discord_http_error",
	}
	for status, want := range cases {
		if got := discordHTTPErrorCategory(status); got != want {
			t.Fatalf("status %d: want %q, got %q", status, want, got)
		}
	}
}

func TestPayloadDisablesImplicitMentions(t *testing.T) {
	payload := Payload{
		Content:         "@everyone <@123> <@&456>",
		AllowedMentions: &AllowedMentions{Users: []string{"123"}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(body)
	if strings.Contains(raw, `"parse"`) || !strings.Contains(raw, `"users":["123"]`) {
		t.Fatalf("payload does not explicitly constrain mentions: %s", raw)
	}
}

func TestSanitizeDiscordTextRemovesRawMarkupAndBroadcastMentions(t *testing.T) {
	got := sanitizeDiscordText("<b>@everyone</b> @here")
	if strings.Contains(got, "<") || strings.Contains(got, ">") || strings.Contains(got, "@everyone") || strings.Contains(got, "@here") {
		t.Fatalf("unsafe source text remained: %q", got)
	}
}

func TestSendMessageRetriesRateLimitThenSucceeds(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"message-1"}`))
	}))
	defer server.Close()

	result := SendMessage(Payload{Content: "safe"}, server.URL, "", "")
	if result.MessageID != "message-1" || attempts != 2 {
		t.Fatalf("expected one rate-limit retry and success, result=%+v attempts=%d", result, attempts)
	}
}

func TestSendMessageClassifiesPermanentPayloadFailureWithoutRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	result := SendMessage(Payload{Content: "safe"}, server.URL, "", "")
	if result.MessageID != "" || result.FailureCategory != "invalid_payload" || result.Retryable || attempts != 1 {
		t.Fatalf("expected permanent payload failure, result=%+v attempts=%d", result, attempts)
	}
}

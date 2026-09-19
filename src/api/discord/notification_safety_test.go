package discord

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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
	var retryAt time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0.1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		retryAt = time.Now()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"message-1"}`))
	}))
	defer server.Close()

	started := time.Now()
	result := SendMessage(Payload{Content: "safe"}, server.URL, "", "")
	if result.MessageID != "message-1" || attempts != 2 {
		t.Fatalf("expected one rate-limit retry and success, result=%+v attempts=%d", result, attempts)
	}
	if retryAt.Sub(started) < 100*time.Millisecond {
		t.Fatalf("retry ignored Retry-After: waited %s", retryAt.Sub(started))
	}
}

func TestSendMessageDefersRateLimitBeyondRetryWaitCap(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "31")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	result := SendMessage(Payload{Content: "safe"}, server.URL, "", "")
	if result.FailureCategory != "rate_limited" || !result.Retryable || result.Unknown || attempts != 1 {
		t.Fatalf("expected bounded rate-limit deferral, result=%+v attempts=%d", result, attempts)
	}
}

func TestSendMessageClassifiesPermanentPayloadFailureWithoutRetry(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts++
				w.WriteHeader(status)
			}))
			defer server.Close()

			result := SendMessage(Payload{Content: "safe"}, server.URL, "", "")
			if result.MessageID != "" || result.Retryable || result.Unknown || attempts != 1 {
				t.Fatalf("expected definitive client failure without retry, result=%+v attempts=%d", result, attempts)
			}
		})
	}
}

func TestSendMessageTransportFailureIsUnknownWithoutRetry(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	attempts := 0
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("connection reset after send")
	})

	result := SendMessage(Payload{Content: "safe"}, "https://discord.example/webhook", "", "")
	if result.FailureCategory != "network_error" || !result.Unknown || result.Retryable || attempts != 1 {
		t.Fatalf("expected one unknown transport outcome, result=%+v attempts=%d", result, attempts)
	}
}

func TestSendMessageServerFailureIsUnknownWithoutRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	result := SendMessage(Payload{Content: "safe"}, server.URL, "", "")
	if result.FailureCategory != "discord_server_error" || !result.Unknown || result.Retryable || attempts != 1 {
		t.Fatalf("expected one unknown server outcome, result=%+v attempts=%d", result, attempts)
	}
}

func TestSendMessageMarksMalformedOrUnusableSuccessResponseUnknown(t *testing.T) {
	for _, body := range []string{"not-json", `{}`} {
		t.Run(body, func(t *testing.T) {
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts++
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			result := SendMessage(Payload{Content: "safe"}, server.URL, "", "")
			if result.MessageID != "" || result.FailureCategory != "unknown_response" || !result.Unknown || result.Retryable || attempts != 1 {
				t.Fatalf("expected one unknown response outcome, result=%+v attempts=%d", result, attempts)
			}
		})
	}
}

func TestSendMessageReturnsDiscordMessageIDOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"message-1"}`))
	}))
	defer server.Close()

	result := SendMessage(Payload{Content: "safe"}, server.URL, "", "")
	if result.MessageID != "message-1" || result.Unknown || result.FailureCategory != "" {
		t.Fatalf("normal success changed: %+v", result)
	}
}

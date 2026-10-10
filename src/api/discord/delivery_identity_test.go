package discord

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyWebhookIdentityRequiresExactGuildChannelAndWebhook(t *testing.T) {
	for _, tc := range []struct {
		name, id, guild, channel string
		status                   int
		wantErr                  bool
	}{
		{name: "exact", id: "webhook-1", guild: "guild-1", channel: "channel-1", status: http.StatusOK},
		{name: "wrong webhook", id: "webhook-2", guild: "guild-1", channel: "channel-1", status: http.StatusOK, wantErr: true},
		{name: "wrong guild", id: "webhook-1", guild: "guild-2", channel: "channel-1", status: http.StatusOK, wantErr: true},
		{name: "wrong channel", id: "webhook-1", guild: "guild-1", channel: "channel-2", status: http.StatusOK, wantErr: true},
		{name: "unavailable", id: "", guild: "", channel: "", status: http.StatusForbidden, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("identity verification must be read-only GET, got %s", r.Method)
				}
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					_, _ = fmt.Fprintf(w, `{"id":%q,"type":1,"guild_id":%q,"channel_id":%q}`, tc.id, tc.guild, tc.channel)
				}
			}))
			defer server.Close()
			err := VerifyWebhookIdentity(server.URL+"/api/webhooks/webhook-1/secret-token", "webhook-1", "guild-1", "channel-1")
			if (err != nil) != tc.wantErr {
				t.Fatalf("identity verification error=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestVerifyWebhookIdentityRejectsRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://discord.com/", http.StatusFound)
	}))
	defer server.Close()
	if err := VerifyWebhookIdentity(server.URL+"/api/webhooks/webhook-1/token", "webhook-1", "guild-1", "channel-1"); err == nil {
		t.Fatal("redirected identity verification must fail closed")
	}
}

func TestWebhookMessagePostDoesNotFollowRedirect(t *testing.T) {
	targetHits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"message-1","channel_id":"channel-1"}`))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("webhook message operation must remain POST, got %s", r.Method)
		}
		http.Redirect(w, r, target.URL+"/capture", http.StatusFound)
	}))
	defer source.Close()
	result := SendMessage(Payload{Content: "synthetic"}, source.URL+"/api/webhooks/webhook-1/secret-token", "", "")
	if targetHits != 0 {
		t.Fatalf("webhook request followed a redirect to another host; target hits=%d", targetHits)
	}
	if result.MessageID != "" || result.FailureCategory != "discord_http_error" || result.Durable || !result.Unknown {
		t.Fatalf("redirect response was not quarantined as an ambiguous delivery outcome: %+v", result)
	}
}

func TestWebhookMessageDeleteDoesNotFollowRedirect(t *testing.T) {
	targetHits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("webhook cleanup must remain DELETE, got %s", r.Method)
		}
		http.Redirect(w, r, target.URL+"/capture", http.StatusFound)
	}))
	defer source.Close()
	if DeleteMessage(source.URL+"/api/webhooks/webhook-1/secret-token", "message-1", "") {
		t.Fatal("redirect was treated as successful message cleanup")
	}
	if targetHits != 0 {
		t.Fatalf("webhook cleanup followed a redirect to another host; target hits=%d", targetHits)
	}
}

func TestWebhookMessageDeleteFailureDoesNotLogWebhookToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	webhookURL := server.URL + "/api/webhooks/webhook-1/synthetic-secret-token"
	server.Close()
	previousLogger := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	if DeleteMessage(webhookURL, "message-1", "") {
		t.Fatal("closed local endpoint unexpectedly reported cleanup success")
	}
	if strings.Contains(logs.String(), "synthetic-secret-token") || strings.Contains(logs.String(), webhookURL) {
		t.Fatalf("webhook credential appeared in failure logs: %q", logs.String())
	}
}

package discord

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyWebhookMessageReadbackRequiresExactMessageDestinationAndPayload(t *testing.T) {
	expected := Payload{Content: "", Embeds: []Embed{{Title: "Files · shot010", Description: "Shared file reference", Color: 0x64748B, Fields: []EmbedField{{Name: "Recipient Task", Value: "https://kitsu.example/tasks/shot010", Inline: false}}}}}
	message := `{"id":"1234567890123456789","channel_id":"987654321098765432","webhook_id":"123456789012345678","content":"","embeds":[{"title":"Files · shot010","description":"Shared file reference","color":6583435,"fields":[{"name":"Recipient Task","value":"https://kitsu.example/tasks/shot010","inline":false}],"type":"rich"}],"author":{"username":"KitsuSync"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/messages/1234567890123456789" || r.URL.Query().Get("thread_id") != "987654321098765432" {
			t.Errorf("readback request=%s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(message))
	}))
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/messages/1234567890123456789?thread_id=987654321098765432", nil)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := verifyWebhookMessageReadback(context.Background(), server.Client(), request, expected, "1234567890123456789", "123456789012345678", "987654321098765432")
	if err != nil || len(hash) != 64 {
		t.Fatalf("matching provider readback hash=%q err=%v", hash, err)
	}
	for _, tc := range []struct {
		name    string
		body    string
		channel string
	}{
		{name: "wrong message", body: strings.Replace(message, `"id":"1234567890123456789"`, `"id":"1234567890123456790"`, 1), channel: "987654321098765432"},
		{name: "wrong destination", body: message, channel: "different-thread"},
		{name: "wrong payload", body: strings.Replace(message, "shot010", "shot999", 1), channel: "987654321098765432"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			channel := tc.channel
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, strings.Replace(body, "987654321098765432", channel, 1))
			}))
			defer ts.Close()
			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/messages/1234567890123456789", nil)
			if _, err := verifyWebhookMessageReadback(context.Background(), ts.Client(), req, expected, "1234567890123456789", "123456789012345678", "987654321098765432"); err == nil {
				t.Fatal("mismatched provider message unexpectedly verified")
			}
		})
	}
}

func TestVerifyWebhookMessageReadbackFailsClosedOnProviderFailureAndRedirect(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusServiceUnavailable, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var redirected int
			final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected++; w.WriteHeader(http.StatusOK) }))
			defer final.Close()
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if status == http.StatusFound {
					w.Header().Set("Location", final.URL+"/message")
				}
				w.WriteHeader(status)
			}))
			defer provider.Close()
			req, _ := http.NewRequest(http.MethodGet, provider.URL+"/message", nil)
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			_, err := verifyWebhookMessageReadback(context.Background(), client, req, Payload{}, "1234567890123456789", "123456789012345678", "678901234567890123")
			if err == nil {
				t.Fatal("provider failure or redirect unexpectedly verified")
			}
			if redirected != 0 {
				t.Fatal("readback followed a redirect")
			}
		})
	}
}

package setup

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCreateCategory_RejectsMissingBotTokenBeforeRequest(t *testing.T) {
	_, err := CreateCategory("guild-123", "Project Alpha", "   ")
	if err == nil {
		t.Fatal("expected missing token error")
	}
	if !strings.Contains(err.Error(), "discord bot token is missing") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}

func TestDiscordBotAPIError_ClassifiesUnauthorized(t *testing.T) {
	err := discordBotAPIError("discord category create failed", http.StatusUnauthorized, []byte(`{"message":"401: Unauthorized","code":0}`))
	if err == nil {
		t.Fatal("expected classified error")
	}
	if !strings.Contains(err.Error(), "missing or invalid") {
		t.Fatalf("expected invalid token classification, got %v", err)
	}
	if strings.Contains(err.Error(), "Guild") || strings.Contains(err.Error(), "permission") {
		t.Fatalf("expected token-specific classification, got %v", err)
	}
}

func TestDiscordBotAPIError_ClassifiesForbiddenAndNotFound(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		wantSubstr string
	}{
		{
			name:       "forbidden",
			status:     http.StatusForbidden,
			body:       `{"message":"Missing Permissions","code":50013}`,
			wantSubstr: "lacks permission",
		},
		{
			name:       "not found",
			status:     http.StatusNotFound,
			body:       `{"message":"Unknown Guild","code":10004}`,
			wantSubstr: "resource was not found",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := discordBotAPIError("discord category create failed", tc.status, []byte(tc.body))
			if err == nil {
				t.Fatal("expected classified error")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("expected %q in %v", tc.wantSubstr, err)
			}
		})
	}
}

func TestDiscordBotAPIErrorDoesNotExposeResponseBody(t *testing.T) {
	const secretLikeBody = `{"message":"webhook token https://example.invalid/api/webhooks/123/secret-token"}`
	err := discordBotAPIError("discord channel delete failed", http.StatusBadRequest, []byte(secretLikeBody))
	if err == nil {
		t.Fatal("expected classified error")
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "webhooks/123") {
		t.Fatalf("response body leaked into error: %v", err)
	}
}

func TestSafeDeleteOperationMessageDoesNotExposeUnderlyingError(t *testing.T) {
	const secretLikeValue = "request failed: webhook token secret-token"
	message := safeDeleteOperationMessage("en", "channel_delete", fmt.Errorf("%s", secretLikeValue))
	if strings.Contains(message, "secret-token") || strings.Contains(message, "webhook") {
		t.Fatalf("underlying error leaked into user-facing message: %q", message)
	}
}

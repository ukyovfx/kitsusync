package setup

import (
	"net/http"
	"strings"
	"testing"
)

func TestPR206UserLinkingEscapesRemoteDirectoryContent(t *testing.T) {
	guildID := "123456789012345678"
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"</strong><script>personXSS</script>","email":"xss@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds": {
			body: `[{"id":"123456789012345678","name":"Studio A"}]`,
		},
		"/api/v10/guilds/123456789012345678/members": {
			body: `[{"user":{"id":"123456789012345679","username":"human","global_name":"<svg onload=memberXSS>","bot":false}}]`,
		},
	})

	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en&discord_guild_id="+guildID)
	for _, raw := range []string{"<script>", "</script>", "<svg "} {
		if strings.Contains(body, raw) {
			t.Fatalf("remote directory content was rendered as raw HTML: %q", raw)
		}
	}
	for _, marker := range []string{"personXSS", "memberXSS"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("remote directory fixture was not rendered: %q", marker)
		}
	}
}

func TestPR206UserLinkingEscapesDiscordGuildSelectorContent(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"Person One","email":"one@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds": {
			body: `[{"id":"123456789012345678","name":"<img src=x onerror=guildXSS>"},{"id":"123456789012345679","name":"Studio B"}]`,
		},
	})

	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en")
	if strings.Contains(body, "<img ") {
		t.Fatal("Discord guild name was rendered as raw HTML")
	}
	if !strings.Contains(body, "guildXSS") {
		t.Fatal("Discord guild fixture was not rendered in the selector")
	}
}

func TestPR206UserLinkingRejectsNonSnowflakeGuildQueryWithoutReflection(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"Person One","email":"one@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds": {
			body: `[{"id":"123456789012345678","name":"Studio A"},{"id":"123456789012345679","name":"Studio B"}]`,
		},
	})

	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en&discord_guild_id=%3Cscript%3EqueryXSS%3C%2Fscript%3E")
	if strings.Contains(body, "<script>") {
		t.Fatal("invalid guild query was reflected as executable HTML")
	}
	if !strings.Contains(body, "Select a Discord server to load members and enable saving.") {
		t.Fatal("invalid guild query was not canonicalized to the unselected state")
	}
}

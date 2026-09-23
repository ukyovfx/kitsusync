package setup

import (
	"net/http"
	"strings"
	"testing"
)

func TestPR206UserLinkingEscapesRemoteDirectoryContent(t *testing.T) {
	guildID := "123456789012345678"
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"</strong><script>alert(1)</script>","email":"xss@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds": {
			body: `[{"id":"123456789012345678","name":"Studio A"}]`,
		},
		"/api/v10/guilds/123456789012345678/members": {
			body: `[{"user":{"id":"123456789012345679","username":"human","global_name":"<svg onload=alert(1)>","bot":false}}]`,
		},
	})

	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en&discord_guild_id="+guildID)
	for _, raw := range []string{"<script>", "</script>", "<svg "} {
		if strings.Contains(body, raw) {
			t.Fatalf("remote directory content was rendered as raw HTML: %q", raw)
		}
	}
	for _, escaped := range []string{"&lt;script&gt;", "&lt;svg onload=alert(1)&gt;"} {
		if !strings.Contains(body, escaped) {
			t.Fatalf("escaped remote directory content missing %q", escaped)
		}
	}
}

func TestPR206UserLinkingEscapesDiscordGuildSelectorContent(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"Person One","email":"one@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds": {
			body: `[{"id":"123456789012345678","name":"<img src=x onerror=alert(1)>"},{"id":"123456789012345679","name":"Studio B"}]`,
		},
	})

	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en")
	if strings.Contains(body, "<img ") {
		t.Fatal("Discord guild name was rendered as raw HTML")
	}
	if !strings.Contains(body, "&lt;img src=x onerror=alert(1)&gt;") {
		t.Fatal("escaped Discord guild name is missing from selector")
	}
}

func TestPR206UserLinkingRejectsNonSnowflakeGuildQueryWithoutReflection(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"Person One","email":"one@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds": {
			body: `[{"id":"123456789012345678","name":"Studio A"},{"id":"123456789012345679","name":"Studio B"}]`,
		},
	})

	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en&discord_guild_id=%3Cscript%3Ealert(1)%3C%2Fscript%3E")
	if strings.Contains(body, "<script>") || strings.Contains(body, "alert(1)") {
		t.Fatal("invalid guild query was reflected into User Linking HTML")
	}
	if !strings.Contains(body, "Select a Discord server to load members and enable saving.") {
		t.Fatal("invalid guild query was not canonicalized to the unselected state")
	}
}

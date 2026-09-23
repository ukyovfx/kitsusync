package setup

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPR206UserLinkingEscapesRemoteDirectoryContent(t *testing.T) {
	guildID := "123456789012345678"
	maliciousPerson := `</strong><script>personXSS</script>`
	maliciousMember := `<svg onload=memberXSS>`
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
	for _, raw := range []string{maliciousPerson, maliciousMember} {
		if strings.Contains(body, raw) {
			t.Fatalf("remote directory content was rendered as raw HTML: %q", raw)
		}
		if !strings.Contains(body, html.EscapeString(raw)) {
			t.Fatalf("remote directory content was not rendered in escaped form: %q", raw)
		}
	}
}

func TestPR206UserLinkingEscapesDiscordGuildSelectorContent(t *testing.T) {
	maliciousGuild := `<img src=x onerror=guildXSS>`
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"Person One","email":"one@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds": {
			body: `[{"id":"123456789012345678","name":"<img src=x onerror=guildXSS>"},{"id":"123456789012345679","name":"Studio B"}]`,
		},
	})

	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en")
	if strings.Contains(body, maliciousGuild) {
		t.Fatal("Discord guild name was rendered as raw HTML")
	}
	if !strings.Contains(body, html.EscapeString(maliciousGuild)) {
		t.Fatal("Discord guild name was not rendered in escaped form")
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
	if strings.Contains(body, "<script>queryXSS</script>") {
		t.Fatal("invalid guild query was reflected into the rendered page")
	}
	if !strings.Contains(body, "Select a Discord server to load members and enable saving.") {
		t.Fatal("invalid guild query was not canonicalized to the unselected state")
	}
}

func TestPR206LanguageToggleEscapesQuerySeparatorsInHref(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/users?discord_guild_id=123456789012345678&filter=active&lang=en", nil)
	body := langToggleHTML(r, "en")
	if strings.Contains(body, `&lang=ja`) {
		t.Fatal("language toggle emitted unescaped query separators in an HTML attribute")
	}
	if !strings.Contains(body, `&amp;lang=ja`) {
		t.Fatal("language toggle did not HTML-escape the query separators")
	}
}

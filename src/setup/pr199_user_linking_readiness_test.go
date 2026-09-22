package setup

import (
	"app/src/model"
	"app/src/utils/request"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestPR199AvailableProjectsPreservesLocalRowsOnLiveFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/data/projects/" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}

	db := newIAViewDB(t)
	model.SetSetting(db, KitsuAPIBaseURLSettingKey, server.URL+"/api")
	if err := setRuntimeKitsuToken(db, "runtime-token"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Project{KitsuProjectID: "local-project", Name: "Local project"}).Error; err != nil {
		t.Fatal(err)
	}

	projects, lookupErr := availableProjectsWithError(db)
	if lookupErr == nil {
		t.Fatal("live lookup failure was collapsed into an empty/success state")
	}
	if len(projects) != 1 || projects[0].KitsuProjectID != "local-project" {
		t.Fatalf("local projects were not preserved: %+v", projects)
	}
}

func TestPR199UserLinkingMissingPrerequisitesShowsSetupOnly(t *testing.T) {
	w := httptest.NewRecorder()
	renderGlobalUserLinking(w, httptest.NewRequest("GET", "/bot/admin/users?lang=en", nil), nil)
	body := w.Body.String()

	for _, want := range []string{"Configure Kitsu", "Connection settings"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing-prerequisite state omitted %q", want)
		}
	}
	for _, forbidden := range []string{`id="global-discord-guild"`, "<table", "Diagnostic details", "Discord users could not be loaded"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("missing-prerequisite state exposed blocked lookup UI %q", forbidden)
		}
	}
}

func TestPR199DiscordGuildQueryIsCanonicalized(t *testing.T) {
	valid := httptest.NewRequest("GET", "/bot/admin/users?discord_guild_id=123456789012345678", nil)
	if got := canonicalDiscordGuildQuery(valid); got != "123456789012345678" {
		t.Fatalf("canonical valid guild = %q", got)
	}

	for _, raw := range []string{"<script>alert(1)</script>", "123abc", "1", ""} {
		r := httptest.NewRequest("GET", "/bot/admin/users?discord_guild_id="+raw, nil)
		if got := canonicalDiscordGuildQuery(r); got != "" {
			t.Fatalf("invalid guild query %q canonicalized to %q", raw, got)
		}
	}
}

package model

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"app/src/utils/request"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSupervisorResolutionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Project{}, &UserMap{}, &ProjectUserMap{}, &CheckerMap{}, &ProjectCheckerMap{}); err != nil {
		t.Fatalf("migrate test schema: %v", err)
	}
	return db
}

func configureSupervisorResolutionTestOrigin(t *testing.T, baseURL string) {
	t.Helper()
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{
		BaseURL:   baseURL,
		PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProductionTeamIdentityPrefersGlobalStableKitsuIDThenLegacyMappings(t *testing.T) {
	db := newSupervisorResolutionTestDB(t)
	project := Project{KitsuProjectID: "team-identity"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&UserMap{KitsuID: "person-1", KitsuEmail: "person@example.test", KitsuName: "Person", DiscordID: "global-id"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectUserMap{ProjectID: project.ID, KitsuEmail: "person@example.test", KitsuName: "Person", DiscordUserID: "legacy-project-email"}).Error; err != nil {
		t.Fatal(err)
	}
	if got := GetUserMapForProjectWithIdentity(db, project.KitsuProjectID, "person-1", "Person", "person@example.test"); got != "global-id" {
		t.Fatalf("stable global Kitsu identity resolved %q, want global-id", got)
	}

	if err := db.Where("kitsu_id = ?", "person-1").Delete(&UserMap{}).Error; err != nil {
		t.Fatal(err)
	}
	if got := GetUserMapForProjectWithIdentity(db, project.KitsuProjectID, "person-1", "Person", "person@example.test"); got != "legacy-project-email" {
		t.Fatalf("legacy project mapping resolved %q, want legacy-project-email", got)
	}
}

func TestResolveReviewersForProjectPrecedence(t *testing.T) {
	tests := []struct {
		name            string
		people          []map[string]string
		details         map[string]map[string]any
		projectTeam     []map[string]string
		projectCheckers []ProjectCheckerMap
		globalCheckers  []CheckerMap
		globalMaps      []UserMap
		want            []string
		status          int
		wantErr         bool
		wantKitsuReads  bool
	}{
		{
			name:            "Production Reviewer override wins without Kitsu reads",
			projectCheckers: []ProjectCheckerMap{{TaskType: "Animation", OverrideDiscordID: "production-reviewer"}},
			globalCheckers:  []CheckerMap{{TaskType: "Animation", DiscordID: "legacy-reviewer"}},
			want:            []string{"production-reviewer"},
		},
		{
			name:           "linked Supervisor precedes legacy global CheckerMap",
			people:         []map[string]string{{"id": "p-1", "role": "supervisor"}},
			details:        map[string]map[string]any{"p-1": {"id": "p-1", "full_name": "Sam One", "email": "sam@example.test", "role": "supervisor", "departments": []string{"dept-1"}}},
			projectTeam:    []map[string]string{{"id": "p-1"}},
			globalCheckers: []CheckerMap{{TaskType: "Animation", DiscordID: "legacy-reviewer"}},
			globalMaps:     []UserMap{{KitsuID: "p-1", DiscordID: "supervisor-reviewer"}},
			want:           []string{"supervisor-reviewer"},
			wantKitsuReads: true,
		},
		{
			name:        "multiple linked Supervisors are returned",
			people:      []map[string]string{{"id": "p-2", "role": "supervisor"}, {"id": "p-1", "role": "supervisor"}},
			projectTeam: []map[string]string{{"id": "p-1"}, {"id": "p-2"}},
			details: map[string]map[string]any{
				"p-1": {"id": "p-1", "full_name": "One", "email": "one@example.test", "role": "supervisor", "departments": []string{"dept-1"}},
				"p-2": {"id": "p-2", "full_name": "Two", "email": "two@example.test", "role": "supervisor", "departments": []string{"dept-1"}},
			},
			globalMaps:     []UserMap{{KitsuID: "p-1", DiscordID: "reviewer-1"}, {KitsuID: "p-2", DiscordID: "reviewer-2"}},
			want:           []string{"reviewer-1", "reviewer-2"},
			wantKitsuReads: true,
		},
		{
			name:           "unlinked Supervisors fall back to global CheckerMap",
			people:         []map[string]string{{"id": "p-1", "role": "supervisor"}},
			details:        map[string]map[string]any{"p-1": {"id": "p-1", "full_name": "Unlinked", "email": "unlinked@example.test", "role": "supervisor", "departments": []string{"dept-1"}}},
			projectTeam:    []map[string]string{{"id": "p-1"}},
			globalCheckers: []CheckerMap{{TaskType: "Animation", DiscordID: "legacy-reviewer"}},
			want:           []string{"legacy-reviewer"},
			wantKitsuReads: true,
		},
		{
			name:           "empty Production team falls back to global CheckerMap",
			globalCheckers: []CheckerMap{{TaskType: "Animation", DiscordID: "legacy-reviewer"}},
			want:           []string{"legacy-reviewer"},
			wantKitsuReads: true,
		},
		{
			name:           "no Supervisor or global Reviewer returns empty",
			want:           []string{},
			wantKitsuReads: true,
		},
		{
			name:           "Supervisor read failure keeps configured legacy global Checker",
			status:         http.StatusServiceUnavailable,
			globalCheckers: []CheckerMap{{TaskType: "Animation", DiscordID: "legacy-reviewer"}},
			want:           []string{"legacy-reviewer"},
			wantErr:        true,
			wantKitsuReads: true,
		},
		{
			name:           "Supervisor read failure with no global Reviewer returns empty and error",
			status:         http.StatusServiceUnavailable,
			want:           []string{},
			wantErr:        true,
			wantKitsuReads: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newSupervisorResolutionTestDB(t)
			project := Project{KitsuProjectID: "production-1", Name: "Production"}
			if err := db.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			for _, row := range tc.projectCheckers {
				row.ProjectID = project.ID
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, row := range tc.globalCheckers {
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, row := range tc.globalMaps {
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}

			kitsuReads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				kitsuReads++
				if r.Header.Get("Authorization") != "Bearer resolver-test-token" {
					t.Errorf("authorization header was not taken from the caller")
				}
				if tc.status != 0 {
					http.Error(w, "Kitsu unavailable", tc.status)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/data/projects/production-1/task-types":
					_, _ = w.Write([]byte(`[{"id":"task-type-1","name":"Animation","department_id":"dept-1"}]`))
				case "/api/data/projects/production-1/team":
					_ = json.NewEncoder(w).Encode(tc.projectTeam)
				case "/api/data/persons/":
					_ = json.NewEncoder(w).Encode(tc.people)
				default:
					const prefix = "/api/data/persons/"
					personID := strings.TrimPrefix(r.URL.Path, prefix)
					if personID != r.URL.Path {
						if detail, ok := tc.details[personID]; ok {
							_ = json.NewEncoder(w).Encode(detail)
							return
						}
					}
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			configureSupervisorResolutionTestOrigin(t, server.URL)

			got, err := ResolveReviewersForProjectWithSupervisors(db, server.URL, "resolver-test-token", "production-1", "task-type-1", "Animation")
			if (err != nil) != tc.wantErr {
				t.Fatalf("resolver error = %v, wantErr %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Reviewer IDs = %v, want %v", got, tc.want)
			}
			if (kitsuReads > 0) != tc.wantKitsuReads {
				t.Fatalf("Kitsu request count = %d, want reads %v", kitsuReads, tc.wantKitsuReads)
			}
		})
	}
}

func TestResolveProjectTaskTypeSupervisorDiscordIDs(t *testing.T) {
	tests := []struct {
		name       string
		people     []map[string]string
		details    map[string]map[string]any
		globalMaps []UserMap
		projectMap []ProjectUserMap
		want       []string
		status     int
		wantErr    bool
	}{
		{
			name:    "one supervisor uses stable Kitsu person ID before global email or name",
			people:  []map[string]string{{"id": "p-1", "role": "supervisor"}},
			details: map[string]map[string]any{"p-1": {"id": "p-1", "full_name": "Sam One", "email": "sam@example.test", "role": "supervisor", "departments": []string{"dept-1"}}},
			globalMaps: []UserMap{
				{KitsuID: "different-person", KitsuName: "Sam One", KitsuEmail: "sam@example.test", DiscordID: "email-match"},
				{KitsuID: "p-1", KitsuName: "Sam One", KitsuEmail: "sam@example.test", DiscordID: "person-id-match"},
			},
			want: []string{"person-id-match"},
		},
		{
			name:   "multiple supervisors return deterministic Discord ID order",
			people: []map[string]string{{"id": "p-z", "role": "supervisor"}, {"id": "p-a", "role": "supervisor"}},
			details: map[string]map[string]any{
				"p-z": {"id": "p-z", "full_name": "Zed", "email": "zed@example.test", "role": "supervisor", "departments": []string{"dept-1"}},
				"p-a": {"id": "p-a", "full_name": "Amy", "email": "amy@example.test", "role": "supervisor", "departments": []string{"dept-1"}},
			},
			globalMaps: []UserMap{{KitsuID: "p-z", DiscordID: "discord-z"}, {KitsuID: "p-a", DiscordID: "discord-a"}},
			want:       []string{"discord-a", "discord-z"},
		},
		{
			name:   "unlinked supervisor is skipped while linked supervisor remains",
			people: []map[string]string{{"id": "p-1", "role": "supervisor"}, {"id": "p-2", "role": "supervisor"}},
			details: map[string]map[string]any{
				"p-1": {"id": "p-1", "full_name": "Linked", "email": "linked@example.test", "role": "supervisor", "departments": []string{"dept-1"}},
				"p-2": {"id": "p-2", "full_name": "Unlinked", "email": "unlinked@example.test", "role": "supervisor", "departments": []string{"dept-1"}},
			},
			globalMaps: []UserMap{{KitsuID: "p-1", DiscordID: "discord-1"}},
			want:       []string{"discord-1"},
		},
		{
			name:    "all supervisors unlinked returns empty result",
			people:  []map[string]string{{"id": "p-1", "role": "supervisor"}},
			details: map[string]map[string]any{"p-1": {"id": "p-1", "full_name": "Unlinked", "email": "unlinked@example.test", "role": "supervisor", "departments": []string{"dept-1"}}},
			want:    []string{},
		},
		{
			name:   "duplicate Discord targets are returned once",
			people: []map[string]string{{"id": "p-1", "role": "supervisor"}, {"id": "p-2", "role": "supervisor"}},
			details: map[string]map[string]any{
				"p-1": {"id": "p-1", "full_name": "One", "email": "one@example.test", "role": "supervisor", "departments": []string{"dept-1"}},
				"p-2": {"id": "p-2", "full_name": "Two", "email": "two@example.test", "role": "supervisor", "departments": []string{"dept-1"}},
			},
			globalMaps: []UserMap{{KitsuID: "p-1", DiscordID: "same-discord"}, {KitsuID: "p-2", DiscordID: "same-discord"}},
			want:       []string{"same-discord"},
		},
		{
			name:       "Production mapping takes precedence over global person identity",
			people:     []map[string]string{{"id": "p-1", "role": "supervisor"}},
			details:    map[string]map[string]any{"p-1": {"id": "p-1", "full_name": "Sam One", "email": "sam@example.test", "role": "supervisor", "departments": []string{"dept-1"}}},
			globalMaps: []UserMap{{KitsuID: "p-1", KitsuName: "Sam One", KitsuEmail: "sam@example.test", DiscordID: "global-discord"}},
			projectMap: []ProjectUserMap{{KitsuName: "Different display name", KitsuEmail: "sam@example.test", DiscordUserID: "production-discord"}},
			want:       []string{"production-discord"},
		},
		{
			name:       "stable person identity takes precedence over Production name-only fallback",
			people:     []map[string]string{{"id": "p-1", "role": "supervisor"}},
			details:    map[string]map[string]any{"p-1": {"id": "p-1", "full_name": "Sam One", "email": "sam@example.test", "role": "supervisor", "departments": []string{"dept-1"}}},
			globalMaps: []UserMap{{KitsuID: "p-1", KitsuName: "Sam One", KitsuEmail: "sam@example.test", DiscordID: "stable-person"}},
			projectMap: []ProjectUserMap{{KitsuName: "Sam One", DiscordUserID: "name-only"}},
			want:       []string{"stable-person"},
		},
		{
			name:       "global UserMap email fallback remains available",
			people:     []map[string]string{{"id": "p-1", "role": "supervisor"}},
			details:    map[string]map[string]any{"p-1": {"id": "p-1", "full_name": "Sam One", "email": "sam@example.test", "role": "supervisor", "departments": []string{"dept-1"}}},
			globalMaps: []UserMap{{KitsuName: "Legacy Name", KitsuEmail: "sam@example.test", DiscordID: "global-email"}},
			want:       []string{"global-email"},
		},
		{
			name:       "global UserMap name fallback remains available",
			people:     []map[string]string{{"id": "p-1", "role": "supervisor"}},
			details:    map[string]map[string]any{"p-1": {"id": "p-1", "full_name": "Sam One", "email": "sam@example.test", "role": "supervisor", "departments": []string{"dept-1"}}},
			globalMaps: []UserMap{{KitsuName: "Sam One", DiscordID: "global-name"}},
			want:       []string{"global-name"},
		},
		{
			name:    "Kitsu read errors propagate",
			status:  http.StatusServiceUnavailable,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newSupervisorResolutionTestDB(t)
			project := Project{KitsuProjectID: "production-1", Name: "Production"}
			if err := db.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			for _, mapping := range tc.projectMap {
				mapping.ProjectID = project.ID
				if err := db.Create(&mapping).Error; err != nil {
					t.Fatal(err)
				}
			}
			for i := range tc.globalMaps {
				if err := db.Create(&tc.globalMaps[i]).Error; err != nil {
					t.Fatal(err)
				}
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer resolver-test-token" {
					t.Errorf("authorization header was not taken from the caller")
				}
				if tc.status != 0 {
					http.Error(w, "Kitsu unavailable", tc.status)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/data/projects/production-1/task-types":
					_, _ = w.Write([]byte(`[{"id":"task-type-1","name":"Animation","department_id":"dept-1"}]`))
				case "/api/data/projects/production-1/team":
					_ = json.NewEncoder(w).Encode(tc.people)
				case "/api/data/persons/":
					_ = json.NewEncoder(w).Encode(tc.people)
				default:
					const personPathPrefix = "/api/data/persons/"
					personID := strings.TrimPrefix(r.URL.Path, personPathPrefix)
					if personID != r.URL.Path {
						if detail, ok := tc.details[personID]; ok {
							_ = json.NewEncoder(w).Encode(detail)
							return
						}
					}
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			configureSupervisorResolutionTestOrigin(t, server.URL)

			got, err := ResolveProjectTaskTypeSupervisorDiscordIDs(db, server.URL, "resolver-test-token", "production-1", "task-type-1")
			if (err != nil) != tc.wantErr {
				t.Fatalf("resolver error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resolved Discord IDs = %v, want %v", got, tc.want)
			}
			if tc.name == "global UserMap email fallback remains available" {
				var user UserMap
				if err := db.Where("kitsu_email = ?", "sam@example.test").First(&user).Error; err != nil {
					t.Fatal(err)
				}
				if user.KitsuName != "Legacy Name" {
					t.Fatalf("read-only resolution changed stored Kitsu name to %q", user.KitsuName)
				}
			}
		})
	}
}

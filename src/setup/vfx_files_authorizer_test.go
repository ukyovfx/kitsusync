package setup

import (
	"context"
	"errors"
	"testing"

	"app/src/api/kitsu"
)

type fakeVFXFilesKitsuIdentitySource struct {
	verified error
	persons  []kitsu.Person
	team     []kitsu.Person
	err      error
}

func (f fakeVFXFilesKitsuIdentitySource) VerifySession(context.Context, KitsuURLModel, string) error {
	return f.verified
}
func (f fakeVFXFilesKitsuIdentitySource) Persons(context.Context, KitsuURLModel, string) ([]kitsu.Person, error) {
	return f.persons, f.err
}
func (f fakeVFXFilesKitsuIdentitySource) ProductionTeam(context.Context, KitsuURLModel, string, string) ([]kitsu.Person, error) {
	return f.team, f.err
}

func TestVFXFilesAuthorizerBindsLiveSessionIdentityToProductionMembership(t *testing.T) {
	const (
		personID     = "10000000-0000-4000-8000-000000000001"
		productionID = "20000000-0000-4000-8000-000000000001"
	)
	source := fakeVFXFilesKitsuIdentitySource{
		persons: []kitsu.Person{{ID: personID, Email: "admin@example.test", Role: "manager", Active: true}},
		team:    []kitsu.Person{{ID: personID, ProjectRole: "manager", Active: true}},
	}
	authorizer := NewVFXFilesKitsuSessionAuthorizer(KitsuURLModel{ResolvedAPIBaseURL: "https://kitsu.example.test/api"}, "admin@example.test", "session-kitsu-token", "manager", source)
	actorID, allowed, err := authorizer.AuthorizeProductionAdmin(context.Background(), productionID)
	if err != nil || !allowed || actorID != personID {
		t.Fatalf("live Production manager was not authorized as immutable Kitsu person ID: actor=%q allowed=%v err=%v", actorID, allowed, err)
	}

	for name, tc := range map[string]struct {
		email, role string
		source      fakeVFXFilesKitsuIdentitySource
	}{
		"no Production membership":    {email: "admin@example.test", role: "manager", source: fakeVFXFilesKitsuIdentitySource{persons: source.persons}},
		"non-admin Production role":   {email: "admin@example.test", role: "manager", source: fakeVFXFilesKitsuIdentitySource{persons: source.persons, team: []kitsu.Person{{ID: personID, ProjectRole: "artist", Active: true}}}},
		"duplicate live email":        {email: "admin@example.test", role: "manager", source: fakeVFXFilesKitsuIdentitySource{persons: append(append([]kitsu.Person{}, source.persons...), kitsu.Person{ID: "10000000-0000-4000-8000-000000000002", Email: "ADMIN@example.test", Role: "manager", Active: true}), team: source.team}},
		"session role denied":         {email: "admin@example.test", role: "artist", source: source},
		"session verification failed": {email: "admin@example.test", role: "manager", source: fakeVFXFilesKitsuIdentitySource{verified: errors.New("expired"), persons: source.persons, team: source.team}},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := NewVFXFilesKitsuSessionAuthorizer(KitsuURLModel{ResolvedAPIBaseURL: "https://kitsu.example.test/api"}, tc.email, "token", tc.role, tc.source)
			actor, allowed, err := candidate.AuthorizeProductionAdmin(context.Background(), productionID)
			if err == nil || allowed || actor != "" {
				t.Fatalf("unverified actor/Production grant was accepted: actor=%q allowed=%v err=%v", actor, allowed, err)
			}
		})
	}
}

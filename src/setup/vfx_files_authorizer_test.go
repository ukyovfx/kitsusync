package setup

import (
	"context"
	"errors"
	"testing"

	"app/src/api/kitsu"
)

type fakeVFXFilesKitsuIdentitySource struct {
	current kitsu.Person
	team    []kitsu.Person
	err     error
}

func (f fakeVFXFilesKitsuIdentitySource) AuthenticatedPerson(context.Context, KitsuURLModel, string) (kitsu.Person, error) {
	return f.current, f.err
}
func (f fakeVFXFilesKitsuIdentitySource) ProductionTeam(context.Context, KitsuURLModel, string, string) ([]kitsu.Person, error) {
	return f.team, f.err
}

func TestVFXFilesAuthorizerUsesAuthenticatedPersonWhenDirectoryOmitsEmails(t *testing.T) {
	const (
		personID     = "10000000-0000-4000-8000-000000000011"
		productionID = "20000000-0000-4000-8000-000000000011"
	)
	current := kitsu.Person{ID: personID, Email: "admin@example.test", Role: "manager", Active: true}
	source := fakeVFXFilesKitsuIdentitySource{
		current: current,
		team:    []kitsu.Person{{ID: personID, ProjectRole: "manager", Active: true}},
	}
	authorizer := NewVFXFilesKitsuSessionAuthorizer(KitsuURLModel{ResolvedAPIBaseURL: "https://kitsu.example.test/api"}, current.Email, "session-kitsu-token", "manager", source)
	actorID, allowed, err := authorizer.AuthorizeProductionAdmin(context.Background(), productionID)
	if err != nil || !allowed || actorID != personID {
		t.Fatalf("authenticated Production manager was rejected when the global directory omitted emails: actor=%q allowed=%v err=%v", actorID, allowed, err)
	}
}

func TestVFXFilesAuthorizerBindsLiveSessionIdentityToProductionMembership(t *testing.T) {
	const (
		personID     = "10000000-0000-4000-8000-000000000001"
		productionID = "20000000-0000-4000-8000-000000000001"
	)
	source := fakeVFXFilesKitsuIdentitySource{
		current: kitsu.Person{ID: personID, Email: "admin@example.test", Role: "manager", Active: true},
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
		"no Production membership":             {email: "admin@example.test", role: "manager", source: fakeVFXFilesKitsuIdentitySource{current: source.current}},
		"non-admin Production role":            {email: "admin@example.test", role: "manager", source: fakeVFXFilesKitsuIdentitySource{current: source.current, team: []kitsu.Person{{ID: personID, ProjectRole: "artist", Active: true}}}},
		"duplicate Production membership":      {email: "admin@example.test", role: "manager", source: fakeVFXFilesKitsuIdentitySource{current: source.current, team: []kitsu.Person{{ID: personID, ProjectRole: "manager", Active: true}, {ID: personID, ProjectRole: "manager", Active: true}}}},
		"current identity email mismatch":      {email: "admin@example.test", role: "manager", source: fakeVFXFilesKitsuIdentitySource{current: kitsu.Person{ID: personID, Email: "other@example.test", Role: "manager", Active: true}, team: source.team}},
		"session role denied":                  {email: "admin@example.test", role: "artist", source: source},
		"current identity verification failed": {email: "admin@example.test", role: "manager", source: fakeVFXFilesKitsuIdentitySource{err: errors.New("expired"), team: source.team}},
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

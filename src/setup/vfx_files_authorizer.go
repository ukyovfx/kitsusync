package setup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"app/src/api/kitsu"
	"app/src/api/vfxfiles"
)

type vfxFilesKitsuIdentitySource interface {
	VerifySession(context.Context, KitsuURLModel, string) error
	Persons(context.Context, KitsuURLModel, string) ([]kitsu.Person, error)
	ProductionTeam(context.Context, KitsuURLModel, string, string) ([]kitsu.Person, error)
}

type liveVFXFilesKitsuIdentitySource struct{}

type vfxFilesKitsuSessionAuthorizer struct {
	connection KitsuURLModel
	email      string
	token      string
	role       string
	source     vfxFilesKitsuIdentitySource
}

// NewVFXFilesKitsuSessionAuthorizer binds a KitsuSync session to the current
// immutable Kitsu Person ID and current role in exactly one Production. A new
// instance is constructed for each HTTP request; PluginClient invokes it for
// every API operation.
func NewVFXFilesKitsuSessionAuthorizer(connection KitsuURLModel, email, token, role string, source vfxFilesKitsuIdentitySource) vfxfiles.ProductionAdminAuthorizer {
	if source == nil {
		source = liveVFXFilesKitsuIdentitySource{}
	}
	return vfxFilesKitsuSessionAuthorizer{
		connection: connection,
		email:      strings.TrimSpace(email), token: strings.TrimSpace(token), role: strings.TrimSpace(role), source: source,
	}
}

func (a vfxFilesKitsuSessionAuthorizer) AuthorizeProductionAdmin(ctx context.Context, productionID string) (string, bool, error) {
	if ctx == nil || !vfxfiles.ValidID(strings.TrimSpace(productionID)) || a.email == "" || a.token == "" || !isStudioManagerOrHigher(a.role) || a.source == nil {
		return "", false, errors.New("Kitsu Production authorization unavailable")
	}
	if err := a.source.VerifySession(ctx, a.connection, a.token); err != nil {
		return "", false, errors.New("Kitsu session is no longer valid")
	}
	persons, err := a.source.Persons(ctx, a.connection, a.token)
	if err != nil {
		return "", false, errors.New("Kitsu actor identity could not be verified")
	}
	var actor *kitsu.Person
	for index := range persons {
		if !strings.EqualFold(strings.TrimSpace(persons[index].Email), a.email) {
			continue
		}
		if actor != nil {
			return "", false, errors.New("Kitsu actor identity is ambiguous")
		}
		actor = &persons[index]
	}
	if actor == nil || !vfxfiles.ValidID(actor.ID) || !actor.Active || actor.Archived || actor.IsBot || !isStudioManagerOrHigher(actor.Role) {
		return "", false, errors.New("Kitsu actor is unavailable or not an administrator")
	}
	team, err := a.source.ProductionTeam(ctx, a.connection, a.token, productionID)
	if err != nil {
		return "", false, errors.New("Kitsu Production membership could not be verified")
	}
	var membership *kitsu.Person
	for index := range team {
		if strings.EqualFold(strings.TrimSpace(team[index].ID), actor.ID) {
			if membership != nil {
				return "", false, errors.New("Kitsu Production membership is ambiguous")
			}
			membership = &team[index]
		}
	}
	if membership == nil || !membership.Active || membership.Archived || membership.IsBot || !isStudioManagerOrHigher(kitsu.EffectiveProductionRole(*membership)) {
		return "", false, errors.New("actor is not an administrator of this Kitsu Production")
	}
	return strings.ToLower(actor.ID), true, nil
}

func (liveVFXFilesKitsuIdentitySource) VerifySession(ctx context.Context, connection KitsuURLModel, token string) error {
	return VerifyKitsuToken(ctx, connection, token)
}

func (liveVFXFilesKitsuIdentitySource) Persons(ctx context.Context, connection KitsuURLModel, token string) ([]kitsu.Person, error) {
	var persons []kitsu.Person
	if err := fetchVFXFilesKitsuJSON(ctx, connection, token, "/data/persons/", &persons); err != nil {
		return nil, err
	}
	return persons, nil
}

func (liveVFXFilesKitsuIdentitySource) ProductionTeam(ctx context.Context, connection KitsuURLModel, token, productionID string) ([]kitsu.Person, error) {
	if !vfxfiles.ValidID(strings.TrimSpace(productionID)) {
		return nil, errors.New("invalid Production ID")
	}
	var team []kitsu.Person
	if err := fetchVFXFilesKitsuJSON(ctx, connection, token, "/data/projects/"+url.PathEscape(productionID)+"/team", &team); err != nil {
		return nil, err
	}
	return team, nil
}

func fetchVFXFilesKitsuJSON(ctx context.Context, connection KitsuURLModel, token, relativePath string, out any) error {
	if ctx == nil || strings.TrimSpace(token) == "" || len(connection.VerifiedIPs) == 0 {
		return errors.New("Kitsu connection is not verified")
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(connection.ResolvedAPIBaseURL), "/"))
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("Kitsu API base is invalid")
	}
	base.Path += relativePath
	base.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return errors.New("Kitsu request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := safeKitsuClient(connection, connection.VerifiedIPs).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("Kitsu API rejected the authorization check")
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<20))
	if err := decoder.Decode(out); err != nil {
		return errors.New("Kitsu API response is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("Kitsu API response contains trailing data")
	}
	return nil
}

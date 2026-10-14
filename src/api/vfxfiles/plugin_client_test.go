package vfxfiles

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPluginClientAddsBoundServiceTokenAndActorHeaders(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var gotPath, gotActor, gotIdempotency string
	var gotToken string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotActor, gotIdempotency = r.URL.Path, r.Header.Get("X-KitsuSync-Actor-ID"), r.Header.Get("Idempotency-Key")
		gotToken = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"contract_version":"1.0","state":"connected"}`)
	}))
	defer server.Close()
	base := server.URL + "/api/plugins/vfx_files/integrations/v1"
	client, err := NewPluginClient(base, server.Client(), ServiceTokenConfig{
		Issuer: "https://sync.example.test", Audience: base, Subject: "kitsusync-service", ClientID: "instance", KeyID: "k1", PrivateKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	productionID := "123e4567-e89b-42d3-a456-426614174001"
	authorizer := testProductionAdminAuthorizer{actorID: "123e4567-e89b-42d3-a456-426614174000", allowed: true}
	response, err := client.Do(context.Background(), PluginRequest{
		Method: http.MethodGet, Path: "/drive-connections", Query: map[string][]string{"production_id": {productionID}},
		ProductionID: productionID, Authorizer: authorizer, Scope: "vfx.connections.read", Now: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !json.Valid(body) || gotPath != "/api/plugins/vfx_files/integrations/v1/drive-connections" || gotActor != authorizer.actorID || gotToken == "" || strings.HasPrefix(gotToken, "Bearer ") == false || gotIdempotency != "" {
		t.Fatalf("unexpected authenticated Plugin request: status=%d path=%s actor=%s idempotency=%s", response.StatusCode, gotPath, gotActor, gotIdempotency)
	}
}

func TestPluginClientRequiresIdempotencyForMutationsAndNeverFollowsRedirects(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	followed := false
	redirectTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed = true
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL+"/capture", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	base := server.URL + "/api/plugins/vfx_files/integrations/v1"
	client, err := NewPluginClient(base, server.Client(), ServiceTokenConfig{
		Issuer: "https://sync.example.test", Audience: base, Subject: "kitsusync-service", ClientID: "instance", KeyID: "k1", PrivateKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	productionID := "123e4567-e89b-42d3-a456-426614174001"
	authorizer := testProductionAdminAuthorizer{actorID: "123e4567-e89b-42d3-a456-426614174000", allowed: true}
	_, err = client.Do(context.Background(), PluginRequest{Method: http.MethodPost, Path: "/productions/" + productionID + "/activation", ProductionID: productionID, Authorizer: authorizer, Scope: "vfx.activation.write"})
	if err == nil {
		t.Fatal("mutation without Idempotency-Key was accepted")
	}
	response, err := client.Do(context.Background(), PluginRequest{
		Method: http.MethodPost, Path: "/productions/" + productionID + "/activation", ProductionID: productionID, Authorizer: authorizer,
		Scope: "vfx.activation.write", IdempotencyKey: "request-01", Mutating: true, Body: []byte(`{"contract_version":"1.0"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect || followed {
		t.Fatalf("Plugin API redirect was followed: response=%d followed=%v", response.StatusCode, followed)
	}
}

func TestPluginClientRejectsUnsafeBaseAndRelativePaths(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	config := ServiceTokenConfig{Issuer: "https://sync.example.test", Audience: "https://zou.example.test/api/plugins/vfx_files/integrations/v1", Subject: "sync", ClientID: "instance", KeyID: "k1", PrivateKey: key}
	for _, target := range []string{
		"http://zou.example.test/api/plugins/vfx_files/integrations/v1",
		"https://user@zou.example.test/api/plugins/vfx_files/integrations/v1",
		"https://zou.example.test/api/integrations/v1",
		"https://zou.example.test/prefix/api/plugins/vfx_files/integrations/v1",
		"https://zou.example.test/api/plugins/vfx_files/integrations/v1/",
	} {
		if _, err := NewPluginClient(target, nil, config); err == nil {
			t.Errorf("unsafe Plugin base URL accepted: %s", target)
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	base := server.URL + "/api/plugins/vfx_files/integrations/v1"
	config.Audience = base
	client, err := NewPluginClient(base, server.Client(), config)
	if err != nil {
		t.Fatal(err)
	}
	productionID := "123e4567-e89b-42d3-a456-426614174001"
	authorizer := testProductionAdminAuthorizer{actorID: "123e4567-e89b-42d3-a456-426614174000", allowed: true}
	for _, path := range []string{"//evil.invalid/x", "/../outside", "/drive-connections/%2e%2e/outside", "https://evil.invalid/path"} {
		if _, err := client.Do(context.Background(), PluginRequest{Method: http.MethodGet, Path: path, ProductionID: productionID, Authorizer: authorizer, Scope: "vfx.connections.read"}); err == nil {
			t.Errorf("unsafe relative Plugin API path accepted: %q", path)
		}
	}
	if _, err := client.Do(context.Background(), PluginRequest{
		Method: http.MethodGet, Path: "/drive-connections", Query: map[string][]string{"production_id": {productionID}}, ProductionID: productionID,
		Authorizer: authorizer, Scope: "vfx.connections.read", Headers: http.Header{"X-KitsuSync-Actor-Id": {"spoofed"}},
	}); err == nil {
		t.Fatal("caller-supplied actor identity header was accepted")
	}
	otherProduction := "123e4567-e89b-42d3-a456-426614174002"
	for _, request := range []PluginRequest{
		{Method: http.MethodGet, Path: "/productions/" + otherProduction + "/desired-config", ProductionID: productionID, Authorizer: authorizer, Scope: "vfx.config.read"},
		{Method: http.MethodGet, Path: "/drive-connections", Query: map[string][]string{"production_id": {otherProduction}}, ProductionID: productionID, Authorizer: authorizer, Scope: "vfx.connections.read"},
		{Method: http.MethodPost, Path: "/productions/" + productionID + "/sync-previews", Body: []byte(`{"production_id":"` + otherProduction + `"}`), ProductionID: productionID, Authorizer: authorizer, Scope: "vfx.sync.preview"},
	} {
		if _, err := client.Do(context.Background(), request); err == nil {
			t.Fatalf("cross-Production Plugin request accepted: method=%s path=%s", request.Method, request.Path)
		}
	}
}

func TestPluginClientRechecksProductionAuthorizationForEveryCall(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	base := server.URL + "/api/plugins/vfx_files/integrations/v1"
	client, err := NewPluginClient(base, server.Client(), ServiceTokenConfig{
		Issuer: "https://sync.example.test", Audience: base, Subject: "kitsusync-service", ClientID: "instance", KeyID: "k1", PrivateKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	productionID := "123e4567-e89b-42d3-a456-426614174001"
	authorizer := testProductionAdminAuthorizer{actorID: "123e4567-e89b-42d3-a456-426614174000", allowed: true}
	request := PluginRequest{Method: http.MethodGet, Path: "/drive-connections", Query: map[string][]string{"production_id": {productionID}}, ProductionID: productionID, Authorizer: authorizer, Scope: "vfx.connections.read"}
	counting := authorizerFunc(func(ctx context.Context, id string) (string, bool, error) {
		called++
		if id != productionID {
			return "", false, nil
		}
		return authorizer.AuthorizeProductionAdmin(ctx, id)
	})
	request.Authorizer = counting
	response, err := client.Do(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if called != 1 {
		t.Fatalf("authorization was not checked exactly once: %d", called)
	}
	request.Authorizer = authorizerFunc(func(context.Context, string) (string, bool, error) { return "", false, nil })
	if _, err := client.Do(context.Background(), request); err == nil {
		t.Fatal("revoked Production permission still allowed a Plugin request")
	}
}

type authorizerFunc func(context.Context, string) (string, bool, error)

func (f authorizerFunc) AuthorizeProductionAdmin(ctx context.Context, productionID string) (string, bool, error) {
	return f(ctx, productionID)
}

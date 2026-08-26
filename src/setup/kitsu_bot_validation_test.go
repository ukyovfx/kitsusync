package setup

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateKitsuBotTokenReadsRequiredEndpoints(t *testing.T) {
	var endpoints []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-bot-token" {
			t.Fatalf("missing bearer token for %s", r.URL.Path)
		}
		endpoints = append(endpoints, r.URL.RequestURI())
		if r.URL.Path == "/api/auth/authenticated" {
			fmt.Fprint(w, `{"authenticated":true,"user":{"id":"bot-id","full_name":"KitsuSync Bot","is_bot":true,"active":true}}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	result := ValidateKitsuBotToken(nil, server.URL, "test-bot-token", true)
	if !result.Compatible() || !result.Authenticated || !result.IdentityIsBot {
		t.Fatalf("unexpected validation result: %+v", result)
	}
	for _, required := range []string{
		"/api/auth/authenticated", "/api/data/projects/", "/api/data/persons/",
		"/api/data/task-status/", "/api/data/entities/", "/api/data/entity-types/", "/api/data/task-types/",
		"/api/data/tasks?relations=true", "/api/data/comments",
	} {
		found := false
		for _, endpoint := range endpoints {
			if endpoint == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("required endpoint was not read: %s", required)
		}
	}
}

func TestValidateKitsuBotTokenClassifiesMissingRequiredEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/authenticated" {
			fmt.Fprint(w, `{"id":"bot-id","full_name":"KitsuSync Bot","is_bot":true}`)
			return
		}
		if r.URL.Path == "/api/data/task-types/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	result := ValidateKitsuBotToken(nil, server.URL, "test-bot-token", false)
	if result.Stage != "task_types" || result.Classification != BotTokenRequiredEndpointFailure {
		t.Fatalf("unexpected missing endpoint result: %+v", result)
	}
}

func TestValidateKitsuBotTokenClassifiesAuthAndPermissionFailures(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		classification string
	}{
		{name: "expired", status: http.StatusUnauthorized, classification: BotTokenExpiredOrInactive},
		{name: "permission", status: http.StatusForbidden, classification: BotTokenPermissionInsufficient},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			result := ValidateKitsuBotToken(nil, server.URL, "test-bot-token", true)
			if result.Classification != test.classification || result.Compatible() {
				t.Fatalf("unexpected classification: %+v", result)
			}
		})
	}
}

func TestValidateKitsuBotTokenRejectsHumanIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"human-id","full_name":"Human","is_bot":false}`)
	}))
	defer server.Close()
	result := ValidateKitsuBotToken(nil, server.URL, "test-bot-token", false)
	if result.Classification != BotTokenAuthenticatedIdentityNotBot || result.Compatible() {
		t.Fatalf("human identity was accepted: %+v", result)
	}
}

func TestValidateKitsuBotTokenDoesNotTreatMissingBotFlagAsInvalidToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"authenticated":true,"user":{"id":"person-id","full_name":"Authenticated person"}}`)
	}))
	defer server.Close()

	result := ValidateKitsuBotToken(nil, server.URL, "test-bot-token", false)
	if !result.Authenticated || result.Classification != BotIdentityVerificationFailed || result.Classification == BotTokenInvalid {
		t.Fatalf("missing bot flag was classified incorrectly: %+v", result)
	}
}

func TestValidateKitsuBotTokenVerifiesNestedCanonicalBotPerson(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/authenticated" {
			fmt.Fprint(w, `{"authenticated":true,"user":{"id":"bot-id","full_name":"KitsuSync Bot","is_bot":true,"active":true,"archived":false}}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	result := ValidateKitsuBotToken(nil, server.URL, "test-bot-token", false)
	if !result.Compatible() || !result.Authenticated || !result.IdentityIsBot || result.IdentityID != "bot-id" {
		t.Fatalf("nested canonical Bot person was not accepted: %+v", result)
	}
}

func TestValidateKitsuBotTokenKeepsCommentsOptional(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/authenticated" {
			fmt.Fprint(w, `{"id":"bot-id","full_name":"KitsuSync Bot","is_bot":true}`)
			return
		}
		if r.URL.Path == "/api/data/comments" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	result := ValidateKitsuBotToken(nil, server.URL, "test-bot-token", true)
	if !result.Compatible() {
		t.Fatalf("optional comments read rejected a usable token: %+v", result)
	}
}

func TestValidateKitsuBotTokenReportsFailureStage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/authenticated" {
			fmt.Fprint(w, `{"id":"bot-id","full_name":"KitsuSync Bot","is_bot":true}`)
			return
		}
		if r.URL.Path == "/api/data/persons/" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	result := ValidateKitsuBotToken(nil, server.URL, "test-bot-token", false)
	if result.Stage != "persons" || result.Classification != BotTokenPermissionInsufficient {
		t.Fatalf("unexpected stage/classification: %+v", result)
	}
}

func TestValidateKitsuBotTokenReportsAuthenticatedResponseParseBug(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{malformed`)
	}))
	defer server.Close()

	result := ValidateKitsuBotToken(nil, server.URL, "test-bot-token", false)
	if result.Classification != BotTokenAuthenticatedParseBug || result.Stage != "bot_identity" {
		t.Fatalf("unexpected malformed response result: %+v", result)
	}
	if result.Failure.Endpoint != "/api/auth/authenticated" || result.Failure.StatusCode != http.StatusOK || result.Failure.ErrorClass != "invalid_json" {
		t.Fatalf("unexpected malformed response failure: %+v", result.Failure)
	}
}

func TestValidateKitsuBotTokenDiagnosticsAreSecretSafe(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	secret := "test-secret-token"
	result := ValidateKitsuBotToken(nil, server.URL, secret, false)
	if result.Classification != BotTokenExpiredOrInactive || result.Compatible() {
		t.Fatalf("unexpected validation result: %+v", result)
	}
	output := logs.String()
	for _, forbidden := range []string{secret, "Bearer", "Authorization"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("diagnostic log exposed %q: %s", forbidden, output)
		}
	}
	for _, expected := range []string{"stage=token_authentication", "endpoint=/api/auth/authenticated", "status=401", "classification=BOT_TOKEN_EXPIRED_OR_INACTIVE", "compatible=false"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("diagnostic log omitted %q: %s", expected, output)
		}
	}
}

func TestValidateKitsuBotTokenClassifiesNetworkFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	endpoint := server.URL
	server.Close()

	result := ValidateKitsuBotToken(nil, endpoint, "test-bot-token", false)
	if result.Classification != KitsuUnreachable || result.Compatible() {
		t.Fatalf("unexpected network failure result: %+v", result)
	}
	if len(result.Reads) != 1 || result.Reads[0].Endpoint != "/api/auth/authenticated" || result.Reads[0].ErrorClass != "network_error" {
		t.Fatalf("unexpected network failure diagnostics: %+v", result.Reads)
	}
}

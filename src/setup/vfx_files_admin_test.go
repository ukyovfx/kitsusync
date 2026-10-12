package setup

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"app/src/api/vfxfiles"
	"app/src/model"
)

const (
	vfxTestProductionID = "10000000-0000-4000-8000-000000000001"
	vfxTestActorID      = "20000000-0000-4000-8000-000000000001"
)

type vfxAdminAuthorizer struct {
	actorID string
	allowed bool
	err     error
}

func (a vfxAdminAuthorizer) AuthorizeProductionAdmin(context.Context, string) (string, bool, error) {
	return a.actorID, a.allowed, a.err
}

func TestVFXFilesAuthorizationActionRedirectsOnlyToGoogleAndScopesProduction(t *testing.T) {
	for _, tc := range []struct {
		name, responseURL string
		wantRedirect      bool
	}{
		{"trusted", "https://accounts.google.com/o/oauth2/v2/auth?state=fake", true},
		{"lookalike", "https://accounts.google.com.evil.invalid/o/oauth2/v2/auth", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var scoped, signed bool
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var decoded map[string]any
				_ = json.Unmarshal(body, &decoded)
				scoped = decoded["production_id"] == vfxTestProductionID && r.Header.Get("X-KitsuSync-Actor-ID") == vfxTestActorID && r.Header.Get("Idempotency-Key") != ""
				signed = strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && r.Method == http.MethodPost && r.URL.Path == vfxfiles.ServiceAudienceSuffix+"/drive-connections/authorizations"
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(`{"contract_version":"1.0","authorization_id":"30000000-0000-4000-8000-000000000001","authorization_url":"` + tc.responseURL + `"}`))
			}))
			defer server.Close()
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			baseURL := server.URL + vfxfiles.ServiceAudienceSuffix
			client, err := vfxfiles.NewPluginClient(baseURL, server.Client(), vfxfiles.ServiceTokenConfig{
				Issuer: "https://kitsusync.example", Audience: baseURL, Subject: "kitsusync",
				ClientID: "kitsusync", KeyID: "test-key", PrivateKey: key,
			})
			if err != nil {
				t.Fatal(err)
			}
			runtime := &vfxfiles.RuntimeConfig{Client: client}
			project := &model.Project{KitsuProjectID: vfxTestProductionID}
			form := url.Values{"action": {"authorize"}, "account_mode": {"shared_drive"}, "idempotency_key": {"ks-files-0123456789abcdef"}}
			req := httptest.NewRequest(http.MethodPost, "/bot/admin/vfx-files?production="+project.KitsuProjectID, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			message, redirect, completed := runVFXFilesAdminAction(context.Background(), nil, runtime, vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: true}, project, req, "en")
			if !scoped || !signed {
				t.Fatal("Plugin authorization request was not signed and bound to the live actor/Production")
			}
			if (redirect != "") != tc.wantRedirect || completed != tc.wantRedirect {
				t.Fatalf("redirect=%q completed=%v want trusted redirect=%v message=%q", redirect, completed, tc.wantRedirect, message)
			}
			if !tc.wantRedirect && !strings.Contains(message, "unsupported authorization destination") {
				t.Fatalf("unsafe redirect error is not actionable: %q", message)
			}
		})
	}
}

func TestVFXFilesAuthorizationActionFailsClosedWithoutProductionGrant(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	baseURL := "https://files.example.invalid" + vfxfiles.ServiceAudienceSuffix
	client, err := vfxfiles.NewPluginClient(baseURL, &http.Client{Timeout: time.Second}, vfxfiles.ServiceTokenConfig{
		Issuer: "https://kitsusync.example", Audience: baseURL, Subject: "kitsusync", ClientID: "kitsusync", KeyID: "test-key", PrivateKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	project := &model.Project{KitsuProjectID: vfxTestProductionID}
	form := url.Values{"action": {"authorize"}, "account_mode": {"my_drive"}, "idempotency_key": {"ks-files-0123456789abcdef"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/vfx-files?production="+project.KitsuProjectID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	message, redirect, completed := runVFXFilesAdminAction(context.Background(), nil, &vfxfiles.RuntimeConfig{Client: client}, vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: false}, project, req, "en")
	if redirect != "" || completed || !strings.Contains(message, "unavailable") {
		t.Fatalf("unauthorized action result redirect=%q completed=%v message=%q", redirect, completed, message)
	}
}

func TestVFXFilesAdminConnectionStatesFailClosed(t *testing.T) {
	for _, state := range []string{"connected", "permission_denied", "expired", "revoke_pending", "error"} {
		if !validVFXFilesConnectionState(state) {
			t.Errorf("valid contract state %q rejected", state)
		}
	}
	for _, state := range []string{"", "ready", "connected-ish", "token_present"} {
		if validVFXFilesConnectionState(state) {
			t.Errorf("unknown state %q accepted", state)
		}
	}
}

func TestVFXFilesAdminActionKeysAreDistinctPerFormAndStableForRetry(t *testing.T) {
	base := "ks-files-0123456789abcdef0123456789abcdef0123456789abcdef"
	authorize := vfxFilesActionKey(base, "authorize")
	revoke := vfxFilesActionKey(base, "revoke-30000000-0000-4000-8000-000000000001")
	activate := vfxFilesActionKey(base, "activate")
	if authorize == revoke || revoke == activate || authorize == activate {
		t.Fatal("separate admin forms share an idempotency key")
	}
	if authorize != vfxFilesActionKey(base, "authorize") || safeSubmittedIdempotencyKey(authorize) != authorize || len(revoke) > 128 {
		t.Fatal("form retry key is not stable or contract-safe")
	}
}

func TestVFXFilesConnectionStateLabelsAreLocalized(t *testing.T) {
	for _, tc := range []struct{ state, ja, en string }{
		{"connected", "接続済み", "Connected"},
		{"permission_denied", "権限不足", "Permission denied"},
		{"revoke_pending", "切断要確認", "Revocation pending"},
	} {
		if got := localizedVFXFilesConnectionState(tc.state, "ja"); got != tc.ja {
			t.Errorf("Japanese state %q = %q, want %q", tc.state, got, tc.ja)
		}
		if got := localizedVFXFilesConnectionState(tc.state, "en"); got != tc.en {
			t.Errorf("English state %q = %q, want %q", tc.state, got, tc.en)
		}
	}
}

func TestDecodeVFXFilesConnectionsUsesContractFieldsAndDropsAdditiveUnknowns(t *testing.T) {
	body := []byte(`{"contract_version":"1.0","connections":[{"contract_version":"1.0","id":"30000000-0000-4000-8000-000000000001","account_mode":"shared_drive","owner_label":"Synthetic account","drive_id":"fake-drive-1","state":"connected","granted_scopes":["https://www.googleapis.com/auth/drive.file"],"future_additive_field":"must not render"}]}`)
	var connections []vfxFilesAdminConnection
	if err := decodeVFXFilesConnections(body, &connections); err != nil {
		t.Fatal(err)
	}
	if len(connections) != 1 || connections[0].State != "connected" || len(connections[0].GrantedScopes) != 1 {
		t.Fatalf("decoded connections=%+v", connections)
	}
	rendered := renderVFXFilesAdminPage(httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files", nil), &model.Project{Name: "Synthetic"}, vfxTestProductionID, "en", vfxFilesAdminPageState{connections: connections})
	if strings.Contains(rendered, "must not render") || strings.Contains(rendered, "fake-drive-1") || strings.Contains(rendered, "googleapis.com") {
		t.Fatal("non-display connection metadata leaked into the management UI")
	}
	for _, invalid := range [][]byte{
		[]byte(`{"contract_version":"1.0","connections":[{"contract_version":"2.0","id":"30000000-0000-4000-8000-000000000001","account_mode":"my_drive","state":"connected"}]}`),
		[]byte(`{"contract_version":"1.0","connections":[]} {}`),
	} {
		if err := decodeVFXFilesConnections(invalid, &connections); err == nil {
			t.Fatalf("invalid Plugin response accepted: %s", invalid)
		}
	}
}

func TestVFXFilesRootVerificationUsesCurrentConnectedProductionBinding(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-KitsuSync-Actor-ID") != vfxTestActorID || r.URL.Query().Get("production_id") != vfxTestProductionID {
			t.Errorf("root operation missing actor/Production binding: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == vfxfiles.ServiceAudienceSuffix+"/drive-connections":
			_, _ = w.Write([]byte(`{"contract_version":"1.0","connections":[{"contract_version":"1.0","id":"30000000-0000-4000-8000-000000000001","account_mode":"shared_drive","drive_id":"fake-drive-1","state":"connected"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == vfxfiles.ServiceAudienceSuffix+"/drive-connections/30000000-0000-4000-8000-000000000001/roots":
			_, _ = w.Write([]byte(`{"contract_version":"1.0","roots":[{"id":"fake-root-1","name":"Synthetic Root","drive_id":"fake-drive-1","account_mode":"shared_drive"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/roots"):
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("root verification missing idempotency key")
			}
			_, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(`{"contract_version":"1.0","connection_id":"30000000-0000-4000-8000-000000000001","root_id":"fake-root-1","state":"verified","capabilities":{"can_read":true,"can_add_children":true}}`))
		default:
			http.Error(w, "unexpected fake Plugin operation", http.StatusNotFound)
		}
	}))
	defer server.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	base := server.URL + vfxfiles.ServiceAudienceSuffix
	client, err := vfxfiles.NewPluginClient(base, server.Client(), vfxfiles.ServiceTokenConfig{Issuer: "https://kitsusync.example", Audience: base, Subject: "kitsusync", ClientID: "instance", KeyID: "test-key", PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	project := &model.Project{KitsuProjectID: vfxTestProductionID}
	form := url.Values{"action": {"verify_root"}, "connection_id": {"30000000-0000-4000-8000-000000000001"}, "root_id": {"fake-root-1"}, "idempotency_key": {"ks-files-0123456789abcdef:listed-root"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/vfx-files?production="+vfxTestProductionID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	message, redirect, completed := runVFXFilesAdminAction(context.Background(), nil, &vfxfiles.RuntimeConfig{Client: client}, vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: true}, project, req, "en")
	if redirect != "" || !completed || !strings.Contains(message, "No configuration was saved") || calls != 3 {
		t.Fatalf("root verification result=%q redirect=%q completed=%v calls=%d", message, redirect, completed, calls)
	}
}

func TestVFXFilesRootCandidateURLRejectsUntrustedFormsBeforePluginAccess(t *testing.T) {
	called := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	base := server.URL + vfxfiles.ServiceAudienceSuffix
	client, err := vfxfiles.NewPluginClient(base, server.Client(), vfxfiles.ServiceTokenConfig{Issuer: "https://kitsusync.example", Audience: base, Subject: "kitsusync", ClientID: "instance", KeyID: "test-key", PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	project := &model.Project{KitsuProjectID: vfxTestProductionID}
	for _, candidate := range []string{"http://drive.google.com/drive/folders/id", "https://drive.google.com.evil.invalid/drive/folders/id", "https://user@drive.google.com/drive/folders/id", "https://drive.google.com/drive/files/id"} {
		form := url.Values{"action": {"verify_root"}, "connection_id": {"30000000-0000-4000-8000-000000000001"}, "candidate_url": {candidate}, "idempotency_key": {"ks-files-0123456789abcdef:url"}}
		req := httptest.NewRequest(http.MethodPost, "/bot/admin/vfx-files?production="+vfxTestProductionID, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, redirect, completed := runVFXFilesAdminAction(context.Background(), nil, &vfxfiles.RuntimeConfig{Client: client}, vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: true}, project, req, "en")
		if redirect != "" || completed || called {
			t.Fatalf("untrusted candidate reached Plugin: candidate=%q redirect=%q completed=%v called=%v", candidate, redirect, completed, called)
		}
	}
}

func TestVFXFilesAdminRootAndTemplateViewIsLocalizedAndValidationOnlyReadOnly(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files?lang=ja", nil)
	state := vfxFilesAdminPageState{
		integrationReady: true,
		actionKey:        "ks-files-0123456789abcdef0123456789abcdef0123456789abcdef",
		selectedID:       "30000000-0000-4000-8000-000000000001",
		connections:      []vfxFilesAdminConnection{{ContractVersion: "1.0", ID: "30000000-0000-4000-8000-000000000001", State: "connected", AccountMode: "shared_drive", DriveID: ptrVFXFilesAdmin("fake-drive-secret")}},
		roots:            []vfxFilesAdminRoot{{ID: "fake-root-1", Name: "Synthetic Root", AccountMode: "shared_drive", DriveID: ptrVFXFilesAdmin("fake-drive-1")}},
		templates:        map[string][]vfxFilesAdminTemplate{"production": {{ID: "template-v1", Revision: 1, Scope: "production", Label: "Production standard", Pattern: "{production}"}}},
	}
	project := &model.Project{Name: "Synthetic Production"}
	page := renderVFXFilesAdminPage(request, project, vfxTestProductionID, "ja", state)
	for _, marker := range []string{"Driveルート確認", "利用可能なテンプレート", "Production standard", "フォルダーURL候補", "作成・移動・共有を行いません"} {
		if !strings.Contains(page, marker) {
			t.Errorf("Japanese Files management view missing %q", marker)
		}
	}
	if strings.Contains(page, "fake-drive-secret") {
		t.Fatal("private Drive identity was rendered")
	}
	readOnly := renderVFXFilesAdminPage(request, &model.Project{Name: "Validation", ValidationOnly: true}, vfxTestProductionID, "en", state)
	if strings.Contains(readOnly, `name="action" value="verify_root"`) || strings.Contains(readOnly, `name="action" value="authorize"`) || strings.Contains(readOnly, `name="action" value="activate"`) {
		t.Fatal("validation-only Production rendered a mutating integration control")
	}
}

func TestVFXFilesAdminDoesNotRenderMutatingControlsWhenPluginIsUnavailable(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files?lang=en", nil)
	state := vfxFilesAdminPageState{
		actionKey:  "ks-files-0123456789abcdef0123456789abcdef0123456789abcdef",
		selectedID: "30000000-0000-4000-8000-000000000001",
		connections: []vfxFilesAdminConnection{{
			ContractVersion: "1.0",
			ID:              "30000000-0000-4000-8000-000000000001",
			State:           "connected",
			AccountMode:     "shared_drive",
		}},
	}
	page := renderVFXFilesAdminPage(request, &model.Project{Name: "Synthetic Production"}, vfxTestProductionID, "en", state)
	for _, action := range []string{"authorize", "verify_root", "revoke", "activate", "deactivate"} {
		if strings.Contains(page, `name="action" value="`+action+`"`) {
			t.Errorf("Plugin-unavailable page rendered mutating action %q", action)
		}
	}
	if !strings.Contains(page, "Google Drive / Files Plugin") {
		t.Fatal("read-only integration status page was not rendered")
	}
}

func ptrVFXFilesAdmin(value string) *string { return &value }

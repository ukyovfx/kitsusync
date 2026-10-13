package setup

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"app/src/api/vfxfiles"
	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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

func (a vfxAdminAuthorizer) BuildVFXFilesSnapshot(_ context.Context, productionID string, _ vfxFilesSnapshotScope) ([]vfxFilesSnapshotObject, string, error) {
	if !a.allowed {
		return nil, "", errors.New("authorization denied")
	}
	objects, fingerprint, err := buildVFXFilesSnapshot(productionID, vfxFilesSnapshotScope{Mode: "future_only"}, vfxFilesSnapshotSourceData{Production: vfxFilesSnapshotRawObject{ID: productionID, ProjectID: productionID, Name: "Synthetic Production"}})
	return objects, fingerprint, err
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
			authorizationExpiry := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var decoded map[string]any
				_ = json.Unmarshal(body, &decoded)
				scoped = decoded["production_id"] == vfxTestProductionID && r.Header.Get("X-KitsuSync-Actor-ID") == vfxTestActorID && r.Header.Get("Idempotency-Key") != ""
				signed = strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && r.Method == http.MethodPost && r.URL.Path == vfxfiles.ServiceAudienceSuffix+"/drive-connections/authorizations"
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"contract_version":"1.0","authorization_id":"30000000-0000-4000-8000-000000000001","authorization_url":"` + tc.responseURL + `","expires_at":"` + authorizationExpiry + `"}`))
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
			db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.VFXFilesAuthorizationIntent{}); err != nil {
				t.Fatal(err)
			}
			project := &model.Project{KitsuProjectID: vfxTestProductionID}
			form := url.Values{"action": {"authorize"}, "account_mode": {"shared_drive"}, "idempotency_key": {"ks-files-0123456789abcdef"}}
			req := httptest.NewRequest(http.MethodPost, "/bot/admin/vfx-files?production="+project.KitsuProjectID, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			message, redirect, completed := runVFXFilesAdminAction(context.Background(), db, runtime, vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: true}, project, req, "en")
			if !scoped || !signed {
				t.Fatal("Plugin authorization request was not signed and bound to the live actor/Production")
			}
			if (redirect != "") != tc.wantRedirect || completed != tc.wantRedirect {
				t.Fatalf("redirect=%q completed=%v want trusted redirect=%v message=%q", redirect, completed, tc.wantRedirect, message)
			}
			if !tc.wantRedirect && !strings.Contains(message, "unsupported authorization destination") {
				t.Fatalf("unsafe redirect error is not actionable: %q", message)
			}
			if tc.wantRedirect {
				intent, err := model.FindVFXFilesAuthorizationIntent(db, "30000000-0000-4000-8000-000000000001", time.Now().UTC())
				if err != nil || intent.ProductionID != vfxTestProductionID || intent.ActorID != vfxTestActorID {
					t.Fatalf("OAuth return context was not durably bound to session actor/Production: %+v err=%v", intent, err)
				}
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
	if redirect != "" || completed || !strings.Contains(message, "administrator starting authorization could not be verified") {
		t.Fatalf("unauthorized action result redirect=%q completed=%v message=%q", redirect, completed, message)
	}
}

func TestVFXFilesOAuthReturnRestoresOnlyStoredAuthorizedProductionContext(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.VFXFilesAuthorizationIntent{}); err != nil {
		t.Fatal(err)
	}
	intent := model.VFXFilesAuthorizationIntent{
		AuthorizationID: "30000000-0000-4000-8000-000000000001",
		ProductionID:    vfxTestProductionID,
		ActorID:         vfxTestActorID,
		ExpiresAt:       time.Now().UTC().Add(5 * time.Minute),
	}
	if err := model.StoreVFXFilesAuthorizationIntent(db, intent); err != nil {
		t.Fatal(err)
	}
	resetSessions()
	t.Cleanup(resetSessions)
	token := newSessionToken("manager@example.test", "session-token", "manager", "")
	handler := vfxFilesOAuthReturnHandler(db, func(context.Context) (KitsuURLModel, error) { return KitsuURLModel{}, nil }, func(KitsuURLModel, string, string, string) vfxfiles.ProductionAdminAuthorizer {
		return vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: true}
	})
	request := httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files/oauth-return?authorization_id="+intent.AuthorizationID, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	wrongActorHandler := vfxFilesOAuthReturnHandler(db, func(context.Context) (KitsuURLModel, error) { return KitsuURLModel{}, nil }, func(KitsuURLModel, string, string, string) vfxfiles.ProductionAdminAuthorizer {
		return vfxAdminAuthorizer{actorID: "20000000-0000-4000-8000-000000000002", allowed: true}
	})
	wrongActor := httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files/oauth-return?authorization_id="+intent.AuthorizationID, nil)
	wrongActor.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	denied := httptest.NewRecorder()
	wrongActorHandler.ServeHTTP(denied, wrongActor)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("different actor reused OAuth context: status=%d", denied.Code)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/bot/admin/vfx-files?lang=ja&production="+vfxTestProductionID {
		t.Fatalf("callback did not restore stored Production context: status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, request)
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("OAuth return intent was not single-use: status=%d", replay.Code)
	}

	credentialCallback := httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files/oauth-return?authorization_id="+intent.AuthorizationID+"&code=must-not-be-accepted", nil)
	credentialCallback.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	credentialResponse := httptest.NewRecorder()
	handler.ServeHTTP(credentialResponse, credentialCallback)
	if credentialResponse.Code != http.StatusBadRequest {
		t.Fatalf("KitsuSync accepted an OAuth credential query: status=%d", credentialResponse.Code)
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

func TestVFXFilesVerifiedMyDriveOwnerMustComeFromMatchingPluginRootProof(t *testing.T) {
	valid := vfxFilesRootVerification{ContractVersion: "1.0", ConnectionID: "30000000-0000-4000-8000-000000000001", AccountMode: "my_drive", RootID: "root-verified", OwnerSubject: "google-subject-opaque", State: "verified", Capabilities: vfxFilesRootCapabilities{CanRead: true, CanAddChildren: true}}
	if _, _, err := verifiedVFXFilesRootIdentity(valid, valid.ConnectionID, valid.AccountMode, valid.RootID, ""); err != nil {
		t.Fatalf("server-verified My Drive subject rejected: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*vfxFilesRootVerification)
	}{
		{"missing subject", func(v *vfxFilesRootVerification) { v.OwnerSubject = "" }},
		{"wrong connection", func(v *vfxFilesRootVerification) { v.ConnectionID = "other" }},
		{"wrong root", func(v *vfxFilesRootVerification) { v.RootID = "other" }},
		{"unverified state", func(v *vfxFilesRootVerification) { v.State = "error" }},
		{"read only", func(v *vfxFilesRootVerification) { v.Capabilities.CanAddChildren = false }},
		{"shared drive cannot claim my drive owner", func(v *vfxFilesRootVerification) { v.AccountMode = "shared_drive" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := valid
			tc.mutate(&got)
			if _, _, err := verifiedVFXFilesRootIdentity(got, valid.ConnectionID, valid.AccountMode, valid.RootID, ""); err == nil {
				t.Fatal("invalid or untrusted root identity was accepted")
			}
		})
	}
}

func TestVFXFilesInitialConfigUsesConditionalCreateAndServerVerifiedOwner(t *testing.T) {
	var saveBody map[string]any
	var created bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == vfxfiles.ServiceAudienceSuffix+"/drive-connections":
			_, _ = io.WriteString(w, `{"contract_version":"1.0","connections":[{"contract_version":"1.0","id":"30000000-0000-4000-8000-000000000001","account_mode":"my_drive","state":"connected"}]}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/roots"):
			_, _ = io.WriteString(w, `{"contract_version":"1.0","roots":[{"id":"root-1","name":"Root","account_mode":"my_drive"}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/roots"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["owner_subject"] != nil {
				t.Error("KitsuSync sent owner identity to Plugin for verification")
			}
			_, _ = io.WriteString(w, `{"contract_version":"1.0","connection_id":"30000000-0000-4000-8000-000000000001","account_mode":"my_drive","root_id":"root-1","drive_id":null,"owner_subject":"verified-plugin-subject","state":"verified","capabilities":{"can_read":true,"can_add_children":true}}`)
		case r.Method == http.MethodGet && r.URL.Path == vfxfiles.ServiceAudienceSuffix+"/templates":
			_, _ = io.WriteString(w, `{"contract_version":"1.0","templates":[{"id":"production-template","revision":2,"scope":"production","label":"Default","pattern":"{production}"}]}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/desired-config"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/desired-config"):
			if r.Header.Get("If-None-Match") != "*" || r.Header.Get("If-Match") != "" {
				t.Errorf("initial save was not conditionally created: If-None-Match=%q If-Match=%q", r.Header.Get("If-None-Match"), r.Header.Get("If-Match"))
			}
			_ = json.NewDecoder(r.Body).Decode(&saveBody)
			w.Header().Set("ETag", `"1"`)
			w.WriteHeader(http.StatusCreated)
			created = true
			_, _ = io.WriteString(w, `{"contract_version":"1.0","production_id":"10000000-0000-4000-8000-000000000001","config_revision":1,"connection_id":"30000000-0000-4000-8000-000000000001","drive":{"account_mode":"my_drive","owner_subject":"verified-plugin-subject","root_id":"root-1","root_state":"verified"},"template_selection":{"selections":[{"scope":"production","template_id":"production-template","revision":2,"overrides":{}}]},"sync_policy":{"mode":"new_production","scope":{"episodes":false,"sequences":false,"shots":false,"assets":false,"tasks":false,"task_type_ids":[]},"notify_new_deliveries":false},"activation":{"enabled":false,"activated_at":null,"notification_epoch":0}}`)
		default:
			http.Error(w, "unexpected fake operation", http.StatusNotFound)
		}
	}))
	defer server.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	base := server.URL + vfxfiles.ServiceAudienceSuffix
	client, err := vfxfiles.NewPluginClient(base, server.Client(), vfxfiles.ServiceTokenConfig{Issuer: "https://kitsusync.example", Audience: base, Subject: "sync", ClientID: "instance", KeyID: "test", PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.VFXFilesProductionConfig{}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"action": {"save_config"}, "connection_id": {"30000000-0000-4000-8000-000000000001"}, "root_id": {"root-1"}, "sync_mode": {"new_production"}, "template_production": {"production-template"}, "owner_subject": {"attacker-controlled-subject"}, "idempotency_key": {"ks-files-0123456789abcdef:save"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/vfx-files?production="+vfxTestProductionID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	message, redirect, completed := runVFXFilesAdminAction(context.Background(), db, &vfxfiles.RuntimeConfig{Client: client}, vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: true}, &model.Project{KitsuProjectID: vfxTestProductionID}, req, "en")
	if redirect != "" || !completed || !created || !strings.Contains(message, "configuration saved") {
		t.Fatalf("initial config result redirect=%q completed=%v created=%v message=%q", redirect, completed, created, message)
	}
	drive := saveBody["drive"].(map[string]any)
	if drive["owner_subject"] != "verified-plugin-subject" || drive["owner_subject"] == "attacker-controlled-subject" {
		t.Fatalf("desired config used unverified Owner identity: %#v", drive)
	}
	var stored model.VFXFilesProductionConfig
	if err := db.Where("production_id = ?", vfxTestProductionID).First(&stored).Error; err != nil || stored.ConfigRevision != 1 {
		t.Fatalf("created config mirror was not persisted: row=%+v err=%v", stored, err)
	}
}

func TestVFXFilesUnsupportedSyncModesRejectBeforeProviderCall(t *testing.T) {
	for _, mode := range []string{"future_only", "adopt_existing", "", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			form := url.Values{"sync_mode": {mode}, "connection_id": {"30000000-0000-4000-8000-000000000001"}, "root_id": {"root-1"}}
			req := httptest.NewRequest(http.MethodPost, "/bot/admin/vfx-files", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if err := req.ParseForm(); err != nil {
				t.Fatal(err)
			}
			message, redirect, completed := saveVFXFilesDesiredConfig(
				context.Background(), nil, nil, nil, vfxTestProductionID, req, "en", "synthetic",
			)
			if completed || redirect != "" || !strings.Contains(message, "not yet supported") {
				t.Fatalf("unsupported %q was accepted or silently retried: message=%q redirect=%q completed=%v", mode, message, redirect, completed)
			}
		})
	}
}

func TestVFXFilesInitialUIOffersOnlySupportedSyncMode(t *testing.T) {
	state := vfxFilesAdminPageState{
		pluginVerified:   true,
		integrationReady: true,
		actionKey:        "ks-files-0123456789abcdef0123456789abcdef0123456789abcdef",
		selectedID:       "30000000-0000-4000-8000-000000000001",
		connections: []vfxFilesAdminConnection{{
			ContractVersion: "1.0", ID: "30000000-0000-4000-8000-000000000001",
			State: "connected", AccountMode: "my_drive",
		}},
		roots: []vfxFilesAdminRoot{{ID: "fake-root", Name: "Synthetic Root", AccountMode: "my_drive"}},
		templates: map[string][]vfxFilesAdminTemplate{
			"production": {{ID: "vfx-files.production.default", Revision: 1, Scope: "production", Label: "Default", Pattern: "{production}"}},
		},
	}
	for _, tc := range []struct{ lang, message string }{
		{"en", "Adopt existing and Future items only are unavailable"},
		{"ja", "既存Productionの取り込みと今後の項目のみの同期"},
	} {
		page := renderVFXFilesAdminPage(
			httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files?lang="+tc.lang, nil),
			&model.Project{Name: "Synthetic Production"}, vfxTestProductionID, tc.lang, state,
		)
		if !strings.Contains(page, `name="sync_mode"><option value="new_production"`) {
			t.Fatalf("%s omitted supported mode selector", tc.lang)
		}
		for _, unsupported := range []string{`option value="future_only"`, `option value="adopt_existing"`} {
			if strings.Contains(page, unsupported) {
				t.Fatalf("%s advertised unimplemented sync mode %q", tc.lang, unsupported)
			}
		}
		if !strings.Contains(page, tc.message) {
			t.Fatalf("%s did not explain currently unavailable modes", tc.lang)
		}
	}
}

func TestVFXFilesMockProviderSnapshotPreviewApplyProgressRetryReconcile(t *testing.T) {
	var snapshotID, previewID, jobID string
	jobState := "failed"
	activationEnabled := false
	activationEpoch := uint64(0)
	configRevision := uint64(1)
	var previewKeys []string
	previewIDsByKey := make(map[string]string)
	var previewCreates int
	var syncJobCalls int
	var calls []string
	makeJob := func(state string, updated string) string {
		return `{"contract_version":"1.0","id":"60000000-0000-4000-8000-000000000001","production_id":"` + vfxTestProductionID + `","config_revision":` + fmt.Sprint(configRevision) + `,"state":"` + state + `","progress":{"completed":1,"total":1},"failure_code":null,"created_at":"2026-10-13T00:00:00Z","updated_at":"` + updated + `"}`
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-KitsuSync-Actor-ID") != vfxTestActorID {
			t.Errorf("actor not bound: %q", r.Header.Get("X-KitsuSync-Actor-ID"))
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/desired-config"):
			w.Header().Set("ETag", `"`+fmt.Sprint(configRevision)+`"`)
			activation := "false"
			activatedAt := "null"
			if activationEnabled {
				activation = "true"
				activatedAt = `"2026-10-13T00:00:00Z"`
			}
			_, _ = io.WriteString(w, `{"contract_version":"1.0","production_id":"`+vfxTestProductionID+`","config_revision":`+fmt.Sprint(configRevision)+`,"connection_id":"30000000-0000-4000-8000-000000000001","drive":{"account_mode":"my_drive","owner_subject":"opaque-subject","root_id":"root-1","root_state":"verified"},"template_selection":{"selections":[{"scope":"production","template_id":"tpl","revision":1,"overrides":{}}]},"sync_policy":{"mode":"new_production","scope":{"episodes":false,"sequences":false,"shots":false,"assets":false,"tasks":false,"task_type_ids":[]},"notify_new_deliveries":false},"activation":{"enabled":`+activation+`,"activated_at":`+activatedAt+`,"notification_epoch":`+fmt.Sprint(activationEpoch)+`}}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/activation"):
			activationEnabled = true
			activationEpoch = 1
			configRevision = 2
			_, _ = io.WriteString(w, `{"contract_version":"1.0"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sync-snapshots"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			snapshotID, _ = body["snapshot_id"].(string)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"contract_version":"1.0","id":"`+snapshotID+`","production_id":"`+vfxTestProductionID+`","config_revision":`+fmt.Sprint(configRevision)+`,"state":"uploading","page_count":1,"received_pages":0,"object_count":1,"expires_at":"2026-10-13T01:00:00Z"}`)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/pages/"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["page_index"] != float64(0) || body["page_sha256"] == nil {
				t.Error("snapshot page lacks index or digest")
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"contract_version":"1.0","id":"`+snapshotID+`","production_id":"`+vfxTestProductionID+`","config_revision":`+fmt.Sprint(configRevision)+`,"state":"uploading","page_count":1,"received_pages":1,"object_count":1,"expires_at":"2026-10-13T01:00:00Z"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"contract_version":"1.0","id":"`+snapshotID+`","production_id":"`+vfxTestProductionID+`","config_revision":`+fmt.Sprint(configRevision)+`,"state":"complete","page_count":1,"received_pages":1,"object_count":1,"expires_at":"2026-10-13T01:00:00Z"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sync-previews"):
			idempotencyKey := r.Header.Get("Idempotency-Key")
			previewKeys = append(previewKeys, idempotencyKey)
			if idempotencyKey == "" {
				t.Error("sync preview request did not carry the contract-required Idempotency-Key")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["snapshot_id"] != snapshotID || body["config_revision"] != float64(configRevision) {
				t.Error("preview did not use uploaded snapshot/config revision")
			}
			previewID = previewIDsByKey[idempotencyKey]
			if previewID == "" {
				previewID = "70000000-0000-4000-8000-000000000001"
				previewIDsByKey[idempotencyKey] = previewID
				previewCreates++
			}
			_, _ = io.WriteString(w, `{"contract_version":"1.0","preview_id":"`+previewID+`","production_id":"`+vfxTestProductionID+`","config_revision":`+fmt.Sprint(configRevision)+`,"root_id":"root-1","state":"preview_ready","summary":{"existing":1,"create":0,"link":0,"excluded":0,"warnings":0},"changes":[],"expires_at":"2026-10-14T00:00:00Z"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sync-jobs"):
			syncJobCalls++
			if !activationEnabled {
				t.Error("Plugin received Apply before activation")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["preview_id"] != previewID {
				t.Error("Apply used a different preview")
			}
			jobID = "60000000-0000-4000-8000-000000000001"
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, makeJob("queued", "2026-10-13T00:00:00Z"))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sync-jobs/"+jobID):
			_, _ = io.WriteString(w, makeJob(jobState, "2026-10-13T00:01:00Z"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/retry"):
			jobState = "reconcile_required"
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, makeJob("reconcile_required", "2026-10-13T00:02:00Z"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reconcile"):
			jobState = "queued"
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, makeJob("queued", "2026-10-13T00:03:00Z"))
		default:
			http.Error(w, "unexpected fake operation", http.StatusNotFound)
		}
	}))
	defer server.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	base := server.URL + vfxfiles.ServiceAudienceSuffix
	client, err := vfxfiles.NewPluginClient(base, server.Client(), vfxfiles.ServiceTokenConfig{Issuer: "https://kitsusync.example", Audience: base, Subject: "sync", ClientID: "instance", KeyID: "test", PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.VFXFilesSyncOperation{}, &model.VFXFilesProductionConfig{}); err != nil {
		t.Fatal(err)
	}
	runtime := &vfxfiles.RuntimeConfig{Client: client}
	authorizer := vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: true}
	project := &model.Project{KitsuProjectID: vfxTestProductionID}
	run := func(action, idempotency string) (string, bool) {
		form := url.Values{"action": {action}, "idempotency_key": {idempotency}}
		req := httptest.NewRequest(http.MethodPost, "/bot/admin/vfx-files?production="+vfxTestProductionID, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		message, _, complete := runVFXFilesAdminAction(context.Background(), db, runtime, authorizer, project, req, "en")
		return message, complete
	}
	firstPreviewActionKey := "ks-files-0123456789abcdef:sync-preview"
	if message, complete := run("preview_sync", firstPreviewActionKey); !complete || !strings.Contains(message, "preview") {
		t.Fatalf("preview: complete=%v message=%q calls=%v", complete, message, calls)
	}
	if _, complete := run("preview_sync", firstPreviewActionKey); !complete {
		t.Fatal("retrying the same preview action should be accepted")
	}
	if len(previewKeys) != 2 || previewKeys[0] != firstPreviewActionKey+":provider-preview" || previewKeys[0] != previewKeys[1] {
		t.Fatalf("same preview retry must reuse one stable idempotency key, got %q", previewKeys)
	}
	if previewCreates != 1 {
		t.Fatalf("same-key retry created %d provider preview records, want one", previewCreates)
	}
	stored, err := model.FindVFXFilesSyncOperation(db, vfxTestProductionID)
	if err != nil || stored.PreviewID != previewID || stored.SnapshotID != snapshotID {
		t.Fatalf("preview not stored: %+v err=%v", stored, err)
	}
	if message, complete := run("apply_sync", "ks-files-1123456789abcdef"); complete || !strings.Contains(message, "explicitly enable") {
		t.Fatalf("Apply before explicit activation should be rejected: complete=%v message=%q", complete, message)
	}
	if syncJobCalls != 0 {
		t.Fatalf("Apply before activation must not call sync-jobs, calls=%d", syncJobCalls)
	}
	if message, complete := run("activate", "ks-files-2123456789abcdef"); !complete || !strings.Contains(message, "activated") {
		t.Fatalf("activation: complete=%v message=%q", complete, message)
	}
	if message, complete := run("apply_sync", "ks-files-3123456789abcdef"); complete || !strings.Contains(message, "changed after preview") {
		t.Fatalf("activation must invalidate pre-activation preview: complete=%v message=%q", complete, message)
	}
	if syncJobCalls != 0 {
		t.Fatalf("stale pre-activation preview must not create a job, calls=%d", syncJobCalls)
	}
	if message, complete := run("preview_sync", "ks-files-7123456789abcdef:sync-preview"); !complete || !strings.Contains(message, "preview") {
		t.Fatalf("post-activation preview: complete=%v message=%q", complete, message)
	}
	if message, complete := run("apply_sync", "ks-files-8123456789abcdef"); !complete || !strings.Contains(message, "accepted") {
		t.Fatalf("apply after activation: complete=%v message=%q", complete, message)
	}
	if message, complete := run("refresh_sync_job", "ks-files-4123456789abcdef"); !complete || !strings.Contains(message, "refreshed") {
		t.Fatalf("job refresh: complete=%v message=%q", complete, message)
	}
	if message, complete := run("retry_sync_job", "ks-files-5123456789abcdef"); !complete || !strings.Contains(message, "accepted") {
		t.Fatalf("job retry: complete=%v message=%q", complete, message)
	}
	if message, complete := run("reconcile_sync_job", "ks-files-6123456789abcdef"); !complete || !strings.Contains(message, "accepted") {
		t.Fatalf("job reconcile: complete=%v message=%q", complete, message)
	}
	for _, path := range []string{"/sync-snapshots", "/pages/0", "/complete", "/sync-previews", "/sync-jobs"} {
		found := false
		for _, call := range calls {
			if strings.Contains(call, path) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("mock integration did not exercise %s: calls=%v", path, calls)
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
			_, _ = w.Write([]byte(`{"contract_version":"1.0","connection_id":"30000000-0000-4000-8000-000000000001","account_mode":"shared_drive","root_id":"fake-root-1","drive_id":"fake-drive-1","owner_subject":null,"state":"verified","capabilities":{"can_read":true,"can_add_children":true}}`))
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
		pluginVerified:   true,
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

func TestVFXFilesActivationAndPreviewApplyLabelsAreLocalizedAndRevisionAware(t *testing.T) {
	project := &model.Project{Name: "Synthetic Production"}
	request := httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files", nil)
	preview := &vfxFilesSyncPreview{ConfigRevision: 3}
	state := vfxFilesAdminPageState{
		pluginVerified:   true,
		integrationReady: true,
		actionKey:        "ks-files-0123456789abcdef0123456789abcdef0123456789abcdef",
		desiredReady:     true,
		desired:          map[string]any{"config_revision": float64(3), "sync_policy": map[string]any{"mode": "new_production"}, "activation": map[string]any{"enabled": false, "notification_epoch": float64(0)}},
		preview:          preview,
	}
	for _, tc := range []struct{ lang, activation, stale string }{
		{"ja", "同期と通知を有効にする", "同期と通知を有効化した後"},
		{"en", "Enable sync and notifications", "After enabling sync and notifications"},
	} {
		page := renderVFXFilesAdminPage(request, project, vfxTestProductionID, tc.lang, state)
		for _, marker := range []string{tc.activation, tc.stale} {
			if !strings.Contains(page, marker) {
				t.Errorf("%s UI missing %q", tc.lang, marker)
			}
		}
		if strings.Contains(page, `name="action" value="apply_sync"`) {
			t.Errorf("%s UI offered Apply before activation and a fresh preview", tc.lang)
		}
	}
	state.desired = map[string]any{"config_revision": float64(4), "sync_policy": map[string]any{"mode": "new_production"}, "activation": map[string]any{"enabled": true, "notification_epoch": float64(2)}}
	page := renderVFXFilesAdminPage(request, project, vfxTestProductionID, "en", state)
	if !strings.Contains(page, "Create a new preview for the current state") || strings.Contains(page, `name="action" value="apply_sync"`) {
		t.Fatal("stale pre-activation preview was rendered as applicable")
	}
	state.preview.ConfigRevision = 4
	page = renderVFXFilesAdminPage(request, project, vfxTestProductionID, "en", state)
	if !strings.Contains(page, `name="action" value="apply_sync"`) || strings.Contains(page, "Enable notifications") {
		t.Fatal("current post-activation preview did not offer Apply with sync+notification terminology")
	}
}

func TestVFXFilesUnsupportedSavedModeHidesUnsafeActionsButCanDisable(t *testing.T) {
	state := vfxFilesAdminPageState{
		pluginVerified:   true,
		integrationReady: true,
		actionKey:        "ks-files-0123456789abcdef0123456789abcdef0123456789abcdef",
		desiredReady:     true,
		desired: map[string]any{
			"config_revision": float64(4),
			"sync_policy":     map[string]any{"mode": "future_only"},
			"activation":      map[string]any{"enabled": false, "notification_epoch": float64(0)},
		},
		preview: &vfxFilesSyncPreview{ConfigRevision: 4},
	}
	request := httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files?lang=en", nil)
	project := &model.Project{Name: "Synthetic Production"}
	page := renderVFXFilesAdminPage(request, project, vfxTestProductionID, "en", state)
	for _, action := range []string{"preview_sync", "apply_sync", "activate"} {
		if strings.Contains(page, `name="action" value="`+action+`"`) {
			t.Fatalf("unsupported saved mode advertised unsafe %q action", action)
		}
	}
	if !strings.Contains(page, "This synchronization mode is not supported yet") {
		t.Fatal("unsupported saved mode did not give a recovery explanation")
	}
	state.desired["activation"] = map[string]any{"enabled": true, "notification_epoch": float64(3)}
	page = renderVFXFilesAdminPage(request, project, vfxTestProductionID, "en", state)
	if !strings.Contains(page, `name="action" value="deactivate"`) {
		t.Fatal("unsupported active configuration must retain explicit deactivation")
	}
	if strings.Contains(page, `name="action" value="apply_sync"`) {
		t.Fatal("unsupported active configuration must not offer Apply")
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

func TestVFXFilesManagementReusesVerifiedConnectionAndDoesNotRenderOutageAsEmpty(t *testing.T) {
	connectionID := "30000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		name               string
		statusCode         int
		templateFailScope  string
		templateFailStatus int
		wantReuse          bool
	}{
		{name: "verified connection reused", statusCode: http.StatusOK, wantReuse: true},
		{name: "temporary provider outage", statusCode: http.StatusServiceUnavailable},
		{name: "partial template capability outage", statusCode: http.StatusOK, templateFailScope: "shot", templateFailStatus: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == vfxfiles.ServiceAudienceSuffix+"/drive-connections" {
					if tc.statusCode != http.StatusOK {
						w.WriteHeader(tc.statusCode)
						return
					}
					_, _ = io.WriteString(w, `{"contract_version":"1.0","connections":[{"contract_version":"1.0","id":"`+connectionID+`","account_mode":"my_drive","state":"connected"}]}`)
					return
				}
				if r.URL.Path == vfxfiles.ServiceAudienceSuffix+"/templates" {
					scope := r.URL.Query().Get("scope")
					if scope == tc.templateFailScope && tc.templateFailStatus != 0 {
						w.WriteHeader(tc.templateFailStatus)
						return
					}
					_, _ = io.WriteString(w, `{"contract_version":"1.0","templates":[{"id":"`+scope+`-template","revision":1,"scope":"`+scope+`","label":"Default","pattern":"{production}"}]}`)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/desired-config") {
					if tc.wantReuse {
						w.Header().Set("ETag", `"1"`)
						_, _ = io.WriteString(w, `{"contract_version":"1.0","production_id":"`+vfxTestProductionID+`","config_revision":1,"connection_id":"`+connectionID+`","drive":{"account_mode":"my_drive","root_id":"root-1","root_state":"verified","owner_subject":"opaque-subject"},"template_selection":{"selections":[{"scope":"production","template_id":"production-template","revision":1,"overrides":{}}]},"sync_policy":{"mode":"future_only","scope":{"episodes":false,"sequences":false,"shots":false,"assets":false,"tasks":false,"task_type_ids":[]},"notify_new_deliveries":false},"activation":{"enabled":false,"activated_at":null,"notification_epoch":0}}`)
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
					return
				}
				if strings.HasSuffix(r.URL.Path, "/roots") {
					_, _ = io.WriteString(w, `{"contract_version":"1.0","roots":[{"id":"root-1","name":"Project root","account_mode":"my_drive"}]}`)
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			baseURL := server.URL + vfxfiles.ServiceAudienceSuffix
			client, err := vfxfiles.NewPluginClient(baseURL, server.Client(), vfxfiles.ServiceTokenConfig{
				Issuer: "https://kitsusync.example", Audience: baseURL, Subject: "kitsusync", ClientID: "kitsusync", KeyID: "test-key", PrivateKey: key,
			})
			if err != nil {
				t.Fatal(err)
			}
			db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.VFXFilesProductionConfig{}); err != nil {
				t.Fatal(err)
			}
			state := vfxFilesAdminPageState{integrationReady: true, actionKey: "ks-files-0123456789abcdef0123456789abcdef0123456789abcdef"}
			loadVFXFilesAdminState(context.Background(), db, &vfxfiles.RuntimeConfig{Client: client}, vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: true}, vfxTestProductionID, "", &state, "en")
			page := renderVFXFilesAdminPage(httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files?lang=en", nil), &model.Project{Name: "Test Production"}, vfxTestProductionID, "en", state)
			if tc.wantReuse {
				if state.pluginState != "available" || !state.pluginVerified || state.selectedID != connectionID || !strings.Contains(page, connectionID) {
					t.Fatalf("verified connection was not reused after reload: state=%+v", state)
				}
				if strings.Contains(page, `name="action" value="authorize"`) || strings.Contains(page, "Connect Google Drive") {
					t.Fatal("valid persisted connection incorrectly prompted for Google OAuth again")
				}
			} else if tc.templateFailScope != "" {
				if state.pluginState != "unavailable" || state.pluginVerified {
					t.Fatalf("partial Plugin capability outage was treated as verified: %+v", state)
				}
				for _, action := range []string{"authorize", "verify_root", "activate", "preview_sync"} {
					if strings.Contains(page, `name="action" value="`+action+`"`) {
						t.Errorf("partial capability outage rendered action %q", action)
					}
				}
			} else {
				if state.pluginState != "unavailable" || state.connectionsOK || state.pluginVerified {
					t.Fatalf("provider outage was treated as verified: %+v", state)
				}
				if strings.Contains(page, "No Drive connections are available.") || strings.Contains(page, `name="action" value="authorize"`) {
					t.Fatal("provider outage was presented as empty state or allowed OAuth action")
				}
			}
		})
	}
}

func ptrVFXFilesAdmin(value string) *string { return &value }

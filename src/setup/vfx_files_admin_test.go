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

func TestDecodeVFXFilesTemplatesRequiresExplicitMaterializationContract(t *testing.T) {
	valid := []byte(`{"contract_version":"1.0","templates":[{"id":"vfx-files.shot.default","revision":1,"scope":"shot","label":"Shot","pattern":"{shot}","materialization":{"schema_revision":1,"root_binding":"verified_root_is_production_root","available_overrides":["shot.folder"]}}]}`)
	var decoded []vfxFilesAdminTemplate
	if err := decodeVFXFilesTemplates(valid, "shot", &decoded); err != nil || len(decoded) != 1 {
		t.Fatalf("valid materialization contract rejected: templates=%+v err=%v", decoded, err)
	}

	for _, body := range [][]byte{
		[]byte(`{"contract_version":"1.0","templates":[{"id":"vfx-files.shot.default","revision":1,"scope":"shot","label":"Shot","pattern":"{shot}"}]}`),
		[]byte(`{"contract_version":"1.0","templates":[{"id":"vfx-files.shot.default","revision":1,"scope":"shot","label":"Shot","pattern":"{shot}","materialization":{"schema_revision":1,"root_binding":"root_is_parent_of_production","available_overrides":[]}}]}`),
	} {
		if err := decodeVFXFilesTemplates(body, "shot", &decoded); err == nil {
			t.Fatal("catalog without the explicit v1 root and materialization semantics was accepted")
		}
	}
}

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
			_, _ = io.WriteString(w, `{"contract_version":"1.0","templates":[{"id":"production-template","revision":2,"scope":"production","label":"Default","pattern":"{production}","materialization":{"schema_revision":1,"root_binding":"verified_root_is_production_root","available_overrides":[]}}]}`)
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

func TestVFXFilesInitialUIIsReadOnlyAndLinksToNativeSettings(t *testing.T) {
	state := vfxFilesAdminPageState{
		pluginVerified:   true,
		integrationReady: true,
		actionKey:        "ks-files-0123456789abcdef0123456789abcdef0123456789abcdef",
		selectedID:       "30000000-0000-4000-8000-000000000001",
		connections: []vfxFilesAdminConnection{{
			ContractVersion: "1.0", ID: "30000000-0000-4000-8000-000000000001",
			State: "connected", AccountMode: "my_drive",
		}},
		roots:             []vfxFilesAdminRoot{{ID: "fake-root", Name: "Synthetic Root", AccountMode: "my_drive"}},
		nativeSettingsURL: vfxFilesNativeSettingsURL("https://kitsu.example.test", vfxTestProductionID),
		templates: map[string][]vfxFilesAdminTemplate{
			"production": {{ID: "vfx-files.production.default", Revision: 1, Scope: "production", Label: "Default", Pattern: "{production}"}},
		},
	}
	for _, tc := range []struct{ lang, marker string }{
		{"en", "Drive connection, verified root, templates, sync settings, and activation are managed in the native Kitsu Files Plugin settings"},
		{"ja", "Drive接続、検証済みルート、テンプレート、同期設定、有効化はKitsuのFiles Plugin設定で管理"},
	} {
		page := renderVFXFilesAdminPage(
			httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files?lang="+tc.lang, nil),
			&model.Project{Name: "Synthetic Production"}, vfxTestProductionID, tc.lang, state,
		)
		if !strings.Contains(page, tc.marker) {
			t.Fatalf("%s omitted read-only explanation", tc.lang)
		}
		if strings.Contains(page, `name="sync_mode"`) || strings.Contains(page, `name="action" value="save_config"`) {
			t.Fatalf("%s rendered editable Drive configuration", tc.lang)
		}
		if !strings.Contains(page, `href="`+esc(vfxFilesNativeSettingsURL("https://kitsu.example.test", vfxTestProductionID))+`"`) {
			t.Fatalf("%s omitted navigation to native Kitsu Plugin settings", tc.lang)
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
	var activationGapErr error
	var activationGapPendingEpoch uint64
	var db *gorm.DB
	previewExpiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	makeJob := func(state string, updated string) string {
		return `{"contract_version":"1.0","id":"` + strings.ToUpper("60000000-0000-4000-8000-000000000001") + `","production_id":"` + strings.ToUpper(vfxTestProductionID) + `","config_revision":` + fmt.Sprint(configRevision) + `,"state":"` + state + `","progress":{"completed":1,"total":1},"failure_code":null,"created_at":"2026-10-13T00:00:00Z","updated_at":"` + updated + `"}`
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
			_, _ = io.WriteString(w, `{"contract_version":"1.0","production_id":"`+strings.ToUpper(vfxTestProductionID)+`","config_revision":`+fmt.Sprint(configRevision)+`,"connection_id":"30000000-0000-4000-8000-000000000001","drive":{"account_mode":"my_drive","owner_subject":"opaque-subject","root_id":"root-1","root_state":"verified"},"template_selection":{"selections":[{"scope":"production","template_id":"vfx-files.production.default","revision":1,"overrides":{}},{"scope":"shot","template_id":"vfx-files.shot.default","revision":1,"overrides":{}},{"scope":"task","template_id":"vfx-files.task.default","revision":1,"overrides":{}}]},"sync_policy":{"mode":"new_production","scope":{"episodes":false,"sequences":false,"shots":true,"assets":false,"tasks":true,"task_type_ids":[]},"notify_new_deliveries":false},"activation":{"enabled":`+activation+`,"activated_at":`+activatedAt+`,"notification_epoch":`+fmt.Sprint(activationEpoch)+`}}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/activation"):
			var localMirror model.VFXFilesProductionConfig
			if err := db.Where("production_id = ?", vfxTestProductionID).First(&localMirror).Error; err != nil {
				t.Errorf("activation marker was not durable before the Plugin call: %v", err)
			} else {
				activationGapPendingEpoch = localMirror.PendingActivationEpoch
				_, activationGapErr = model.AcceptVFXFilesEvents(db, "activation-test-key", "activation-window-nonce-0001", []model.VFXFilesEventInput{{
					ProducerID: "vfx-files-plugin", EventID: "60000000-0000-4000-8000-000000000011", EventType: "folder.provisioning.completed",
					ProductionID: vfxTestProductionID, ActivationEpoch: 1, Payload: []byte(`{"event":"activation-window"}`),
				}}, time.Now().UTC())
			}
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
				previewID = strings.ToUpper("70000000-0000-4000-8000-000000000001")
				previewIDsByKey[idempotencyKey] = previewID
				previewCreates++
			}
			_, _ = io.WriteString(w, `{"contract_version":"1.0","preview_id":"`+previewID+`","production_id":"`+strings.ToUpper(vfxTestProductionID)+`","config_revision":`+fmt.Sprint(configRevision)+`,"root_id":"root-1","state":"preview_ready","summary":{"existing":1,"create":0,"link":0,"excluded":0,"warnings":0},"changes":[],"expires_at":"`+previewExpiresAt+`"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sync-jobs"):
			syncJobCalls++
			if !activationEnabled {
				t.Error("Plugin received Apply before activation")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if providerPreviewID, ok := body["preview_id"].(string); !ok || !strings.EqualFold(providerPreviewID, previewID) {
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
	db, err = gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.VFXFilesSyncOperation{}, &model.VFXFilesProductionConfig{}, &model.VFXFilesEventInbox{}, &model.VFXFilesEventNonce{}, &model.VFXFilesEventConflict{}); err != nil {
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
	if err != nil || !strings.EqualFold(stored.PreviewID, previewID) || stored.SnapshotID != snapshotID {
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
	if activationGapPendingEpoch != 1 || !errors.Is(activationGapErr, model.ErrVFXFilesActivationPending) {
		t.Fatalf("activation did not hold the exact next-epoch event before Plugin readback: pending_epoch=%d event_error=%v", activationGapPendingEpoch, activationGapErr)
	}
	var activationGapEvents, activationGapNonces int64
	if err := db.Model(&model.VFXFilesEventInbox{}).Where("event_id = ?", "60000000-0000-4000-8000-000000000011").Count(&activationGapEvents).Error; err != nil || activationGapEvents != 0 {
		t.Fatalf("activation-window event was persisted before mirror readback: count=%d err=%v", activationGapEvents, err)
	}
	if err := db.Model(&model.VFXFilesEventNonce{}).Where("key_id = ? AND nonce = ?", "activation-test-key", "activation-window-nonce-0001").Count(&activationGapNonces).Error; err != nil || activationGapNonces != 1 {
		t.Fatalf("activation-window signed request nonce was not durably consumed: count=%d err=%v", activationGapNonces, err)
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

func TestVFXFilesAdminSettingsViewIsLocalizedAndReadOnly(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files?lang=ja", nil)
	state := vfxFilesAdminPageState{
		pluginVerified:    true,
		integrationReady:  true,
		actionKey:         "ks-files-0123456789abcdef0123456789abcdef0123456789abcdef",
		selectedID:        "30000000-0000-4000-8000-000000000001",
		connections:       []vfxFilesAdminConnection{{ContractVersion: "1.0", ID: "30000000-0000-4000-8000-000000000001", State: "connected", AccountMode: "shared_drive", DriveID: ptrVFXFilesAdmin("fake-drive-secret")}},
		roots:             []vfxFilesAdminRoot{{ID: "fake-root-1", Name: "Synthetic Root", AccountMode: "shared_drive", DriveID: ptrVFXFilesAdmin("fake-drive-1")}},
		nativeSettingsURL: vfxFilesNativeSettingsURL("https://kitsu.example.test", vfxTestProductionID),
		templates:         map[string][]vfxFilesAdminTemplate{"production": {{ID: "template-v1", Revision: 1, Scope: "production", Label: "Production standard", Pattern: "{production}"}}},
	}
	project := &model.Project{Name: "Synthetic Production"}
	page := renderVFXFilesAdminPage(request, project, vfxTestProductionID, "ja", state)
	for _, marker := range []string{"Drive状態", "Kitsu Files Plugin設定を開く", "Discord通知ルートを管理"} {
		if !strings.Contains(page, marker) {
			t.Errorf("Japanese read-only Files view missing %q", marker)
		}
	}
	for _, private := range []string{"fake-drive-secret", "fake-root-1", "Production standard", "name=\"action\""} {
		if strings.Contains(page, private) {
			t.Fatalf("read-only Files view rendered Drive configuration detail %q", private)
		}
	}
	readOnly := renderVFXFilesAdminPage(request, &model.Project{Name: "Validation", ValidationOnly: true}, vfxTestProductionID, "en", state)
	if strings.Contains(readOnly, `name="action" value="verify_root"`) || strings.Contains(readOnly, `name="action" value="authorize"`) || strings.Contains(readOnly, `name="action" value="activate"`) {
		t.Fatal("validation-only Production rendered a mutating integration control")
	}
}

func TestVFXFilesAdminNeverRendersDriveMutationControls(t *testing.T) {
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
	for _, lang := range []string{"ja", "en"} {
		page := renderVFXFilesAdminPage(request, project, vfxTestProductionID, lang, state)
		for _, action := range []string{"save_config", "activate", "deactivate", "preview_sync", "apply_sync"} {
			if strings.Contains(page, `name="action" value="`+action+`"`) {
				t.Errorf("%s rendered Drive mutation %q", lang, action)
			}
		}
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
	project := &model.Project{Name: "Synthetic Production"}
	for _, lang := range []string{"ja", "en"} {
		state.desired["activation"] = map[string]any{"enabled": false, "notification_epoch": float64(0)}
		request := httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files?lang="+lang, nil)
		page := renderVFXFilesAdminPage(request, project, vfxTestProductionID, lang, state)
		for _, action := range []string{"preview_sync", "apply_sync", "activate"} {
			if strings.Contains(page, `name="action" value="`+action+`"`) {
				t.Errorf("%s Drive settings page rendered unsafe %q action", lang, action)
			}
		}
		state.desired["activation"] = map[string]any{"enabled": true, "notification_epoch": float64(3)}
		page = renderVFXFilesAdminPage(request, project, vfxTestProductionID, lang, state)
		if strings.Contains(page, `name="action" value="deactivate"`) {
			t.Errorf("%s activation must be changed in the native Plugin settings", lang)
		}
		if strings.Contains(page, `name="action" value="apply_sync"`) {
			t.Errorf("%s Drive settings page rendered a sync mutation", lang)
		}
	}
}

type vfxAdminAuthorizerSnapshotCounter struct {
	vfxAdminAuthorizer
	snapshotCalls int
}

func (a *vfxAdminAuthorizerSnapshotCounter) BuildVFXFilesSnapshot(context.Context, string, vfxFilesSnapshotScope) ([]vfxFilesSnapshotObject, string, error) {
	a.snapshotCalls++
	return nil, "", errors.New("snapshot should not be requested for an unsupported mode")
}

func TestVFXFilesUnsupportedSavedModeRejectsDirectPreviewApplyAndActivation(t *testing.T) {
	var desiredReads int
	var unexpectedRequests []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/desired-config") {
			desiredReads++
			w.Header().Set("ETag", `"4"`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"contract_version":"1.0","production_id":"`+vfxTestProductionID+`","config_revision":4,"connection_id":"30000000-0000-4000-8000-000000000001","drive":{"account_mode":"my_drive","owner_subject":"opaque-subject","root_id":"root-1","root_state":"verified"},"template_selection":{"selections":[{"scope":"production","template_id":"tpl","revision":1,"overrides":{}}]},"sync_policy":{"mode":"future_only","scope":{"episodes":false,"sequences":false,"shots":false,"assets":false,"tasks":false,"task_type_ids":[]},"notify_new_deliveries":false},"activation":{"enabled":true,"activated_at":"2026-10-13T00:00:00Z","notification_epoch":3}}`)
			return
		}
		unexpectedRequests = append(unexpectedRequests, r.Method+" "+r.URL.Path)
		http.Error(w, "unexpected provider operation", http.StatusNotFound)
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
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.VFXFilesProductionConfig{}, &model.VFXFilesSyncOperation{}); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
	preview := []byte(`{"contract_version":"1.0","preview_id":"40000000-0000-4000-8000-000000000001","production_id":"` + vfxTestProductionID + `","config_revision":4,"root_id":"root-1","state":"preview_ready","expires_at":"` + expires + `","changes":[]}`)
	if err := model.StoreVFXFilesSyncPreview(db, vfxTestProductionID, "50000000-0000-4000-8000-000000000001", 4, preview); err != nil {
		t.Fatal(err)
	}
	authorizer := &vfxAdminAuthorizerSnapshotCounter{vfxAdminAuthorizer: vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: true}}
	runtime := &vfxfiles.RuntimeConfig{Client: client}
	project := &model.Project{KitsuProjectID: vfxTestProductionID}
	for _, locale := range []struct{ lang, preview, apply, activate string }{
		{"en", "not yet supported for Preview", "not yet supported for Apply", "cannot be activated yet"},
		{"ja", "まだPreviewに対応していません", "まだApplyに対応していません", "まだ有効化できません"},
	} {
		for _, tc := range []struct{ action, key, want string }{
			{"preview_sync", "ks-files-0123456789abcdef", locale.preview},
			{"apply_sync", "ks-files-1123456789abcdef", locale.apply},
			{"activate", "ks-files-2123456789abcdef", locale.activate},
		} {
			t.Run(locale.lang+"/"+tc.action, func(t *testing.T) {
				form := url.Values{"action": {tc.action}, "idempotency_key": {tc.key}}
				req := httptest.NewRequest(http.MethodPost, "/bot/admin/vfx-files", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				message, redirect, completed := runVFXFilesAdminAction(context.Background(), db, runtime, authorizer, project, req, locale.lang)
				if completed || redirect != "" || !strings.Contains(message, tc.want) {
					t.Fatalf("%s unsupported saved mode action %q was not rejected clearly: message=%q redirect=%q completed=%v", locale.lang, tc.action, message, redirect, completed)
				}
			})
		}
	}
	if desiredReads != 6 || len(unexpectedRequests) != 0 || authorizer.snapshotCalls != 0 {
		t.Fatalf("unsupported mode reached work/provider mutation: desired reads=%d unexpected=%v snapshot calls=%d", desiredReads, unexpectedRequests, authorizer.snapshotCalls)
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

func TestVFXFilesAdminRendersReadOnlyPluginAuthorityAndDiscordNavigation(t *testing.T) {
	productionID := vfxTestProductionID
	connectionID := "30000000-0000-4000-8000-000000000001"
	rootID := "synthetic-root"
	rootState := "verified"
	status := vfxFilesDriveAuthorityStatus{
		Owner: "plugin", State: "configured", ConfigRevision: 4, ActivationEpoch: 7, ActivationEnabled: true,
		ConnectionID: &connectionID, RootID: &rootID, RootState: &rootState,
	}
	if !validVFXFilesDriveAuthorityStatus(status) {
		t.Fatal("valid Plugin authority status was rejected")
	}
	page := renderVFXFilesAdminPage(
		httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files", nil),
		&model.Project{Name: "Synthetic Production"}, productionID, "en",
		vfxFilesAdminPageState{
			readOnlyDrive:     &status,
			nativeSettingsURL: vfxFilesNativeSettingsURL("https://kitsu.example.test", productionID),
		},
	)
	for _, marker := range []string{
		"KitsuSync is read-only", "Connection", "Verified", "Active", "4 / 7",
		"/api/plugins/vfx_files/frontend/?drive_settings=1&amp;production_id=" + productionID,
		"Discord delivery and recipient Task Type routing remain managed by KitsuSync",
		"tab=notifications",
	} {
		if !strings.Contains(page, marker) {
			t.Errorf("read-only Files page missing %q", marker)
		}
	}
	for _, action := range []string{"authorize", "verify_root", "revoke", "save_config", "activate", "deactivate"} {
		if strings.Contains(page, `name="action" value="`+action+`"`) {
			t.Errorf("read-only Files page rendered Drive mutation %q", action)
		}
	}
	if strings.Contains(page, rootID) || strings.Contains(page, connectionID) {
		t.Fatal("provider connection or root identifier was unnecessarily rendered")
	}
}

func TestVFXFilesNativeSettingsURLAndAuthorityStatusFailClosed(t *testing.T) {
	if got := vfxFilesNativeSettingsURL("javascript:alert(1)", vfxTestProductionID); got != "" {
		t.Fatalf("unsafe native settings URL accepted: %q", got)
	}
	if got := vfxFilesNativeSettingsURL("https://kitsu.example.test", "bad-id"); got != "" {
		t.Fatalf("invalid Production ID accepted: %q", got)
	}
	status := vfxFilesDriveAuthorityStatus{Owner: "plugin", State: "configured", ActivationEnabled: true}
	if validVFXFilesDriveAuthorityStatus(status) {
		t.Fatal("configured authority without a verified root was accepted")
	}
	legacy := vfxFilesDriveAuthorityStatus{Owner: "legacy_kitsusync", State: "legacy_import_required"}
	if !validVFXFilesDriveAuthorityStatus(legacy) {
		t.Fatal("explicit legacy import-required status was rejected")
	}
}

func TestVFXFilesManagementConsumesAuthorityStatusReadOnly(t *testing.T) {
	connectionID := "30000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		name       string
		statusCode int
		configured bool
	}{
		{name: "verified Plugin authority", statusCode: http.StatusOK, configured: true},
		{name: "authority status unavailable", statusCode: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/drive-authority") {
					if tc.statusCode != http.StatusOK {
						w.WriteHeader(tc.statusCode)
						return
					}
					_, _ = io.WriteString(w, `{"owner":"plugin","state":"configured","config_revision":4,"activation_epoch":7,"activation_enabled":true,"connection_id":"`+connectionID+`","root_id":"synthetic-root","account_mode":"my_drive","root_state":"verified"}`)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/desired-config") {
					w.Header().Set("ETag", `"4"`)
					_, _ = io.WriteString(w, `{"contract_version":"1.0","production_id":"`+vfxTestProductionID+`","config_revision":4,"connection_id":"`+connectionID+`","drive":{"account_mode":"my_drive","owner_subject":"opaque-subject","root_id":"synthetic-root","root_state":"verified"},"template_selection":{"selections":[{"scope":"production","template_id":"production-template","revision":1,"overrides":{}}]},"sync_policy":{"mode":"new_production","scope":{"episodes":false,"sequences":false,"shots":false,"assets":false,"tasks":false,"task_type_ids":[]},"notify_new_deliveries":false},"activation":{"enabled":true,"activated_at":"2026-10-17T00:00:00Z","notification_epoch":7}}`)
					return
				}
				http.Error(w, "unexpected Plugin request", http.StatusNotFound)
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
			state := vfxFilesAdminPageState{}
			loadVFXFilesAdminState(context.Background(), db, &vfxfiles.RuntimeConfig{Client: client}, vfxAdminAuthorizer{actorID: vfxTestActorID, allowed: true}, vfxTestProductionID, "", &state, "en")
			page := renderVFXFilesAdminPage(httptest.NewRequest(http.MethodGet, "/bot/admin/vfx-files?lang=en", nil), &model.Project{Name: "Test Production"}, vfxTestProductionID, "en", state)
			for _, call := range calls {
				if strings.HasPrefix(call, "POST ") || strings.HasPrefix(call, "PUT ") || strings.Contains(call, "/drive-connections") || strings.HasSuffix(call, "/templates") {
					t.Fatalf("read-only status page called a Drive configuration endpoint: %s", call)
				}
			}
			if tc.configured {
				if state.pluginState != "available" || !state.pluginVerified || state.readOnlyDrive == nil || !state.desiredReady {
					t.Fatalf("Plugin authority or read-only mirror was not verified: %+v", state)
				}
				if strings.Contains(page, connectionID) || strings.Contains(page, "opaque-subject") || strings.Contains(page, `name="action"`) {
					t.Fatal("read-only page exposed provider identity or a Drive mutation")
				}
			} else if state.pluginState != "unavailable" || state.pluginVerified || state.readOnlyDrive != nil || strings.Contains(page, "Connected") {
				t.Fatalf("Plugin outage was treated as verified status: %+v", state)
			}
		})
	}
}

func ptrVFXFilesAdmin(value string) *string { return &value }

package setup

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestVFXFilesAdminRejectsPostBeforeResolvingKitsu(t *testing.T) {
	resetSessions()
	t.Cleanup(resetSessions)

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Project{KitsuProjectID: vfxTestProductionID, Name: "Synthetic Production"}).Error; err != nil {
		t.Fatal(err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	baseURL := "https://plugin.example.test" + vfxfiles.ServiceAudienceSuffix
	client, err := vfxfiles.NewPluginClient(baseURL, http.DefaultClient, vfxfiles.ServiceTokenConfig{
		Issuer: "https://kitsusync.example.test", Audience: baseURL, Subject: "kitsusync",
		ClientID: "kitsusync", KeyID: "test-key", PrivateKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}

	token := newSessionToken("manager@example.test", "synthetic-session-token", "manager", "")
	request := httptest.NewRequest(http.MethodPost, "/bot/admin/vfx-files?production="+vfxTestProductionID, strings.NewReader("action=activate"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

	resolveCalls := 0
	handler := VFXFilesAdminHandler(db, &vfxfiles.RuntimeConfig{Client: client}, func(context.Context) (KitsuURLModel, error) {
		resolveCalls++
		return KitsuURLModel{}, errors.New("unexpected Kitsu resolution for read-only POST")
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d: %s", response.Code, http.StatusMethodNotAllowed, response.Body.String())
	}
	if resolveCalls != 0 {
		t.Fatalf("read-only POST triggered %d Kitsu connection resolutions, want 0", resolveCalls)
	}
}

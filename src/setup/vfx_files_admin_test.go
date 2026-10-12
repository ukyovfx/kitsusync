package setup

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
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
			_, _ = io.WriteString(w, `{"contract_version":"1.0","production_id":"10000000-0000-4000-8000-000000000001","config_revision":1,"connection_id":"30000000-0000-4000-8000-000000000001","drive":{"account_mode":"my_drive","owner_subject":"verified-plugin-subject","root_id":"root-1","root_state":"verified"},"template_selection":{"selections":[{"scope":"production","template_id":"production-template","revision":2,"overrides":{}}]},"sync_policy":{"mode":"future_only","scope":{"episodes":false,"sequences":false,"shots":false,"assets":false,"tasks":false,"task_type_ids":[]},"notify_new_deliveries":false},"activation":{"enabled":false,"activated_at":null,"notification_epoch":0}}`)
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
	form := url.Values{"action": {"save_config"}, "connection_id": {"30000000-0000-4000-8000-000000000001"}, "root_id": {"root-1"}, "sync_mode": {"future_only"}, "template_production": {"production-template"}, "owner_subject": {"attacker-controlled-subject"}, "idempotency_key": {"ks-files-0123456789abcdef:save"}}
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

func TestVFXFilesMockProviderSnapshotPreviewApplyProgressRetryReconcile(t *testing.T) {
	var snapshotID, previewID, jobID string
	jobState := "failed"
	var calls []string
	makeJob := func(state string, updated string) string {
		return `{"contract_version":"1.0","id":"60000000-0000-4000-8000-000000000001","production_id":"` + vfxTestProductionID + `","config_revision":1,"state":"` + state + `","progress":{"completed":1,"total":1},"failure_code":null,"created_at":"2026-10-13T00:00:00Z","updated_at":"` + updated + `"}`
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-KitsuSync-Actor-ID") != vfxTestActorID {
			t.Errorf("actor not bound: %q", r.Header.Get("X-KitsuSync-Actor-ID"))
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/desired-config"):
			w.Header().Set("ETag", `"1"`)
			_, _ = io.WriteString(w, `{"contract_version":"1.0","production_id":"`+vfxTestProductionID+`","config_revision":1,"connection_id":"30000000-0000-4000-8000-000000000001","drive":{"account_mode":"my_drive","owner_subject":"opaque-subject","root_id":"root-1","root_state":"verified"},"template_selection":{"selections":[{"scope":"production","template_id":"tpl","revision":1,"overrides":{}}]},"sync_policy":{"mode":"future_only","scope":{"episodes":false,"sequences":false,"shots":false,"assets":false,"tasks":false,"task_type_ids":[]},"notify_new_deliveries":false},"activation":{"enabled":false,"activated_at":null,"notification_epoch":0}}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sync-snapshots"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			snapshotID, _ = body["snapshot_id"].(string)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"contract_version":"1.0","id":"`+snapshotID+`","production_id":"`+vfxTestProductionID+`","config_revision":1,"state":"uploading","page_count":1,"received_pages":0,"object_count":1,"expires_at":"2026-10-13T01:00:00Z"}`)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/pages/"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["page_index"] != float64(0) || body["page_sha256"] == nil {
				t.Error("snapshot page lacks index or digest")
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"contract_version":"1.0","id":"`+snapshotID+`","production_id":"`+vfxTestProductionID+`","config_revision":1,"state":"uploading","page_count":1,"received_pages":1,"object_count":1,"expires_at":"2026-10-13T01:00:00Z"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"contract_version":"1.0","id":"`+snapshotID+`","production_id":"`+vfxTestProductionID+`","config_revision":1,"state":"complete","page_count":1,"received_pages":1,"object_count":1,"expires_at":"2026-10-13T01:00:00Z"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sync-previews"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["snapshot_id"] != snapshotID || body["config_revision"] != float64(1) {
				t.Error("preview did not use uploaded snapshot/config revision")
			}
			previewID = "70000000-0000-4000-8000-000000000001"
			_, _ = io.WriteString(w, `{"contract_version":"1.0","preview_id":"`+previewID+`","production_id":"`+vfxTestProductionID+`","config_revision":1,"root_id":"root-1","state":"preview_ready","summary":{"existing":1,"create":0,"link":0,"excluded":0,"warnings":0},"changes":[],"expires_at":"2026-10-14T00:00:00Z"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sync-jobs"):
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
	if err := db.AutoMigrate(&model.VFXFilesSyncOperation{}); err != nil {
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
	if message, complete := run("preview_sync", "ks-files-0123456789abcdef"); !complete || !strings.Contains(message, "preview") {
		t.Fatalf("preview: complete=%v message=%q calls=%v", complete, message, calls)
	}
	stored, err := model.FindVFXFilesSyncOperation(db, vfxTestProductionID)
	if err != nil || stored.PreviewID != previewID || stored.SnapshotID != snapshotID {
		t.Fatalf("preview not stored: %+v err=%v", stored, err)
	}
	if message, complete := run("apply_sync", "ks-files-1123456789abcdef"); !complete || !strings.Contains(message, "accepted") {
		t.Fatalf("apply: complete=%v message=%q", complete, message)
	}
	if message, complete := run("refresh_sync_job", "ks-files-2123456789abcdef"); !complete || !strings.Contains(message, "refreshed") {
		t.Fatalf("job refresh: complete=%v message=%q", complete, message)
	}
	if message, complete := run("retry_sync_job", "ks-files-3123456789abcdef"); !complete || !strings.Contains(message, "accepted") {
		t.Fatalf("job retry: complete=%v message=%q", complete, message)
	}
	if message, complete := run("reconcile_sync_job", "ks-files-4123456789abcdef"); !complete || !strings.Contains(message, "accepted") {
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

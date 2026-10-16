package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"app/src/api/vfxfiles"
	"app/src/model"
	"gorm.io/gorm"
)

type vfxFilesAdminPageState struct {
	message           string
	pluginState       string
	pluginVerified    bool
	actionKey         string
	diagnostics       *model.VFXFilesEventDiagnostics
	diagnosticsOK     bool
	integrationReady  bool
	connections       []vfxFilesAdminConnection
	roots             []vfxFilesAdminRoot
	rootStatus        string
	templates         map[string][]vfxFilesAdminTemplate
	selectedID        string
	desired           map[string]any
	etag              string
	desiredReady      bool
	connectionsOK     bool
	syncOperation     *model.VFXFilesSyncOperation
	preview           *vfxFilesSyncPreview
	job               *vfxFilesSyncJob
	readOnlyDrive     *vfxFilesDriveAuthorityStatus
	nativeSettingsURL string
}

type vfxFilesDriveAuthorityStatus struct {
	Owner             string  `json:"owner"`
	State             string  `json:"state"`
	ConfigRevision    uint64  `json:"config_revision"`
	ActivationEpoch   uint64  `json:"activation_epoch"`
	ActivationEnabled bool    `json:"activation_enabled"`
	ConnectionID      *string `json:"connection_id"`
	RootID            *string `json:"root_id"`
	AccountMode       *string `json:"account_mode"`
	RootState         *string `json:"root_state"`
}

type vfxFilesSyncPreview struct {
	ContractVersion string `json:"contract_version"`
	PreviewID       string `json:"preview_id"`
	ProductionID    string `json:"production_id"`
	ConfigRevision  uint64 `json:"config_revision"`
	RootID          string `json:"root_id"`
	State           string `json:"state"`
	Summary         struct {
		Existing int `json:"existing"`
		Create   int `json:"create"`
		Link     int `json:"link"`
		Excluded int `json:"excluded"`
		Warnings int `json:"warnings"`
	} `json:"summary"`
	Changes []struct {
		Action      string `json:"action"`
		ObjectType  string `json:"object_type"`
		ObjectID    string `json:"object_id"`
		DisplayName string `json:"display_name"`
		Reason      string `json:"reason"`
	} `json:"changes"`
	ExpiresAt string `json:"expires_at"`
}

type vfxFilesSyncJob struct {
	ContractVersion string `json:"contract_version"`
	ID              string `json:"id"`
	ProductionID    string `json:"production_id"`
	ConfigRevision  uint64 `json:"config_revision"`
	State           string `json:"state"`
	Progress        struct {
		Completed int `json:"completed"`
		Total     int `json:"total"`
	} `json:"progress"`
	FailureCode *string `json:"failure_code"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type vfxFilesAdminConnection struct {
	ContractVersion string   `json:"contract_version"`
	ID              string   `json:"id"`
	State           string   `json:"state"`
	AccountMode     string   `json:"account_mode"`
	OwnerLabel      *string  `json:"owner_label"`
	DriveID         *string  `json:"drive_id"`
	GrantedScopes   []string `json:"granted_scopes"`
}

type vfxFilesAdminRoot struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	DriveID     *string `json:"drive_id"`
	AccountMode string  `json:"account_mode"`
}

type vfxFilesAdminTemplate struct {
	ID              string   `json:"id"`
	Revision        uint64   `json:"revision"`
	Scope           string   `json:"scope"`
	Label           string   `json:"label"`
	Pattern         string   `json:"pattern"`
	Tokens          []string `json:"available_tokens"`
	Materialization struct {
		SchemaRevision     int      `json:"schema_revision"`
		RootBinding        string   `json:"root_binding"`
		AvailableOverrides []string `json:"available_overrides"`
	} `json:"materialization"`
}

type vfxFilesRootCapabilities struct {
	CanRead        bool `json:"can_read"`
	CanAddChildren bool `json:"can_add_children"`
}

type vfxFilesRootVerification struct {
	ContractVersion string                   `json:"contract_version"`
	ConnectionID    string                   `json:"connection_id"`
	AccountMode     string                   `json:"account_mode"`
	RootID          string                   `json:"root_id"`
	DriveID         *string                  `json:"drive_id"`
	OwnerSubject    string                   `json:"owner_subject"`
	State           string                   `json:"state"`
	Capabilities    vfxFilesRootCapabilities `json:"capabilities"`
}

// verifiedVFXFilesRootIdentity accepts identity only from the authenticated
// Plugin verification response, bound to the exact connection/root/mode that
// KitsuSync just checked. Browser form values are never an identity source.
func verifiedVFXFilesRootIdentity(result vfxFilesRootVerification, connectionID, accountMode, rootID, expectedDriveID string) (ownerSubject, driveID string, err error) {
	if result.ContractVersion != "1.0" || result.ConnectionID != connectionID || result.AccountMode != accountMode || result.RootID != rootID || result.State != "verified" || !result.Capabilities.CanRead || !result.Capabilities.CanAddChildren {
		return "", "", errors.New("root verification binding invalid")
	}
	if accountMode == "my_drive" {
		owner := strings.TrimSpace(result.OwnerSubject)
		if owner == "" || len(owner) > 255 || strings.ContainsAny(owner, "\x00\r\n") || result.DriveID != nil && strings.TrimSpace(*result.DriveID) != "" {
			return "", "", errors.New("verified My Drive subject invalid")
		}
		return owner, "", nil
	}
	if accountMode != "shared_drive" || strings.TrimSpace(result.OwnerSubject) != "" || result.DriveID == nil || strings.TrimSpace(*result.DriveID) == "" || strings.TrimSpace(*result.DriveID) != strings.TrimSpace(expectedDriveID) {
		return "", "", errors.New("verified Shared Drive identity invalid")
	}
	return "", strings.TrimSpace(*result.DriveID), nil
}

var candidateDriveURLRE = regexp.MustCompile(`^https://drive\.google\.com/drive/(u/[0-9]+/)?folders/[A-Za-z0-9_-]+([?#][A-Za-z0-9_=&%-]*)?$`)

type vfxFilesAdminPluginResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// VFXFilesAdminHandler binds every Plugin request to the authenticated Kitsu
// session and freshly checked Production membership. The Plugin remains the
// sole owner of Drive credentials, objects, and sync jobs.
func VFXFilesAdminHandler(db *gorm.DB, runtime *vfxfiles.RuntimeConfig, resolveKitsu func(context.Context) (KitsuURLModel, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lang := currentLang(r)
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		email, token, role, ok := CurrentSessionKitsuAuth(r)
		if !ok || !isStudioManagerOrHigher(role) {
			http.Error(w, "administrator access required", http.StatusForbidden)
			return
		}
		productionID := strings.TrimSpace(r.URL.Query().Get("production"))
		project := model.FindProjectByKitsuID(db, productionID)
		if project == nil || !vfxfiles.ValidID(productionID) {
			http.NotFound(w, r)
			return
		}
		state := vfxFilesAdminPageState{}
		if runtime == nil || runtime.Client == nil {
			http.NotFound(w, r)
			return
		}
		if diagnostics, err := model.ReadVFXFilesEventDiagnostics(db, productionID); err == nil {
			state.diagnostics, state.diagnosticsOK = &diagnostics, true
		}
		var authorizer vfxfiles.ProductionAdminAuthorizer
		if runtime != nil && runtime.Client != nil {
			if resolveKitsu == nil {
				state.message = t(lang, "Kitsuの接続先を確認できません。", "The Kitsu authority could not be verified.")
			} else if connection, err := resolveKitsu(r.Context()); err != nil {
				state.message = t(lang, "Kitsuの接続先を確認できません。", "The Kitsu authority could not be verified.")
			} else {
				state.nativeSettingsURL = vfxFilesNativeSettingsURL(connection.DisplayBaseURL, productionID)
				authorizer = NewVFXFilesKitsuSessionAuthorizer(connection, email, token, role, nil)
				if _, allowed, err := authorizer.AuthorizeProductionAdmin(r.Context(), productionID); err != nil || !allowed {
					http.Error(w, "Production administrator access could not be verified", http.StatusForbidden)
					return
				}
				state.integrationReady = true
			}
		}
		if r.Method == http.MethodPost {
			http.Error(w, "Drive settings are managed in the native Kitsu Files Plugin; this page is read-only", http.StatusMethodNotAllowed)
			return
		}
		if runtime != nil && runtime.Client != nil && authorizer != nil {
			selected := strings.TrimSpace(r.URL.Query().Get("connection_id"))
			if r.Method == http.MethodPost && state.selectedID != "" {
				selected = state.selectedID
			}
			loadVFXFilesAdminState(r.Context(), db, runtime, authorizer, productionID, selected, &state, lang)
		}
		if state.actionKey == "" {
			state.actionKey, _ = newVFXFilesIdempotencyKey()
		}
		body := renderVFXFilesAdminPage(r, project, productionID, lang, state)
		fmt.Fprint(w, adminPage(lang, t(lang, "Files連携", "Files integration"), r, body))
	}
}

// VFXFilesOAuthReturnHandler restores the Production context after the
// Plugin-owned OAuth callback. The opaque intent ID is only a lookup key; the
// Production and actor are loaded from SQLite and re-authorized against Kitsu.
// This endpoint never accepts or exchanges an OAuth code or token.
func VFXFilesOAuthReturnHandler(db *gorm.DB, resolveKitsu func(context.Context) (KitsuURLModel, error)) http.HandlerFunc {
	return vfxFilesOAuthReturnHandler(db, resolveKitsu, func(connection KitsuURLModel, email, token, role string) vfxfiles.ProductionAdminAuthorizer {
		return NewVFXFilesKitsuSessionAuthorizer(connection, email, token, role, nil)
	})
}

func vfxFilesOAuthReturnHandler(db *gorm.DB, resolveKitsu func(context.Context) (KitsuURLModel, error), makeAuthorizer func(KitsuURLModel, string, string, string) vfxfiles.ProductionAdminAuthorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		email, token, role, ok := CurrentSessionKitsuAuth(r)
		if !ok || !isStudioManagerOrHigher(role) || resolveKitsu == nil || makeAuthorizer == nil {
			http.Error(w, "administrator access required", http.StatusForbidden)
			return
		}
		query := r.URL.Query()
		if query.Has("code") || query.Has("access_token") || query.Has("refresh_token") {
			http.Error(w, "OAuth credentials are not accepted by KitsuSync", http.StatusBadRequest)
			return
		}
		authorizationID := strings.TrimSpace(r.URL.Query().Get("authorization_id"))
		intent, err := model.FindVFXFilesAuthorizationIntent(db, authorizationID, time.Now().UTC())
		if err != nil {
			http.Error(w, "authorization return could not be verified", http.StatusBadRequest)
			return
		}
		connection, err := resolveKitsu(r.Context())
		if err != nil {
			http.Error(w, "Production administrator access could not be verified", http.StatusForbidden)
			return
		}
		authorizer := makeAuthorizer(connection, email, token, role)
		actorID, allowed, err := authorizer.AuthorizeProductionAdmin(r.Context(), intent.ProductionID)
		if err != nil || !allowed || !strings.EqualFold(actorID, intent.ActorID) {
			http.Error(w, "Production administrator access could not be verified", http.StatusForbidden)
			return
		}
		if err := model.ConsumeVFXFilesAuthorizationIntent(db, intent.AuthorizationID, intent.ProductionID, intent.ActorID, time.Now().UTC()); err != nil {
			http.Error(w, "authorization return could not be verified", http.StatusBadRequest)
			return
		}
		prefix := ""
		if strings.HasPrefix(r.URL.Path, "/bot/") {
			prefix = "/bot"
		}
		destination := prefix + "/admin/vfx-files?production=" + url.QueryEscape(intent.ProductionID)
		http.Redirect(w, r, withLang(destination, r), http.StatusSeeOther)
	}
}

func loadVFXFilesAdminState(ctx context.Context, db *gorm.DB, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID, _ string, state *vfxFilesAdminPageState, lang string) {
	response, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/productions/"+productionID+"/drive-authority", nil, nil, "vfx.config.read", false, "", "")
	if err != nil || response.StatusCode != http.StatusOK {
		state.pluginState = "unavailable"
		state.message = t(lang, "Files PluginのDrive状態を確認できません。設定変更はこの画面では利用できません。", "The Files Plugin Drive status is unavailable. Drive settings cannot be changed here.")
		return
	}
	var authority vfxFilesDriveAuthorityStatus
	if json.Unmarshal(response.Body, &authority) != nil || !validVFXFilesDriveAuthorityStatus(authority) {
		state.pluginState = "incompatible"
		state.message = t(lang, "Files PluginのDrive状態応答を確認できません。", "The Files Plugin Drive status response is incompatible.")
		return
	}
	state.readOnlyDrive = &authority
	state.pluginState = "available"
	state.pluginVerified = true
	if (authority.State != "configured" && authority.State != "legacy_import_required") || authority.ConnectionID == nil || authority.RootID == nil {
		return
	}

	// The signed Plugin read is a mirror for event fencing and notifications;
	// KitsuSync never writes Drive configuration or renders the mirrored fields.
	config, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/productions/"+productionID+"/desired-config", nil, nil, "vfx.config.read", false, "", "")
	if err != nil || config.StatusCode != http.StatusOK || strings.TrimSpace(config.Header.Get("ETag")) == "" {
		state.message = t(lang, "読み取り専用のDrive状態は確認できましたが、通知安全ミラーを更新できません。", "Drive status is read-only, but the notification safety mirror could not be refreshed.")
		return
	}
	var desired map[string]any
	if json.Unmarshal(config.Body, &desired) != nil || model.StoreVFXFilesDesiredConfigMirror(db, productionID, config.Body) != nil {
		state.message = t(lang, "読み取り専用のDrive状態は確認できましたが、通知安全ミラーを更新できません。", "Drive status is read-only, but the notification safety mirror could not be refreshed.")
		return
	}
	state.desired, state.etag, state.desiredReady = desired, config.Header.Get("ETag"), true
	operation, operationErr := model.FindVFXFilesSyncOperation(db, productionID)
	if operationErr == nil && operation.ConfigRevision == uint64FromJSON(desired["config_revision"]) {
		state.syncOperation = &operation
		if operation.PreviewJSON != "" && operation.PreviewExpiresAt != nil && operation.PreviewExpiresAt.After(time.Now().UTC()) {
			var preview vfxFilesSyncPreview
			if json.Unmarshal([]byte(operation.PreviewJSON), &preview) == nil && strings.EqualFold(preview.PreviewID, operation.PreviewID) && strings.EqualFold(preview.ProductionID, productionID) && preview.ConfigRevision == operation.ConfigRevision && preview.State == "preview_ready" {
				state.preview = &preview
			}
		}
		if operation.JobJSON != "" {
			var job vfxFilesSyncJob
			if json.Unmarshal([]byte(operation.JobJSON), &job) == nil && strings.EqualFold(job.ID, operation.JobID) && strings.EqualFold(job.ProductionID, productionID) && job.ConfigRevision == operation.ConfigRevision {
				state.job = &job
			}
		}
	} else if operationErr != nil && !errors.Is(operationErr, gorm.ErrRecordNotFound) && state.message == "" {
		state.message = t(lang, "Filesの同期状態を安全に読み取れません。", "Files sync state could not be read safely.")
	}
}

func loadVFXFilesAdminRoots(ctx context.Context, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID string, connection *vfxFilesAdminConnection, state *vfxFilesAdminPageState) {
	if connection == nil || connection.State != "connected" {
		return
	}
	roots, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/drive-connections/"+connection.ID+"/roots", url.Values{"production_id": {productionID}}, nil, "vfx.roots.read", false, "", "")
	if err != nil || roots.StatusCode == http.StatusUnauthorized || roots.StatusCode == http.StatusForbidden || roots.StatusCode >= http.StatusInternalServerError {
		state.rootStatus = "unavailable"
		return
	}
	if roots.StatusCode != http.StatusOK || decodeVFXFilesRoots(roots.Body, &state.roots) != nil {
		state.rootStatus = "incompatible"
		return
	}
	state.rootStatus = "verified"
}

func runVFXFilesAdminAction(ctx context.Context, db *gorm.DB, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, project *model.Project, r *http.Request, lang string) (message, redirect string, completed bool) {
	if err := r.ParseForm(); err != nil {
		return t(lang, "リクエストを確認してください。", "Check the request and try again."), "", false
	}
	productionID := project.KitsuProjectID
	key := safeSubmittedIdempotencyKey(r.Form.Get("idempotency_key"))
	if key == "" {
		return t(lang, "操作識別子の有効期限が切れました。画面を再読み込みしてください。", "The action token expired. Reload the page and try again."), "", false
	}
	switch r.Form.Get("action") {
	case "save_config":
		return saveVFXFilesDesiredConfig(ctx, db, runtime, authorizer, productionID, r, lang, key)
	case "preview_sync":
		return previewVFXFilesSync(ctx, db, runtime, authorizer, project, key, lang)
	case "apply_sync":
		return applyVFXFilesSync(ctx, db, runtime, authorizer, productionID, key, lang)
	case "refresh_sync_job":
		return refreshVFXFilesSyncJob(ctx, db, runtime, authorizer, productionID, lang)
	case "retry_sync_job", "reconcile_sync_job":
		return commandVFXFilesSyncJob(ctx, db, runtime, authorizer, productionID, r.Form.Get("action"), key, lang)
	case "verify_root":
		connectionID := strings.TrimSpace(r.Form.Get("connection_id"))
		rootID := strings.TrimSpace(r.Form.Get("root_id"))
		candidateURL := strings.TrimSpace(r.Form.Get("candidate_url"))
		if !vfxfiles.ValidID(connectionID) || (rootID == "") == (candidateURL == "") || len(candidateURL) > 2048 || candidateURL != "" && !candidateDriveURLRE.MatchString(candidateURL) {
			return t(lang, "接続とDriveルートを確認してください。", "Choose a connection and exactly one valid Drive root candidate."), "", false
		}
		connections, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/drive-connections", url.Values{"production_id": {productionID}}, nil, "vfx.connections.read", false, "", "")
		if err != nil || connections.StatusCode != http.StatusOK {
			return t(lang, "Drive接続を再確認できません。", "Drive connection could not be reverified."), "", false
		}
		var available []vfxFilesAdminConnection
		if err := decodeVFXFilesConnections(connections.Body, &available); err != nil {
			return t(lang, "Drive接続の応答が不正です。", "The Drive connection response is invalid."), "", false
		}
		var selected *vfxFilesAdminConnection
		for index := range available {
			if available[index].ID == connectionID {
				selected = &available[index]
				break
			}
		}
		if selected == nil || selected.State != "connected" {
			return t(lang, "接続済みのDrive接続を選択してください。", "Choose a connected Drive account."), "", false
		}
		requestBody := map[string]any{"contract_version": "1.0", "production_id": productionID, "account_mode": selected.AccountMode}
		if rootID != "" {
			requestBody["root_id"] = rootID
			rootsResponse, rootsErr := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/drive-connections/"+connectionID+"/roots", url.Values{"production_id": {productionID}}, nil, "vfx.roots.read", false, "", "")
			var roots []vfxFilesAdminRoot
			if rootsErr != nil || rootsResponse.StatusCode != http.StatusOK || decodeVFXFilesRoots(rootsResponse.Body, &roots) != nil {
				return t(lang, "Driveルート候補を確認できません。", "Drive root candidates could not be verified."), "", false
			}
			found := false
			for _, root := range roots {
				if root.ID == rootID && root.AccountMode == selected.AccountMode {
					found = true
					if root.DriveID != nil {
						requestBody["drive_id"] = *root.DriveID
					}
					break
				}
			}
			if !found {
				return t(lang, "ルート候補を一覧から再選択してください。", "Select a current root candidate from the list."), "", false
			}
		} else {
			requestBody["candidate_url"] = candidateURL
		}
		if selected.AccountMode == "shared_drive" && requestBody["drive_id"] == nil {
			if selected.DriveID == nil || len(*selected.DriveID) == 0 {
				return t(lang, "共有ドライブの識別情報がありません。", "The Shared Drive identity could not be verified."), "", false
			}
			requestBody["drive_id"] = *selected.DriveID
		}
		body, _ := json.Marshal(requestBody)
		verified, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPost, "/drive-connections/"+connectionID+"/roots", url.Values{"production_id": {productionID}}, body, "vfx.roots.verify", true, key, "")
		if err != nil || verified.StatusCode != http.StatusOK {
			return t(lang, "Driveルートを検証できませんでした。URLの保存やフォルダ変更は行われていません。", "The Drive root could not be verified. No URL was saved and no folder was changed."), "", false
		}
		var result vfxFilesRootVerification
		expectedRootID := rootID
		if expectedRootID == "" {
			parsed, _ := url.Parse(candidateURL)
			segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if len(segments) > 0 {
				expectedRootID = segments[len(segments)-1]
			}
		}
		expectedDriveID := ""
		if selected.DriveID != nil {
			expectedDriveID = *selected.DriveID
		}
		if candidateDrive, ok := requestBody["drive_id"].(string); ok {
			expectedDriveID = candidateDrive
		}
		if json.Unmarshal(verified.Body, &result) != nil {
			return t(lang, "Driveルートの読み取り・作成権限を確認できません。", "Drive root read and child-creation permissions could not be verified."), "", false
		}
		if _, _, identityErr := verifiedVFXFilesRootIdentity(result, connectionID, selected.AccountMode, expectedRootID, expectedDriveID); identityErr != nil {
			return t(lang, "Driveルートの読み取り・作成権限を確認できません。", "Drive root read and child-creation permissions could not be verified."), "", false
		}
		return t(lang, "Driveルートを確認しました。設定保存や同期、フォルダ作成は行っていません。", "Drive root verified. No configuration was saved, sync was started, or folder created."), "", true
	case "authorize":
		mode := strings.TrimSpace(r.Form.Get("account_mode"))
		connectionID := strings.TrimSpace(r.Form.Get("connection_id"))
		actorID, allowed, actorErr := authorizer.AuthorizeProductionAdmin(ctx, productionID)
		if actorErr != nil || !allowed || !vfxfiles.ValidID(actorID) {
			return t(lang, "認証を開始する管理者を確認できません。", "The administrator starting authorization could not be verified."), "", false
		}
		bodyValue := map[string]any{"contract_version": "1.0", "production_id": productionID}
		if connectionID != "" {
			if !vfxfiles.ValidID(connectionID) {
				return t(lang, "再認証するDrive接続を確認できません。", "The Drive connection to reauthorize could not be verified."), "", false
			}
			connectionsResponse, listErr := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/drive-connections", url.Values{"production_id": {productionID}}, nil, "vfx.connections.read", false, "", "")
			var available []vfxFilesAdminConnection
			if listErr != nil || connectionsResponse.StatusCode != http.StatusOK || decodeVFXFilesConnections(connectionsResponse.Body, &available) != nil {
				return t(lang, "再認証の前にDrive接続状態を確認してください。", "Verify the Drive connection state before reauthorization."), "", false
			}
			found := false
			for _, connection := range available {
				if connection.ID == connectionID && (connection.State == "expired" || connection.State == "permission_denied") {
					mode = connection.AccountMode
					found = true
					break
				}
			}
			if !found {
				return t(lang, "接続が実際に期限切れまたは権限不足であることを確認できません。", "The Plugin did not confirm that this connection needs reauthorization."), "", false
			}
			bodyValue["connection_id"] = connectionID
		} else if mode != "my_drive" && mode != "shared_drive" {
			return t(lang, "Driveの種類を選択してください。", "Choose a Drive account type."), "", false
		}
		bodyValue["account_mode"] = mode
		body, _ := json.Marshal(bodyValue)
		response, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPost, "/drive-connections/authorizations", nil, body, "vfx.connections.authorize", true, key, "")
		if err != nil {
			return safeVFXFilesError(err, lang), "", false
		}
		if response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusCreated {
			return t(lang, "Google Drive認証を開始できませんでした。", "Google Drive authorization could not be started."), "", false
		}
		var intent struct {
			ContractVersion  string `json:"contract_version"`
			AuthorizationID  string `json:"authorization_id"`
			AuthorizationURL string `json:"authorization_url"`
			ExpiresAt        string `json:"expires_at"`
		}
		if json.Unmarshal(response.Body, &intent) != nil {
			return t(lang, "認証応答を確認できません。", "The authorization response could not be verified."), "", false
		}
		expiresAt, expiresErr := time.Parse(time.RFC3339, intent.ExpiresAt)
		if intent.ContractVersion != "1.0" || !vfxfiles.ValidID(intent.AuthorizationID) || !vfxfiles.IsTrustedAuthorizationURL(intent.AuthorizationURL) || expiresErr != nil || !expiresAt.After(time.Now().UTC()) {
			return t(lang, "許可されていない認証先が返されました。", "The Plugin returned an unsupported authorization destination."), "", false
		}
		if err := model.StoreVFXFilesAuthorizationIntent(db, model.VFXFilesAuthorizationIntent{AuthorizationID: intent.AuthorizationID, ProductionID: productionID, ActorID: actorID, ExpiresAt: expiresAt.UTC()}); err != nil {
			return t(lang, "認証の戻り先を安全に保存できません。管理者に確認してください。", "The authorization return context could not be stored safely. Contact an administrator."), "", false
		}
		return "", intent.AuthorizationURL, true
	case "revoke":
		connectionID := strings.TrimSpace(r.Form.Get("connection_id"))
		if !vfxfiles.ValidID(connectionID) {
			return t(lang, "Drive接続を確認してください。", "Select a valid Drive connection."), "", false
		}
		body, _ := json.Marshal(map[string]string{"contract_version": "1.0"})
		response, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPost, "/drive-connections/"+connectionID+"/revoke", url.Values{"production_id": {productionID}}, body, "vfx.connections.revoke", true, key, "")
		if err != nil {
			return safeVFXFilesError(err, lang), "", false
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return t(lang, "Drive接続の解除を確認できません。", "Drive connection revocation could not be confirmed."), "", false
		}
		return t(lang, "Drive接続の解除を要求しました。状態を再確認してください。", "Drive revocation was requested. Refresh to verify its state."), "", true
	case "activate", "deactivate":
		if project.ValidationOnly || project.ReadOnlyPreview {
			return t(lang, "検証用Productionでは変更できません。", "Validation-only Productions cannot be changed."), "", false
		}
		desired, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/productions/"+productionID+"/desired-config", nil, nil, "vfx.config.read", false, "", "")
		if err != nil || desired.StatusCode != http.StatusOK || strings.TrimSpace(desired.Header.Get("ETag")) == "" {
			return t(lang, "有効なFiles設定を確認できません。先に設定を完了してください。", "A valid Files configuration could not be verified. Complete configuration first."), "", false
		}
		if r.Form.Get("action") == "activate" {
			var config model.VFXFilesDesiredConfigMirror
			if json.Unmarshal(desired.Body, &config) != nil || config.SyncPolicy.Mode != "new_production" {
				return t(lang, "この同期方式はまだ有効化できません。安全な同期方式を選び直してください。", "This synchronization mode cannot be activated yet. Choose a supported sync mode."), "", false
			}
		}
		if err := model.StoreVFXFilesDesiredConfigMirror(db, productionID, desired.Body); err != nil {
			return t(lang, "Files設定の安全状態を保存できません。操作を中止しました。", "The Files safety state could not be stored; the action was stopped."), "", false
		}
		if r.Form.Get("action") == "activate" {
			if err := model.BeginVFXFilesActivationMirror(db, productionID); err != nil {
				return t(lang, "有効化状態を再確認中です。画面を再読み込みして状態を確認してください。", "Activation is already being reconciled. Reload and verify its current state."), "", false
			}
		}
		if r.Form.Get("action") == "deactivate" {
			if err := model.DisableVFXFilesEventIntake(db, productionID); err != nil {
				return t(lang, "通知を安全に一時停止できません。管理者に確認してください。", "Event delivery could not be paused safely. Contact an administrator."), "", false
			}
		}
		path := "/productions/" + productionID + "/activation"
		if r.Form.Get("action") == "deactivate" {
			path = "/productions/" + productionID + "/deactivation"
		}
		body, _ := json.Marshal(map[string]string{"contract_version": "1.0"})
		response, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPost, path, nil, body, "vfx.activation.write", true, key, desired.Header.Get("ETag"))
		if err != nil {
			if r.Form.Get("action") == "activate" {
				return t(lang, "Pluginの応答が不明なため有効化状態を保留しています。再読み込みして状態を確認してください。", "The Plugin outcome is unknown, so activation remains pending. Reload to reconcile the state."), "", false
			}
			return safeVFXFilesError(err, lang), "", false
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			if r.Form.Get("action") == "activate" && response.StatusCode >= 400 && response.StatusCode < 500 {
				if cancelErr := model.CancelVFXFilesActivationMirror(db, productionID); cancelErr != nil {
					return t(lang, "有効化は拒否されましたが、保留状態を解除できません。再読み込みして状態を確認してください。", "Activation was rejected, but its pending marker could not be cleared. Reload and verify the state."), "", false
				}
			}
			return t(lang, "Filesの有効状態を変更できませんでした。必要なら状態を再確認してください。", "Files activation could not be changed. Recheck the remote state before retrying."), "", false
		}
		updated, getErr := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/productions/"+productionID+"/desired-config", nil, nil, "vfx.config.read", false, "", "")
		if getErr != nil || updated.StatusCode != http.StatusOK || model.StoreVFXFilesDesiredConfigMirror(db, productionID, updated.Body) != nil {
			return t(lang, "Files Pluginの状態は変更されましたが、KitsuSyncの安全状態を同期できません。通知は送信されません。再読み込みして修復してください。", "The Plugin state changed, but KitsuSync could not synchronize its safety state. Notifications remain blocked; reload to reconcile."), "", false
		}
		label := t(lang, "有効化しました。", "Files integration activated.")
		if r.Form.Get("action") == "deactivate" {
			label = t(lang, "無効化しました。", "Files integration deactivated.")
		}
		return label, "", true
	default:
		return t(lang, "この操作は利用できません。", "This action is unavailable."), "", false
	}
}

func uint64FromJSON(value any) uint64 {
	number, ok := value.(float64)
	if !ok || number < 1 || number > 9e15 || number != float64(uint64(number)) {
		return 0
	}
	return uint64(number)
}

func readVFXFilesDesiredConfig(ctx context.Context, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID string) (model.VFXFilesDesiredConfigMirror, string, error) {
	var config model.VFXFilesDesiredConfigMirror
	response, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/productions/"+productionID+"/desired-config", nil, nil, "vfx.config.read", false, "", "")
	if err != nil || response.StatusCode != http.StatusOK || strings.TrimSpace(response.Header.Get("ETag")) == "" || len(response.Body) == 0 || len(response.Body) > 256<<10 {
		return config, "", errors.New("desired Files configuration is unavailable")
	}
	if err := json.Unmarshal(response.Body, &config); err != nil || config.ContractVersion != "1.0" || !strings.EqualFold(config.ProductionID, productionID) || config.ConfigRevision == 0 || config.Drive.RootState != "verified" {
		return config, "", errors.New("desired Files configuration is invalid")
	}
	return config, response.Header.Get("ETag"), nil
}

func previewVFXFilesSync(ctx context.Context, db *gorm.DB, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, project *model.Project, idempotency, lang string) (string, string, bool) {
	productionID := project.KitsuProjectID
	if db == nil {
		return t(lang, "同期状態を保存できません。", "Sync state storage is unavailable."), "", false
	}
	config, _, err := readVFXFilesDesiredConfig(ctx, runtime, authorizer, productionID)
	if err != nil {
		return t(lang, "Files設定を再確認できません。", "Files configuration could not be reverified."), "", false
	}
	if config.SyncPolicy.Mode != "new_production" {
		return t(lang, "この同期方式はまだPreviewに対応していません。", "This synchronization mode is not yet supported for Preview."), "", false
	}
	reader, ok := authorizer.(vfxFilesKitsuSnapshotReader)
	if !ok {
		return t(lang, "Kitsu Snapshotの読み取りが利用できません。", "The verified Kitsu snapshot reader is unavailable."), "", false
	}
	scope := vfxFilesSnapshotScope{Mode: config.SyncPolicy.Mode, Episodes: config.SyncPolicy.Scope.Episodes, Sequences: config.SyncPolicy.Scope.Sequences, Shots: config.SyncPolicy.Scope.Shots, Assets: config.SyncPolicy.Scope.Assets, Tasks: config.SyncPolicy.Scope.Tasks, TaskTypes: config.SyncPolicy.Scope.TaskTypeIDs, IncludeIDs: config.SyncPolicy.Scope.IncludeIDs, ExcludeIDs: config.SyncPolicy.Scope.ExcludeIDs}
	objects, sourceETag, err := reader.BuildVFXFilesSnapshot(ctx, productionID, scope)
	if err != nil {
		return t(lang, "Kitsuの読み取り専用Snapshotを作成できません。変更は行われていません。", "A complete read-only Kitsu snapshot could not be created. No changes were made."), "", false
	}
	snapshotID, err := uploadVFXFilesSnapshot(ctx, runtime, authorizer, productionID, config.ConfigRevision, sourceETag, objects, idempotency)
	if err != nil {
		return t(lang, "Kitsu Snapshotを安全にアップロードできません。同期は開始されていません。", "The Kitsu snapshot could not be safely uploaded. Sync has not started."), "", false
	}
	previewBody, _ := json.Marshal(map[string]any{"contract_version": "1.0", "config_revision": config.ConfigRevision, "snapshot_id": snapshotID})
	previewResponse, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPost, "/productions/"+productionID+"/sync-previews", nil, previewBody, "vfx.sync.preview", true, vfxFilesActionKey(idempotency, "provider-preview"), "")
	if err != nil || previewResponse.StatusCode != http.StatusOK {
		return t(lang, "差分Previewを確認できません。同期は開始されていません。", "The sync preview could not be verified. Sync has not started."), "", false
	}
	var preview vfxFilesSyncPreview
	if json.Unmarshal(previewResponse.Body, &preview) != nil || preview.ContractVersion != "1.0" || !vfxfiles.ValidID(preview.PreviewID) || !strings.EqualFold(preview.ProductionID, productionID) || preview.ConfigRevision != config.ConfigRevision || preview.RootID != config.Drive.RootID || preview.State != "preview_ready" || len(preview.Changes) > 500 {
		return t(lang, "PluginのPreview応答が契約と一致しません。同期は開始されていません。", "The Plugin preview response did not match the contract. Sync has not started."), "", false
	}
	if err := model.StoreVFXFilesSyncPreview(db, productionID, snapshotID, config.ConfigRevision, previewResponse.Body); err != nil {
		return t(lang, "Previewは作成されましたが、確認状態を保存できません。Applyは無効です。", "A preview was created but could not be stored safely. Apply remains unavailable."), "", false
	}
	return t(lang, "差分Previewを作成しました。内容を確認してから明示的にApplyしてください。", "A diff preview is ready. Review it before explicitly applying sync."), "", true
}

func applyVFXFilesSync(ctx context.Context, db *gorm.DB, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID, idempotency, lang string) (string, string, bool) {
	operation, err := model.FindVFXFilesSyncOperation(db, productionID)
	if err != nil || operation.PreviewID == "" || operation.PreviewExpiresAt == nil || !operation.PreviewExpiresAt.After(time.Now().UTC()) {
		return t(lang, "有効なPreviewがありません。再作成してください。", "There is no current preview. Create a new preview."), "", false
	}
	config, _, err := readVFXFilesDesiredConfig(ctx, runtime, authorizer, productionID)
	if err != nil || config.ConfigRevision != operation.ConfigRevision {
		return t(lang, "Preview後にFiles設定が変更されました。新しいPreviewを作成してください。", "Files configuration changed after preview. Create a new preview."), "", false
	}
	if config.SyncPolicy.Mode != "new_production" {
		return t(lang, "この同期方式はまだApplyに対応していません。", "This synchronization mode is not yet supported for Apply."), "", false
	}
	if !config.Activation.Enabled || config.Activation.NotificationEpoch == 0 {
		return t(lang, "同期と通知を開始するには、管理者が先に明示的に有効化してください。Previewは引き続き読み取り専用です。", "An administrator must explicitly enable sync and notifications before Apply. Preview remains read-only."), "", false
	}
	previewJSON := []byte(operation.PreviewJSON)
	var preview vfxFilesSyncPreview
	if json.Unmarshal(previewJSON, &preview) != nil || preview.State != "preview_ready" || !strings.EqualFold(preview.PreviewID, operation.PreviewID) || !strings.EqualFold(preview.ProductionID, productionID) || preview.ConfigRevision != config.ConfigRevision {
		return t(lang, "Previewの保存状態を確認できません。", "The saved preview could not be verified."), "", false
	}
	operation, err = model.PrepareVFXFilesSyncCommand(db, productionID, "apply", vfxFilesActionKey(idempotency, "sync-apply"))
	if err != nil {
		return t(lang, "Apply識別子を安全に保存できません。", "The Apply idempotency record could not be stored safely."), "", false
	}
	body, _ := json.Marshal(map[string]any{"contract_version": "1.0", "preview_id": operation.PreviewID, "config_revision": operation.ConfigRevision})
	response, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPost, "/productions/"+productionID+"/sync-jobs", nil, body, "vfx.sync.apply", true, operation.ApplyIdempotency, "")
	if err != nil || response.StatusCode != http.StatusAccepted || model.StoreVFXFilesSyncJob(db, productionID, response.Body) != nil {
		return t(lang, "Applyの受付状態を確認できません。保存済み識別子で再確認してください。", "The Apply outcome could not be confirmed. Retry with the stored idempotency record or reconcile with the Plugin."), "", false
	}
	return t(lang, "同期ジョブを受け付けました。完了状態はジョブで確認してください。", "The sync job was accepted. Check its job status for completion."), "", true
}

func refreshVFXFilesSyncJob(ctx context.Context, db *gorm.DB, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID, lang string) (string, string, bool) {
	operation, err := model.FindVFXFilesSyncOperation(db, productionID)
	if err != nil || !vfxfiles.ValidID(operation.JobID) {
		return t(lang, "同期ジョブがありません。", "No sync job is available."), "", false
	}
	response, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/sync-jobs/"+operation.JobID, nil, nil, "vfx.sync.read", false, "", "")
	if err != nil || response.StatusCode != http.StatusOK || model.StoreVFXFilesSyncJob(db, productionID, response.Body) != nil {
		return t(lang, "最新のジョブ状態を確認できません。以前の状態は保持されています。", "The latest job state could not be confirmed. The last verified state is retained."), "", false
	}
	return t(lang, "ジョブ状態を更新しました。", "Job status was refreshed."), "", true
}

func commandVFXFilesSyncJob(ctx context.Context, db *gorm.DB, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID, action, idempotency, lang string) (string, string, bool) {
	operation, err := model.FindVFXFilesSyncOperation(db, productionID)
	if err != nil || !vfxfiles.ValidID(operation.JobID) || operation.JobJSON == "" {
		return t(lang, "同期ジョブを確認できません。", "The sync job could not be verified."), "", false
	}
	var current vfxFilesSyncJob
	if json.Unmarshal([]byte(operation.JobJSON), &current) != nil {
		return t(lang, "保存済みジョブ状態が不正です。", "The saved sync job state is invalid."), "", false
	}
	command, path, scope := "", "", ""
	if action == "retry_sync_job" {
		if current.State != "failed" {
			return t(lang, "失敗状態のジョブのみ再試行できます。", "Only a failed job can be retried."), "", false
		}
		command, path, scope = "retry", "/sync-jobs/"+operation.JobID+"/retry", "vfx.sync.retry"
	} else {
		if current.State != "reconcile_required" {
			return t(lang, "照合が必要なジョブのみ再照合できます。", "Only a job requiring reconciliation can be reconciled."), "", false
		}
		command, path, scope = "reconcile", "/sync-jobs/"+operation.JobID+"/reconcile", "vfx.sync.reconcile"
	}
	operation, err = model.PrepareVFXFilesSyncCommand(db, productionID, command, vfxFilesActionKey(idempotency, "sync-"+command))
	if err != nil {
		return t(lang, "操作識別子を安全に保存できません。", "The command idempotency record could not be stored safely."), "", false
	}
	body, _ := json.Marshal(map[string]string{"contract_version": "1.0", "reason_code": "operator_requested"})
	idempotencyKey := operation.RetryIdempotency
	if command == "reconcile" {
		idempotencyKey = operation.ReconcileIdempotency
	}
	response, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPost, path, nil, body, scope, true, idempotencyKey, "")
	if err != nil || response.StatusCode != http.StatusAccepted || model.StoreVFXFilesSyncJob(db, productionID, response.Body) != nil {
		return t(lang, "ジョブ操作の受付状態を確認できません。", "The job command outcome could not be confirmed."), "", false
	}
	return t(lang, "ジョブ操作を受け付けました。状態を再確認してください。", "The job command was accepted. Refresh its status."), "", true
}

func vfxFilesPluginCall(ctx context.Context, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID, method, path string, query url.Values, body []byte, scope string, mutating bool, idempotency, etag string) (*vfxFilesAdminPluginResponse, error) {
	headers := make(http.Header)
	if etag != "" {
		if etag == "*" {
			headers.Set("If-None-Match", "*")
		} else {
			headers.Set("If-Match", etag)
		}
	}
	response, err := runtime.Client.Do(ctx, vfxfiles.PluginRequest{
		Method: method, Path: path, Query: query, Headers: headers, Body: body, ProductionID: productionID,
		Authorizer: authorizer, Scope: scope, IdempotencyKey: idempotency, Mutating: mutating,
	})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(responseBody) > 4<<20 {
		return nil, errors.New("Plugin response is too large or unreadable")
	}
	return &vfxFilesAdminPluginResponse{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: responseBody}, nil
}

func saveVFXFilesDesiredConfig(ctx context.Context, db *gorm.DB, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID string, r *http.Request, lang, idempotency string) (string, string, bool) {
	connectionID := strings.TrimSpace(r.Form.Get("connection_id"))
	rootID := strings.TrimSpace(r.Form.Get("root_id"))
	mode := strings.TrimSpace(r.Form.Get("sync_mode"))
	// Future-only needs a durable creation watermark; existing-tree adoption
	// needs provider-verified identity proof. The current Plugin does neither.
	// Reject unsupported modes before any provider call or configuration write.
	if mode != "new_production" {
		return t(lang, "この同期方式はまだ利用できません。新規Productionを選択してください。", "This sync mode is not yet supported. Choose New Production."), "", false
	}
	if !vfxfiles.ValidID(connectionID) || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`).MatchString(rootID) {
		return t(lang, "接続、ルート、同期方式を確認してください。", "Check the Drive connection, root, and sync mode."), "", false
	}
	connections, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/drive-connections", url.Values{"production_id": {productionID}}, nil, "vfx.connections.read", false, "", "")
	var available []vfxFilesAdminConnection
	if err != nil || connections.StatusCode != http.StatusOK || decodeVFXFilesConnections(connections.Body, &available) != nil {
		return t(lang, "Drive接続を再確認できません。", "Drive connection could not be reverified."), "", false
	}
	var selected *vfxFilesAdminConnection
	for index := range available {
		if available[index].ID == connectionID && available[index].State == "connected" {
			selected = &available[index]
			break
		}
	}
	if selected == nil || selected.AccountMode != "my_drive" && selected.AccountMode != "shared_drive" {
		return t(lang, "接続済みDriveアカウントを再確認できません。", "The connected Drive account could not be reverified."), "", false
	}
	rootsResponse, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/drive-connections/"+connectionID+"/roots", url.Values{"production_id": {productionID}}, nil, "vfx.roots.read", false, "", "")
	var roots []vfxFilesAdminRoot
	if err != nil || rootsResponse.StatusCode != http.StatusOK || decodeVFXFilesRoots(rootsResponse.Body, &roots) != nil {
		return t(lang, "Driveルート候補を再確認できません。", "Drive roots could not be reverified."), "", false
	}
	var selectedRoot *vfxFilesAdminRoot
	for index := range roots {
		if roots[index].ID == rootID && roots[index].AccountMode == selected.AccountMode {
			selectedRoot = &roots[index]
			break
		}
	}
	if selectedRoot == nil {
		return t(lang, "Driveルート候補を一覧から再選択してください。", "Select a current Drive root from the list."), "", false
	}
	verifyBody := map[string]any{"contract_version": "1.0", "production_id": productionID, "account_mode": selected.AccountMode, "root_id": rootID}
	expectedDriveID := ""
	if selectedRoot.DriveID != nil {
		expectedDriveID = strings.TrimSpace(*selectedRoot.DriveID)
	}
	if selected.AccountMode == "shared_drive" {
		if selected.DriveID == nil || strings.TrimSpace(*selected.DriveID) == "" || expectedDriveID != "" && expectedDriveID != strings.TrimSpace(*selected.DriveID) {
			return t(lang, "共有ドライブを検証できません。", "The Shared Drive identity could not be verified."), "", false
		}
		expectedDriveID = strings.TrimSpace(*selected.DriveID)
		verifyBody["drive_id"] = expectedDriveID
	}
	verifyJSON, _ := json.Marshal(verifyBody)
	verified, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPost, "/drive-connections/"+connectionID+"/roots", url.Values{"production_id": {productionID}}, verifyJSON, "vfx.roots.verify", true, vfxFilesActionKey(idempotency, "verify-config-root"), "")
	var proof vfxFilesRootVerification
	if err != nil || verified.StatusCode != http.StatusOK || json.Unmarshal(verified.Body, &proof) != nil {
		return t(lang, "Driveルートの検証に失敗しました。設定は保存されていません。", "Drive root verification failed. No configuration was saved."), "", false
	}
	ownerSubject, verifiedDriveID, err := verifiedVFXFilesRootIdentity(proof, connectionID, selected.AccountMode, rootID, expectedDriveID)
	if err != nil {
		return t(lang, "Driveアカウントの本人性を検証できません。設定は保存されていません。", "The Drive account identity could not be verified. No configuration was saved."), "", false
	}
	selections := make([]map[string]any, 0, 5)
	for _, scope := range []string{"production", "asset", "sequence", "shot", "task"} {
		templateID := strings.TrimSpace(r.Form.Get("template_" + scope))
		if templateID == "" {
			if scope == "production" {
				return t(lang, "Productionテンプレートを選択してください。", "Choose a Production template."), "", false
			}
			continue
		}
		catalog, callErr := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/templates", url.Values{"scope": {scope}}, nil, "vfx.templates.read", false, "", "")
		var templates []vfxFilesAdminTemplate
		if callErr != nil || catalog.StatusCode != http.StatusOK || decodeVFXFilesTemplates(catalog.Body, scope, &templates) != nil {
			return t(lang, "テンプレート一覧を再確認できません。", "The template catalog could not be reverified."), "", false
		}
		var chosen *vfxFilesAdminTemplate
		for index := range templates {
			if templates[index].ID == templateID {
				chosen = &templates[index]
				break
			}
		}
		if chosen == nil || chosen.Revision == 0 {
			return t(lang, "テンプレートを一覧から再選択してください。", "Select a current template from the catalog."), "", false
		}
		selections = append(selections, map[string]any{"scope": scope, "template_id": chosen.ID, "revision": chosen.Revision, "overrides": map[string]string{}})
	}
	taskTypeIDs := make([]string, 0)
	for _, id := range r.Form["task_type_id"] {
		id = strings.TrimSpace(id)
		if !vfxfiles.ValidID(id) {
			return t(lang, "Task Typeの選択が不正です。", "A selected Task Type is invalid."), "", false
		}
		taskTypeIDs = append(taskTypeIDs, id)
	}
	sort.Strings(taskTypeIDs)
	for i := 1; i < len(taskTypeIDs); i++ {
		if taskTypeIDs[i] == taskTypeIDs[i-1] {
			return t(lang, "Task Typeが重複しています。", "A Task Type was selected more than once."), "", false
		}
	}
	syncPolicy := map[string]any{"mode": mode, "scope": map[string]any{
		"episodes": r.Form.Get("scope_episodes") == "on", "sequences": r.Form.Get("scope_sequences") == "on", "shots": r.Form.Get("scope_shots") == "on", "assets": r.Form.Get("scope_assets") == "on", "tasks": r.Form.Get("scope_tasks") == "on", "task_type_ids": taskTypeIDs,
	}, "notify_new_deliveries": false}
	drive := map[string]any{"account_mode": selected.AccountMode, "root_id": rootID, "root_state": "verified"}
	if selected.AccountMode == "my_drive" {
		drive["owner_subject"] = ownerSubject
	} else {
		drive["drive_id"] = verifiedDriveID
		drive["owner_subject"] = nil
	}
	body, err := json.Marshal(map[string]any{"contract_version": "1.0", "connection_id": connectionID, "drive": drive, "template_selection": map[string]any{"selections": selections}, "sync_policy": syncPolicy})
	if err != nil {
		return t(lang, "設定を作成できません。", "Configuration could not be created."), "", false
	}
	current, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/productions/"+productionID+"/desired-config", nil, nil, "vfx.config.read", false, "", "")
	if err != nil || current.StatusCode != http.StatusOK && current.StatusCode != http.StatusNotFound {
		return t(lang, "現在のFiles設定を再確認できません。", "The current Files configuration could not be reverified."), "", false
	}
	precondition := "*"
	if current.StatusCode == http.StatusOK {
		precondition = strings.TrimSpace(current.Header.Get("ETag"))
		if !regexp.MustCompile(`^"[1-9][0-9]*"$`).MatchString(precondition) {
			return t(lang, "設定の版を確認できません。再読み込みしてください。", "The configuration revision could not be verified. Reload and try again."), "", false
		}
	}
	saved, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodPut, "/productions/"+productionID+"/desired-config", nil, body, "vfx.config.write", true, vfxFilesActionKey(idempotency, "save-config"), precondition)
	if err != nil || saved.StatusCode != http.StatusOK && saved.StatusCode != http.StatusCreated || strings.TrimSpace(saved.Header.Get("ETag")) == "" {
		return t(lang, "Files設定を保存できませんでした。競合する場合は再読み込みしてください。", "Files configuration was not saved. Reload if another administrator changed it."), "", false
	}
	if err := model.StoreVFXFilesDesiredConfigMirror(db, productionID, saved.Body); err != nil {
		return t(lang, "Files Pluginの設定は更新されましたが、安全状態を同期できません。通知と同期は停止しています。管理者に確認してください。", "The Plugin configuration changed, but KitsuSync could not synchronize its safety state. Notifications and sync remain blocked; contact an administrator."), "", false
	}
	return t(lang, "Files設定を保存しました。同期はまだ開始されていません。差分Previewで確認してください。", "Files configuration saved. Sync has not started; review the diff preview before applying."), "", true
}

func decodeVFXFilesConnections(body []byte, destination *[]vfxFilesAdminConnection) error {
	var result struct {
		ContractVersion string                    `json:"contract_version"`
		Connections     []vfxFilesAdminConnection `json:"connections"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.ContractVersion != "1.0" || len(result.Connections) > 100 {
		return errors.New("invalid Plugin connections response")
	}
	for _, connection := range result.Connections {
		if connection.ContractVersion != "1.0" || !vfxfiles.ValidID(connection.ID) || !validVFXFilesConnectionState(connection.State) || connection.AccountMode != "my_drive" && connection.AccountMode != "shared_drive" || len(connection.GrantedScopes) > 32 {
			return errors.New("invalid Plugin connection state")
		}
		if connection.OwnerLabel != nil && len([]rune(*connection.OwnerLabel)) > 160 {
			return errors.New("invalid Plugin connection label")
		}
		if connection.DriveID != nil && len(*connection.DriveID) > 200 {
			return errors.New("invalid Plugin drive identity")
		}
		for _, scope := range connection.GrantedScopes {
			if len(scope) == 0 || len(scope) > 240 || strings.ContainsAny(scope, "\r\n") {
				return errors.New("invalid Plugin granted scope")
			}
		}
	}
	*destination = result.Connections
	return nil
}

func decodeVFXFilesRoots(body []byte, destination *[]vfxFilesAdminRoot) error {
	var result struct {
		ContractVersion string              `json:"contract_version"`
		Roots           []vfxFilesAdminRoot `json:"roots"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.ContractVersion != "1.0" || len(result.Roots) > 500 {
		return errors.New("invalid Plugin root list")
	}
	for _, root := range result.Roots {
		if len(root.ID) == 0 || len(root.ID) > 200 || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(root.ID) || len([]rune(root.Name)) == 0 || len([]rune(root.Name)) > 240 || root.AccountMode != "my_drive" && root.AccountMode != "shared_drive" {
			return errors.New("invalid Plugin root candidate")
		}
		if root.DriveID != nil && (len(*root.DriveID) == 0 || len(*root.DriveID) > 200 || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(*root.DriveID)) {
			return errors.New("invalid Plugin Drive identity")
		}
	}
	*destination = result.Roots
	return nil
}

func decodeVFXFilesTemplates(body []byte, scope string, destination *[]vfxFilesAdminTemplate) error {
	var result struct {
		ContractVersion string                  `json:"contract_version"`
		Templates       []vfxFilesAdminTemplate `json:"templates"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.ContractVersion != "1.0" || len(result.Templates) > 500 {
		return errors.New("invalid Plugin template catalog")
	}
	for _, template := range result.Templates {
		if template.Scope != scope || len(template.ID) == 0 || len(template.ID) > 160 || template.Revision == 0 || len([]rune(template.Label)) == 0 || len([]rune(template.Label)) > 160 || len([]rune(template.Pattern)) == 0 || len([]rune(template.Pattern)) > 240 || len(template.Tokens) > 64 || template.Materialization.SchemaRevision != 1 || template.Materialization.RootBinding != "verified_root_is_production_root" || len(template.Materialization.AvailableOverrides) > 16 {
			return errors.New("invalid Plugin template")
		}
		seenOverrides := map[string]bool{}
		for _, override := range template.Materialization.AvailableOverrides {
			if len(override) == 0 || len(override) > 80 || strings.ContainsAny(override, "\r\n") || seenOverrides[override] {
				return errors.New("invalid Plugin template materialization")
			}
			seenOverrides[override] = true
		}
		for _, token := range template.Tokens {
			if len(token) == 0 || len(token) > 100 || strings.ContainsAny(token, "\r\n") {
				return errors.New("invalid Plugin template token")
			}
		}
	}
	*destination = result.Templates
	return nil
}

func validVFXFilesConnectionState(state string) bool {
	switch state {
	case "not_configured", "authorizing", "connected", "permission_denied", "expired", "revoking", "revoke_pending", "error":
		return true
	default:
		return false
	}
}

func localizedVFXFilesConnectionState(state, lang string) string {
	switch state {
	case "not_configured":
		return t(lang, "未設定", "Not configured")
	case "authorizing":
		return t(lang, "認証中", "Authorizing")
	case "connected":
		return t(lang, "接続済み", "Connected")
	case "permission_denied":
		return t(lang, "権限不足", "Permission denied")
	case "expired":
		return t(lang, "認証期限切れ", "Expired")
	case "revoking":
		return t(lang, "切断中", "Revoking")
	case "revoke_pending":
		return t(lang, "切断要確認", "Revocation pending")
	case "error":
		return t(lang, "エラー", "Error")
	default:
		return t(lang, "状態不明", "Unknown state")
	}
}

func validVFXFilesDriveAuthorityStatus(status vfxFilesDriveAuthorityStatus) bool {
	if status.Owner != "plugin" && status.Owner != "legacy_kitsusync" {
		return false
	}
	if status.State != "unconfigured" && status.State != "configured" && status.State != "legacy_import_required" {
		return false
	}
	if status.ActivationEnabled && (status.State == "unconfigured" || status.ActivationEpoch == 0) {
		return false
	}
	if status.State == "configured" {
		return status.ConnectionID != nil && *status.ConnectionID != "" && status.RootID != nil && *status.RootID != "" && status.RootState != nil && *status.RootState == "verified"
	}
	return true
}

func vfxFilesNativeSettingsURL(base, productionID string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") || !vfxfiles.ValidID(productionID) {
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/plugins/vfx_files/frontend/"
	query := u.Query()
	query.Set("production_id", productionID)
	query.Set("drive_settings", "1")
	u.RawQuery = query.Encode()
	return u.String()
}

func renderVFXFilesAdminPage(r *http.Request, project *model.Project, productionID, lang string, state vfxFilesAdminPageState) string {
	body := `<section class="section-stack"><div class="page-heading"><div><div class="eyebrow">` + esc(project.Name) + `</div><h2>Google Drive / Files Plugin</h2><p class="hint">` + esc(t(lang, "Drive接続、検証済みルート、テンプレート、同期設定、有効化はKitsuのFiles Plugin設定で管理されます。KitsuSyncは読み取り専用です。", "Drive connection, verified root, templates, sync settings, and activation are managed in the native Kitsu Files Plugin settings. KitsuSync is read-only.")) + `</p></div></div>`
	if state.message != "" {
		body += `<p role="status">` + esc(state.message) + `</p>`
	}
	body += `<section class="production-detail-surface"><h3>` + esc(t(lang, "Drive状態", "Drive status")) + `</h3>`
	if state.readOnlyDrive == nil {
		body += `<p role="status">` + esc(t(lang, "Files Pluginの状態を確認できません。設定は変更されていません。", "Files Plugin status is unavailable. No settings were changed.")) + `</p>`
	} else {
		status := state.readOnlyDrive
		ownerLabel := t(lang, "Files Plugin", "Files Plugin")
		if status.Owner == "legacy_kitsusync" {
			ownerLabel = t(lang, "既存KitsuSync設定（Pluginへの明示的な移行が必要）", "Existing KitsuSync settings (explicit Plugin import required)")
		}
		connectionLabel := t(lang, "未設定", "Not configured")
		rootLabel := t(lang, "未検証", "Not verified")
		if status.ConnectionID != nil {
			connectionLabel = t(lang, "接続済み", "Connected")
		}
		if status.RootState != nil && *status.RootState == "verified" {
			rootLabel = t(lang, "検証済み", "Verified")
		}
		activationLabel := t(lang, "無効", "Inactive")
		if status.ActivationEnabled {
			activationLabel = t(lang, "有効", "Active")
		}
		body += `<ul class="status-list"><li class="status-row">` + esc(t(lang, "所有者", "Authority")) + `: <strong>` + esc(ownerLabel) + `</strong></li><li class="status-row">` + esc(t(lang, "接続", "Connection")) + `: <strong>` + esc(connectionLabel) + `</strong></li><li class="status-row">` + esc(t(lang, "Productionルート", "Production root")) + `: <strong>` + esc(rootLabel) + `</strong></li><li class="status-row">` + esc(t(lang, "同期有効化", "Sync activation")) + `: <strong>` + esc(activationLabel) + `</strong></li><li class="status-row">` + esc(t(lang, "設定リビジョン / 有効化エポック", "Configuration revision / activation epoch")) + `: <strong>` + fmt.Sprint(status.ConfigRevision) + ` / ` + fmt.Sprint(status.ActivationEpoch) + `</strong></li></ul>`
	}
	if state.nativeSettingsURL != "" {
		body += `<p><a class="btn" href="` + esc(state.nativeSettingsURL) + `" target="_blank" rel="noopener noreferrer">` + esc(t(lang, "Kitsu Files Plugin設定を開く", "Open Kitsu Files Plugin settings")) + `</a></p>`
	} else {
		body += `<p class="field-help">` + esc(t(lang, "Kitsu Files Plugin設定リンクを確認できません。Kitsu接続設定を確認してください。", "The native Kitsu Files Plugin link is unavailable. Check the Kitsu connection settings.")) + `</p>`
	}
	body += `</section><section class="production-detail-surface"><h3>` + esc(t(lang, "Discord通知", "Discord notifications")) + `</h3><p>` + esc(t(lang, "Discord配送と受信Task TypeのルーティングはKitsuSyncが管理します。", "Discord delivery and recipient Task Type routing remain managed by KitsuSync.")) + `</p><p><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(productionID)+"&tab=notifications", r)) + `">` + esc(t(lang, "Discord通知ルートを管理", "Manage Discord notification routes")) + `</a></p></section>`
	body += `<section class="production-detail-surface"><h3>` + esc(t(lang, "イベント診断", "Event diagnostics")) + `</h3>`
	if state.diagnosticsOK && state.diagnostics != nil {
		body += `<ul class="status-list"><li class="status-row">` + esc(t(lang, "未処理の配布通知", "Pending delivery events")) + `: <strong>` + fmt.Sprint(state.diagnostics.PendingDelivery) + `</strong></li><li class="status-row">` + esc(t(lang, "隔離イベント", "Quarantined events")) + `: <strong>` + fmt.Sprint(state.diagnostics.Quarantined) + `</strong></li><li class="status-row">` + esc(t(lang, "イベントID競合", "Event identity conflicts")) + `: <strong>` + fmt.Sprint(state.diagnostics.IdentityConflict) + `</strong></li></ul>`
	} else {
		body += `<p role="status">` + esc(t(lang, "イベント診断を確認できません。", "Event diagnostics are unavailable.")) + `</p>`
	}
	body += `</section></section>`
	return body
}

func localizedVFXFilesSyncJobState(state, lang string) string {
	switch state {
	case "queued":
		return t(lang, "待機中", "Queued")
	case "running":
		return t(lang, "実行中", "Running")
	case "completed":
		return t(lang, "完了", "Completed")
	case "failed":
		return t(lang, "失敗", "Failed")
	case "reconcile_required":
		return t(lang, "結果の再照合が必要", "Reconciliation required")
	case "cancelled":
		return t(lang, "キャンセル", "Cancelled")
	default:
		return t(lang, "不明", "Unknown")
	}
}

func safeSubmittedIdempotencyKey(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 16 || len(value) > 128 {
		return ""
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._:", char)) {
			return ""
		}
	}
	return value
}

func vfxFilesActionKey(base, action string) string {
	base = safeSubmittedIdempotencyKey(base)
	if base == "" {
		return ""
	}
	// One rendered page may contain several mutation forms. Keep their keys
	// distinct while retaining the same key across a failed retry of one form.
	return base + ":" + action
}

func newVFXFilesIdempotencyKey() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "ks-files-" + hex.EncodeToString(value[:]), nil
}

func safeVFXFilesError(err error, lang string) string {
	_ = err
	return t(lang, "Files Pluginに接続できません。時間をおいて状態を再確認してください。", "Files Plugin is unavailable. Retry after checking the current state.")
}

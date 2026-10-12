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
	"strings"

	"app/src/api/vfxfiles"
	"app/src/model"
	"gorm.io/gorm"
)

type vfxFilesAdminPageState struct {
	message          string
	actionKey        string
	diagnostics      *model.VFXFilesEventDiagnostics
	diagnosticsOK    bool
	integrationReady bool
	connections      []vfxFilesAdminConnection
	roots            []vfxFilesAdminRoot
	templates        map[string][]vfxFilesAdminTemplate
	selectedID       string
	desired          map[string]any
	etag             string
	desiredReady     bool
	connectionsOK    bool
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
	ID       string   `json:"id"`
	Revision uint64   `json:"revision"`
	Scope    string   `json:"scope"`
	Label    string   `json:"label"`
	Pattern  string   `json:"pattern"`
	Tokens   []string `json:"available_tokens"`
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
				authorizer = NewVFXFilesKitsuSessionAuthorizer(connection, email, token, role, nil)
				if _, allowed, err := authorizer.AuthorizeProductionAdmin(r.Context(), productionID); err != nil || !allowed {
					http.Error(w, "Production administrator access could not be verified", http.StatusForbidden)
					return
				}
				state.integrationReady = true
			}
		}
		if r.Method == http.MethodPost {
			if project.ValidationOnly || project.ReadOnlyPreview {
				http.Error(w, "validation-only Production is read-only", http.StatusForbidden)
				return
			}
			if runtime == nil || runtime.Client == nil || authorizer == nil {
				state.message = t(lang, "Files Pluginが設定されていないため操作できません。", "The Files Plugin is not configured; this action is unavailable.")
			} else {
				message, redirect, completed := runVFXFilesAdminAction(r.Context(), db, runtime, authorizer, project, r, lang)
				state.message = message
				if redirect != "" {
					http.Redirect(w, r, redirect, http.StatusSeeOther)
					return
				}
				if completed {
					state.actionKey, _ = newVFXFilesIdempotencyKey()
				} else {
					state.actionKey = safeSubmittedIdempotencyKey(r.FormValue("idempotency_key"))
				}
			}
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

func loadVFXFilesAdminState(ctx context.Context, db *gorm.DB, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID, selectedID string, state *vfxFilesAdminPageState, lang string) {
	response, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/drive-connections", url.Values{"production_id": {productionID}}, nil, "vfx.connections.read", false, "", "")
	if err != nil {
		state.message = safeVFXFilesError(err, lang)
		return
	}
	if response.StatusCode != http.StatusOK || decodeVFXFilesConnections(response.Body, &state.connections) != nil {
		state.message = t(lang, "Drive接続の状態を確認できません。", "Drive connection state could not be verified.")
		return
	}
	state.connectionsOK = true
	state.selectedID = selectedID
	selectedConnection := (*vfxFilesAdminConnection)(nil)
	for _, connection := range state.connections {
		if connection.ID == selectedID {
			selectedConnection = &connection
			break
		}
		state.selectedID = ""
	}
	if state.selectedID == "" && len(state.connections) > 0 {
		state.selectedID = state.connections[0].ID
		for index := range state.connections {
			if state.connections[index].ID == state.selectedID {
				selectedConnection = &state.connections[index]
				break
			}
		}
	}
	if selectedConnection != nil && selectedConnection.State == "connected" {
		roots, rootsErr := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/drive-connections/"+selectedConnection.ID+"/roots", url.Values{"production_id": {productionID}}, nil, "vfx.roots.read", false, "", "")
		if rootsErr == nil && roots.StatusCode == http.StatusOK {
			_ = decodeVFXFilesRoots(roots.Body, &state.roots)
		}
	}
	state.templates = make(map[string][]vfxFilesAdminTemplate)
	for _, scope := range []string{"production", "sequence", "shot", "asset", "task"} {
		catalog, catalogErr := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/templates", url.Values{"scope": {scope}}, nil, "vfx.templates.read", false, "", "")
		if catalogErr == nil && catalog.StatusCode == http.StatusOK {
			var templates []vfxFilesAdminTemplate
			if decodeVFXFilesTemplates(catalog.Body, scope, &templates) == nil {
				state.templates[scope] = templates
			}
		}
	}
	config, err := vfxFilesPluginCall(ctx, runtime, authorizer, productionID, http.MethodGet, "/productions/"+productionID+"/desired-config", nil, nil, "vfx.config.read", false, "", "")
	if err != nil {
		if state.message == "" {
			state.message = safeVFXFilesError(err, lang)
		}
		return
	}
	if config.StatusCode == http.StatusNotFound {
		return
	}
	if config.StatusCode != http.StatusOK {
		if state.message == "" {
			state.message = t(lang, "ProductionのFiles設定を確認できません。", "Production Files configuration could not be verified.")
		}
		return
	}
	var desired map[string]any
	if json.Unmarshal(config.Body, &desired) != nil || strings.TrimSpace(config.Header.Get("ETag")) == "" {
		if state.message == "" {
			state.message = t(lang, "ProductionのFiles設定が不正です。", "Production Files configuration is invalid.")
		}
		return
	}
	if err := model.StoreVFXFilesDesiredConfigMirror(db, productionID, config.Body); err != nil {
		if state.message == "" {
			state.message = t(lang, "通知の安全状態を同期できません。操作は無効です。", "Notification safety state could not be synchronized. Actions are disabled.")
		}
		return
	}
	state.desired, state.etag, state.desiredReady = desired, config.Header.Get("ETag"), true
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
		var result struct {
			ContractVersion string `json:"contract_version"`
			ConnectionID    string `json:"connection_id"`
			RootID          string `json:"root_id"`
			State           string `json:"state"`
			Capabilities    struct {
				CanRead        bool `json:"can_read"`
				CanAddChildren bool `json:"can_add_children"`
			} `json:"capabilities"`
		}
		if json.Unmarshal(verified.Body, &result) != nil || result.ContractVersion != "1.0" || result.ConnectionID != connectionID || result.RootID == "" || result.State != "verified" || !result.Capabilities.CanRead || !result.Capabilities.CanAddChildren {
			return t(lang, "Driveルートの読み取り・作成権限を確認できません。", "Drive root read and child-creation permissions could not be verified."), "", false
		}
		return t(lang, "Driveルートを確認しました。設定保存や同期、フォルダ作成は行っていません。", "Drive root verified. No configuration was saved, sync was started, or folder created."), "", true
	case "authorize":
		mode := strings.TrimSpace(r.Form.Get("account_mode"))
		if mode != "my_drive" && mode != "shared_drive" {
			return t(lang, "Driveの種類を選択してください。", "Choose a Drive account type."), "", false
		}
		body, _ := json.Marshal(map[string]any{"contract_version": "1.0", "production_id": productionID, "account_mode": mode})
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
		}
		if json.Unmarshal(response.Body, &intent) != nil || intent.ContractVersion != "1.0" || !vfxfiles.ValidID(intent.AuthorizationID) || !vfxfiles.IsTrustedAuthorizationURL(intent.AuthorizationURL) {
			return t(lang, "許可されていない認証先が返されました。", "The Plugin returned an unsupported authorization destination."), "", false
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
		if err := model.StoreVFXFilesDesiredConfigMirror(db, productionID, desired.Body); err != nil {
			return t(lang, "Files設定の安全状態を保存できません。操作を中止しました。", "The Files safety state could not be stored; the action was stopped."), "", false
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
			return safeVFXFilesError(err, lang), "", false
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
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

func vfxFilesPluginCall(ctx context.Context, runtime *vfxfiles.RuntimeConfig, authorizer vfxfiles.ProductionAdminAuthorizer, productionID, method, path string, query url.Values, body []byte, scope string, mutating bool, idempotency, etag string) (*vfxFilesAdminPluginResponse, error) {
	headers := make(http.Header)
	if etag != "" {
		headers.Set("If-Match", etag)
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
		if template.Scope != scope || len(template.ID) == 0 || len(template.ID) > 160 || template.Revision == 0 || len([]rune(template.Label)) == 0 || len([]rune(template.Label)) > 160 || len([]rune(template.Pattern)) == 0 || len([]rune(template.Pattern)) > 240 || len(template.Tokens) > 64 {
			return errors.New("invalid Plugin template")
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

func renderVFXFilesAdminPage(r *http.Request, project *model.Project, productionID, lang string, state vfxFilesAdminPageState) string {
	var connections strings.Builder
	for _, connection := range state.connections {
		mode := t(lang, "マイドライブ", "My Drive")
		if connection.AccountMode == "shared_drive" {
			mode = t(lang, "共有ドライブ", "Shared Drive")
		}
		label := localizedVFXFilesConnectionState(connection.State, lang) + " · " + mode
		if connection.OwnerLabel != nil {
			label += " · " + *connection.OwnerLabel
		}
		connections.WriteString(`<li class="status-row"><span>` + esc(label) + `</span>`)
		if state.integrationReady && !project.ValidationOnly && !project.ReadOnlyPreview && connection.State != "revoking" && connection.State != "revoke_pending" {
			connections.WriteString(`<form method="post"><input type="hidden" name="action" value="revoke"><input type="hidden" name="connection_id" value="` + esc(connection.ID) + `"><input type="hidden" name="idempotency_key" value="` + esc(vfxFilesActionKey(state.actionKey, "revoke-"+connection.ID)) + `"><button class="btn-ghost" type="submit">` + esc(t(lang, "接続を解除", "Revoke connection")) + `</button></form>`)
		}
		connections.WriteString(`</li>`)
	}
	if connections.Len() == 0 {
		connections.WriteString(`<li class="status-row"><span>` + esc(t(lang, "Drive接続はありません。", "No Drive connections are available.")) + `</span></li>`)
	}
	rootOptions := `<option value="">` + esc(t(lang, "ルート候補を選択", "Select a root candidate")) + `</option>`
	for _, root := range state.roots {
		label := root.Name
		if root.AccountMode == "shared_drive" {
			label += " · " + t(lang, "共有ドライブ", "Shared Drive")
		}
		rootOptions += `<option value="` + esc(root.ID) + `">` + esc(label) + `</option>`
	}
	connectedOptions := ``
	for _, connection := range state.connections {
		if connection.State != "connected" || connection.ID != state.selectedID {
			continue
		}
		mode := t(lang, "マイドライブ", "My Drive")
		if connection.AccountMode == "shared_drive" {
			mode = t(lang, "共有ドライブ", "Shared Drive")
		}
		connectedOptions += `<option value="` + esc(connection.ID) + `">` + esc(mode+" · "+localizedVFXFilesConnectionState(connection.State, lang)) + `</option>`
	}
	message := state.message
	if message == "" {
		message = t(lang, "Drive URLの保存はアクセス確認を意味しません。認証・設定はFiles Pluginで検証されます。", "A saved Drive URL does not prove access. The Files Plugin verifies authorization and configuration.")
	}
	body := `<section class="section-stack"><div class="page-heading"><div><div class="eyebrow">` + esc(project.Name) + `</div><h2>Google Drive / Files Plugin</h2><p class="hint">` + esc(t(lang, "KitsuSyncは接続状態と通知有効化を管理します。Driveの秘密情報、ファイル、同期ジョブはPluginが保持します。", "KitsuSync manages connection state and notification activation. The Plugin owns Drive credentials, files, and sync jobs.")) + `</p></div></div><section class="production-detail-surface"><h3>` + esc(t(lang, "Drive接続", "Drive connection")) + `</h3><p role="status">` + esc(message) + `</p><ul class="status-list">` + connections.String() + `</ul>`
	if state.integrationReady && !project.ValidationOnly && !project.ReadOnlyPreview {
		body += `<form method="post" class="form-stack"><input type="hidden" name="action" value="authorize"><input type="hidden" name="idempotency_key" value="` + esc(vfxFilesActionKey(state.actionKey, "authorize")) + `"><label>` + esc(t(lang, "接続先", "Account type")) + `<select name="account_mode" required><option value="my_drive">My Drive</option><option value="shared_drive">Shared Drive</option></select></label><button class="btn" type="submit">` + esc(t(lang, "Google Driveを接続", "Connect Google Drive")) + `</button></form>`
	}
	body += `</section><section class="production-detail-surface"><h3>` + esc(t(lang, "Driveルート確認", "Drive root verification")) + `</h3><p class="field-help">` + esc(t(lang, "候補を選ぶかURLを貼り付けて検証します。URLは候補として扱い、確認時にPluginが権限を検証します。ここでは保存・作成・移動・共有を行いません。", "Choose a listed root or paste a candidate URL. The Plugin verifies access; the URL is untrusted until then. This check does not save settings, create, move, or share folders.")) + `</p>`
	if state.integrationReady && !project.ValidationOnly && !project.ReadOnlyPreview && connectedOptions != "" {
		body += `<form method="post" class="form-stack"><input type="hidden" name="action" value="verify_root"><input type="hidden" name="idempotency_key" value="` + esc(vfxFilesActionKey(state.actionKey, "verify-listed-root")) + `"><input type="hidden" name="connection_id" value="` + esc(state.selectedID) + `"><label>` + esc(t(lang, "一覧から選択", "Choose listed root")) + `<select name="root_id" required>` + rootOptions + `</select></label><button class="btn-ghost" type="submit">` + esc(t(lang, "アクセスを確認", "Verify access")) + `</button></form><form method="post" class="form-stack"><input type="hidden" name="action" value="verify_root"><input type="hidden" name="idempotency_key" value="` + esc(vfxFilesActionKey(state.actionKey, "verify-root-url")) + `"><input type="hidden" name="connection_id" value="` + esc(state.selectedID) + `"><label>` + esc(t(lang, "またはDriveフォルダーURL候補", "Or verify a Drive folder URL candidate")) + `<input type="url" name="candidate_url" required maxlength="2048" placeholder="https://drive.google.com/drive/folders/..."></label><button class="btn-ghost" type="submit">` + esc(t(lang, "URLのアクセスを確認", "Verify URL access")) + `</button></form>`
	} else if connectedOptions == "" {
		body += `<p class="status-row">` + esc(t(lang, "利用できる接続済みDriveアカウントがありません。", "No connected Drive account is available.")) + `</p>`
	}
	body += `<h4>` + esc(t(lang, "利用可能なテンプレート", "Available templates")) + `</h4>`
	for _, scope := range []string{"production", "sequence", "shot", "asset", "task"} {
		if len(state.templates[scope]) == 0 {
			continue
		}
		body += `<p class="status-row"><strong>` + esc(scope) + `</strong>: `
		labels := make([]string, 0, len(state.templates[scope]))
		for _, template := range state.templates[scope] {
			labels = append(labels, esc(template.Label)+" v"+fmt.Sprint(template.Revision))
		}
		body += strings.Join(labels, ` · `) + `</p>`
	}
	body += `</section><section class="production-detail-surface"><h3>` + esc(t(lang, "Files設定と通知", "Files configuration and notifications")) + `</h3>`
	if !state.desiredReady {
		body += `<p class="status-row">` + esc(t(lang, "有効なDriveルートと同期設定はまだ確認できません。同期Applyと通知有効化は停止しています。", "A verified Drive root and desired sync configuration are not available. Sync Apply and notification activation are blocked.")) + `</p>`
	} else {
		activation, _ := state.desired["activation"].(map[string]any)
		active, _ := activation["enabled"].(bool)
		epoch, _ := activation["notification_epoch"].(float64)
		status := t(lang, "無効", "Disabled")
		if active && epoch > 0 {
			status = t(lang, "有効", "Enabled")
		}
		body += `<p class="status-row">` + esc(t(lang, "通知状態", "Notification state")) + `: <strong>` + esc(status) + `</strong> · ` + esc(t(lang, "設定リビジョン", "Config revision")) + ` ` + esc(fmt.Sprint(state.desired["config_revision"])) + `</p>`
		if state.integrationReady && !project.ValidationOnly && !project.ReadOnlyPreview {
			action := "activate"
			label := t(lang, "通知を有効にする", "Enable notifications")
			if active && epoch > 0 {
				action = "deactivate"
				label = t(lang, "通知を無効にする", "Disable notifications")
			}
			body += `<form method="post" class="button-row"><input type="hidden" name="action" value="` + action + `"><input type="hidden" name="idempotency_key" value="` + esc(vfxFilesActionKey(state.actionKey, action)) + `"><button class="btn" type="submit">` + esc(label) + `</button></form>`
		}
	}
	body += `</section><section class="production-detail-surface"><h3>` + esc(t(lang, "イベント診断", "Event diagnostics")) + `</h3>`
	if state.diagnosticsOK && state.diagnostics != nil {
		body += `<ul class="status-list"><li class="status-row">` + esc(t(lang, "未処理の配布通知", "Pending delivery events")) + `: <strong>` + fmt.Sprint(state.diagnostics.PendingDelivery) + `</strong></li><li class="status-row">` + esc(t(lang, "隔離イベント", "Quarantined events")) + `: <strong>` + fmt.Sprint(state.diagnostics.Quarantined) + `</strong></li><li class="status-row">` + esc(t(lang, "イベントID競合", "Event identity conflicts")) + `: <strong>` + fmt.Sprint(state.diagnostics.IdentityConflict) + `</strong></li></ul>`
	} else {
		body += `<p role="status">` + esc(t(lang, "イベント診断を確認できません。状態を空として扱わず、操作前にデータベースを確認してください。", "Event diagnostics are unavailable. Do not treat this as an empty state; check the database before taking action.")) + `</p>`
	}
	body += `</section><p class="field-help">` + esc(t(lang, "初回同期・差分Preview・Job管理はKitsuの読み取り専用Snapshot生成を接続するまで無効です。", "Initial sync, diff preview, and job management remain disabled until read-only Kitsu snapshot generation is connected.")) + `</p><p><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(productionID)+"&tab=settings", r)) + `">` + esc(t(lang, "Production設定へ戻る", "Back to Production settings")) + `</a></p></section>`
	return body
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

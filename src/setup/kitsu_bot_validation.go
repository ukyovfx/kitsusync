package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"app/src/model"
	"gorm.io/gorm"
)

const (
	BotTokenFullyCompatible             = "BOT_TOKEN_FULLY_COMPATIBLE"
	BotTokenPermissionInsufficient      = "BOT_TOKEN_PERMISSION_INSUFFICIENT"
	BotTokenInvalid                     = "BOT_TOKEN_INVALID"
	BotTokenExpiredOrInactive           = "BOT_TOKEN_EXPIRED_OR_INACTIVE"
	BotTokenRequiredEndpointFailure     = "REQUIRED_ENDPOINT_FAILURE"
	BotTokenAuthenticatedParseBug       = "AUTHENTICATED_RESPONSE_PARSE_BUG"
	BotTokenAuthenticatedIdentityNotBot = "AUTHENTICATED_IDENTITY_NOT_BOT"
	BotIdentityVerificationFailed       = "BOT_IDENTITY_VERIFICATION_FAILED"
	KitsuUnreachable                    = "KITSU_UNREACHABLE"
	UnknownFailure                      = "UNKNOWN_FAILURE"

	RuntimeKitsuAuthModeSettingKey         = "kitsu.runtime_auth_mode"
	RuntimeKitsuBotIDSettingKey            = "kitsu.runtime_bot_id"
	RuntimeKitsuBotNameSettingKey          = "kitsu.runtime_bot_name"
	RuntimeKitsuTokenValidatedAtSettingKey = "kitsu.runtime_token_validated_at"
	RuntimeKitsuTokenErrorSettingKey       = "kitsu.runtime_token_error_class"
)

type BotTokenReadResult struct {
	Endpoint   string `json:"endpoint"`
	StatusCode int    `json:"status_code"`
	Success    bool   `json:"success"`
	ErrorClass string `json:"error_class,omitempty"`
}

type BotTokenValidationResult struct {
	Classification string
	Stage          string
	Authenticated  bool
	IdentityID     string
	IdentityName   string
	IdentityIsBot  bool
	Reads          []BotTokenReadResult
	Failure        BotTokenReadResult
}

func (r BotTokenValidationResult) Compatible() bool {
	return r.Classification == BotTokenFullyCompatible
}

func recordBotTokenRead(result *BotTokenValidationResult, stage string, read BotTokenReadResult) {
	result.Reads = append(result.Reads, read)
	classification := "READ_OK"
	if !read.Success {
		classification = classifyBotTokenReadFailure(read)
	}
	slog.Info("Kitsu Bot token validation read",
		"stage", stage,
		"endpoint", read.Endpoint,
		"status", read.StatusCode,
		"classification", classification,
		"error_class", read.ErrorClass,
		"compatible", false,
	)
}

type botTokenIdentity struct {
	ID        string `json:"id"`
	Name      string `json:"full_name"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	IsBot     *bool  `json:"is_bot"`
	Active    *bool  `json:"active"`
	Archived  *bool  `json:"archived"`
}

type botTokenAuthenticatedResponse struct {
	Authenticated bool              `json:"authenticated"`
	User          *botTokenIdentity `json:"user"`
	ID            string            `json:"id"`
	Name          string            `json:"full_name"`
	FirstName     string            `json:"first_name"`
	LastName      string            `json:"last_name"`
	IsBot         *bool             `json:"is_bot"`
	Active        *bool             `json:"active"`
	Archived      *bool             `json:"archived"`
}

func ValidateKitsuBotToken(db *gorm.DB, hostname, token string, includeComments bool) BotTokenValidationResult {
	result := BotTokenValidationResult{Classification: BotTokenInvalid, Stage: "host_resolution"}
	token = strings.TrimSpace(token)
	if strings.TrimSpace(hostname) == "" || token == "" {
		result.Classification = BotTokenInvalid
		return result
	}
	connection, err := ResolveAndProbeKitsu(context.Background(), hostname, APISourceExplicit)
	if err != nil {
		result.Classification = KitsuUnreachable
		result.Failure = BotTokenReadResult{ErrorClass: connectionErrorClass(err)}
		return result
	}
	base := strings.TrimSuffix(connection.ResolvedAPIBaseURL, "/api")
	client := safeKitsuClient(connection, connection.VerifiedIPs)
	identityBody, read := botTokenGET(client, base+"/api/auth/authenticated", token, nil)
	result.Stage = "token_authentication"
	recordBotTokenRead(&result, result.Stage, read)
	if !read.Success {
		result.Failure = read
		switch read.StatusCode {
		case http.StatusUnauthorized:
			result.Classification = BotTokenExpiredOrInactive
		case http.StatusForbidden:
			result.Classification = BotTokenPermissionInsufficient
		case 0:
			result.Classification = KitsuUnreachable
		default:
			result.Classification = BotTokenInvalid
		}
		return result
	}
	result.Authenticated = true
	result.Stage = "bot_identity"
	var authenticated botTokenAuthenticatedResponse
	if err := json.Unmarshal(identityBody, &authenticated); err != nil {
		result.Classification = BotTokenAuthenticatedParseBug
		result.Failure = BotTokenReadResult{Endpoint: "/api/auth/authenticated", StatusCode: http.StatusOK, ErrorClass: "invalid_json"}
		logBotTokenIdentityFailure(result)
		return result
	}
	identity := authenticated.User
	if identity == nil {
		identity = &botTokenIdentity{
			ID:        authenticated.ID,
			Name:      authenticated.Name,
			FirstName: authenticated.FirstName,
			LastName:  authenticated.LastName,
			IsBot:     authenticated.IsBot,
			Active:    authenticated.Active,
			Archived:  authenticated.Archived,
		}
	}
	result.IdentityID = strings.TrimSpace(identity.ID)
	result.IdentityName = strings.TrimSpace(identity.Name)
	if result.IdentityName == "" {
		result.IdentityName = strings.TrimSpace(strings.TrimSpace(identity.FirstName) + " " + strings.TrimSpace(identity.LastName))
	}
	if result.IdentityID == "" || identity.IsBot == nil {
		result.Classification = BotIdentityVerificationFailed
		result.Failure = BotTokenReadResult{Endpoint: "/api/auth/authenticated", StatusCode: http.StatusOK, ErrorClass: "bot_identity_unavailable"}
		logBotTokenIdentityFailure(result)
		return result
	}
	result.IdentityIsBot = *identity.IsBot
	if !result.IdentityIsBot {
		result.Classification = BotTokenAuthenticatedIdentityNotBot
		result.Failure = BotTokenReadResult{Endpoint: "/api/auth/authenticated", StatusCode: http.StatusOK, ErrorClass: "authenticated_identity_is_not_bot"}
		logBotTokenIdentityFailure(result)
		return result
	}
	if (identity.Active != nil && !*identity.Active) || (identity.Archived != nil && *identity.Archived) {
		result.Classification = BotIdentityVerificationFailed
		result.Failure = BotTokenReadResult{Endpoint: "/api/auth/authenticated", StatusCode: http.StatusOK, ErrorClass: "bot_identity_inactive_or_archived"}
		logBotTokenIdentityFailure(result)
		return result
	}

	productionIDs := localKitsuProductionIDs(db)
	// These reads are used by the running notifier and current setup/user
	// linking flows. Comments are diagnostic/enrichment data and must not make
	// an otherwise usable Bot token fail validation.
	checks := []struct {
		stage    string
		endpoint string
	}{
		{"production_list", "/api/data/projects/"},
		{"persons", "/api/data/persons/"},
		{"task_status", "/api/data/task-status/"},
		{"entities", "/api/data/entities/"},
		{"entity_types", "/api/data/entity-types/"},
		{"task_types", "/api/data/task-types/"},
		{"tasks", "/api/data/tasks?relations=true"},
	}
	for _, check := range checks {
		result.Stage = check.stage
		_, read = botTokenGET(client, base+check.endpoint, token, nil)
		recordBotTokenRead(&result, result.Stage, read)
		if !read.Success {
			result.Failure = read
			result.Classification = classifyBotTokenReadFailure(read)
			return result
		}
	}
	if includeComments {
		// Optional diagnostic read: record its result, but do not reject a token
		// when comments are permission-restricted or unavailable.
		_, read = botTokenGET(client, base+"/api/data/comments", token, nil)
		recordBotTokenRead(&result, "comments", read)
	}
	for _, productionID := range productionIDs {
		for _, endpoint := range []string{
			"/api/data/projects/" + url.PathEscape(productionID),
			"/api/data/projects/" + url.PathEscape(productionID) + "/task-types",
		} {
			if strings.HasSuffix(endpoint, "/task-types") {
				result.Stage = "production_task_types"
			} else {
				result.Stage = "target_production"
			}
			_, read = botTokenGET(client, base+endpoint, token, nil)
			recordBotTokenRead(&result, result.Stage, read)
			if !read.Success {
				result.Failure = read
				result.Classification = classifyBotTokenReadFailure(read)
				return result
			}
		}
	}
	result.Classification = BotTokenFullyCompatible
	slog.Info("Kitsu Bot token validation complete",
		"stage", result.Stage,
		"endpoint", "",
		"status", 200,
		"classification", result.Classification,
		"error_class", "",
		"compatible", true,
	)
	return result
}

func logBotTokenIdentityFailure(result BotTokenValidationResult) {
	slog.Info("Kitsu Bot token identity verification",
		"stage", result.Stage,
		"endpoint", result.Failure.Endpoint,
		"status", result.Failure.StatusCode,
		"classification", result.Classification,
		"error_class", result.Failure.ErrorClass,
		"compatible", false,
	)
}

func localKitsuProductionIDs(db *gorm.DB) []string {
	if db == nil {
		return nil
	}
	projects := model.ListProjects(db)
	ids := make([]string, 0, len(projects))
	for _, project := range projects {
		if id := strings.TrimSpace(project.KitsuProjectID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func classifyBotTokenReadFailure(read BotTokenReadResult) string {
	if read.StatusCode == 0 {
		return KitsuUnreachable
	}
	if read.StatusCode == http.StatusUnauthorized {
		return BotTokenExpiredOrInactive
	}
	if read.StatusCode == http.StatusForbidden {
		return BotTokenPermissionInsufficient
	}
	if read.StatusCode == http.StatusNotFound {
		return BotTokenRequiredEndpointFailure
	}
	return UnknownFailure
}

func botTokenGET(client *http.Client, endpoint, token string, target interface{}) ([]byte, BotTokenReadResult) {
	read := BotTokenReadResult{Endpoint: endpointPath(endpoint)}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		read.ErrorClass = "request_creation_error"
		return nil, read
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		read.ErrorClass = "network_error"
		return nil, read
	}
	defer resp.Body.Close()
	read.StatusCode = resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		read.ErrorClass = "response_read_error"
		return nil, read
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		read.ErrorClass = fmt.Sprintf("http_%d", resp.StatusCode)
		return nil, read
	}
	if target != nil && len(body) > 0 {
		if err := json.Unmarshal(body, target); err != nil {
			read.ErrorClass = "invalid_json"
			return nil, read
		}
	}
	read.Success = true
	return body, read
}

func endpointPath(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "unknown"
	}
	path := parsed.Path
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	return path
}

func botTokenValidationError(result BotTokenValidationResult) error {
	if result.Compatible() {
		return nil
	}
	if result.Failure.Endpoint == "" {
		return errors.New(result.Classification)
	}
	return fmt.Errorf("%s at %s (HTTP %d; %s)", result.Classification, result.Failure.Endpoint, result.Failure.StatusCode, result.Failure.ErrorClass)
}

func botTokenValidationUserMessage(lang string, result BotTokenValidationResult) string {
	switch result.Classification {
	case BotTokenExpiredOrInactive:
		return t(lang, "Bot tokenが無効または期限切れです。Botの状態を確認してください。", "The Bot token is invalid, expired, or inactive. Check the Bot status.")
	case BotTokenPermissionInsufficient:
		return t(lang, "Bot tokenは有効ですが、必要なKitsu APIの読み取り権限が不足しています。", "The Bot token is valid, but it lacks a required Kitsu API read permission.")
	case KitsuUnreachable:
		return t(lang, "Kitsuサーバーに接続できませんでした。接続先を確認してください。", "Kitsu could not be reached. Check the configured host.")
	case BotTokenInvalid:
		return t(lang, "Kitsu Bot tokenを確認できませんでした。", "The Kitsu Bot token could not be verified.")
	default:
		return t(lang, "Kitsu Bot tokenの検証に失敗しました。", "Kitsu Bot token validation failed.")
	}
}

func botTokenValidationUserMessageSafe(lang string, result BotTokenValidationResult) string {
	switch result.Classification {
	case BotTokenExpiredOrInactive:
		return t(lang, "Bot tokenが無効、期限切れ、または無効化されています。Botの状態を確認してください。", "The Bot token is invalid, expired, or inactive. Check the Bot status.")
	case BotTokenPermissionInsufficient:
		return t(lang, "Bot tokenは有効ですが、Kitsu APIの読み取り権限が不足しています。", "The Bot token is valid, but it lacks a required Kitsu API read permission.")
	case KitsuUnreachable:
		return t(lang, "Kitsuサーバーに接続できませんでした。接続先を確認してください。", "Kitsu could not be reached. Check the configured host.")
	case BotTokenInvalid:
		return t(lang, "Kitsu Bot tokenを確認できませんでした。", "The Kitsu Bot token could not be verified.")
	default:
		return t(lang, "Kitsu Bot tokenの検証に失敗しました。", "Kitsu Bot token validation failed.")
	}
}

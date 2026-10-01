package setup

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

type notificationApplyRequest struct {
	ProjectID        string `json:"project_id"`
	ExpectedRevision string `json:"expected_revision"`
	Routes           []struct {
		TaskTypeID           string `json:"task_type_id"`
		DestinationWebhookID uint   `json:"destination_webhook_id"`
		CreateChannelName    string `json:"create_channel_name"`
	} `json:"routes"`
	ReviewerChanges []model.ProductionReviewerDelta `json:"reviewer_changes"`
}

type createdRoutingDiscordResource struct {
	ChannelID string
	WebhookID string
}

var currentRoutingCreateChannel = CreateTextChannel
var currentRoutingCreateWebhook = CreateWebhook
var currentRoutingDeleteWebhook = DeleteWebhook

func handleProductionNotificationApply(w http.ResponseWriter, r *http.Request, lang string, db *gorm.DB, botTokens ...string) bool {
	if r.Method != http.MethodPost || r.URL.Path != "/bot/admin/projects" || r.URL.Query().Get("apply_notifications") != "1" {
		return false
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var request notificationApplyRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return true
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return true
	}
	productionID := strings.TrimSpace(request.ProjectID)
	project := model.FindProjectByKitsuID(db, productionID)
	if project == nil || model.IsValidationOnlyProject(db, productionID) {
		http.Error(w, `{"error":"production_not_writable"}`, http.StatusForbidden)
		return true
	}
	if err := model.ValidateReviewerDeltasForRemovedRoutes(convertApplyRoutes(request, productionID), request.ReviewerChanges); err != nil {
		http.Error(w, `{"error":"reviewer_delta_for_removed_route"}`, http.StatusBadRequest)
		return true
	}
	known := reviewerTaskTypesForProduction(db, productionID)
	knownByID := make(map[string]kitsu.TaskType, len(known))
	for _, task := range known {
		knownByID[strings.TrimSpace(task.ID)] = task
	}
	routes := make([]model.ProductionNotificationRoute, 0, len(request.Routes))
	createNames := make(map[string]string)
	newWebhooks := make([]*model.ProjectWebhook, 0)
	existingRoutes := make([]model.ProductionNotificationRoute, 0, len(request.Routes))
	seen := map[string]bool{}
	for _, item := range request.Routes {
		id := strings.TrimSpace(item.TaskTypeID)
		task, ok := knownByID[id]
		createName := strings.TrimSpace(item.CreateChannelName)
		if id == "" || !ok || seen[id] || (item.DestinationWebhookID == 0) == (createName == "") {
			http.Error(w, `{"error":"invalid_route"}`, http.StatusBadRequest)
			return true
		}
		seen[id] = true
		route := model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: id, TaskTypeName: task.Name, DestinationWebhookID: item.DestinationWebhookID}
		routes = append(routes, route)
		if createName != "" {
			normalized := NormalizeTaskTypeChannelName(createName)
			if createNames[id] != "" {
				http.Error(w, `{"error":"invalid_route"}`, http.StatusBadRequest)
				return true
			}
			createNames[id] = normalized
			newWebhooks = append(newWebhooks, &model.ProjectWebhook{KitsuProjectID: productionID, ChannelName: normalized, TaskType: id})
		} else {
			existingRoutes = append(existingRoutes, route)
		}
	}
	if len(routes) == 0 {
		http.Error(w, `{"error":"invalid_routing"}`, http.StatusBadRequest)
		return true
	}
	if err := validateCurrentRoutingDestinations(*project, existingRoutes, db); err != nil {
		http.Error(w, `{"error":"destination_not_owned"}`, http.StatusBadRequest)
		return true
	}
	tokenFallback := ""
	if len(botTokens) > 0 {
		tokenFallback = botTokens[0]
	}
	discordToken := storedRuntimeDiscordBotToken(db)
	if discordToken == "" {
		discordToken = tokenFallback
	}
	if err := validateNewRoutingChannelNames(*project, createNames, discordToken); err != nil {
		http.Error(w, `{"error":"channel_name_unavailable"}`, http.StatusBadRequest)
		return true
	}
	if err := validateProductionReviewerDeltas(db, *project, request.ReviewerChanges, tokenFallback); err != nil {
		http.Error(w, `{"error":"reviewer_target_not_eligible"}`, http.StatusBadRequest)
		return true
	}
	config, routesBefore, targetsBefore, err := model.SnapshotProductionNotificationState(db, project.ID, productionID)
	if err != nil {
		http.Error(w, `{"error":"state_unavailable"}`, http.StatusInternalServerError)
		return true
	}
	if model.ProductionNotificationRevision(db, project.ID, productionID) != request.ExpectedRevision {
		http.Error(w, `{"error":"stale_edit"}`, http.StatusConflict)
		return true
	}
	routingChanged := !sameProductionNotificationRoutes(routesBefore, routes)
	var priorOrder []DiscordChannelPosition
	if routingChanged && len(routesBefore)+len(existingRoutes) > 0 {
		priorOrder, err = captureCurrentRoutingPositions(*project, append(append([]model.ProductionNotificationRoute{}, routesBefore...), existingRoutes...), db)
		if err != nil {
			http.Error(w, `{"error":"discord_preflight_failed"}`, http.StatusBadRequest)
			return true
		}
	}
	createdResources := make([]createdRoutingDiscordResource, 0, len(newWebhooks))
	cleanupCreated := func() error { return cleanupCreatedRoutingResources(createdResources, discordToken) }
	if len(newWebhooks) > 0 {
		guild, category := strings.TrimSpace(project.DiscordGuildID), strings.TrimSpace(project.DiscordCategoryID)
		status := currentRoutingDiscordCheck(discordToken, guild)
		if !routingDiscordStatusReady(status) || !status.Permissions.ManageWebhooks {
			http.Error(w, `{"error":"discord_preflight_failed"}`, http.StatusBadRequest)
			return true
		}
		for _, webhook := range newWebhooks {
			channelID, createErr := currentRoutingCreateChannel(guild, category, webhook.ChannelName, discordToken)
			if createErr != nil {
				cleanupErr := cleanupCreated()
				// The Discord request may have reached the server even when the
				// client received an error. We have no channel ID to safely
				// compensate, so report the creation outcome as unknown.
				writeRoutingChannelCreateFailure(w, cleanupErr)
				return true
			}
			resource := createdRoutingDiscordResource{ChannelID: strings.TrimSpace(channelID)}
			createdResources = append(createdResources, resource)
			if resource.ChannelID == "" {
				writeRoutingChannelCreateFailure(w, cleanupCreated())
				return true
			}
			channels, listErr := currentRoutingListChannels(guild, discordToken)
			if listErr != nil {
				writeRoutingCreateFailure(w, "channel_verification_failed", cleanupCreated())
				return true
			}
			verified := false
			for _, channel := range channels {
				if channel.ID == resource.ChannelID && channel.Type == 0 && strings.TrimSpace(channel.ParentID) == category {
					verified = true
					break
				}
			}
			if !verified {
				writeRoutingCreateFailure(w, "channel_verification_failed", cleanupCreated())
				return true
			}
			webhookURL, createErr := currentRoutingCreateWebhook(resource.ChannelID, webhook.ChannelName, discordToken)
			if createErr != nil {
				writeRoutingCreateFailure(w, "webhook_create_failed", cleanupCreated())
				return true
			}
			webhookID, parseErr := discordWebhookID(webhookURL)
			if parseErr != nil {
				writeRoutingCreateFailure(w, "webhook_response_invalid", cleanupCreated())
				return true
			}
			createdResources[len(createdResources)-1].WebhookID = webhookID
			webhook.DiscordChannelID = resource.ChannelID
			webhook.WebhookURL = webhookURL
		}
	}
	appliedRevision, applyErr := model.ApplyProductionNotificationState(db, project.ID, productionID, project.Name, request.ExpectedRevision, routes, request.ReviewerChanges, newWebhooks...)
	err = applyErr
	if errors.Is(err, model.ErrProductionNotificationRevisionConflict) {
		cleanupErr := cleanupCreated()
		if cleanupErr != nil {
			writeRoutingCreateFailure(w, "stale_edit", cleanupErr)
			return true
		}
		http.Error(w, `{"error":"stale_edit"}`, http.StatusConflict)
		return true
	}
	if err != nil {
		if cleanupErr := cleanupCreated(); cleanupErr != nil {
			writeRoutingCreateFailure(w, "apply_failed", cleanupErr)
		} else {
			http.Error(w, `{"error":"apply_failed"}`, http.StatusInternalServerError)
		}
		return true
	}
	routes = model.ListProductionNotificationRoutes(db, productionID)
	if routingChanged {
		err = syncCurrentRoutingDiscordOrder(*project, routes, db)
	}
	if err != nil {
		dbRestoreErr, discordRestoreErr := compensateProductionNotificationApply(
			func() error {
				ids := make([]uint, 0, len(newWebhooks))
				for _, webhook := range newWebhooks {
					if webhook.ID > 0 {
						ids = append(ids, webhook.ID)
					}
				}
				return model.RestoreProductionNotificationState(db, project.ID, productionID, appliedRevision, config, routesBefore, targetsBefore, ids...)
			},
			func() error {
				if len(priorOrder) == 0 {
					return nil
				}
				return currentRoutingSetPositions(project.DiscordGuildID, priorOrder, discordToken)
			},
		)
		var resourceErr error
		resourceCleanup := "not_required"
		if dbRestoreErr == nil {
			if len(createdResources) > 0 {
				resourceCleanup = "complete"
				resourceErr = cleanupCreated()
			}
		} else if len(createdResources) > 0 {
			// Keep external resources when the DB restore failed: deleting them
			// could leave persisted state pointing at missing Discord objects.
			resourceCleanup = "deferred_database_restore_failed"
		}
		status, body := productionNotificationRecoveryDiagnostic(dbRestoreErr, discordRestoreErr, resourceErr)
		body["resource_cleanup"] = resourceCleanup
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
		return true
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "applied"})
	return true
}

func productionNotificationRecoveryDiagnostic(databaseErr, discordErr error, resourceErrors ...error) (int, map[string]string) {
	status := http.StatusBadGateway
	var resourceErr error
	if len(resourceErrors) > 0 {
		resourceErr = resourceErrors[0]
	}
	if databaseErr != nil || discordErr != nil || resourceErr != nil {
		status = http.StatusInternalServerError
	}
	body := map[string]string{"error": "discord_order_failed", "recovery": "complete", "database_recovery": "restored", "discord_order_recovery": "restored", "resource_cleanup": "not_required"}
	if databaseErr != nil {
		body["database_recovery"] = "failed"
		body["recovery"] = "incomplete"
	}
	if discordErr != nil {
		body["discord_order_recovery"] = "failed"
		body["recovery"] = "incomplete"
	}
	if resourceErr != nil {
		body["resource_cleanup"] = "failed"
		body["recovery"] = "incomplete"
	} else if len(resourceErrors) > 0 {
		body["resource_cleanup"] = "complete"
	}
	return status, body
}

func sameProductionNotificationRoutes(before, after []model.ProductionNotificationRoute) bool {
	if len(before) != len(after) {
		return false
	}
	for i := range before {
		if strings.TrimSpace(before[i].TaskTypeID) != strings.TrimSpace(after[i].TaskTypeID) || before[i].DestinationWebhookID != after[i].DestinationWebhookID {
			return false
		}
	}
	return true
}

func compensateProductionNotificationApply(restoreDatabase, restoreDiscordOrder func() error) (error, error) {
	var databaseErr, discordErr error
	if restoreDatabase != nil {
		databaseErr = restoreDatabase()
	}
	if restoreDiscordOrder != nil {
		discordErr = restoreDiscordOrder()
	}
	return databaseErr, discordErr
}

func convertApplyRoutes(request notificationApplyRequest, productionID string) []model.ProductionNotificationRoute {
	routes := make([]model.ProductionNotificationRoute, 0, len(request.Routes))
	for _, route := range request.Routes {
		routes = append(routes, model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: strings.TrimSpace(route.TaskTypeID), DestinationWebhookID: route.DestinationWebhookID})
	}
	return routes
}

func validateProductionReviewerDeltas(db *gorm.DB, project model.Project, changes []model.ProductionReviewerDelta, botTokenFallback string) error {
	if len(changes) == 0 {
		return nil
	}
	team, err := reviewerProductionTeamReader(db, project.KitsuProjectID)
	if err != nil {
		return err
	}
	users := filterAssignableUsers(model.ListUserMap(db), botAccountEmail(db))
	token := storedRuntimeDiscordBotToken(db)
	if token == "" {
		token = botTokenFallback
	}
	members, err := reviewerGuildMembersForGuild(project.DiscordGuildID, token)
	if err != nil {
		return err
	}
	eligibleUsers := currentProductionLinkedHumanDiscordIDs(team, users, members)
	roles, err := reviewerDiscordRolesForGuild(project.DiscordGuildID, token)
	if err != nil {
		return err
	}
	eligibleRoles := map[string]bool{}
	for _, role := range mentionableReviewerRoles(project.DiscordGuildID, roles) {
		eligibleRoles[role.ID] = true
	}
	for _, change := range changes {
		targets, _, readErr := model.ListProjectReviewerTargetsForTaskType(db, project.ID, strings.TrimSpace(change.TaskTypeID))
		if readErr != nil {
			return readErr
		}
		existing := map[string]bool{}
		for _, target := range targets {
			existing[target.TargetKind+":"+target.DiscordID] = true
		}
		for _, id := range change.AddUserIDs {
			if eligibleUsers[strings.TrimSpace(id)] == "" {
				return errors.New("user target is not a linked current Production member")
			}
		}
		for _, id := range change.AddRoleIDs {
			if !eligibleRoles[strings.TrimSpace(id)] {
				return errors.New("role target is not mentionable in the linked Guild")
			}
		}
		for _, id := range change.RemoveUserIDs {
			if !existing[model.ReviewerTargetUser+":"+strings.TrimSpace(id)] {
				return errors.New("user target removal is not an existing additional target")
			}
		}
		for _, id := range change.RemoveRoleIDs {
			if !existing[model.ReviewerTargetRole+":"+strings.TrimSpace(id)] {
				return errors.New("role target removal is not an existing additional target")
			}
		}
	}
	return nil
}

func validateCurrentRoutingDestinations(project model.Project, routes []model.ProductionNotificationRoute, db *gorm.DB) error {
	if len(routes) == 0 {
		return nil
	}
	guild, category, token := strings.TrimSpace(project.DiscordGuildID), strings.TrimSpace(project.DiscordCategoryID), storedRuntimeDiscordBotToken(db)
	status := currentRoutingDiscordCheck(token, guild)
	if !routingDiscordStatusReady(status) {
		return errors.New("Discord ownership preflight failed")
	}
	channels, err := currentRoutingListChannels(guild, token)
	if err != nil {
		return err
	}
	byID := make(map[string]DiscordGuildChannel, len(channels))
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	seen := map[string]bool{}
	for _, route := range routes {
		webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID)
		if webhook == nil || webhook.KitsuProjectID != project.KitsuProjectID {
			return errors.New("routing destination is not owned by this Production")
		}
		id := strings.TrimSpace(webhook.DiscordChannelID)
		channel, ok := byID[id]
		if !ok || channel.Type != 0 || strings.TrimSpace(channel.ParentID) != category || seen[id] {
			return errors.New("routing destination is not a unique verified child of the managed category")
		}
		seen[id] = true
	}
	return nil
}

func validateNewRoutingChannelNames(project model.Project, names map[string]string, token string) error {
	if len(names) == 0 {
		return nil
	}
	guild, category := strings.TrimSpace(project.DiscordGuildID), strings.TrimSpace(project.DiscordCategoryID)
	if guild == "" || category == "" || !routingDiscordStatusReady(currentRoutingDiscordCheck(token, guild)) {
		return errors.New("Discord channel creation preflight failed")
	}
	channels, err := currentRoutingListChannels(guild, token)
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, channel := range channels {
		used[NormalizeTaskTypeChannelName(channel.Name)] = true
	}
	requested := map[string]bool{}
	for _, name := range names {
		normalized := NormalizeTaskTypeChannelName(name)
		if requested[normalized] || used[normalized] {
			return errors.New("Discord channel name is already present")
		}
		requested[normalized] = true
	}
	return nil
}

func discordWebhookID(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "discord.com" {
		return "", errors.New("Discord webhook response was not a trusted URL")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "webhooks" || strings.TrimSpace(parts[2]) == "" || strings.TrimSpace(parts[3]) == "" {
		return "", errors.New("Discord webhook response was incomplete")
	}
	return parts[2], nil
}

func cleanupCreatedRoutingResources(resources []createdRoutingDiscordResource, token string) error {
	failed := false
	for i := len(resources) - 1; i >= 0; i-- {
		resource := resources[i]
		if resource.WebhookID != "" {
			if err := currentRoutingDeleteWebhook(resource.WebhookID, token); err != nil {
				failed = true
			}
		}
		if resource.ChannelID != "" {
			if err := currentRoutingDeleteChannel(resource.ChannelID, token); err != nil {
				failed = true
			}
		}
	}
	if failed {
		return errors.New("newly created Discord resources could not all be removed")
	}
	return nil
}

func writeRoutingCreateFailure(w http.ResponseWriter, reason string, cleanupErr error) {
	status := http.StatusBadGateway
	cleanup := "complete"
	if cleanupErr != nil {
		status = http.StatusInternalServerError
		cleanup = "incomplete"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": reason, "resource_cleanup": cleanup})
}

func writeRoutingChannelCreateFailure(w http.ResponseWriter, cleanupErr error) {
	status := http.StatusBadGateway
	cleanup := "known_resources_complete"
	if cleanupErr != nil {
		status = http.StatusInternalServerError
		cleanup = "known_resources_incomplete"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":            "channel_create_failed",
		"channel_creation": "outcome_unknown",
		"resource_cleanup": cleanup,
	})
}

func captureCurrentRoutingPositions(project model.Project, routes []model.ProductionNotificationRoute, db *gorm.DB) ([]DiscordChannelPosition, error) {
	guild := strings.TrimSpace(project.DiscordGuildID)
	token := storedRuntimeDiscordBotToken(db)
	status := currentRoutingDiscordCheck(token, guild)
	if !routingDiscordStatusReady(status) {
		return nil, errors.New("Discord channel-order preflight failed")
	}
	channels, err := currentRoutingListChannels(guild, token)
	if err != nil {
		return nil, err
	}
	byWebhook := map[string]bool{}
	positions := []DiscordChannelPosition{}
	seen := map[string]bool{}
	for _, route := range routes {
		webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID)
		if webhook == nil || webhook.KitsuProjectID != project.KitsuProjectID {
			return nil, errors.New("route destination ownership is incomplete")
		}
		byWebhook[strings.TrimSpace(webhook.DiscordChannelID)] = true
	}
	for _, channel := range channels {
		if byWebhook[channel.ID] && !seen[channel.ID] {
			positions = append(positions, DiscordChannelPosition{ID: channel.ID, Position: channel.Position})
			seen[channel.ID] = true
		}
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i].Position < positions[j].Position })
	return positions, nil
}

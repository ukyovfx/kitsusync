package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"app/src/model"
	"gorm.io/gorm"
)

type notificationApplyRequest struct {
	ProjectID        string                          `json:"project_id"`
	ExpectedRevision string                          `json:"expected_revision"`
	ReviewerChanges  []model.ProductionReviewerDelta `json:"reviewer_changes"`
}

var productionNotificationApplyLocks sync.Map

func lockProductionNotificationApply(productionID string) func() {
	productionID = strings.TrimSpace(productionID)
	value, _ := productionNotificationApplyLocks.LoadOrStore(productionID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

// handleProductionNotificationApply applies reviewer-target changes only.
// Notification routing itself (forum channels per task type) is owned by the
// forum settings screen.
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
	unlockApply := lockProductionNotificationApply(productionID)
	defer unlockApply()
	project = model.FindProjectByKitsuID(db, productionID)
	if project == nil || model.IsValidationOnlyProject(db, productionID) {
		http.Error(w, `{"error":"production_not_writable"}`, http.StatusForbidden)
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
	if err := validateProductionReviewerDeltas(db, *project, request.ReviewerChanges, discordToken); err != nil {
		if errors.Is(err, errReviewerEligibilityUnavailable) {
			http.Error(w, `{"error":"reviewer_eligibility_unavailable"}`, http.StatusBadGateway)
		} else {
			http.Error(w, `{"error":"reviewer_target_not_eligible"}`, http.StatusBadRequest)
		}
		return true
	}
	if _, err := model.ApplyProductionReviewerDeltas(db, project.ID, productionID, request.ExpectedRevision, request.ReviewerChanges); err != nil {
		if errors.Is(err, model.ErrProductionNotificationRevisionConflict) {
			http.Error(w, `{"error":"stale_edit"}`, http.StatusConflict)
			return true
		}
		if errors.Is(err, model.ErrReviewerDeltaForRemovedRoute) {
			http.Error(w, `{"error":"reviewer_delta_for_removed_route"}`, http.StatusBadRequest)
			return true
		}
		http.Error(w, `{"error":"apply_failed"}`, http.StatusInternalServerError)
		return true
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "applied"})
	return true
}

func validateProductionReviewerDeltas(db *gorm.DB, project model.Project, changes []model.ProductionReviewerDelta, token string) error {
	if len(changes) == 0 {
		return nil
	}
	var eligibleUsers map[string]string
	usersLoaded := false
	eligibleRolesByTask := make(map[string]map[string]struct{})
	for _, change := range changes {
		taskTypeID := strings.TrimSpace(change.TaskTypeID)
		targets, _, readErr := model.ListProjectReviewerTargetsForTaskType(db, project.ID, taskTypeID)
		if readErr != nil {
			return readErr
		}
		existing := map[string]bool{}
		for _, target := range targets {
			existing[target.TargetKind+":"+target.DiscordID] = true
		}
		for _, id := range change.AddUserIDs {
			if !usersLoaded {
				team, err := reviewerProductionTeamReader(db, project.KitsuProjectID)
				if err != nil {
					return fmt.Errorf("%w: read Kitsu Production Team", errReviewerEligibilityUnavailable)
				}
				members, err := reviewerGuildMembersForGuild(project.DiscordGuildID, token)
				if err != nil {
					return fmt.Errorf("%w: read linked Discord Guild membership", errReviewerEligibilityUnavailable)
				}
				eligibleUsers = currentProductionLinkedHumanDiscordIDs(team, filterAssignableUsers(model.ListUserMap(db), botAccountEmail(db)), members)
				usersLoaded = true
			}
			if eligibleUsers[strings.TrimSpace(id)] == "" {
				return errors.New("user target is not a linked current Production member")
			}
		}
		for _, id := range change.AddRoleIDs {
			eligibleRoles := eligibleRolesByTask[taskTypeID]
			if eligibleRoles == nil {
				var err error
				eligibleRoles, err = ForumReviewerRoles(db, project.KitsuProjectID, taskTypeID, token)
				if err != nil {
					return fmt.Errorf("%w: verify role target for forum channel", errReviewerEligibilityUnavailable)
				}
				eligibleRolesByTask[taskTypeID] = eligibleRoles
			}
			if _, ok := eligibleRoles[strings.TrimSpace(id)]; !ok {
				return errors.New("role target is not eligible for the verified destination")
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

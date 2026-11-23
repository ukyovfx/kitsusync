package forum

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"app/src/api/kitsu"
	"app/src/model"
)

// HandoffWatcher is the status-transition handoff (redesign core v2,
// 2026-11-23). It replaces the publish-comment detector: a handoff fires when
// a source task ENTERS a done-nature status, and the wired downstream tasks
// on the same entity are told "揃った、進めていい". It runs after the planner
// wrote this poll's transitions and reads exactly those rows, so a handoff
// can never fire twice for the same entry and never fires for state that
// predates the poll (the cutover baseline stays silent).
type HandoffWatcher struct {
	DB       *gorm.DB
	Policies map[string]model.StudioStatusPolicy
	Now      time.Time
}

// HandoffWatchResult counts one watcher pass.
type HandoffWatchResult struct {
	Fired      int // episode events recorded
	Suppressed int // firings the rules held back (see the decision log)
}

// WatchHandoffs processes the transitions with ID > sinceTransitionID (the
// watermark from before this poll's planning).
func (w HandoffWatcher) WatchHandoffs(payloads []kitsu.MessagePayload, sinceTransitionID uint) (HandoffWatchResult, error) {
	var result HandoffWatchResult
	if w.DB == nil {
		return result, nil
	}
	if w.Now.IsZero() {
		w.Now = time.Now().UTC()
	}
	if w.Policies == nil {
		policies, err := model.LoadStudioStatusPolicies(w.DB)
		if err != nil {
			return result, err
		}
		w.Policies = policies
	}
	var transitions []model.ForumTransition
	if err := w.DB.Where("id > ?", sinceTransitionID).Order("id asc").Find(&transitions).Error; err != nil {
		return result, err
	}
	if len(transitions) == 0 {
		return result, nil
	}
	byTask := map[string]kitsu.MessagePayload{}
	byEntity := map[string][]kitsu.MessagePayload{}
	for _, payload := range payloads {
		if payload.Task.Task.ID == "" {
			continue
		}
		byTask[payload.Task.Task.ID] = payload
		entityID := strings.TrimSpace(payload.Entity.Entity.ID)
		if entityID != "" {
			byEntity[entityID] = append(byEntity[entityID], payload)
		}
	}
	planners := map[string]*Planner{}
	plannerFor := func(productionID string) *Planner {
		if cached, ok := planners[productionID]; ok {
			return cached
		}
		settings, found, err := model.LoadProductionForumSettings(w.DB, productionID)
		if err != nil || !found || len(model.ForumSettingsProblems(settings)) > 0 {
			planners[productionID] = nil
			return nil
		}
		pl := Planner{DB: w.DB, Production: productionID, Settings: settings, Policies: w.Policies, Now: w.Now}
		planners[productionID] = &pl
		return &pl
	}
	noRouteRaised := map[string]bool{}
	for _, transition := range transitions {
		source, ok := byTask[transition.TaskID]
		if !ok {
			continue
		}
		productionID := source.Project.Project.ID
		planner := plannerFor(productionID)
		if planner == nil {
			continue
		}
		if planner.NatureForStatus(transition.ToStatusID) != model.StatusNatureDone {
			continue
		}
		if transition.FromStatusID != "" && planner.NatureForStatus(transition.FromStatusID) == model.StatusNatureDone {
			// Movement inside the done nature (Approved -> Done) is the same
			// episode: no second voice.
			continue
		}
		fired, suppressed := w.fireEpisode(source, transition, planner, byEntity[strings.TrimSpace(source.Entity.Entity.ID)], noRouteRaised)
		result.Fired += fired
		result.Suppressed += suppressed
	}
	return result, nil
}

// fireEpisode records the handoff events of one done-nature entry and writes
// the decision rows for every outcome (fired, digest, suppressed).
func (w HandoffWatcher) fireEpisode(source kitsu.MessagePayload, transition model.ForumTransition, planner *Planner, entityPayloads []kitsu.MessagePayload, noRouteRaised map[string]bool) (fired, suppressed int) {
	productionID := source.Project.Project.ID
	sourceTaskID := source.Task.Task.ID
	sourceTypeID := source.TaskType.TaskType.ID
	sourceTypeName := source.TaskType.TaskType.Name
	episodeKey := fmt.Sprintf("t%d", transition.ID)

	notifyIDs, origin, found, err := model.EffectiveTaskRoute(w.DB, productionID, sourceTypeID)
	if err != nil {
		slog.Warn("handoff watcher: route lookup failed", "error_class", "handoff_route_lookup_failed", "task_id", sourceTaskID, "err", err)
		return 0, 0
	}
	base := model.NotificationDecision{
		ProductionID: productionID,
		TaskID:       sourceTaskID,
		TaskTypeName: sourceTypeName,
		EntityName:   source.Entity.Entity.Name,
		SignalKind:   "handoff",
		RefID:        episodeKey,
	}
	if !found || len(notifyIDs) == 0 {
		reason := "no_route"
		if found {
			reason = "route_none"
		}
		decision := base
		decision.RuleID = "handoff.no_route"
		decision.Outcome = model.DecisionOutcomeSuppressed
		decision.SuppressReason = reason
		decision.Facts = model.DecisionFactsJSON(map[string]interface{}{"origin": origin})
		_ = model.RecordNotificationDecision(w.DB, decision)
		// The first no-route per task type is a human judgment (fix C-3):
		// later episodes of the same type only count in the decision log.
		if !noRouteRaised[sourceTypeID] {
			noRouteRaised[sourceTypeID] = true
			_ = model.RaiseAdminException(w.DB, model.ExceptionNoRoute, sourceTypeID, sourceTypeName, productionID, "受け渡しの配線がありません（done遷移で検出）")
		}
		return 0, 1
	}

	var receivers []kitsu.MessagePayload
	for _, candidate := range entityPayloads {
		if candidate.Task.Task.ID == sourceTaskID {
			continue
		}
		if containsID(notifyIDs, candidate.TaskType.TaskType.ID) {
			receivers = append(receivers, candidate)
		}
	}
	if len(receivers) == 0 {
		decision := base
		decision.RuleID = "handoff.no_receiving_task"
		decision.Outcome = model.DecisionOutcomeSuppressed
		decision.SuppressReason = "no_receiving_task"
		decision.Facts = model.DecisionFactsJSON(map[string]interface{}{"origin": origin, "notify_task_type_ids": notifyIDs})
		_ = model.RecordNotificationDecision(w.DB, decision)
		return 0, 1
	}

	// The episode number counts this firing: a refire after a retake is the
	// second (or later) time the same source completed. The label itself is
	// recomputed at dispatch time (the event row does not store it).
	episodeNo := 1
	if count, err := model.CountHandoffEpisodes(w.DB, sourceTaskID); err == nil {
		episodeNo = count + 1
	}

	// The no-assignee handling comes from the Production's handoff config;
	// an unconfigured Production falls back to the admins (operating model).
	noAssignee := model.HandoffFallbackAdmin
	fallbackUserID := ""
	if handoffSettings, found, err := model.LoadProductionHandoffSettings(w.DB, productionID); err == nil && found {
		noAssignee = handoffSettings.Config.NoAssignee
		fallbackUserID = handoffSettings.Config.FallbackUserID
		if noAssignee == "" {
			noAssignee = model.HandoffFallbackAdmin
		}
	}
	for _, receiver := range receivers {
		receiverTaskID := receiver.Task.Task.ID
		// A receiver that already finished does not need the word (fix C-4).
		if planner.NatureForStatus(receiver.TaskStatus.TaskStatus.ID) == model.StatusNatureDone {
			decision := base
			decision.RuleID = "handoff.status_route"
			decision.Outcome = model.DecisionOutcomeSuppressed
			decision.SuppressReason = "receiver_closed"
			decision.Facts = model.DecisionFactsJSON(map[string]interface{}{"origin": origin, "receiver_task_id": receiverTaskID})
			_ = model.RecordNotificationDecision(w.DB, decision)
			suppressed++
			continue
		}
		event := model.HandoffEvent{
			EpisodeKey:         episodeKey,
			ProductionID:       productionID,
			SourceTaskID:       sourceTaskID,
			SourceTaskTypeID:   sourceTypeID,
			SourceTaskTypeName: sourceTypeName,
			SourceAuthorID:     source.LatestComment.Author.Person.ID,
			SourceAuthorName:   source.LatestComment.Author.Person.FullName,
			EntityID:           source.Entity.Entity.ID,
			EntityKind:         EntityKindFor(source.EntityType.EntityType.Name),
			EntityName:         source.Entity.Entity.Name,
			NotifyTaskID:       receiverTaskID,
			NotifyTaskTypeID:   receiver.TaskType.TaskType.ID,
			NotifyTaskTypeName: receiver.TaskType.TaskType.Name,
		}
		ids := assigneeIDs(receiver)
		switch {
		case len(ids) > 0:
			event.FallbackUsed = model.HandoffFallbackNone
		default:
			event.FallbackUsed = noAssignee
			switch noAssignee {
			case model.HandoffFallbackUsers:
				ids = model.SplitHandoffPersonIDs(fallbackUserID)
			case model.HandoffFallbackDisabled:
				event.State = model.HandoffEventNoTarget
			}
		}
		event.NotifyPersonIDs = model.JoinHandoffNotifyIDs(ids)
		existing, err := model.RecordHandoffEventForEpisode(w.DB, event)
		if err != nil {
			slog.Warn("handoff watcher: event record failed", "error_class", "handoff_event_record_failed", "task_id", sourceTaskID, "err", err)
			continue
		}
		if existing {
			continue
		}
		fired++
		decision := base
		decision.RuleID = "handoff.status_route"
		decision.Recipients = model.DecisionRecipientsJSON(ids)
		decision.Facts = model.DecisionFactsJSON(map[string]interface{}{
			"origin":           origin,
			"receiver_task_id": receiverTaskID,
			"episode":          episodeNo,
			"fallback_used":    event.FallbackUsed,
		})
		if event.State == model.HandoffEventNoTarget {
			decision.Outcome = model.DecisionOutcomeSuppressed
			decision.SuppressReason = "no_recipient"
		} else if event.FallbackUsed != "" && event.FallbackUsed != model.HandoffFallbackNone {
			decision.RuleID = "handoff.fallback_digest"
			decision.Outcome = model.DecisionOutcomeDigest
		} else {
			decision.Outcome = model.DecisionOutcomeQueued
		}
		_ = model.RecordNotificationDecision(w.DB, decision)
		// Fix C-8: when the same person is both the finisher (named by the
		// source's DONE signal) and a receiver, the DONE signal drops its
		// mention; the handoff signal keeps it.
		w.dropDoneMentionForSharedPerson(source, ids)
	}
	return fired, suppressed
}

// dropDoneMentionForSharedPerson rewrites the source task's still-pending
// DONE signal so it no longer pings a person the handoff signal names.
func (w HandoffWatcher) dropDoneMentionForSharedPerson(source kitsu.MessagePayload, receiverRecipients []string) {
	if len(receiverRecipients) == 0 {
		return
	}
	sourceAssignees := assigneeIDs(source)
	shared := false
	for _, id := range receiverRecipients {
		if containsID(sourceAssignees, id) {
			shared = true
			break
		}
	}
	if !shared {
		return
	}
	var row model.ForumOutbox
	err := w.DB.Where("task_id = ? AND coalesce_key = ? AND state = ?", source.Task.Task.ID, model.ForumStatusCoalesceKey(source.Task.Task.ID), model.ForumOutboxPending).
		Order("id desc").First(&row).Error
	if err != nil {
		return
	}
	var payload model.ForumPostMessagePayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return
	}
	if payload.Intent != model.ForumIntentStatusDone || payload.SuppressMentions {
		return
	}
	payload.SuppressMentions = true
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = w.DB.Model(&model.ForumOutbox{}).Where("id = ?", row.ID).Update("payload", string(encoded)).Error
}

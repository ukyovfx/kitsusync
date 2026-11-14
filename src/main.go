package main

import (
	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/setup"
	"app/src/utils/config"
	logutil "app/src/utils/log"
	"app/src/utils/safego"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	stdslog "log/slog"
	"net/http"
	"path/filepath"

	"log"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/beefsack/go-rate"
	"github.com/gookit/slog"
	"github.com/gookit/slog/handler"

	"github.com/pieterclaerhout/go-waitgroup"
	"github.com/robfig/cron/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func MakeKitsuResponse(conf config.Config) ([]kitsu.MessagePayload, error) {
	pollFetchStart := time.Now()
	taskCommentReads.Store(0)

	projects, err := kitsu.GetProjectsWithError()
	if err != nil {
		return nil, fmt.Errorf("fetch projects: %w", err)
	}
	if conf.Log {
		slog.Info("Got projects: " + strconv.Itoa(len(projects.Each)))
	}

	// Tasks are read per project (they carry assignees there). A project whose
	// read fails is skipped for this poll only: its tasks are simply absent, so
	// nothing is diffed or notified for it. Only when every project fails is the
	// poll itself an error.
	var tasks kitsu.Tasks
	failedProjects := make(map[string]struct{})
	tasksFetchStart := time.Now()
	fetched := fetchProjectTasks(projects.Each, conf.Threads)
	var slowestProject time.Duration
	for i, result := range fetched {
		if result.elapsed > slowestProject {
			slowestProject = result.elapsed
		}
		if result.err != nil {
			failedProjects[projects.Each[i].ID] = struct{}{}
			slog.Warn("Project tasks fetch failed; skipping this project for this poll", "error_class", "kitsu_project_tasks_fetch_failed", "projectID", projects.Each[i].ID, "error", result.err)
			continue
		}
		tasks.Each = append(tasks.Each, result.tasks...)
	}
	if len(projects.Each) > 0 && len(failedProjects) == len(projects.Each) {
		return nil, fmt.Errorf("fetch tasks: all %d projects failed", len(projects.Each))
	}
	lastPollFailedProjects = failedProjects
	tasksFetchElapsed := time.Since(tasksFetchStart)
	if conf.Log {
		slog.Info("Got tasks: " + strconv.Itoa(len(tasks.Each)))
	}

	taskStatuses, err := kitsu.GetTaskStatusesWithError()
	if err != nil {
		return nil, fmt.Errorf("fetch task statuses: %w", err)
	}
	if conf.Log {
		slog.Info("Got taskStatuses: " + strconv.Itoa(len(taskStatuses.Each)))
	}

	entities, err := kitsu.GetEntitiesWithError()
	if err != nil {
		return nil, fmt.Errorf("fetch entities: %w", err)
	}
	if conf.Log {
		slog.Info("Got entities: " + strconv.Itoa(len(entities.Each)))
	}

	enitityTypes, err := kitsu.GetEntityTypesWithError()
	if err != nil {
		return nil, fmt.Errorf("fetch entity types: %w", err)
	}
	if conf.Log {
		slog.Info("Got enitityTypes: " + strconv.Itoa(len(enitityTypes.Each)))
	}

	taskTypes, err := kitsu.GetTaskTypesWithError()
	if err != nil {
		return nil, fmt.Errorf("fetch task types: %w", err)
	}
	if conf.Log {
		slog.Info("Got taskTypes: " + strconv.Itoa(len(taskTypes.Each)))
	}

	// Raw per-production task counts, before any age filtering. This is the
	// bot-visibility signal for the per-production health view and the
	// open-production liveness monitor.
	lastPollProductionTaskCounts = make(map[string]int, len(taskTypes.Each))
	for _, task := range tasks.Each {
		if id := strings.TrimSpace(task.ProjectID); id != "" {
			lastPollProductionTaskCounts[id]++
		}
	}

	persons, err := kitsu.GetPersonsWithError()
	if err != nil {
		return nil, fmt.Errorf("fetch persons: %w", err)
	}
	if conf.Log {
		slog.Info("Got persons: " + strconv.Itoa(len(persons.Each)))
	}

	// Comments are read per task, and only for tasks whose last_comment_date
	// moved since the previous poll (the comments LIST endpoint is admin only on
	// Zou 1.1x). The cache is rebuilt each poll so it only holds tasks still seen.
	connectionKey := kitsu.ConnectionKey()
	previousComments := snapshotTaskCommentCache(connectionKey)
	nextComments := make(map[string]cachedTaskComments, len(tasks.Each))
	var nextCommentsMu sync.Mutex

	start := time.Now()

	response := make([]kitsu.MessagePayload, len(tasks.Each))

	wg := waitgroup.NewWaitGroup(conf.Threads)

	for i := 0; i < len(response); i++ {
		wg.BlockAdd()
		go func(i int) {
			defer wg.Done()

			layout := "2006-01-02T15:04:05"
			taskDate, err := time.Parse(layout, tasks.Each[i].UpdatedAt)
			if err != nil {
				slog.Info(err)
			}
			daysCount := int(start.Sub(taskDate).Hours() / 24)

			if conf.IgnoreMessagesDaysOld != 0 && daysCount > conf.IgnoreMessagesDaysOld {
				return
			}

			response[i].Task.Task = tasks.Each[i]

			for _, elem := range taskStatuses.Each {
				if elem.ID == tasks.Each[i].TaskStatusID {
					response[i].TaskStatus.TaskStatus = elem
					break
				}
			}

			for _, elem := range entities.Each {
				if elem.ID == tasks.Each[i].EntityID {
					response[i].Entity.Entity = elem
					break
				}
			}

			for _, elem := range enitityTypes.Each {
				if elem.ID == response[i].Entity.Entity.EntityTypeID {
					response[i].EntityType.EntityType = elem
					break
				}
			}

			for _, elem := range entities.Each {
				if elem.ID == response[i].Entity.Entity.ParentID {
					response[i].Parent.Entity = elem
				}
			}

			for _, elem := range projects.Each {
				if elem.ID == response[i].Entity.Entity.ProjectID {
					response[i].Project.Project = elem
					break
				}
			}

			for _, elem := range taskTypes.Each {
				if elem.ID == tasks.Each[i].TaskTypeID {
					response[i].TaskType.TaskType = elem
					break
				}
			}

			if conf.Kitsu.SkipComments == false {
				taskComments, commentsOK := loadTaskComments(tasks.Each[i], previousComments, nextComments, &nextCommentsMu)
				if !commentsOK {
					// Keep the stored comment checkpoint; the next poll retries.
					response[i].CommentsUnavailable = true
				}

				if len(taskComments.Each) > 0 {
					sort.Slice(taskComments.Each, func(i, j int) bool {
						layout := "2006-01-02T15:04:05"
						a, err := time.Parse(layout, taskComments.Each[i].UpdatedAt)
						if err != nil {
							slog.Info(err)
						}
						b, err := time.Parse(layout, taskComments.Each[j].UpdatedAt)
						if err != nil {
							slog.Info(err)
						}
						return a.Unix() > b.Unix()
					})

					response[i].LatestComment.Comment.Comment = taskComments.Each[0]
				}

				if statusComment, ok := statusChangeComment(tasks.Each[i], taskComments.Each); ok {
					for _, elem := range persons.Each {
						if elem.ID == statusComment.PersonID {
							response[i].StatusChangeAuthor.Person = elem
							break
						}
					}
				}

				for _, elem := range persons.Each {
					if len(taskComments.Each) > 0 {
						if elem.ID == taskComments.Each[0].PersonID {
							response[i].LatestComment.Author.Person = elem
							break
						}
					}
				}
			}

			if len(tasks.Each[i].Assignees) > 0 {
				for _, assigneeID := range tasks.Each[i].Assignees {
					for _, person := range persons.Each {
						if assigneeID == person.ID {
							response[i].Assignees = append(response[i].Assignees, person)
						}
					}
				}
			}

		}(i)
	}
	wg.Wait()

	if conf.Log {
		log.Printf("Done primary loop in %s", time.Since(start))
	}

	var out []kitsu.MessagePayload
	for _, elem := range response {
		if len(elem.Task.Task.ID) > 0 {
			out = append(out, elem)
		}
	}
	replaceTaskCommentCache(connectionKey, nextComments)
	// One line per poll: the data for choosing the polling interval. Counts and
	// durations only; no Kitsu content.
	slog.Info("Kitsu fetch timing",
		"event", "kitsu_fetch_timing",
		"projects", len(projects.Each),
		"failedProjects", len(failedProjects),
		"tasks", len(tasks.Each),
		"tasksFetchMs", tasksFetchElapsed.Milliseconds(),
		"slowestProjectMs", slowestProject.Milliseconds(),
		"commentReads", taskCommentReads.Load(),
		"totalMs", time.Since(pollFetchStart).Milliseconds())

	if conf.Log {
		log.Printf("Done secondary loop in %s", time.Since(start))
	}

	return out, nil
}

// statusChangeComment returns only a comment that Zou deterministically ties
// to the task's current status transition. A merely recent comment is not
// sufficient evidence of who changed the status.
func statusChangeComment(task kitsu.Task, comments []kitsu.Comment) (kitsu.Comment, bool) {
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.TaskStatusID) == "" || strings.TrimSpace(task.LastCommentDate) == "" {
		return kitsu.Comment{}, false
	}
	for _, comment := range comments {
		if strings.TrimSpace(comment.ObjectID) != strings.TrimSpace(task.ID) || strings.TrimSpace(comment.TaskStatusID) != strings.TrimSpace(task.TaskStatusID) || strings.TrimSpace(comment.PersonID) == "" {
			continue
		}
		if timestampsEqual(comment.CreatedAt, task.LastCommentDate) || timestampsEqual(comment.UpdatedAt, task.LastCommentDate) {
			return comment, true
		}
	}
	return kitsu.Comment{}, false
}

func timestampsEqual(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05"} {
		a, aErr := time.Parse(layout, left)
		b, bErr := time.Parse(layout, right)
		if aErr == nil && bErr == nil && a.Equal(b) {
			return true
		}
	}
	return false
}

type notificationRouteStats struct {
	DBRouted int
	Dropped  int
}

func overallNotificationReadiness(kitsuReady, botConfigured, discordValidated, routingReady bool) string {
	if kitsuReady && botConfigured && discordValidated && routingReady {
		return "ready"
	}
	if kitsuReady && botConfigured && routingReady {
		return "ready_pending_discord_validation"
	}
	return "blocked"
}

func previewTasks(tasks []kitsu.MessagePayload, limit int) []string {
	if limit <= 0 || len(tasks) == 0 {
		return nil
	}
	if limit > len(tasks) {
		limit = len(tasks)
	}
	preview := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		task := tasks[i]
		preview = append(preview, fmt.Sprintf("%s|%s|%s|%s|%s",
			task.Task.ID,
			task.Project.Name,
			task.Entity.Name,
			task.TaskType.TaskType.Name,
			task.TaskStatus.TaskStatus.ShortName,
		))
	}
	return preview
}

func labelsFromSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	labels := make([]string, 0, len(set))
	for label := range set {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

func logRouteDispatch(routeSource string, routeLabels []string, tasks []kitsu.MessagePayload, webhookConfigured bool) {
	logger := slog.Info
	message := "Notification route dispatch"
	if !webhookConfigured {
		logger = slog.Warn
		message = "Notification route has no webhook configured; tasks will not be sent"
	}
	logger(message,
		"routeSource", routeSource,
		"routeLabels", routeLabels,
		"taskCount", len(tasks),
		"taskPreview", previewTasks(tasks, 5))
}

func logDroppedTasks(reason string, tasks []kitsu.MessagePayload) {
	slog.Warn("Notification dropped",
		"reason", reason,
		"taskCount", len(tasks),
		"taskPreview", previewTasks(tasks, 5))
}

func FilterTasks(data []kitsu.MessagePayload, conf config.Config, db *gorm.DB) {
	if len(data) == 0 {
		if conf.Log {
			fmt.Printf("Nothing to do\n")
		}
	}

	var filtered []kitsu.MessagePayload
	for i := 0; i < len(data); i++ {
		dbResult, findErr := model.FindTaskWithError(db, data[i].Task.ID)
		if findErr != nil {
			slog.Error("Notification state read failed; event not dispatched", "error_class", "delivery_checkpoint_read_failed")
			continue
		}

		currentStatus := data[i].TaskStatus.TaskStatus.ShortName
		data[i].PreviousStatusName = model.PreviousTaskStatus(dbResult, currentStatus)

		if len(dbResult.TaskID) > 0 {
			deliveredStatus := dbResult.DeliveredTaskStatus
			deliveredTaskUpdatedAt := dbResult.DeliveredTaskUpdatedAt
			deliveredCommentID := dbResult.DeliveredCommentID
			deliveredCommentUpdatedAt := dbResult.DeliveredCommentUpdatedAt
			if deliveredStatus == "" {
				// Old records have only one checkpoint snapshot. Preserve it as a
				// historical baseline; it is never replayed merely because the new
				// additive journal has no row for it.
				deliveredStatus, deliveredTaskUpdatedAt = dbResult.TaskStatus, dbResult.TaskUpdatedAt
				deliveredCommentID, deliveredCommentUpdatedAt = dbResult.CommentID, dbResult.CommentUpdatedAt
			}
			statusChanged := deliveredStatus != currentStatus
			timestampChanged := deliveredTaskUpdatedAt != data[i].Task.Task.UpdatedAt
			commentChanged := deliveredCommentID != data[i].LatestComment.Comment.ID || deliveredCommentUpdatedAt != data[i].LatestComment.Comment.UpdatedAt

			if data[i].CommentsUnavailable {
				// Comments could not be fetched this poll (or were skipped):
				// never diff against empty comment data and keep the stored
				// checkpoint so the next successful poll resumes the real delta.
				commentChanged = false
				data[i].LatestComment.Comment.ID = deliveredCommentID
				data[i].LatestComment.Comment.UpdatedAt = deliveredCommentUpdatedAt
			}

			if statusChanged || timestampChanged || commentChanged {
				// Channel-as-queue: when a task leaves WFA, its waiting-for-check
				// post is deleted. The transition must come from the current
				// Kitsu snapshot, and the initial-checkpoint guard keeps
				// pre-existing tasks silent.
				if strings.EqualFold(deliveredStatus, "wfa") && !strings.EqualFold(currentStatus, "wfa") && !setup.InitialCheckpointPending(data[i].Project.ID) {
					if lifecycleEvent, lifecycleErr := model.FindLatestDeliveredChannelEventForTask(db, data[i].Task.ID, data[i].Project.ID); lifecycleErr != nil {
						slog.Error("Lifecycle cleanup lookup failed", "error_class", "lifecycle_cleanup_lookup_failed", "taskID", data[i].Task.ID)
					} else if lifecycleEvent != nil {
						if markErr := model.MarkNotificationLifecycleCleanupPending(db, lifecycleEvent.EventID); markErr != nil {
							slog.Error("Lifecycle cleanup could not be marked", "error_class", "lifecycle_cleanup_mark_failed", "taskID", data[i].Task.ID)
						}
					} else if memberEvent, memberErr := model.FindSummaryMemberEventForTask(db, data[i].Task.ID, data[i].Project.ID); memberErr != nil {
						slog.Error("Summary membership lookup failed", "error_class", "lifecycle_cleanup_lookup_failed", "taskID", data[i].Task.ID)
					} else if memberEvent != nil {
						// The task was delivered inside a bulk summary: its WFA
						// entry must leave the summary, so the summary itself
						// is marked for reconciliation.
						if markErr := model.MarkNotificationLifecycleCleanupPending(db, memberEvent.EventID); markErr != nil {
							slog.Error("Summary lifecycle cleanup could not be marked", "error_class", "lifecycle_cleanup_mark_failed", "taskID", data[i].Task.ID)
						}
					}
				}
				observed := model.NotificationDeliveryIntent{
					TaskID: data[i].Task.ID, ProductionID: data[i].Project.ID, TaskTypeID: data[i].TaskType.ID,
					TaskStatus: currentStatus, TaskUpdatedAt: data[i].Task.Task.UpdatedAt,
					CommentID: data[i].LatestComment.Comment.ID, CommentUpdatedAt: data[i].LatestComment.Comment.UpdatedAt,
					IsAssignNotification: strings.EqualFold(currentStatus, "none"),
				}
				if _, supersedeErr := model.SupersedeObsoleteKnownRejectedNotificationDeliveries(db, observed); supersedeErr != nil {
					slog.Error("Older known-rejected notification could not be reconciled against the current Kitsu event", "error_class", "delivery_supersession_failed")
					continue
				}
				// Mark notifications that only changed comment content. Persisting
				// the observation is deferred until routing and delivery are known.
				data[i].IsCommentOnly = commentChanged && !statusChanged && !timestampChanged
				// Mention DMs fire when a NEW comment arrives; the observed
				// comment checkpoint deduplicates re-offers until observation
				// advances below. Comment-read degradation is recorded durably.
				if data[i].CommentsUnavailable {
					recordCommentReadCapability(db, false)
				} else {
					recordCommentReadCapability(db, true)
					if commentChanged && data[i].LatestComment.Comment.ID != dbResult.CommentID && !conf.SilentUpdateDB {
						dispatchMentionDMs(db, data[i])
					}
				}
			} else {
				// An unfinished durable intent is still eligible even when the
				// Kitsu event matches the last observed snapshot. The check is
				// lane-agnostic: a DM-lane row carries a different event ID
				// than the content-only channel identity.
				unresolved, unresolvedErr := model.HasUnresolvedNotificationDeliveryForTask(db, data[i].Task.ID)
				if unresolvedErr != nil {
					slog.Error("Notification delivery state read failed; event not dispatched", "error_class", "delivery_journal_read_failed")
					continue
				}
				if !unresolved {
					continue
				}
			}
		}

		if conf.SilentUpdateDB {
			if conf.Log {
				log.Printf("Ignoring message\n")
			}
			if err := persistTaskObservation(db, data[i]); err != nil {
				slog.Error("Task observation could not be persisted", "error_class", "task_observation_write_failed")
			}
			continue
		}
		// StatusFilter: only notify on WFA, RETAKE, DONE.
		// Treat "none" status as an assign notification when enabled.
		if strings.EqualFold(currentStatus, "none") {
			if !conf.Notification.NotifyOnAssign {
				if err := persistTaskObservation(db, data[i]); err != nil {
					slog.Error("Task observation could not be persisted", "error_class", "task_observation_write_failed")
				}
				continue
			}
			data[i].IsAssignNotification = true
		} else if !isNotifiableStatus(currentStatus) {
			persistTaskObservation(db, data[i])
			continue
		}
		filtered = append(filtered, data[i])
	}

	// The explicit Production-ID router is the only dispatch path for task
	// notifications. Legacy name/global fallbacks are intentionally excluded:
	// an unconfigured Production must fail closed.
	stats := notificationRouteStats{}
	routes := make(map[string][]kitsu.MessagePayload)
	webhooks := make(map[string]string)
	labels := make(map[string]map[string]struct{})
	for _, payload := range filtered {
		plan := planProductionNotification(db, payload)
		if plan.LaneType == model.LaneRecordOnly {
			// The initial-checkpoint guard comes first: a production whose
			// first checkpoint has not completed must stay silent, and its
			// pre-existing tasks must not pollute the delivery ledger.
			if setup.InitialCheckpointPending(payload.Project.ID) {
				if err := persistTaskObservation(db, payload); err != nil {
					slog.Error("Task observation could not be persisted", "error_class", "task_observation_write_failed")
				}
				stats.Dropped++
				continue
			}
			// Record-only lane: write the delivery ledger entry without any
			// network send and keep the observation checkpoint current.
			if err := model.RecordNotificationOnlyDelivery(db, payload.Task.ID, payload.Project.ID, payload.TaskType.ID, strings.ToUpper(payload.TaskStatus.TaskStatus.ShortName), payload.Task.UpdatedAt, payload.LatestComment.Comment.ID, payload.LatestComment.Comment.UpdatedAt, payload.IsAssignNotification); err != nil {
				slog.Error("Record-only delivery could not be persisted", "error_class", "record_only_persist_failed", "taskID", payload.Task.ID)
			}
			if err := persistTaskObservation(db, payload); err != nil {
				slog.Error("Task observation could not be persisted", "error_class", "task_observation_write_failed")
			}
			stats.Dropped++
			continue
		}
		if !plan.ShouldSend {
			recordProductionRoutingSkip(db, payload, plan.SkipReason)
			stats.Dropped++
			continue
		}
		// Persist the observed Kitsu sequence only after the event has an
		// explicit route. Delivery state remains separate so transient failures
		// retry with the same real transition, while unrouted events stay
		// available if a route is configured later.
		if err := model.ObserveTaskStatus(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.TaskStatus.ShortName, payload.LatestComment.Comment.ID, payload.LatestComment.Comment.UpdatedAt, payload.Project.ID, payload.Entity.Name); err != nil {
			slog.Error("Task observation could not be persisted; event not dispatched", "error_class", "task_observation_write_failed")
			continue
		}
		// Channel-lane deliveries go through the aggregation window so rapid
		// round-trips and initial bulk collapse into few posts; DM approvals
		// stay immediate. The initial-checkpoint guard keeps pre-existing
		// tasks from ever entering the buffer.
		if plan.LaneType == model.LaneChannel {
			if setup.InitialCheckpointPending(payload.Project.ID) {
				stats.Dropped++
				continue
			}
			buffered, bufferErr := bufferChannelTransition(db, payload, plan)
			if bufferErr != nil {
				slog.Error("Aggregation buffer write failed; event not dispatched", "error_class", "aggregation_buffer_write_failed", "taskID", payload.Task.ID)
				continue
			}
			if buffered {
				stats.DBRouted++
			}
			continue
		}
		webhooks[plan.DestinationID] = plan.WebhookURL
		routes[plan.DestinationID] = append(routes[plan.DestinationID], payload)
		if labels[plan.DestinationID] == nil {
			labels[plan.DestinationID] = make(map[string]struct{})
		}
		labels[plan.DestinationID][fmt.Sprintf("productionID=%s taskTypeID=%s", payload.Project.ID, payload.TaskType.ID)] = struct{}{}
	}
	// A production whose initial checkpoint has not finished yet must stay
	// silent: its tasks are pre-existing work, not new transitions.
	for destinationID, payloads := range routes {
		if setup.InitialCheckpointPending(payloads[0].Project.ID) {
			delete(routes, destinationID)
			delete(webhooks, destinationID)
			stats.Dropped += len(payloads)
			slog.Info("Notification suppressed until initial checkpoint completes", "productionID", payloads[0].Project.ID, "taskCount", len(payloads))
		}
	}
	for destinationID, payloads := range routes {
		routeLabels := labelsFromSet(labels[destinationID])
		stats.DBRouted += len(payloads)
		logRouteDispatch("production.task_type_id", routeLabels, payloads, webhooks[destinationID] != "")
		notificationDispatch(payloads, conf, webhooks[destinationID], db, "production.task_type_id", routeLabels)
	}
	// Close aggregation windows that expired while routing ran (tests set the
	// window to 0 so buffered transitions dispatch immediately).
	dispatchDueAggregatedNotifications(conf, db)

	if len(data) > 0 && (stats.DBRouted > 0 || stats.Dropped > 0) {
		slog.Info("Notification routing summary",
			"incomingTasks", len(data),
			"dbRouted", stats.DBRouted,
			"dropped", stats.Dropped)
	}
}

// bufferChannelTransition stores a channel-lane transition in its aggregation
// window. It reports whether the buffer accepted a new transition (re-observing
// the same transition before the window closes is a no-op).
func bufferChannelTransition(db *gorm.DB, payload kitsu.MessagePayload, plan productionRoutingPlan) (bool, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	status := strings.ToUpper(strings.TrimSpace(payload.TaskStatus.TaskStatus.ShortName))
	identity := model.NotificationEventIdentity(model.NotificationDeliveryIntent{
		TaskID: payload.Task.ID, ProductionID: payload.Project.ID, TaskTypeID: payload.TaskType.ID,
		TaskStatus: status, TaskUpdatedAt: payload.Task.UpdatedAt,
		CommentID: payload.LatestComment.Comment.ID, CommentUpdatedAt: payload.LatestComment.Comment.UpdatedAt,
		IsAssignNotification: payload.IsAssignNotification,
	})
	return model.BufferNotificationTransition(db, model.NotificationAggregationBuffer{
		TaskID: payload.Task.ID, ProductionID: payload.Project.ID, TaskTypeID: payload.TaskType.ID,
		DestinationID: plan.DestinationID, LaneType: model.LaneChannel, WebhookURL: plan.WebhookURL,
		LastStatus: status, LastPayloadJSON: string(payloadJSON), LastIdentity: identity,
	})
}

// channelAggregationWindow is the task×destination window; tests set it to 0
// for immediate dispatch.
var channelAggregationWindow = model.NotificationAggregationWindow

// dispatchDueAggregatedNotifications closes expired aggregation windows.
// Flap-downgraded tasks are consumed into the ledger without a send; bulk
// groups send one summary message per destination; everything else dispatches
// per task through the normal durable path. Buffer rows are only removed once
// the delivery ledger owns the outcome, so a crash never loses a notification.
func dispatchDueAggregatedNotifications(conf config.Config, db *gorm.DB) {
	if db == nil || conf.SilentUpdateDB {
		return
	}
	grouped, err := model.TakeDueAggregatedNotifications(db, time.Now().UTC(), channelAggregationWindow)
	if err != nil {
		slog.Error("Aggregation window read failed", "error_class", "aggregation_read_failed")
		return
	}
	for destinationID, buffers := range grouped {
		var normal []model.NotificationAggregationBuffer
		for _, buffer := range buffers {
			if buffer.FlapDowngraded {
				consumeAggregatedBuffer(db, buffer, "flap_downgraded_to_digest", "", 0)
				continue
			}
			if aggregatedTransitionSuperseded(db, buffer) {
				// The task already left the buffered state before the window
				// closed (e.g. resolved into a non-notifiable status): the
				// stale WFA post would be deleted by the next poll anyway.
				consumeAggregatedBuffer(db, buffer, "aggregation_transition_superseded", "", 0)
				continue
			}
			normal = append(normal, buffer)
		}
		if len(normal) == 0 {
			continue
		}
		if len(normal) > model.NotificationBulkSummaryThreshold {
			dispatchSummaryGroup(db, destinationID, normal)
			continue
		}
		var payloads []kitsu.MessagePayload
		var consumed []uint
		labelSet := make(map[string]struct{})
		for _, buffer := range normal {
			var payload kitsu.MessagePayload
			if err := json.Unmarshal([]byte(buffer.LastPayloadJSON), &payload); err != nil {
				// An undecodable buffer can never be delivered; consume it so
				// it cannot wedge the window.
				consumeAggregatedBuffer(db, buffer, "aggregation_payload_undecodable", "", 0)
				continue
			}
			payloads = append(payloads, payload)
			consumed = append(consumed, buffer.ID)
			labelSet[fmt.Sprintf("productionID=%s taskTypeID=%s", payload.Project.ID, payload.TaskType.ID)] = struct{}{}
		}
		if len(payloads) == 0 {
			continue
		}
		routeLabels := labelsFromSet(labelSet)
		logRouteDispatch("aggregation.window", routeLabels, payloads, webhookURLFor(destinationID, normal) != "")
		notificationDispatch(payloads, conf, webhookURLFor(destinationID, normal), db, "aggregation.window", routeLabels)
		if err := model.DeleteAggregationBuffers(db, consumed); err != nil {
			slog.Error("Aggregation buffer cleanup failed", "error_class", "aggregation_buffer_cleanup_failed")
		}
	}
}

// webhookURLFor returns the destination's current webhook URL from the buffers
// (the dispatch path re-resolves destinations itself).
func webhookURLFor(destinationID string, buffers []model.NotificationAggregationBuffer) string {
	for _, buffer := range buffers {
		if buffer.DestinationID == destinationID && strings.TrimSpace(buffer.WebhookURL) != "" {
			return buffer.WebhookURL
		}
	}
	return ""
}

// aggregatedTransitionSuperseded reports whether the task's current observed
// status already differs from the buffered transition, which means the
// buffered state is no longer the task's state.
func aggregatedTransitionSuperseded(db *gorm.DB, buffer model.NotificationAggregationBuffer) bool {
	task, err := model.FindTaskWithError(db, buffer.TaskID)
	if err != nil {
		return false
	}
	current := strings.ToLower(strings.TrimSpace(task.LastObservedStatus))
	if current == "" {
		current = strings.ToLower(strings.TrimSpace(task.TaskStatus))
	}
	buffered := strings.ToLower(strings.TrimSpace(buffer.LastStatus))
	return current != "" && current != buffered
}

// consumeAggregatedBuffer records an aggregated transition in the ledger and
// advances the delivered checkpoint so later polls never re-offer it. For
// bulk-summary members the summary message ID and webhook row are recorded so
// the summary lifecycle can reconcile the message later.
func consumeAggregatedBuffer(db *gorm.DB, buffer model.NotificationAggregationBuffer, reason, summaryMessageID string, webhookRowID uint) {
	var payload kitsu.MessagePayload
	if err := json.Unmarshal([]byte(buffer.LastPayloadJSON), &payload); err != nil {
		// Without a decodable payload the checkpoint cannot advance; deleting
		// the row still unblocks the window and the audit log records the loss.
		slog.Error("Aggregated payload could not be decoded; buffer consumed without checkpoint", "error_class", reason, "taskID", buffer.TaskID)
		_ = model.DeleteAggregationBuffers(db, []uint{buffer.ID})
		return
	}
	if err := model.RecordAggregatedNotificationConsumption(db, payload.Task.ID, payload.Project.ID, payload.TaskType.ID,
		strings.ToUpper(strings.TrimSpace(payload.TaskStatus.TaskStatus.ShortName)), payload.Task.UpdatedAt,
		payload.LatestComment.Comment.ID, payload.LatestComment.Comment.UpdatedAt, payload.IsAssignNotification, reason, summaryMessageID, webhookRowID); err != nil {
		slog.Error("Aggregated consumption could not be recorded", "error_class", "aggregation_consume_failed", "taskID", buffer.TaskID)
		return
	}
	if err := model.MarkNotificationDeliveredCheckpoint(db, payload.Task.ID, payload.Task.UpdatedAt,
		payload.TaskStatus.TaskStatus.ShortName, payload.LatestComment.Comment.ID, payload.LatestComment.Comment.UpdatedAt,
		payload.Project.ID, payload.Entity.Name); err != nil {
		slog.Error("Aggregated delivered checkpoint could not be advanced", "error_class", "aggregation_consume_failed", "taskID", buffer.TaskID)
		return
	}
	_ = model.DeleteAggregationBuffers(db, []uint{buffer.ID})
}

// summaryMember is one task's live entry in an aggregated summary body.
type summaryMember struct {
	Name   string
	Status string
	TaskID string
}

// aggregatedSummaryContentLimit keeps the assembled body inside Discord's
// 2000-character message limit with headroom for the trailing notice.
const aggregatedSummaryContentLimit = 1900

// buildAggregatedSummaryBody renders the bulk-summary text from the members'
// current states: per-status counts plus up to 20 previews, truncated to the
// Discord content limit. Kitsu-originated names are sanitized so the body can
// never carry live mentions or markup.
func buildAggregatedSummaryBody(destinationID string, members []summaryMember) string {
	counts := map[string]int{}
	for _, member := range members {
		counts[member.Status]++
	}
	var breakdown []string
	for _, status := range []string{"WFA", "RETAKE", "DONE"} {
		if counts[status] > 0 {
			breakdown = append(breakdown, fmt.Sprintf("%s: %d", status, counts[status]))
		}
	}
	header := fmt.Sprintf("📢 %s — %d 件の更新を集約しました (%s)", destinationID, len(members), strings.Join(breakdown, ", "))
	previews := memberPreviews(members)
	content := header
	if len(previews) > 0 {
		content += "\n" + strings.Join(previews, "\n")
	}
	if len(content) <= aggregatedSummaryContentLimit {
		return content
	}
	// Length cap: drop previews from the tail until the body fits.
	for len(previews) > 0 {
		previews = previews[:len(previews)-1]
		content = header
		if len(previews) > 0 {
			content += "\n" + strings.Join(previews, "\n")
		}
		content += fmt.Sprintf("\n…他 %d 件", len(members)-len(previews))
		if len(content) <= aggregatedSummaryContentLimit {
			return content
		}
	}
	return header + fmt.Sprintf("\n…他 %d 件", len(members))
}

func memberPreviews(members []summaryMember) []string {
	previews := make([]string, 0, len(members))
	for _, member := range members {
		if len(previews) >= 20 {
			break
		}
		name := discord.SanitizeText(member.Name)
		if strings.TrimSpace(name) == "" {
			name = member.TaskID
		}
		previews = append(previews, fmt.Sprintf("• %s (%s)", name, member.Status))
	}
	return previews
}

// summaryOutcome classifies a bulk-summary send. delivered/permanent/unknown
// consume the buffers (unknown keeps an explicit ledger trace for manual
// reconciliation instead of risking a double post); retryable keeps them for
// the next poll.
type summaryOutcome string

const (
	summaryDelivered  summaryOutcome = "delivered"
	summaryPermanent  summaryOutcome = "permanent"
	summaryUnknown    summaryOutcome = "unknown"
	summaryRetryable  summaryOutcome = "retryable"
	summaryUnresolved summaryOutcome = "unresolved"
)

// dispatchSummaryGroup sends one destination's bulk summary after re-resolving
// and verifying the destination webhook, then consumes the buffers according
// to the send outcome.
func dispatchSummaryGroup(db *gorm.DB, destinationID string, buffers []model.NotificationAggregationBuffer) {
	webhook, resolveOutcome := resolveSummaryWebhook(db, buffers[0].ProductionID, destinationID)
	switch resolveOutcome {
	case summaryUnresolved:
		// The destination no longer resolves (route removed or channel
		// deleted): record the transitions in the ledger and unblock the
		// window instead of buffering forever.
		for _, buffer := range buffers {
			consumeAggregatedBuffer(db, buffer, "aggregation_destination_unresolved", "", 0)
		}
		return
	case summaryRetryable:
		slog.Warn("Bulk summary destination verification failed; buffers kept", "error_class", "summary_destination_verification_failed", "destination", destinationID)
		return
	}
	outcome, messageID := sendAggregatedSummary(destinationID, webhook, buffers)
	switch outcome {
	case summaryRetryable:
		return
	case summaryUnknown:
		for _, buffer := range buffers {
			consumeAggregatedBuffer(db, buffer, "aggregated_bulk_unknown", "", 0)
		}
		return
	case summaryDelivered:
		for _, buffer := range buffers {
			consumeAggregatedBuffer(db, buffer, "aggregated_bulk_summary", messageID, webhook.ID)
		}
	default: // permanent rejection by Discord
		for _, buffer := range buffers {
			consumeAggregatedBuffer(db, buffer, "aggregated_bulk_rejected", "", 0)
		}
	}
}

// resolveSummaryWebhook re-resolves the destination's webhook fresh from the
// routing state instead of trusting the URL captured when the transition was
// buffered; this is the same destination verification the normal delivery
// path applies.
func resolveSummaryWebhook(db *gorm.DB, productionID, destinationID string) (model.ProjectWebhook, summaryOutcome) {
	var webhook model.ProjectWebhook
	err := db.Where("kitsu_project_id = ? AND discord_channel_id = ?", productionID, destinationID).First(&webhook).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return webhook, summaryUnresolved
	}
	if err != nil {
		return webhook, summaryRetryable
	}
	var project model.Project
	if err := db.Where("kitsu_project_id = ?", productionID).First(&project).Error; err != nil {
		return webhook, summaryRetryable
	}
	webhookID, idErr := discord.DiscordWebhookIDFromURL(webhook.WebhookURL)
	if idErr != nil {
		return webhook, summaryRetryable
	}
	if verifyErr := discord.VerifyWebhookIdentity(webhook.WebhookURL, webhookID, project.DiscordGuildID, webhook.DiscordChannelID); verifyErr != nil {
		return webhook, summaryRetryable
	}
	return webhook, ""
}

// sendAggregatedSummary sends one summary message per destination when a
// window closes with a bulk of transitions.
func sendAggregatedSummary(destinationID string, webhook model.ProjectWebhook, buffers []model.NotificationAggregationBuffer) (summaryOutcome, string) {
	members := make([]summaryMember, 0, len(buffers))
	for _, buffer := range buffers {
		var payload kitsu.MessagePayload
		if err := json.Unmarshal([]byte(buffer.LastPayloadJSON), &payload); err != nil {
			continue
		}
		name := payload.Entity.Name
		if strings.TrimSpace(name) == "" {
			name = payload.Task.ID
		}
		members = append(members, summaryMember{Name: name, Status: strings.ToUpper(strings.TrimSpace(buffer.LastStatus)), TaskID: payload.Task.ID})
	}
	content := buildAggregatedSummaryBody(destinationID, members)
	// parse:[] keeps Kitsu-originated text (task names, previews) from ever
	// pinging anyone through the summary.
	result := discord.SendMessage(discord.Payload{Content: content, AllowedMentions: &discord.AllowedMentions{Parse: []string{}}}, webhook.WebhookURL, "", "")
	if result.MessageID != "" {
		return summaryDelivered, result.MessageID
	}
	if result.Unknown {
		return summaryUnknown, ""
	}
	if result.Retryable {
		return summaryRetryable, ""
	}
	return summaryPermanent, ""
}

// reconcileAggregatedSummaries keeps bulk summaries aligned with the
// channel-as-queue lifecycle: when a WFA task inside a summary resolves, the
// summary is edited down to the still-waiting tasks and deleted once none
// remain. Members are the record-only ledger rows carrying the summary's
// message ID.
func reconcileAggregatedSummaries(db *gorm.DB) {
	if db == nil {
		return
	}
	var pending []model.NotificationDeliveryEvent
	if err := db.Where("lane_type = ? AND disposition = ? AND lifecycle_cleanup_pending = ? AND lifecycle_cleanup_in_flight = ? AND discord_message_id != ''",
		model.LaneRecordOnly, model.DeliveryRecorded, true, false).Find(&pending).Error; err != nil {
		slog.Error("Summary lifecycle read failed", "error_class", "summary_lifecycle_read_failed")
		return
	}
	if len(pending) == 0 {
		return
	}
	// Rebuild the FULL membership of each affected summary: the pending rows
	// only mark which summaries need reconciliation.
	messageIDs := make([]string, 0, len(pending))
	seenMessage := map[string]bool{}
	for _, member := range pending {
		if !seenMessage[member.DiscordMessageID] {
			seenMessage[member.DiscordMessageID] = true
			messageIDs = append(messageIDs, member.DiscordMessageID)
		}
	}
	var members []model.NotificationDeliveryEvent
	if err := db.Where("discord_message_id IN ? AND lane_type = ? AND disposition = ?", messageIDs, model.LaneRecordOnly, model.DeliveryRecorded).Find(&members).Error; err != nil {
		slog.Error("Summary membership read failed", "error_class", "summary_lifecycle_read_failed")
		return
	}
	byMessage := map[string][]model.NotificationDeliveryEvent{}
	order := append([]string(nil), messageIDs...)
	for _, member := range members {
		byMessage[member.DiscordMessageID] = append(byMessage[member.DiscordMessageID], member)
	}
	for _, messageID := range order {
		group := byMessage[messageID]
		webhook, ok := summaryLifecycleWebhook(db, group[0].WebhookRowID)
		if !ok {
			slog.Warn("Summary lifecycle webhook unavailable; retrying next poll", "error_class", "summary_lifecycle_webhook_missing")
			continue
		}
		waiting := 0
		liveMembers := make([]summaryMember, 0, len(group))
		for _, member := range group {
			status := taskObservedStatus(db, member.TaskID)
			if status == "" {
				status = strings.ToUpper(strings.TrimSpace(member.TaskStatus))
			}
			if strings.EqualFold(status, "wfa") {
				waiting++
			}
			liveMembers = append(liveMembers, summaryMember{Name: taskEntityName(db, member.TaskID), Status: status, TaskID: member.TaskID})
		}
		if waiting == 0 {
			if discord.DeleteMessage(webhook.WebhookURL, messageID, "") {
				_ = model.CompleteSummaryLifecycleCleanup(db, messageID, true, true, "")
			} else {
				_ = model.CompleteSummaryLifecycleCleanup(db, messageID, false, false, "summary_message_delete_failed")
			}
			continue
		}
		body := buildAggregatedSummaryBody(webhook.DiscordChannelID, liveMembers)
		if discord.EditMessage(webhook.WebhookURL, messageID, "", discord.Payload{Content: body, AllowedMentions: &discord.AllowedMentions{Parse: []string{}}}) {
			_ = model.CompleteSummaryLifecycleCleanup(db, messageID, true, false, "")
		} else {
			_ = model.CompleteSummaryLifecycleCleanup(db, messageID, false, false, "summary_message_edit_failed")
		}
	}
}

// summaryLifecycleWebhook loads and verifies the webhook row a summary was
// delivered through before editing or deleting the message.
func summaryLifecycleWebhook(db *gorm.DB, webhookRowID uint) (model.ProjectWebhook, bool) {
	webhook := model.FindProjectWebhookByID(db, webhookRowID)
	if webhook == nil {
		return model.ProjectWebhook{}, false
	}
	webhookID, idErr := discord.DiscordWebhookIDFromURL(webhook.WebhookURL)
	if idErr != nil {
		return model.ProjectWebhook{}, false
	}
	var project model.Project
	if err := db.Where("kitsu_project_id = ?", webhook.KitsuProjectID).First(&project).Error; err != nil {
		return model.ProjectWebhook{}, false
	}
	if verifyErr := discord.VerifyWebhookIdentity(webhook.WebhookURL, webhookID, project.DiscordGuildID, webhook.DiscordChannelID); verifyErr != nil {
		return model.ProjectWebhook{}, false
	}
	return *webhook, true
}

// taskObservedStatus returns the task's current observed status.
func taskObservedStatus(db *gorm.DB, taskID string) string {
	task, err := model.FindTaskWithError(db, taskID)
	if err != nil {
		return ""
	}
	status := strings.ToUpper(strings.TrimSpace(task.LastObservedStatus))
	if status == "" {
		status = strings.ToUpper(strings.TrimSpace(task.TaskStatus))
	}
	return status
}

// taskEntityName returns the display name recorded with the task observation,
// falling back to the task ID.
func taskEntityName(db *gorm.DB, taskID string) string {
	task, err := model.FindTaskWithError(db, taskID)
	if err != nil || strings.TrimSpace(task.EntityName) == "" {
		return taskID
	}
	return strings.TrimSpace(task.EntityName)
}

// lastDeliveryMetricsLine deduplicates the metrics log: the line is only
// emitted when the ledger shape changes.
var lastDeliveryMetricsLine string

// logDeliveryMetrics emits the ledger-derived delivery metrics (per lane,
// disposition and failure class plus aggregation state) when they change.
func logDeliveryMetrics(db *gorm.DB) {
	metrics, err := model.BuildNotificationDeliveryMetrics(db)
	if err != nil {
		return
	}
	line := fmt.Sprintf("lanes=%v failures=%v buffers=%d flapDowngraded=%d", metrics.LaneDisposition, metrics.FailureClasses, metrics.AggregationBuffers, metrics.FlapDowngradedBuffers)
	if line == lastDeliveryMetricsLine {
		return
	}
	lastDeliveryMetricsLine = line
	slog.Info("Notification delivery metrics", "lanes", metrics.LaneDisposition, "failures", metrics.FailureClasses, "aggregationBuffers", metrics.AggregationBuffers, "flapDowngraded", metrics.FlapDowngradedBuffers)
}

func isNotifiableStatus(status string) bool {
	// Only send notifications for these statuses
	notifiableStatuses := []string{"wfa", "retake", "done"}
	lowerStatus := strings.ToLower(status)
	for _, s := range notifiableStatuses {
		if lowerStatus == s {
			return true
		}
	}
	return false
}

func DiscordQueueSend(data []kitsu.MessagePayload, conf config.Config, webhookURL string, db *gorm.DB, routeSource string, routeLabels []string) []kitsu.MessagePayload {
	if webhookURL == "" {
		slog.Warn("Notification send skipped: webhook is empty",
			"routeSource", routeSource,
			"routeLabels", routeLabels,
			"taskCount", len(data),
			"taskPreview", previewTasks(data, 5))
		return data
	}

	rl := rate.New(conf.Discord.RequestsPerMinute, time.Minute)

	// Previous Discord messages are loaded from the durable destination-bound
	// delivery journal inside the sender. Legacy Task rows have no verified
	// destination identity and are not reused for edits or deletion.
	previousMessageIDs := make(map[string]string)
	previousWebhookURLs := make(map[string]string)
	previousThreadIDs := make(map[string]string)
	projectNotificationLanguages := make(map[string]string)
	for _, elem := range data {
		if _, ok := projectNotificationLanguages[elem.Project.ID]; !ok {
			projectNotificationLanguages[elem.Project.ID] = "ja"
			if project := model.FindProjectByKitsuID(db, elem.Project.ID); project != nil && strings.EqualFold(strings.TrimSpace(project.Language), "en") {
				projectNotificationLanguages[elem.Project.ID] = "en"
			}
		}
	}

	var payload []kitsu.MessagePayload
	sentCount := 0
	failedCount := 0
	for i := 0; i < len(data); i++ {
		payload = append(payload, data[i])

		if (i+1)%conf.Discord.EmbedsPerRequests == 0 || (i+1)%len(data) == 0 {
			if conf.Log {
				log.Printf("Sending bunch of messages: %d", len(payload))
			}

			newResults := discord.SendMessageBunch(conf, payload, webhookURL, previousMessageIDs, previousWebhookURLs, previousThreadIDs, projectNotificationLanguages, db)

			// Persist message/thread IDs and write audit entries.
			for taskID, res := range newResults {
				var task kitsu.MessagePayload
				for _, p := range payload {
					if p.Task.ID == taskID {
						task = p
						break
					}
				}
				// Resolve guild ID from project mapping for audit logging.
				projectGuildID := ""
				if projectRow := model.FindProjectByKitsuID(db, task.Project.ID); projectRow != nil {
					projectGuildID = projectRow.DiscordGuildID
				}
				actorKind, actorID, actorName := auditActorFromPayload(task)
				model.WriteAuditLog(db, model.AuditLog{
					TaskID:       taskID,
					ProjectID:    task.Project.ID,
					ProjectName:  task.Project.Name,
					GuildID:      projectGuildID,
					EntityName:   task.Entity.Name,
					TaskType:     task.TaskType.TaskType.Name,
					NewStatus:    task.TaskStatus.TaskStatus.ShortName,
					DiscordMsgID: map[bool]string{true: res.MessageID}[res.Durable],
					Success:      res.Durable,
					ErrorMessage: res.FailureCategory,
					ActorKind:    actorKind,
					ActorID:      actorID,
					ActorName:    actorName,
				})
				if res.Durable {
					if !res.AlreadyDelivered {
						sentCount++
					}
				} else {
					failedCount++
				}
			}

			payload = nil
			rl.Wait()
		}
	}

	if sentCount > 0 || failedCount > 0 {
		slog.Info("Notification send result",
			"routeSource", routeSource,
			"routeLabels", routeLabels,
			"sentCount", sentCount,
			"failedCount", failedCount)
		errSummary := ""
		if failedCount > 0 {
			errSummary = fmt.Sprintf("route=%s failed=%d", routeSource, failedCount)
		}
		setup.Stats.RecordSend(sentCount, failedCount, webhookURL, errSummary)
	}

	return data
}

// Kitsu credentials/settings helpers.
// Prefer DB settings, then environment variables, then conf.toml.
func getKitsuCreds(db *gorm.DB, conf config.Config) (hostname, email, password string) {
	if strings.TrimSpace(os.Getenv("KITSU_HOSTNAME")) == "" && strings.TrimSpace(conf.Kitsu.Hostname) != "" {
		setup.SetAppManagedKitsuHostname(conf.Kitsu.Hostname)
	}
	if apiBase := strings.TrimSpace(model.GetSetting(db, setup.KitsuAPIBaseURLSettingKey)); apiBase != "" {
		if normalized, err := setup.NormalizeKitsuURL(apiBase, setup.APISourceExplicit); err == nil {
			os.Setenv("KITSU_API_BASE_URL", normalized.ResolvedAPIBaseURL)
		}
	}
	hostname = setup.DiscoverKitsuHost(db).RuntimeHost
	email = model.GetSetting(db, setup.RuntimeKitsuEmailSettingKey)
	if email == "" {
		email = os.Getenv(setup.RuntimeKitsuEmailEnv)
	}
	if email == "" {
		email = conf.Kitsu.Email
	}
	password = setup.StoredRuntimeKitsuPassword(db)
	if password == "" {
		password = conf.Kitsu.Password
	}
	if hostname != "" && !strings.HasSuffix(hostname, "/") {
		hostname += "/"
	}
	return
}

func getDiscordSettings(db *gorm.DB, conf config.Config) (botToken, guildID, webhookURL string) {
	botToken = setup.RuntimeDiscordBotToken(db)
	if botToken == "" && strings.TrimSpace(model.GetSetting(db, setup.RuntimeDiscordBotTokenSettingKey)) == "" {
		botToken = conf.Discord.BotToken
	}
	guildID = model.GetSetting(db, "discord.guildID")
	if guildID == "" {
		guildID = os.Getenv("DISCORD_GUILD_ID")
	}
	if guildID == "" {
		guildID = conf.Discord.GuildID
	}
	webhookURL = os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		webhookURL = conf.Discord.WebhookURL
	}
	return
}

// Polling concurrency guard.
// pollMu prevents overlapping polling cycles in the same process.
var pollMu sync.Mutex

// notificationDispatch is replaceable in tests so routing behavior can be
// verified without making a Discord request.
var notificationDispatch = DiscordQueueSend
var makeKitsuResponse = MakeKitsuResponse

func persistTaskObservation(db *gorm.DB, payload kitsu.MessagePayload) error {
	return model.MarkTaskObserved(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.ShortName, payload.LatestComment.Comment.ID, payload.LatestComment.Comment.UpdatedAt, payload.Project.ID, payload.Entity.Name)
}

func redactExpiredNotificationPayloads(conf config.Config, db *gorm.DB, now time.Time) {
	days := conf.Notification.DeliveryPayloadRetentionDays
	if db == nil || days <= 0 {
		return
	}
	redacted, err := model.RedactDeliveredNotificationPayloads(db, now.UTC().AddDate(0, 0, -days), now.UTC(), model.NotificationPayloadRedactionBatchSize)
	if err != nil {
		slog.Error("Notification delivery payload retention failed", "error_class", "delivery_payload_retention_failed")
	} else if redacted > 0 {
		slog.Info("Notification delivery payload retention completed", "redacted_payloads", redacted)
	}
}

// auditActorFromPayload uses only actor data tied to the event we can prove.
// The latest comment author is an actor for comment-only changes. A task
// status payload does not expose its changer, so it remains unknown rather
// than being guessed from an assignee or an unrelated comment.
func auditActorFromPayload(payload kitsu.MessagePayload) (kind, id, name string) {
	statusAuthor := payload.StatusChangeAuthor.Person
	if strings.TrimSpace(statusAuthor.ID) != "" {
		if statusAuthor.IsBot {
			return model.AuditActorSystem, strings.TrimSpace(statusAuthor.ID), ""
		}
		if strings.TrimSpace(statusAuthor.FullName) == "" {
			return model.AuditActorUnknown, "", ""
		}
		return model.AuditActorHuman, strings.TrimSpace(statusAuthor.ID), strings.TrimSpace(statusAuthor.FullName)
	}
	if !payload.IsCommentOnly {
		return model.AuditActorUnknown, "", ""
	}
	author := payload.LatestComment.Author.Person
	if author.IsBot {
		return model.AuditActorSystem, author.ID, ""
	}
	if strings.TrimSpace(author.ID) == "" || strings.TrimSpace(author.FullName) == "" {
		return model.AuditActorUnknown, "", ""
	}
	return model.AuditActorHuman, strings.TrimSpace(author.ID), strings.TrimSpace(author.FullName)
}

// lastPollProductionTaskCounts holds the raw per-production task counts from
// the most recent successful Kitsu poll, before any age filtering. It is the
// bot-visibility signal for the health view and the open-production liveness
// monitor. Written by MakeKitsuResponse, read by runOnePoll; the poll loop is
// single-flight (pollMu), so no additional synchronization is required.
var lastPollProductionTaskCounts map[string]int

// lastPollFailedProjects lists the projects whose task read failed in the last
// poll. Their zero count is a fetch failure, not a stall, so the liveness
// monitor skips them.
var lastPollFailedProjects map[string]struct{}

// projectTasksResult is one project's task read.
type projectTasksResult struct {
	tasks   []kitsu.Task
	err     error
	elapsed time.Duration
}

// fetchProjectTasks reads every project's tasks with at most `limit` reads in
// flight (conf.Threads, minimum 1). Results keep the projects' order so the
// poll stays deterministic.
func fetchProjectTasks(projects []kitsu.Project, limit int) []projectTasksResult {
	if limit < 1 {
		limit = 1
	}
	results := make([]projectTasksResult, len(projects))
	slots := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range projects {
		slots <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-slots }()
			started := time.Now()
			read, err := kitsu.GetProjectTasksWithError(projects[i].ID)
			results[i] = projectTasksResult{tasks: read.Each, err: err, elapsed: time.Since(started)}
		}(i)
	}
	wg.Wait()
	return results
}

// cachedTaskComments is a task's comments as of the last_comment_date they were
// read at, so an unchanged task costs no comment request.
type cachedTaskComments struct {
	lastCommentDate string
	comments        []kitsu.Comment
}

// taskCommentReads counts the per-task comment requests of the current poll.
var taskCommentReads atomic.Int64

var (
	taskCommentCacheMu sync.Mutex
	taskCommentCache   = map[string]cachedTaskComments{}
	// taskCommentCacheKey is the Kitsu connection the cache was filled from.
	taskCommentCacheKey string
)

// snapshotTaskCommentCache returns the cache only when it belongs to the given
// connection. After a switch of Kitsu host or token the previous connection's
// entries are dropped, so one connection's last_comment_date can never make
// another connection skip a comment read.
func snapshotTaskCommentCache(connectionKey string) map[string]cachedTaskComments {
	taskCommentCacheMu.Lock()
	defer taskCommentCacheMu.Unlock()
	if taskCommentCacheKey != connectionKey {
		return map[string]cachedTaskComments{}
	}
	snapshot := make(map[string]cachedTaskComments, len(taskCommentCache))
	for id, entry := range taskCommentCache {
		snapshot[id] = entry
	}
	return snapshot
}

func replaceTaskCommentCache(connectionKey string, next map[string]cachedTaskComments) {
	taskCommentCacheMu.Lock()
	taskCommentCache = next
	taskCommentCacheKey = connectionKey
	taskCommentCacheMu.Unlock()
}

// loadTaskComments returns the task's comments, reading them only when
// last_comment_date differs from the cached read. A task without a
// last_comment_date has no comments. On a failed read the previous cache entry
// is kept (so the next poll retries) and ok is false.
func loadTaskComments(task kitsu.Task, previous, next map[string]cachedTaskComments, nextMu *sync.Mutex) (comments kitsu.Comments, ok bool) {
	date := strings.TrimSpace(task.LastCommentDate)
	if date == "" {
		return kitsu.Comments{}, true
	}
	cached, hit := previous[task.ID]
	if hit && cached.lastCommentDate == date {
		nextMu.Lock()
		next[task.ID] = cached
		nextMu.Unlock()
		return kitsu.Comments{Each: append([]kitsu.Comment(nil), cached.comments...)}, true
	}
	taskCommentReads.Add(1)
	fetched, err := kitsu.GetTaskCommentsWithError(task.ID)
	if err != nil {
		slog.Warn("Task comments fetch failed; comment diff skipped for this task", "error_class", "kitsu_comments_fetch_failed", "taskID", task.ID, "error", err)
		if hit {
			nextMu.Lock()
			next[task.ID] = cached
			nextMu.Unlock()
		}
		return kitsu.Comments{}, false
	}
	nextMu.Lock()
	next[task.ID] = cachedTaskComments{lastCommentDate: date, comments: fetched.Each}
	nextMu.Unlock()
	return kitsu.Comments{Each: append([]kitsu.Comment(nil), fetched.Each...)}, true
}

// pollStallThreshold is the number of consecutive zero-task polls before an
// open production is reported as stalled to the operations channel.
const pollStallThreshold = 3

// notifyProductionStall sends the one-shot stall notice for a production to
// the operations channel. It is best-effort; delivery failures are logged.
func notifyProductionStall(conf config.Config, db *gorm.DB, project model.Project, zeroPolls int) {
	_, _, opsWebhookURL := getDiscordSettings(db, conf)
	if opsWebhookURL == "" {
		slog.Warn("Open production reports zero visible tasks but no operations channel is configured",
			"error_class", "production_stall_no_ops_channel",
			"productionID", project.KitsuProjectID,
			"productionName", project.Name,
			"consecutiveZeroPolls", zeroPolls)
		return
	}
	lang := "ja"
	if project.Language == "en" {
		lang = "en"
	}
	title := "KitsuSync 死活監視: タスクが見えません"
	body := fmt.Sprintf("Production「%s」(%s) が open のまま、%d 回連続でポーリング結果が 0 件です。Kitsu 側の疎通・権限を確認してください。",
		project.Name, project.KitsuProjectID, zeroPolls)
	if lang == "en" {
		title = "KitsuSync liveness: no tasks visible"
		body = fmt.Sprintf("Production %q (%s) is open but %d consecutive polls returned 0 tasks. Check Kitsu connectivity and permissions.",
			project.Name, project.KitsuProjectID, zeroPolls)
	}
	payload := discord.Payload{Content: "**" + title + "**\n" + body}
	if res := discord.SendMessage(payload, opsWebhookURL, "", ""); res.MessageID == "" {
		slog.Warn("Production stall notice could not be delivered to the operations channel",
			"error_class", "production_stall_notify_failed",
			"productionID", project.KitsuProjectID,
			"category", res.FailureCategory)
	} else {
		slog.Warn("Open production reports zero visible tasks", "error_class", "production_stall_detected",
			"productionID", project.KitsuProjectID,
			"productionName", project.Name,
			"consecutiveZeroPolls", zeroPolls)
	}
}

func runOnePoll(conf config.Config, db *gorm.DB) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("KITSUSYNC_DISABLE_POLL")), "1") {
		slog.Info("Poll skipped by local audit guard")
		return
	}
	if !pollMu.TryLock() {
		slog.Warn("Previous poll still running; skipping this cycle to prevent duplicate Discord messages")
		return
	}
	defer pollMu.Unlock()
	started := time.Now()
	if !conf.SilentUpdateDB {
		// Retryable notification intents are retried only after this poll fetches
		// current Kitsu data and FilterTasks freshly renders their recipients and
		// resolves the current authorized destination.
		discord.RetryPriorMessageCleanup(db, 50)
		// Channel-as-queue: delete waiting-for-check posts whose task left WFA.
		discord.DeletePendingLifecycleMessages(db, 50)
	}

	// Poll Kitsu with runtime credentials from DB/env/conf priority.
	kitsuResponse, err := makeKitsuResponse(conf)
	if err != nil {
		setup.Stats.RecordPollError(err.Error())
		slog.Error("Kitsu poll failed", "error_class", "kitsu_fetch_failed", "err", err)
		return
	}
	if conf.Log {
		slog.Info("Done MakeKitsuResponse")
	}
	taskCount := len(kitsuResponse)
	// Auto-resolve stale unknown ledger rows before the retry check so a
	// delivery whose outcome was never observed (send timeout, process death)
	// cannot block the task's later notifications forever.
	if resolved, err := model.ResolveStaleUnknownNotificationDeliveries(db, time.Now().UTC().Add(-model.NotificationUnknownAutoResolveAfter)); err != nil {
		slog.Error("Stale unknown delivery resolution failed", "error_class", "unknown_auto_resolve_failed", "err", err)
	} else if resolved > 0 {
		slog.Warn("Stale unknown deliveries auto-resolved to terminal", "count", resolved)
	}
	FilterTasks(kitsuResponse, conf, db)
	if conf.Log {
		slog.Info("Done FilterTasks")
	}
	// Close expired aggregation windows: buffered channel transitions dispatch
	// here, either per task or as one bulk summary per destination.
	dispatchDueAggregatedNotifications(conf, db)
	// Daily 09:00 JST digest DMs to configured reviewers.
	runMorningDigestIfDue(conf, db)
	// Log-only delivery metrics, emitted when the ledger shape changes.
	logDeliveryMetrics(db)
	// Bulk summaries follow the channel-as-queue lifecycle: contained WFA
	// tasks that resolved get edited out of (or delete) their summary.
	reconcileAggregatedSummaries(db)
	// Per-production visibility and liveness: raw task counts (before age
	// filtering) tell whether the bot still sees each open production.
	// Validation-only profile projects are read-only fixtures and must never
	// raise liveness alerts.
	if db != nil {
		for _, project := range model.ListProjects(db) {
			if project.KitsuProjectID == "" || project.ValidationOnly {
				continue
			}
			if _, failed := lastPollFailedProjects[project.KitsuProjectID]; failed {
				continue
			}
			visible := lastPollProductionTaskCounts[project.KitsuProjectID]
			setup.Stats.RecordProductionPoll(project.KitsuProjectID, visible)
			if setup.Stats.TakeStallNotification(project.KitsuProjectID) {
				notifyProductionStall(conf, db, project, pollStallThreshold)
			}
		}
	}
	setup.Stats.RecordPollWithDuration(taskCount, time.Since(started))
}

func configureSQLite(db *gorm.DB) (*sql.DB, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)
	sqlDB.SetConnMaxIdleTime(0)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
	}
	for _, pragma := range pragmas {
		if err := db.Exec(pragma).Error; err != nil {
			return nil, err
		}
	}

	var journalMode string
	var synchronous int
	var busyTimeout int
	var foreignKeys int
	if err := sqlDB.QueryRow("PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		return nil, err
	}
	if err := sqlDB.QueryRow("PRAGMA synchronous;").Scan(&synchronous); err != nil {
		return nil, err
	}
	if err := sqlDB.QueryRow("PRAGMA busy_timeout;").Scan(&busyTimeout); err != nil {
		return nil, err
	}
	if err := sqlDB.QueryRow("PRAGMA foreign_keys;").Scan(&foreignKeys); err != nil {
		return nil, err
	}
	if foreignKeys != 1 {
		return nil, fmt.Errorf("sqlite foreign key enforcement is disabled")
	}

	slog.Info("SQLite pragmas configured",
		"journalMode", journalMode,
		"synchronous", synchronous,
		"busyTimeoutMs", busyTimeout,
		"foreignKeys", foreignKeys,
		"maxOpenConns", 1)

	return sqlDB, nil
}

// migrateApplicationSchema applies the complete persistent schema before any
// handler can accept an authenticated request. In particular, AdminSession was
// added after earlier installations already had SQLite data, so migration
// failures must stop startup instead of surfacing later as a generic login
// persistence error.
func migrateApplicationSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is unavailable")
	}
	if err := db.Exec("DROP INDEX IF EXISTS idx_checker_maps_task_type").Error; err != nil {
		return fmt.Errorf("remove legacy checker map index: %w", err)
	}
	if err := db.AutoMigrate(
		&model.Task{},
		&model.Project{},
		&model.ProjectWebhook{},
		&model.ProductionChannelMapping{},
		&model.ProductionNotificationConfig{},
		&model.ProductionNotificationRoute{},
		&model.NotificationRoutingDiagnosis{},
		&model.UserMap{},
		&model.CheckerMap{},
		&model.Setting{},
		&model.AdminSession{},
		&model.AuditLog{},
		&model.ProjectUserMap{},
		&model.ProjectCheckerMap{},
		&model.ProjectReviewerTarget{},
		&model.ProjectSetting{},
		&model.ProductionNotificationWriteLock{},
		&model.NotificationDeliveryEvent{},
		&model.NotificationAggregationBuffer{},
		&model.ManagedChannelDeletion{},
		&model.ProductionForumConfig{},
		&model.ProductionForumRoute{},
		&model.ProductionForumStatusRole{},
		&model.ProductionForumTag{},
	); err != nil {
		return fmt.Errorf("migrate application schema: %w", err)
	}
	if err := model.QuarantineInflightNotificationDeliveries(db); err != nil {
		return fmt.Errorf("quarantine interrupted notification deliveries: %w", err)
	}
	// Pre-lane ledgers (v0.5.4 and earlier) store event IDs without the lane
	// components. Re-derive them so still-unresolved rows stay resumable
	// instead of silently blocking newer intents for the same task.
	if err := model.MigrateLegacyNotificationEventIdentities(db); err != nil {
		return fmt.Errorf("migrate legacy notification delivery identities: %w", err)
	}
	// Lifecycle cleanup claims left in-flight by a restart go back to pending,
	// mirroring the delivery quarantine; otherwise the channel post would
	// never be deleted.
	if _, err := model.ReleaseStuckNotificationLifecycleCleanups(db); err != nil {
		return fmt.Errorf("release stuck lifecycle cleanups: %w", err)
	}
	// Unknown rows whose send outcome was never observed must not block their
	// task forever across restarts either.
	if _, err := model.ResolveStaleUnknownNotificationDeliveries(db, time.Now().UTC().Add(-model.NotificationUnknownAutoResolveAfter)); err != nil {
		return fmt.Errorf("resolve stale unknown notification deliveries: %w", err)
	}
	return nil
}

func newApplicationHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":8090",
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
}

func main() {
	// The standard library logger writes to stderr and is used by the CLI
	// paths. Keep it on the same redacting boundary as the application logger.
	log.SetOutput(logutil.NewRedactingWriter(os.Stderr))
	stdslog.SetDefault(stdslog.New(stdslog.NewTextHandler(logutil.NewRedactingWriter(os.Stderr), nil)))
	slog.Configure(func(logger *slog.SugaredLogger) {
		f := logger.Formatter.(*slog.TextFormatter)
		f.EnableColor = true
		f.SetTemplate("[{{datetime}}] [{{level}}] [{{caller}}]\t{{message}} {{data}} {{extra}}\n")
		f.ColorTheme = slog.ColorTheme
		// Wrap stdout so docker compose logs output is also redacted
		logger.Output = logutil.NewRedactingWriter(os.Stdout)
	})

	// Ensure file logging survives fresh containers where ./logs may not exist yet.
	logFile, err := logutil.OpenAppendFile("./logs/all-levels.log")
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	defer logFile.Close()

	redactingWriter := logutil.NewRedactingWriter(logFile)
	h1 := handler.NewIOWriterHandler(redactingWriter, slog.AllLevels)
	slog.PushHandler(h1)

	// Set log level based on APP_ENV (production = INFO, development = DEBUG)
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "production" {
		slog.SetLogLevel(slog.InfoLevel)
		slog.Info("App started", "env", appEnv, "log_level", "INFO")
	} else {
		slog.SetLogLevel(slog.DebugLevel)
		slog.Debug("App started", "env", appEnv, "log_level", "DEBUG")
	}

	start := time.Now()

	conf := config.Read()
	// Make the configured conf.toml host available to the UI/runtime fallback
	// path without overriding an explicitly saved SQLite endpoint.
	if strings.TrimSpace(os.Getenv("KITSU_HOSTNAME")) == "" && strings.TrimSpace(conf.Kitsu.Hostname) != "" {
		setup.SetAppManagedKitsuHostname(conf.Kitsu.Hostname)
	}

	if issues := conf.Validate(); len(issues) > 0 {
		fatalConfig := false
		for _, msg := range issues {
			if strings.HasPrefix(msg, "[FATAL]") {
				fatalConfig = true
				slog.Error("config validation: " + msg)
			} else {
				slog.Warn("config validation: " + msg)
			}
		}
		if fatalConfig {
			log.Fatal("configuration validation failed")
		}
	}

	if conf.Debug {
		os.Setenv("Debug", "true")
	}
	if err := secureRuntimeDataFiles("data"); err != nil {
		slog.Fatal("failed to secure runtime data files", "error_class", "runtime_file_permissions_failed")
		os.Exit(1)
	}

	db, err := gorm.Open(sqlite.Open(filepath.Join("data", "sqlite.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		slog.Fatal("failed to connect database")
		os.Exit(1)
	}
	setup.ConfigureSessionStore(db)
	sqlDB, err := configureSQLite(db)
	if err != nil {
		slog.Fatal("failed to configure sqlite", "err", err)
		os.Exit(1)
	}
	if err := migrateApplicationSchema(db); err != nil {
		slog.Fatal("failed to migrate SQLite schema", "error_class", "schema_migration_failed")
		os.Exit(1)
	}
	model.PurgeLegacySensitiveData(db)

	setup.SeedConfigIfFixture(db, conf)
	if err := setup.MigrateRuntimeDiscordBotToken(db); err != nil {
		slog.Warn("Discord runtime token migration deferred", "error_class", "credential_migration_failed")
	}
	if persistedDiscordToken := setup.StoredRuntimeDiscordBotToken(db); persistedDiscordToken != "" {
		os.Setenv("DISCORD_BOT_TOKEN", persistedDiscordToken)
		slog.Debug("Discord runtime token loaded",
			"token_present", true,
		)
	}
	_, seedGuildID, _ := getDiscordSettings(db, conf)
	model.SeedProjectGuildFallback(db, seedGuildID)

	discord.UserMapResolver = func(projectID, kitsuPersonID, kitsuName, kitsuEmail string) string {
		return model.GetUserMapForProjectWithIdentity(db, projectID, kitsuPersonID, kitsuName, kitsuEmail)
	}
	discord.CheckerResolver = func(projectID, taskTypeID, taskTypeName string) []string {
		return model.GetCheckerForProjectByTaskTypeID(db, projectID, taskTypeID, taskTypeName)
	}
	discord.ReviewerResolver = func(projectID, taskTypeID, taskTypeName, senderWebhookURL string) ([]discord.ReviewerTarget, error) {
		hostname, _, _ := getKitsuCreds(db, conf)
		connection, connectionErr := setup.ResolveKitsuConnection(context.Background(), hostname, model.GetSetting(db, setup.KitsuAPIBaseURLSettingKey))
		baseURL := ""
		if connectionErr == nil {
			baseURL = connection.ResolvedAPIBaseURL
		}
		targets, err := setup.ResolveProductionWFAReviewerTargets(db, baseURL, setup.StoredRuntimeKitsuToken(db), setup.StoredRuntimeDiscordBotToken(db), projectID, taskTypeID, senderWebhookURL)
		resolved := make([]discord.ReviewerTarget, 0, len(targets))
		for _, target := range targets {
			resolved = append(resolved, discord.ReviewerTarget{Kind: target.TargetKind, ID: target.DiscordID})
		}
		return resolved, err
	}
	discord.GoogleDriveURLResolver = func(projectID string) string {
		return model.GetProjectStorageURL(db, projectID)
	}
	discord.DiscordBotTokenResolver = func() string {
		botToken, _, _ := getDiscordSettings(db, conf)
		return botToken
	}
	discord.KitsuPublicURLResolver = func() string {
		return setup.PublicKitsuURL(db)
	}

	if conf.Log {
		slog.Info("Connected to database in %s", time.Since(start))

		if _, err := os.Stat("./dump"); os.IsNotExist(err) {
			err := os.Mkdir("./dump", os.ModeDir)
			if err != nil {
				slog.Fatal("failed to create dir")
				os.Exit(1)
			}
		}
	}

	c := cron.New(cron.WithChain(
		safego.CronRecover(),
		cron.DelayIfStillRunning(cron.DefaultLogger),
	))

	runtime := newRuntimeManager()
	refreshRuntime := func() bool {
		hostname, _, _ := getKitsuCreds(db, conf)
		slog.Debug("Kitsu runtime credential availability",
			"hostname_present", strings.TrimSpace(hostname) != "",
			"bot_token_present", setup.StoredRuntimeKitsuToken(db) != "",
		)
		if token := setup.StoredRuntimeKitsuToken(db); token != "" {
			connection, connectionErr := setup.ResolveKitsuConnection(context.Background(), hostname, model.GetSetting(db, setup.KitsuAPIBaseURLSettingKey))
			if connectionErr != nil {
				setup.RecordKitsuRuntimeAuthMode(db, "bot_token_failed", "connection_unverified")
				return false
			}
			validation := setup.ValidateKitsuBotToken(db, strings.TrimSuffix(connection.ResolvedAPIBaseURL, "/api"), token, true)
			slog.Debug("Kitsu Bot token runtime validation",
				"classification", validation.Classification,
				"stage", validation.Stage,
				"authenticated", validation.Authenticated,
				"failure_endpoint", validation.Failure.Endpoint,
				"failure_status", validation.Failure.StatusCode,
				"failure_error_class", validation.Failure.ErrorClass,
			)
			if validation.Compatible() {
				setup.RecordKitsuRuntimeAuthMode(db, "bot_token", "")
				return runtime.authenticateToken(connection, token)
			}
			setup.RecordKitsuRuntimeAuthMode(db, "bot_token_failed", validation.Classification)
			return false
		}
		setup.RecordKitsuRuntimeAuthMode(db, "bot_token_missing", "")
		return false
	}

	if len(conf.Discord.Productions) > 0 || len(conf.Discord.TaskTypeWebhooks) > 0 {
		slog.Warn("conf.toml routing (Productions/TaskTypeWebhooks) is deprecated — manage channel assignments via Admin UI (/bot/setup) instead")
	}

	// HTTP server: health checks, project setup APIs, and admin UI routes.
	mux := http.NewServeMux()
	mux.Handle("GET /favicon.ico", setup.FaviconHandler())
	mux.Handle("GET /kitsusync.svg", setup.KitsuSyncIconHandler())
	mux.HandleFunc("/health", healthHandler(runtime, func(ctx context.Context) error {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.PingContext(ctx)
	}))
	mux.HandleFunc("/ready", readinessHandler(runtime))

	onRuntimeConfigured := func() {
		slog.Debug("Kitsu reconnect attempted", "attempted", true)
		if !refreshRuntime() {
			slog.Warn("Kitsu reconnect result", "success", false)
			return
		}
		slog.Debug("Kitsu reconnect result", "success", true)
		safego.Go("kitsu reconnect poll", func() { runtime.runWhenReady(func() { runOnePoll(conf, db) }) })
	}

	setupHandler := func(w http.ResponseWriter, r *http.Request) {
		if setup.ValidationOnlyModeEnabled() && r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "validation-only profile is read-only", http.StatusForbidden)
			return
		}
		kitsuHost, _, _ := getKitsuCreds(db, conf)
		botToken, fallbackGuildID, _ := getDiscordSettings(db, conf)
		setup.Handler(kitsuHost, fallbackGuildID, botToken, db, runtime.ready, onRuntimeConfigured)(w, r)
	}

	setupCredsFunc := func() (string, string, string, string) {
		h, _, _ := getKitsuCreds(db, conf)
		tok, gid, wh := getDiscordSettings(db, conf)
		return h, tok, gid, wh
	}

	loginHandler := setup.LoginRateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setup.LoginHandlerWithTrustedAuthority(func() setup.KitsuLoginAuthority {
			result := setup.DiscoverKitsuHost(db)
			apiBaseURL := strings.TrimSpace(model.GetSetting(db, setup.KitsuAPIBaseURLSettingKey))
			if apiBaseURL == "" {
				apiBaseURL = strings.TrimSpace(os.Getenv("KITSU_API_BASE_URL"))
			}
			return setup.KitsuLoginAuthority{RuntimeHost: result.RuntimeHost, APIBaseURL: apiBaseURL, Source: result.Source}
		}, func() string {
			if display := strings.TrimSpace(model.GetSetting(db, setup.KitsuDisplayURLSettingKey)); display != "" {
				return display
			}
			return model.GetSetting(db, "kitsu.hostname")
		}, func(displayHost, runtimeHost, apiOverride string) {
			model.SetSetting(db, setup.KitsuDisplayURLSettingKey, displayHost)
			model.SetSetting(db, "kitsu.hostname", runtimeHost)
			if strings.TrimSpace(apiOverride) == "" {
				model.DeleteSetting(db, setup.KitsuAPIBaseURLSettingKey)
				os.Unsetenv("KITSU_API_BASE_URL")
			} else if normalized, err := setup.NormalizeKitsuURL(apiOverride, setup.APISourceExplicit); err == nil {
				model.SetSetting(db, setup.KitsuAPIBaseURLSettingKey, normalized.ResolvedAPIBaseURL)
				os.Setenv("KITSU_API_BASE_URL", normalized.ResolvedAPIBaseURL)
			}
		})(w, r)
	}))
	mux.Handle("/login", loginHandler)
	mux.Handle("/bot/login", loginHandler)
	mux.HandleFunc("/bot/", setup.BotRootHandler(runtime.ready))

	mux.HandleFunc("/logout", setup.LogoutHandler())
	mux.HandleFunc("/bot/logout", setup.LogoutHandler())

	setupReadOnlyRoute := setup.RequireSession(setup.ReadOnlyAuditRoute(runtime.ready, setupHandler))
	mux.HandleFunc("/setup", setupReadOnlyRoute)
	mux.HandleFunc("/bot/setup", setupReadOnlyRoute)

	// Setup diagnostic JSON API — registered under both root and /bot prefix.
	setupAPIRoutes := func(prefix string) {
		mux.HandleFunc(prefix+"/api/setup/status", setup.RequireSession(setup.SetupStatusHandler(
			db, conf.PollIntervalSeconds(), setupCredsFunc,
		)))
		mux.HandleFunc(prefix+"/api/setup/observability", setup.RequireSession(setup.TelemetrySnapshotHandler()))
		mux.HandleFunc(prefix+"/api/setup/projects", setup.RequireSession(setup.ReadOnlyAuditRoute(runtime.ready, setup.ProjectsHandler(db))))
		mux.HandleFunc(prefix+"/api/setup/preview-project", setup.RequireSession(setup.ReadOnlyAuditPreviewRoute(runtime.ready, setup.PreviewProjectHandler(db, setupCredsFunc))))
		mux.HandleFunc(prefix+"/api/setup/apply-project", setup.RequireSession(setup.RuntimeReadyRequired(runtime.ready, setup.RejectValidationMutation(setup.ApplyProjectHandler(db, setupCredsFunc)))))
		mux.HandleFunc(prefix+"/api/setup/test-kitsu", setup.RequireSession(setup.RejectValidationMutation(setup.TestKitsuHandler(db, onRuntimeConfigured))))
		mux.HandleFunc(prefix+"/api/setup/test-discord", setup.RequireSession(setup.RejectValidationMutation(setup.TestDiscordHandler(db))))
		// Test Notification is a synthetic Discord-only verification. It must remain
		// available while Kitsu runtime polling is disconnected; the handler still
		// verifies the selected project destination and uses the configured Discord
		// credentials, without changing normal routing or sending Kitsu data.
		mux.HandleFunc(prefix+"/api/setup/test-notification", setup.RequireSession(setup.RejectValidationMutation(setup.TestNotificationHandler(db, setupCredsFunc))))
		mux.HandleFunc(prefix+"/api/setup/mapping", setup.RequireSession(setup.RuntimeReadyRequired(runtime.ready, setup.MappingStateHandler(db))))
		mux.HandleFunc(prefix+"/api/setup/mapping/users", setup.RequireSession(setup.RuntimeReadyRequired(runtime.ready, setup.RejectValidationMutation(setup.SaveUserMappingHandler(db)))))
		mux.HandleFunc(prefix+"/api/setup/mapping/checkers", setup.RequireSession(setup.RuntimeReadyRequired(runtime.ready, setup.RejectValidationMutation(setup.SaveCheckerMappingHandler(db)))))
	}
	setupAPIRoutes("")
	setupAPIRoutes("/bot")

	registerAdminRoutes := func(prefix string) {
		mux.HandleFunc(prefix+"/admin", setup.RequireSession(setup.AdminIndexWithRuntime(db, runtime.ready)))
		mux.HandleFunc(prefix+"/admin/users", setup.RequireSession(setup.ReadOnlyAuditRoute(runtime.ready, func(w http.ResponseWriter, r *http.Request) {
			h, _, _ := getKitsuCreds(db, conf)
			setup.UsersHandler(db, h)(w, r)
		})))
		mux.HandleFunc(prefix+"/admin/drive", setup.RequireSession(setup.DriveHandler(db)))
		// BotHandler persists shared runtime credentials and triggers reconnect.
		kitsuReconnect := func() { onRuntimeConfigured() }
		botHandler := setup.BotHandlerWithRuntime(db, kitsuReconnect, runtime.ready)
		mux.HandleFunc(prefix+"/admin/bot", setup.RequireSession(setup.RejectValidationMutation(func(w http.ResponseWriter, r *http.Request) {
			slog.Debug("Admin bot route dispatched", "method", r.Method, "path", r.URL.Path)
			botHandler(w, r)
		})))
		mux.HandleFunc(prefix+"/admin/projects", setup.RequireSession(setup.ReadOnlyAuditRoute(runtime.ready, func(w http.ResponseWriter, r *http.Request) {
			botToken, fallbackGuildID, _ := getDiscordSettings(db, conf)
			setup.AdminProjectsHandler(db, fallbackGuildID, botToken)(w, r)
		})))
		// Forum settings are local configuration: saving needs neither Kitsu nor the
		// runtime to be ready, so this page is not behind the runtime-ready gate.
		// A global validation-only profile still refuses writes.
		mux.HandleFunc(prefix+"/admin/forum", setup.RequireSession(setup.RejectValidationMutation(func(w http.ResponseWriter, r *http.Request) {
			botToken, fallbackGuildID, _ := getDiscordSettings(db, conf)
			setup.ForumSettingsHandler(db, func() (string, string) { return botToken, fallbackGuildID })(w, r)
		})))
		mux.HandleFunc(prefix+"/admin/audit", setup.RequireSession(setup.AuditLogHandler(db)))
		mux.HandleFunc(prefix+"/admin/health", setup.RequireSession(setup.HealthHandler(db)))
	}
	registerAdminRoutes("")
	registerAdminRoutes("/bot")

	server := newApplicationHTTPServer(setup.RequestBodyLimit(setup.RequestTrace(setup.CSRFProtection(mux))))
	go func() {
		slog.Info("HTTP server listening on :8090  (/health, /login, /setup, /admin/*)")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "err", err)
		}
	}()

	if refreshRuntime() {
		slog.Info("Kitsu runtime configured")
		if setup.ValidationOnlyModeEnabled() {
			if count, err := setup.SeedValidationOnlyProfile(db); err != nil {
				slog.Warn("validation-only profile import failed", "err", err)
			} else {
				slog.Info("validation-only profile imported", "productions", count)
			}
		}
		safego.Go("initial poll", func() {
			runtime.runWhenReady(func() {
				runOnePoll(conf, db)
				if conf.Log {
					slog.Info("Done initial poll", "duration", time.Since(start).String())
				}
			})
		})
	} else {
		slog.Warn("Kitsu runtime setup required; HTTP UI is available and notifications are paused")
	}

	c.AddFunc("@every 1h", func() {
		if !refreshRuntime() {
			slog.Warn("Kitsu token refresh failed; keeping the previous usable token when available")
		}
	})
	c.AddFunc("@every 20s", func() {
		hostname, _, _ := getKitsuCreds(db, conf)
		kitsuToken := setup.StoredRuntimeKitsuToken(db)
		apiOverride := model.GetSetting(db, setup.KitsuAPIBaseURLSettingKey)
		discordToken, _, _ := getDiscordSettings(db, conf)
		setup.ObserveRuntimeAPIMonitoring(hostname, apiOverride, kitsuToken, discordToken)
	})
	c.AddFunc("@every "+conf.PollInterval().String(), func() {
		if !runtime.ready() && !refreshRuntime() {
			return
		}
		runtime.runWhenReady(func() { runOnePoll(conf, db) })
	})
	c.AddFunc("@every 1h", func() { redactExpiredNotificationPayloads(conf, db, time.Now()) })
	c.AddFunc("0 3 * * *", func() {
		deleted := model.PurgeOldAuditLogs(db, 90)
		if deleted > 0 {
			slog.Info("audit log purge", "deleted_rows", deleted)
		}
	})
	redactExpiredNotificationPayloads(conf, db, time.Now())

	c.Start()

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	<-sigCtx.Done()
	slog.Info("Shutdown signal received; stopping HTTP server, cron, and SQLite")

	stopCtx := c.Stop()
	select {
	case <-stopCtx.Done():
		slog.Info("Cron stopped cleanly")
	case <-time.After(10 * time.Second):
		slog.Warn("Cron stop timed out")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTP server shutdown had an error", "err", err)
	} else {
		slog.Info("HTTP server shutdown complete")
	}

	if err := sqlDB.Close(); err != nil {
		slog.Warn("SQLite close failed", "err", err)
	} else {
		slog.Info("SQLite connection closed cleanly")
	}
}

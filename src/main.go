package main

import (
	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/forum"
	"app/src/model"
	"app/src/setup"
	"app/src/utils/config"
	logutil "app/src/utils/log"
	"app/src/utils/request"
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

	"github.com/gookit/slog"
	"github.com/gookit/slog/handler"
	"github.com/gookit/slog/rotatefile"

	"github.com/pieterclaerhout/go-waitgroup"
	"github.com/robfig/cron/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// MakeKitsuResponse returns the payloads to notify on, and separately the
// payloads to baseline on. The baseline set skips the ignoreMessagesDaysOld
// filter: a task older than the notification window still has to be recorded
// as existing, or forum mode would later treat it as new and post it.
func MakeKitsuResponse(conf config.Config) (notifiable []kitsu.MessagePayload, baseline []kitsu.MessagePayload, err error) {
	return MakeKitsuResponseContext(context.Background(), conf)
}

// MakeKitsuResponseContext is MakeKitsuResponse with a cancellation root: when
// ctx is cancelled every Kitsu read in flight is aborted so a shutdown does not
// wait on the request timeouts.
func MakeKitsuResponseContext(ctx context.Context, conf config.Config) (notifiable []kitsu.MessagePayload, baseline []kitsu.MessagePayload, err error) {
	pollFetchStart := time.Now()
	taskCommentReads.Store(0)

	projects, err := kitsu.GetProjectsWithContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch projects: %w", err)
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
	fetched := fetchProjectTasks(ctx, projects.Each, conf.Threads)
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
		return nil, nil, fmt.Errorf("fetch tasks: all %d projects failed", len(projects.Each))
	}
	lastPollFailedProjects = failedProjects
	tasksFetchElapsed := time.Since(tasksFetchStart)
	if conf.Log {
		slog.Info("Got tasks: " + strconv.Itoa(len(tasks.Each)))
	}

	taskStatuses, err := kitsu.GetTaskStatusesWithContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch task statuses: %w", err)
	}
	if conf.Log {
		slog.Info("Got taskStatuses: " + strconv.Itoa(len(taskStatuses.Each)))
	}

	entities, err := kitsu.GetEntitiesWithContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch entities: %w", err)
	}
	if conf.Log {
		slog.Info("Got entities: " + strconv.Itoa(len(entities.Each)))
	}

	enitityTypes, err := kitsu.GetEntityTypesWithContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch entity types: %w", err)
	}
	if conf.Log {
		slog.Info("Got enitityTypes: " + strconv.Itoa(len(enitityTypes.Each)))
	}

	taskTypes, err := kitsu.GetTaskTypesWithContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch task types: %w", err)
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

	persons, err := kitsu.GetPersonsWithContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch persons: %w", err)
	}
	if conf.Log {
		slog.Info("Got persons: " + strconv.Itoa(len(persons.Each)))
	}

	identities := make(map[string]kitsuPersonIdentity, len(persons.Each))
	for _, person := range persons.Each {
		if id := strings.TrimSpace(person.ID); id != "" {
			identities[id] = kitsuPersonIdentity{name: person.FullName, email: person.Email}
		}
	}
	lastPollPersonIdentities = identities

	// Comments are read per task, and only for tasks whose last_comment_date
	// moved since the previous poll (the comments LIST endpoint is admin only on
	// Zou 1.1x). The cache is rebuilt each poll so it only holds tasks still seen.
	connectionKey := kitsu.ConnectionKey()
	previousComments := snapshotTaskCommentCache(connectionKey)
	nextComments := make(map[string]cachedTaskComments, len(tasks.Each))
	var nextCommentsMu sync.Mutex

	start := time.Now()

	response := make([]kitsu.MessagePayload, len(tasks.Each))
	// baselineIndex marks the slots that were only filled for the baseline
	// set (too old to notify on).
	baselineIndex := make([]bool, len(tasks.Each))

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

			// The ledger still gets this task (see baselineOut below); only the
			// notification path drops it.
			if conf.IgnoreMessagesDaysOld != 0 && daysCount > conf.IgnoreMessagesDaysOld {
				baselineIndex[i] = true
				response[i].Task.Task = tasks.Each[i]
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
				taskComments, commentsOK := loadTaskComments(ctx, tasks.Each[i], previousComments, nextComments, &nextCommentsMu)
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
	var baselineOut []kitsu.MessagePayload
	for i, elem := range response {
		if len(elem.Task.Task.ID) == 0 {
			continue
		}
		if baselineIndex[i] {
			baselineOut = append(baselineOut, elem)
			continue
		}
		out = append(out, elem)
		baselineOut = append(baselineOut, elem)
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

	return out, baselineOut, nil
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

// forumCardRenderer renders the first message of a forum post with the same
// card the lane path posts. The card never mentions anyone and carries no
// parse targets because the first message must stay silently non-pinging (D-04).
func forumCardRenderer(conf config.Config, db *gorm.DB) func(kitsu.MessagePayload) (json.RawMessage, error) {
	return func(payload kitsu.MessagePayload) (json.RawMessage, error) {
		return forum.RenderCard(payload, func(item kitsu.MessagePayload) (discord.Payload, error) {
			return renderForumCard(item, conf, db)
		})
	}
}

// renderForumCard builds the card for one task. It reuses the lane renderer with
// the same saved settings (template preset and the Production's notification
// language) so the post looks exactly like the existing notification.
func renderForumCard(payload kitsu.MessagePayload, conf config.Config, db *gorm.DB) (discord.Payload, error) {
	language := "ja"
	if db != nil {
		if project := model.FindProjectByKitsuID(db, payload.Project.ID); project != nil {
			language = discord.NotificationLanguage(project.Language)
		}
	}
	template, err := discord.NotificationCardTemplate(conf, payload, "", language, nil)
	if err != nil {
		return discord.Payload{}, err
	}
	// The first message never mentions anyone (D-04). The template carries no
	// mention fields anymore for the forum card path, so nothing must be
	// cleared here; the empty AllowedMentions parse still guarantees silence.
	card := discord.RenderNotificationPayload(template, conf.TplPreset)
	card.AllowedMentions = &discord.AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}}
	return card, nil
}

// runForumPoll feeds forum-mode Productions into the forum ledger and drains
// the outbox. Lane-mode Productions are untouched: the forum ledger only ever
// sees Productions whose settings say "forum".
func runForumPoll(data []kitsu.MessagePayload, conf config.Config, db *gorm.DB) {
	if db == nil || conf.SilentUpdateDB {
		return
	}
	poller := forum.Poller{DB: db, RenderCard: forumCardRenderer(conf, db)}
	// Handoff detection reads the same payloads: a publish comment with no
	// status change on a 受け渡し row becomes a handoff event for K3.
	detector := forum.HandoffDetector{DB: db, Comments: kitsu.GetTaskCommentsWithError}
	if hr, err := detector.Detect(data); err != nil {
		slog.Error("Handoff detection failed", "error_class", "handoff_detect_failed", "err", err)
	} else if hr.Events > 0 || hr.Known > 0 {
		slog.Info("Handoff detection summary", "event", "handoff_detect_summary", "events", hr.Events, "known", hr.Known)
	}
	// A handoff addressed to a concrete assignee is reported now; the rest
	// wait for the morning digest, which lists them until they are answered.
	if sent, err := sendImmediateHandoffs(conf, db); err != nil {
		slog.Error("Handoff notification failed", "error_class", "handoff_notify_failed", "err", err)
	} else if sent > 0 {
		slog.Info("Handoff notification summary", "event", "handoff_notify_summary", "sent", sent)
	}
	result, err := poller.PollProjects(data)
	if err != nil {
		slog.Error("Forum poll failed", "error_class", "forum_poll_failed", "err", err)
		return
	}
	if result.Productions > 0 {
		slog.Info("Forum poll summary",
			"event", "forum_poll_summary",
			"productions", result.Productions,
			"baselined", result.Baselined,
			"created", result.Created,
			"synced", result.Synced,
			"signalled", result.Signalled,
			"skipped", result.Skipped)
	}
	botToken, _, _ := getDiscordSettings(db, conf)
	recipients := forum.Recipients{
		DB: db,
		ResolveAssignees: func(projectID string, kitsuPersonIDs []string) []string {
			return discord.ResolveAssigneeDiscordIDsWithIdentity(projectID, kitsuPersonIDs, forumAssigneeIdentity)
		},
		ResolveReviewers: func(projectID, taskTypeID, taskTypeName string) ([]discord.ReviewerTarget, error) {
			return forumReviewerTargets(db, conf, botToken, projectID, taskTypeID)
		},
		NotifyAssigneesOnComment: commentNotifyAssignee(db, conf),
	}
	executor := forum.Executor{
		DB:       db,
		BotToken: func() string { return botToken },
		TaskURL: func(taskID string) string {
			return forum.TaskURLFor(db, func() string { return setup.PublicKitsuURL(db) }, taskID)
		},
		Recipients: func(taskID string, payload model.ForumPostMessagePayload) ([]string, []string, error) {
			return recipients.ForTask(taskID, payload)
		},
		AssigneeNames: forumAssigneeNames,
		RecordSend:    setup.Stats.RecordSend,
	}
	if _, err := executor.Drain(forumOutboxDrainLimit); err != nil {
		// A rate-limited drain is a pause, not a failure: the rows stay due and
		// the next poll resumes them.
		if errors.Is(err, forum.ErrForumRateLimited) {
			slog.Warn("Forum outbox drain paused by a discord rate limit", "error_class", "forum_outbox_rate_limited")
			return
		}
		slog.Error("Forum outbox drain failed", "error_class", "forum_outbox_drain_failed", "err", err)
	}
}

// forumReviewerTargets resolves WFA reviewers for a forum post: the Bot posts
// into the forum channel, so there is no lane sender webhook to validate with.
func forumReviewerTargets(db *gorm.DB, conf config.Config, botToken, projectID, taskTypeID string) ([]discord.ReviewerTarget, error) {
	hostname, _, _ := getKitsuCreds(db, conf)
	baseURL := ""
	if connection, connectionErr := setup.ResolveKitsuConnection(context.Background(), hostname, model.GetSetting(db, setup.KitsuAPIBaseURLSettingKey)); connectionErr == nil {
		baseURL = connection.ResolvedAPIBaseURL
	}
	targets, err := setup.ResolveForumWFAReviewerTargets(db, baseURL, setup.StoredRuntimeKitsuToken(db), botToken, projectID, taskTypeID)
	resolved := make([]discord.ReviewerTarget, 0, len(targets))
	for _, target := range targets {
		resolved = append(resolved, discord.ReviewerTarget{Kind: target.TargetKind, ID: target.DiscordID})
	}
	return resolved, err
}

// commentNotifyAssignee prefers the admin-UI setting: a stored true/false wins,
// and conf.toml only supplies the initial value while nothing is stored.
func commentNotifyAssignee(db *gorm.DB, conf config.Config) bool {
	switch model.GetSetting(db, setup.CommentNotifyAssigneeSettingKey) {
	case "true":
		return true
	case "false":
		return false
	}
	return conf.Notification.CommentNotifyAssignee
}

// forumOutboxDrainLimit bounds one poll's Discord calls.
const forumOutboxDrainLimit = 50

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

// pollWork tracks the poll goroutines that may still touch the database, so
// shutdown waits for them before closing SQLite.
var pollWork sync.WaitGroup

// pollCtx is the cancellation root for polling. Shutdown cancels it first so
// in-flight Kitsu reads return instead of outliving SQLite.
var pollCtx, stopPolling = context.WithCancel(context.Background())

// trackPoll runs fn as poll work that shutdown waits for.
func trackPoll(fn func()) {
	pollWork.Add(1)
	go func() {
		defer pollWork.Done()
		fn()
	}()
}

var makeKitsuResponse = MakeKitsuResponseContext

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

// kitsuPersonIdentity is the name/email fallback identity of one Kitsu person.
type kitsuPersonIdentity struct {
	name  string
	email string
}

// lastPollPersonIdentities holds the identity of every person seen in the last
// successful Kitsu read, so forum assignee resolution can retry the user map
// with a name/email instead of only the Person ID. Written by
// MakeKitsuResponseContext, read by runForumPoll in the same poll cycle.
var lastPollPersonIdentities map[string]kitsuPersonIdentity

// forumAssigneeIdentity returns the name/email of an assignee, if the person
// was fetched in this poll.
func forumAssigneeIdentity(kitsuPersonID string) (string, string) {
	identity, ok := lastPollPersonIdentities[kitsuPersonID]
	if !ok {
		return "", ""
	}
	return identity.name, identity.email
}

// forumAssigneeNames resolves the footer 担当 names of a task's assignees from
// the last poll's Kitsu identities. Unresolved IDs are skipped: an ID appearing
// in the footer as raw text helps nobody.
func forumAssigneeNames(projectID string, personIDs []string) []string {
	names := make([]string, 0, len(personIDs))
	for _, id := range personIDs {
		name, _ := forumAssigneeIdentity(id)
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// projectTasksResult is one project's task read.
type projectTasksResult struct {
	tasks   []kitsu.Task
	err     error
	elapsed time.Duration
}

// fetchProjectTasks reads every project's tasks with at most `limit` reads in
// flight (conf.Threads, minimum 1). Results keep the projects' order so the
// poll stays deterministic.
func fetchProjectTasks(ctx context.Context, projects []kitsu.Project, limit int) []projectTasksResult {
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
			read, err := kitsu.GetProjectTasksWithContext(ctx, projects[i].ID)
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
func loadTaskComments(ctx context.Context, task kitsu.Task, previous, next map[string]cachedTaskComments, nextMu *sync.Mutex) (comments kitsu.Comments, ok bool) {
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
	fetched, err := kitsu.GetTaskCommentsWithContext(ctx, task.ID)
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
	runOnePollContext(context.Background(), conf, db)
}

// runOnePollContext runs one poll with a cancellation root. Cancelling ctx
// aborts the Kitsu reads in flight and leaves the remaining work undone.
func runOnePollContext(ctx context.Context, conf config.Config, db *gorm.DB) {
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

	// Poll Kitsu with runtime credentials from DB/env/conf priority.
	kitsuResponse, baselineResponse, err := makeKitsuResponse(ctx, conf)
	if err != nil {
		setup.Stats.RecordPollError(err.Error())
		slog.Error("Kitsu poll failed", "error_class", "kitsu_fetch_failed", "err", err)
		return
	}
	if conf.Log {
		slog.Info("Done MakeKitsuResponse")
	}
	taskCount := len(kitsuResponse)
	runForumPoll(baselineResponse, conf, db)
	// Daily 09:00 JST digest DMs to configured reviewers.
	runMorningDigestIfDue(conf, db)
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
	// The outbox claim query filters on next_attempt_at (not not_before) and
	// orders rows within a task. Drop the old indexes so AutoMigrate rebuilds
	// them as (state, next_attempt_at, id) and (task_id, state, id).
	for _, idx := range []string{"idx_forum_outbox_due", "idx_forum_outbox_task"} {
		if err := db.Exec("DROP INDEX IF EXISTS " + idx).Error; err != nil {
			return fmt.Errorf("remove legacy forum outbox index: %w", err)
		}
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
		&model.ProductionHandoffConfig{},
		&model.ProductionHandoffProductType{},
		&model.ProductionHandoffRoute{},
		&model.HandoffEvent{},
		&model.ForumBaseline{},
		&model.ForumTask{},
		&model.ForumTransition{},
		&model.ForumOutbox{},
		&model.ForumFlapState{},
		&model.ForumDMChannel{},
	); err != nil {
		return fmt.Errorf("migrate application schema: %w", err)
	}
	// Forum outbox rows claimed by a previous process are re-queued rather than
	// resolved by lookup: a duplicate post is worse than a late one.
	if err := forum.ReconcileSendingRows(db); err != nil {
		return fmt.Errorf("reconcile forum outbox: %w", err)
	}
	// Legacy rows written with "" instead of NULL collide on the thread unique
	// index; move them once at startup.
	if err := model.MigrateLegacyEmptyForumThreadIDs(db); err != nil {
		return fmt.Errorf("migrate forum thread ids: %w", err)
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

// logLevelForEnv maps APP_ENV to the log level: development opts into DEBUG,
// everything else (production, staging, unset) stays at INFO.
func logLevelForEnv(appEnv string) slog.Level {
	if appEnv == "development" {
		return slog.DebugLevel
	}
	return slog.InfoLevel
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

	// Log level: default INFO, APP_ENV=development opts into DEBUG. The file
	// handler needs the level explicitly: gookit/slog applies SetLogLevel only
	// to the std handler, a handler built with AllLevels ignores it.
	appEnv := os.Getenv("APP_ENV")
	logLevel := logLevelForEnv(appEnv)

	rotateW, err := rotatefile.NewWriter(&rotatefile.Config{
		Filepath:   "./logs/all-levels.log",
		MaxSize:    10 * rotatefile.OneMByte,
		BackupNum:  5,
		FilePerm:   rotatefile.DefaultFilePerm,
		RenameFunc: rotatefile.DefaultFilenameFn,
	})
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	// The rotate writer is intentionally not closed: gookit/slog v0.5.5's
	// rotatefile races Writer.Close against the clean-up goroutine it starts
	// on the first write (both touch the stop-channel fields without
	// synchronization). Writes go straight to the file, so process exit
	// loses nothing.

	redactingWriter := logutil.NewRedactingWriter(rotateW)
	h1 := handler.NewSimpleHandler(redactingWriter, logLevel)
	slog.PushHandler(h1)
	slog.SetLogLevel(logLevel)

	if logLevel == slog.DebugLevel {
		slog.Debug("App started", "env", appEnv, "log_level", "DEBUG")
	} else {
		slog.Info("App started", "env", appEnv, "log_level", "INFO")
	}

	start := time.Now()

	conf := config.Read()
	// Make the configured conf.toml host available to the UI/runtime fallback
	// path without overriding an explicitly saved SQLite endpoint.
	if strings.TrimSpace(os.Getenv("KITSU_HOSTNAME")) == "" && strings.TrimSpace(conf.Kitsu.Hostname) != "" {
		setup.SetAppManagedKitsuHostname(conf.Kitsu.Hostname)
	}

	// Kitsu 応答本文の上限を反映（F1）。未設定（0）なら既定の 64 MiB のまま。
	if conf.Kitsu.MaxResponseMB > 0 {
		if err := request.ConfigureMaxResponseBodyLimit(int64(conf.Kitsu.MaxResponseMB) << 20); err != nil {
			slog.Warn("config validation: kitsu.maxResponseMB ignored", "error_class", "invalid_max_response_mb", "err", err)
		}
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

	// Forum-mode Productions are validated once at startup: a wrong forum or tag
	// ID would otherwise only surface as a failed post much later.
	safego.Go("forum target validation", func() {
		botToken, fallbackGuildID, _ := getDiscordSettings(db, conf)
		for _, project := range model.ListProjects(db) {
			if project.ValidationOnly || project.KitsuProjectID == "" {
				continue
			}
			settings, ok, err := model.LoadProductionForumSettings(db, project.KitsuProjectID)
			if err != nil || !ok {
				continue
			}
			guildID := strings.TrimSpace(settings.Config.GuildID)
			if guildID == "" {
				guildID = model.ResolveProjectGuildID(db, project.KitsuProjectID, fallbackGuildID)
			}
			problems := forum.ValidateForumTargets(db, project.KitsuProjectID, guildID, setup.ForumChannelLister(), botToken)
			problems = append(problems, setup.CheckForumPermissions(settings, botToken)...)
			for _, problem := range problems {
				slog.Warn("Forum target validation failed: "+problem, "event", "forum_target_validation_failed", "production_id", project.KitsuProjectID)
			}
		}
	})

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
		safego.Go("kitsu reconnect poll", func() { runtime.runWhenReady(func() { runOnePollContext(pollCtx, conf, db) }) })
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
		mux.HandleFunc(prefix+"/api/setup/test-kitsu", setup.RequireSession(setup.RejectValidationMutation(setup.TestKitsuHandler(db, onRuntimeConfigured))))
		mux.HandleFunc(prefix+"/api/setup/test-discord", setup.RequireSession(setup.RejectValidationMutation(setup.TestDiscordHandler(db))))
		// Test Notification is a synthetic Discord-only verification. It must remain
		// available while Kitsu runtime polling is disconnected; the handler still
		// verifies the selected project destination and uses the configured Discord
		// credentials, without changing normal routing or sending Kitsu data.
		mux.HandleFunc(prefix+"/api/setup/test-notification", setup.RequireSession(setup.RejectValidationMutation(setup.TestNotificationHandler(db, setupCredsFunc))))
		mux.HandleFunc(prefix+"/api/setup/mapping", setup.RequireSession(setup.RuntimeReadyRequired(runtime.ready, setup.MappingStateHandler(db))))
		mux.HandleFunc(prefix+"/api/setup/mapping/users", setup.RequireSession(setup.RuntimeReadyRequired(runtime.ready, setup.RejectValidationMutation(setup.SaveUserMappingHandler(db)))))
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
			setup.ForumSettingsHandler(db, conf.Notification.CommentNotifyAssignee, func() (string, string) { return botToken, fallbackGuildID })(w, r)
		})))
		// The handoff mapping is local per-Production configuration as well: it is
		// stored by KitsuSync and needs neither Kitsu nor Discord to save. An
		// unconfigured Production has no rows, so the handoff path stays inactive.
		mux.HandleFunc(prefix+"/admin/handoff", setup.RequireSession(setup.RejectValidationMutation(setup.HandoffSettingsHandler(db))))
		// Manual refetch of a guild's channel list, bypassing the admin pages'
		// cache. A plain GET link (PRG): the operator explicitly asked to wait
		// for Discord's answer, so botDo's retries run synchronously here.
		mux.HandleFunc("GET "+prefix+"/admin/refresh-discord-channels", setup.RequireSession(setup.RefreshDiscordChannelsHandler(func() string {
			botToken, _, _ := getDiscordSettings(db, conf)
			return botToken
		})))
		mux.HandleFunc(prefix+"/admin/audit", setup.RequireSession(setup.AuditLogHandler(db)))
		mux.HandleFunc(prefix+"/admin/health", setup.RequireSession(setup.HealthHandler(db, conf.PollInterval())))
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
		trackPoll(func() {
			runtime.runWhenReady(func() {
				runOnePollContext(pollCtx, conf, db)
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
		runOnePollContext(pollCtx, conf, db)
	})
	c.AddFunc("@every 1h", func() { redactExpiredNotificationPayloads(conf, db, time.Now()) })
	c.AddFunc("@every 1h", func() {
		if deleted := model.PurgeOldForumOutbox(db, model.ForumOutboxRetentionDays); deleted > 0 {
			slog.Info("forum outbox purge", "deleted_rows", deleted)
		}
	})
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

	// Abort in-flight Kitsu reads first: an un-cancelled request can otherwise
	// outlive the 10s grace periods and touch SQLite after it is closed.
	stopPolling()

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

	pollWork.Wait()

	if err := sqlDB.Close(); err != nil {
		slog.Warn("SQLite close failed", "err", err)
	} else {
		slog.Info("SQLite connection closed cleanly")
	}
}

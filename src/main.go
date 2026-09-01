package main

import (
	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/setup"
	"app/src/utils/config"
	logutil "app/src/utils/log"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"

	"log"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
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

	tasks, err := kitsu.GetTasksWithError()
	if err != nil {
		return nil, fmt.Errorf("fetch tasks: %w", err)
	}
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

	projects, err := kitsu.GetProjectsWithError()
	if err != nil {
		return nil, fmt.Errorf("fetch projects: %w", err)
	}
	if conf.Log {
		slog.Info("Got projects: " + strconv.Itoa(len(projects.Each)))
	}

	taskTypes, err := kitsu.GetTaskTypesWithError()
	if err != nil {
		return nil, fmt.Errorf("fetch task types: %w", err)
	}
	if conf.Log {
		slog.Info("Got taskTypes: " + strconv.Itoa(len(taskTypes.Each)))
	}

	persons, err := kitsu.GetPersonsWithError()
	if err != nil {
		return nil, fmt.Errorf("fetch persons: %w", err)
	}
	if conf.Log {
		slog.Info("Got persons: " + strconv.Itoa(len(persons.Each)))
	}

	var comments kitsu.Comments
	if conf.Kitsu.SkipComments == false {
		comments, err = kitsu.GetCommentsWithError()
		if err != nil {
			return nil, fmt.Errorf("fetch comments: %w", err)
		}
		if conf.Log {
			slog.Info("Got comments: " + strconv.Itoa(len(comments.Each)))
		}
	}

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
				var taskComments kitsu.Comments
				for _, elem := range comments.Each {
					if elem.ObjectID == tasks.Each[i].ID {
						taskComments.Each = append(taskComments.Each, elem)
					}
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

	if conf.Log {
		log.Printf("Done secondary loop in %s", time.Since(start))
	}

	return out, nil
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

		dbResult := model.FindTask(db, data[i].Task.ID)

		data[i].PreviousStatusName = dbResult.TaskStatus

		if len(dbResult.TaskID) > 0 {
			statusChanged := dbResult.TaskStatus != data[i].TaskStatus.TaskStatus.ShortName
			timestampChanged := dbResult.TaskUpdatedAt != data[i].Task.Task.UpdatedAt
			commentChanged := dbResult.CommentUpdatedAt != data[i].LatestComment.Comment.UpdatedAt

			if statusChanged || timestampChanged || commentChanged {
				// Mark notifications that only changed comment content. Persisting
				// the observation is deferred until routing and delivery are known.
				data[i].IsCommentOnly = commentChanged && !statusChanged && !timestampChanged
			} else {
				continue
			}
		}

		if conf.SilentUpdateDB {
			if conf.Log {
				log.Printf("Ignoring message\n")
			}
			persistTaskObservation(db, data[i])
			continue
		}
		// StatusFilter: only notify on WFA, RETAKE, DONE.
		currentStatus := data[i].TaskStatus.TaskStatus.ShortName
		// Treat "none" status as an assign notification when enabled.
		if strings.EqualFold(currentStatus, "none") {
			if !conf.Notification.NotifyOnAssign {
				persistTaskObservation(db, data[i])
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
		if !plan.ShouldSend {
			recordProductionRoutingSkip(db, payload, plan.SkipReason)
			stats.Dropped++
			continue
		}
		webhooks[plan.DestinationID] = plan.WebhookURL
		routes[plan.DestinationID] = append(routes[plan.DestinationID], payload)
		if labels[plan.DestinationID] == nil {
			labels[plan.DestinationID] = make(map[string]struct{})
		}
		labels[plan.DestinationID][fmt.Sprintf("productionID=%s taskTypeID=%s", payload.Project.ID, payload.TaskType.ID)] = struct{}{}
	}
	for destinationID, payloads := range routes {
		routeLabels := labelsFromSet(labels[destinationID])
		stats.DBRouted += len(payloads)
		logRouteDispatch("production.task_type_id", routeLabels, payloads, webhooks[destinationID] != "")
		notificationDispatch(payloads, conf, webhooks[destinationID], db, "production.task_type_id", routeLabels)
	}

	if len(data) > 0 && (stats.DBRouted > 0 || stats.Dropped > 0) {
		slog.Info("Notification routing summary",
			"incomingTasks", len(data),
			"dbRouted", stats.DBRouted,
			"dropped", stats.Dropped)
	}
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

	// Cache previous message/thread state for edit/reply behavior.
	previousMessageIDs := make(map[string]string)
	previousWebhookURLs := make(map[string]string)
	previousThreadIDs := make(map[string]string)
	projectNotificationLanguages := make(map[string]string)
	for _, elem := range data {
		dbResult := model.FindTask(db, elem.Task.ID)
		if dbResult.DiscordMessageID != "" {
			previousMessageIDs[elem.Task.ID] = dbResult.DiscordMessageID
			previousWebhookURLs[elem.Task.ID] = dbResult.WebhookURL
		}
		if dbResult.DiscordThreadID != "" {
			previousThreadIDs[elem.Task.ID] = dbResult.DiscordThreadID
		}
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
				log.Printf("Sending bunch of messages: " + strconv.Itoa(len(payload)))
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
					DiscordMsgID: res.MessageID,
					WebhookURL:   webhookURL,
					Success:      res.MessageID != "",
					ErrorMessage: res.FailureCategory,
					ActorKind:    actorKind,
					ActorID:      actorID,
					ActorName:    actorName,
				})
				if res.MessageID != "" {
					sentCount++
					model.UpdateTaskWithDiscord(
						db,
						taskID,
						task.Task.Task.UpdatedAt,
						task.TaskStatus.TaskStatus.ShortName,
						task.LatestComment.Comment.ID,
						task.LatestComment.Comment.UpdatedAt,
						res.MessageID,
						webhookURL,
						res.ThreadID,
					)
				} else {
					failedCount++
					if !res.Retryable || res.Unknown {
						// Permanent or unknown outcomes are not retried forever and
						// never clear a previously delivered message reference.
						model.MarkTaskObserved(db, taskID, task.Task.UpdatedAt, task.TaskStatus.TaskStatus.ShortName, task.LatestComment.Comment.ID, task.LatestComment.Comment.UpdatedAt)
					}
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
		os.Setenv("KITSU_HOSTNAME", conf.Kitsu.Hostname)
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
	botToken = strings.TrimSpace(model.GetSetting(db, setup.RuntimeDiscordBotTokenKey))
	if botToken == "" {
		botToken = os.Getenv("DISCORD_BOT_TOKEN")
	}
	if botToken == "" {
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

func persistTaskObservation(db *gorm.DB, payload kitsu.MessagePayload) {
	model.MarkTaskObserved(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.ShortName, payload.LatestComment.Comment.ID, payload.LatestComment.Comment.UpdatedAt)
}

// auditActorFromPayload uses only actor data tied to the event we can prove.
// The latest comment author is an actor for comment-only changes. A task
// status payload does not expose its changer, so it remains unknown rather
// than being guessed from an assignee or an unrelated comment.
func auditActorFromPayload(payload kitsu.MessagePayload) (kind, id, name string) {
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
	FilterTasks(kitsuResponse, conf, db)
	if conf.Log {
		slog.Info("Done FilterTasks")
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

func main() {
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
		os.Setenv("KITSU_HOSTNAME", conf.Kitsu.Hostname)
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
	// Remove the legacy single-column unique index before the composite migration.
	db.Exec("DROP INDEX IF EXISTS idx_checker_maps_task_type")
	db.AutoMigrate(
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
		&model.ProjectSetting{},
	)
	model.PurgeLegacySensitiveData(db)

	setup.SeedConfigIfFixture(db, conf)
	if persistedDiscordToken := strings.TrimSpace(model.GetSetting(db, setup.RuntimeDiscordBotTokenKey)); persistedDiscordToken != "" {
		os.Setenv("DISCORD_BOT_TOKEN", persistedDiscordToken)
		slog.Debug("Discord runtime token loaded",
			"token_present", true,
			"token_fingerprint", setup.DiscordBotTokenFingerprint(persistedDiscordToken),
		)
	}
	_, seedGuildID, _ := getDiscordSettings(db, conf)
	model.SeedProjectGuildFallback(db, seedGuildID)

	discord.UserMapResolver = func(projectID, kitsuName, kitsuEmail string) string {
		return model.GetUserMapForProject(db, projectID, kitsuName, kitsuEmail)
	}
	discord.CheckerResolver = func(projectID, taskType string) []string {
		return model.GetCheckerForProject(db, projectID, taskType)
	}
	discord.GoogleDriveURLResolver = func(projectID string) string {
		return model.GetProjectStorageURL(db, projectID)
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
		cron.DelayIfStillRunning(cron.DefaultLogger),
	))

	runtime := newRuntimeManager()
	healthReadinessProvider = func() readinessSnapshot {
		host, _, _ := getKitsuCreds(db, conf)
		botToken, fallbackGuildID, _ := getDiscordSettings(db, conf)
		discordValidated, routingReady := setup.ProjectDiscordReadiness(db, botToken, fallbackGuildID)
		slog.Debug("Project Discord readiness evaluated",
			"api_validated", discordValidated,
			"routing_ready", routingReady,
			"project_count", len(model.ListProjects(db)),
		)
		kitsuToken := setup.StoredRuntimeKitsuToken(db)
		kitsuConfigured := strings.TrimSpace(host) != "" && strings.TrimSpace(kitsuToken) != ""
		kitsuReady := runtime.ready()
		botConfigured := strings.TrimSpace(botToken) != ""
		overall := overallNotificationReadiness(kitsuReady, botConfigured, discordValidated, routingReady)
		return readinessSnapshot{
			KitsuConfigured:              kitsuConfigured,
			KitsuConnected:               kitsuReady,
			KitsuReady:                   kitsuReady,
			DiscordBotConfigured:         botConfigured,
			DiscordAPIValidated:          discordValidated,
			ProductionRoutingConfigured:  routingReady,
			OverallNotificationReadiness: overall,
		}
	}
	refreshRuntime := func() bool {
		hostname, _, _ := getKitsuCreds(db, conf)
		slog.Debug("Kitsu runtime credential availability",
			"hostname_present", strings.TrimSpace(hostname) != "",
			"bot_token_present", setup.StoredRuntimeKitsuToken(db) != "",
		)
		if token := setup.StoredRuntimeKitsuToken(db); token != "" {
			validationStarted := time.Now()
			validation := setup.ValidateKitsuBotToken(db, hostname, token, true)
			setup.Stats.RecordAPIObservation("kitsu", validationStarted, validation.Compatible(), validation.Classification)
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
				return runtime.authenticateToken(hostname, token)
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
	mux.HandleFunc("/health", healthHandler(runtime))

	onRuntimeConfigured := func() {
		slog.Debug("Kitsu reconnect attempted", "attempted", true)
		if !refreshRuntime() {
			slog.Warn("Kitsu reconnect result", "success", false)
			return
		}
		slog.Debug("Kitsu reconnect result", "success", true)
		go runtime.runWhenReady(func() { runOnePoll(conf, db) })
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

	loginHandler := func(w http.ResponseWriter, r *http.Request) {
		setup.LoginHandlerWithDiscovery(func() (string, string) {
			result := setup.DiscoverKitsuHost(db)
			return result.RuntimeHost, result.Source
		}, func(host string) {
			model.SetSetting(db, "kitsu.hostname", host)
		})(w, r)
	}
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/bot/login", loginHandler)
	mux.HandleFunc("/bot/", setup.BotRootHandler(runtime.ready))

	mux.HandleFunc("/logout", setup.LogoutHandler())
	mux.HandleFunc("/bot/logout", setup.LogoutHandler())

	setupReadOnlyRoute := setup.RequireSession(setup.ReadOnlyAuditRoute(runtime.ready, setupHandler))
	mux.HandleFunc("/setup", setupReadOnlyRoute)
	mux.HandleFunc("/bot/setup", setupReadOnlyRoute)

	// Setup diagnostic JSON API — registered under both root and /bot prefix.
	setupAPIRoutes := func(prefix string) {
		mux.HandleFunc(prefix+"/api/setup/status", setup.RequireSession(setup.SetupStatusHandler(
			db, conf.Kitsu.RequestInterval, setupCredsFunc,
		)))
		mux.HandleFunc(prefix+"/api/setup/observability", setup.RequireSession(setup.TelemetrySnapshotHandler()))
		mux.HandleFunc(prefix+"/api/setup/projects", setup.RequireSession(setup.ReadOnlyAuditRoute(runtime.ready, setup.ProjectsHandler(db))))
		mux.HandleFunc(prefix+"/api/setup/preview-project", setup.RequireSession(setup.ReadOnlyAuditPreviewRoute(runtime.ready, setup.PreviewProjectHandler(db, setupCredsFunc))))
		mux.HandleFunc(prefix+"/api/setup/apply-project", setup.RequireSession(setup.RuntimeReadyRequired(runtime.ready, setup.RejectValidationMutation(setup.ApplyProjectHandler(db, setupCredsFunc)))))
		mux.HandleFunc(prefix+"/api/setup/test-kitsu", setup.RequireSession(setup.TestKitsuHandler(db, onRuntimeConfigured)))
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
		mux.HandleFunc(prefix+"/admin/audit", setup.RequireSession(setup.AuditLogHandler(db)))
		mux.HandleFunc(prefix+"/admin/health", setup.RequireSession(setup.HealthHandler(db)))
	}
	registerAdminRoutes("")
	registerAdminRoutes("/bot")

	server := &http.Server{
		Addr:    ":8090",
		Handler: setup.RequestTrace(mux),
	}
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
		go runtime.runWhenReady(func() {
			runOnePoll(conf, db)
			if conf.Log {
				slog.Info("Done initial poll", "duration", time.Since(start).String())
			}
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
		setup.ObserveKitsuRuntime(hostname, kitsuToken)
		discordToken, _, _ := getDiscordSettings(db, conf)
		setup.ObserveDiscordRuntime(discordToken)
	})
	c.AddFunc("@every "+strconv.Itoa(conf.Kitsu.RequestInterval)+"m", func() {
		if !runtime.ready() && !refreshRuntime() {
			return
		}
		runtime.runWhenReady(func() { runOnePoll(conf, db) })
	})
	c.AddFunc("0 3 * * *", func() {
		deleted := model.PurgeOldAuditLogs(db, 90)
		if deleted > 0 {
			slog.Info("audit log purge", "deleted_rows", deleted)
		}
	})

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

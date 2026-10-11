package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"app/src/api/kitsu"
	"app/src/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type retryWebhookRecorder struct {
	mu         sync.Mutex
	postBodies [][]byte
}

func newRetryWebhook(t *testing.T, webhookID, guildID, channelID string) (*httptest.Server, *retryWebhookRecorder) {
	return newRetryWebhookWithFirstStatus(t, webhookID, guildID, channelID, true)
}

func newRetryWebhookWithFirstStatus(t *testing.T, webhookID, guildID, channelID string, rejectFirst bool) (*httptest.Server, *retryWebhookRecorder) {
	t.Helper()
	recorder := &retryWebhookRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":%q,"type":1,"guild_id":%q,"channel_id":%q}`, webhookID, guildID, channelID)
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read failed", http.StatusInternalServerError)
				return
			}
			recorder.mu.Lock()
			recorder.postBodies = append(recorder.postBodies, body)
			attempt := len(recorder.postBodies)
			recorder.mu.Unlock()
			if rejectFirst && attempt == 1 {
				w.Header().Set("Retry-After", "31")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"message-accepted"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	return server, recorder
}

func openRetryFileSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	return openRetryFileSQLiteAt(t, filepath.Join(t.TempDir(), "delivery-retry.db"))
}

func openRetryFileSQLiteAt(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+filepath.ToSlash(path)+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Project{}, &model.ProjectWebhook{}, &model.Task{}, &model.UserMap{}, &model.ProjectUserMap{},
		&model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.ProjectReviewerTarget{},
		&model.ProductionNotificationWriteLock{}, &model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{},
	); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func reopenRetryFileSQLite(t *testing.T, db *gorm.DB, path string) *gorm.DB {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	return openRetryFileSQLiteAt(t, path)
}

func setupRetryFileSQLite(t *testing.T, db *gorm.DB, webhookURL string) *model.Project {
	t.Helper()
	project := model.Project{KitsuProjectID: "prod-1", Name: "Synthetic Production", DiscordGuildID: "guild-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := model.ProjectWebhook{KitsuProjectID: project.KitsuProjectID, DiscordChannelID: "channel-1", WebhookURL: webhookURL}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProductionNotificationRoute{ProductionID: project.KitsuProjectID, TaskTypeID: "task-type-1", DestinationWebhookID: webhook.ID, DestinationChannelID: webhook.DiscordChannelID}).Error; err != nil {
		t.Fatal(err)
	}
	return &project
}

func retryMessagePayload() kitsu.MessagePayload {
	var elem kitsu.MessagePayload
	elem.Project.ID = "prod-1"
	elem.Project.Name = "Synthetic Production"
	elem.Task.ID = "task-1"
	elem.Task.UpdatedAt = "2026-10-11T00:00:00Z"
	elem.TaskType.ID = "task-type-1"
	elem.TaskType.Name = "Compositing"
	elem.TaskStatus.ShortName = "WFA"
	elem.LatestComment.Comment.ID = "comment-1"
	elem.LatestComment.Comment.UpdatedAt = "2026-10-11T00:00:00Z"
	return elem
}

func retryMentionsPayload(userID string) Payload {
	content := "Please review"
	users := []string(nil)
	if userID != "" {
		content = "<@" + userID + "> Please review"
		users = []string{userID}
	}
	return Payload{Content: content, AllowedMentions: &AllowedMentions{Users: users}}
}

func retryWebhookBodies(recorder *retryWebhookRecorder) [][]byte {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	copyOfBodies := make([][]byte, len(recorder.postBodies))
	for i := range recorder.postBodies {
		copyOfBodies[i] = append([]byte(nil), recorder.postBodies[i]...)
	}
	return copyOfBodies
}

func TestRetryableDeliveryUsesOnlyFreshRenderedIntent(t *testing.T) {
	t.Run("revoked global link cannot receive the persisted mention", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "delivery-retry.db")
		db := openRetryFileSQLiteAt(t, dbPath)
		server, recorder := newRetryWebhook(t, "webhook-1", "guild-1", "channel-1")
		setupRetryFileSQLite(t, db, server.URL+"/api/webhooks/webhook-1/synthetic-token")
		if err := db.Create(&model.UserMap{KitsuID: "person-1", DiscordGuildID: "guild-1", DiscordID: "discord-user-1"}).Error; err != nil {
			t.Fatal(err)
		}
		project := model.FindProjectByKitsuID(db, "prod-1")
		revision, err := model.ProductionNotificationRevisionWithError(db, project.ID, project.KitsuProjectID)
		if err != nil {
			t.Fatal(err)
		}
		elem := retryMessagePayload()
		first := sendDurableNotification(db, elem, retryMentionsPayload("discord-user-1"), server.URL+"/api/webhooks/webhook-1/synthetic-token", "", revision)
		if !first.Retryable || first.Unknown || len(retryWebhookBodies(recorder)) != 1 {
			t.Fatalf("first deterministic rejection was not recorded retryable: result=%+v posts=%d", first, len(retryWebhookBodies(recorder)))
		}
		db = reopenRetryFileSQLite(t, db, dbPath)
		if err := db.Where("kitsu_id = ?", "person-1").Delete(&model.UserMap{}).Error; err != nil {
			t.Fatal(err)
		}
		if got := model.ProductionNotificationRevision(db, project.ID, project.KitsuProjectID); got != revision {
			t.Fatalf("fixture unexpectedly changed the routing/reviewer revision: %q != %q", got, revision)
		}

		// A retry happens only after this fresh render; the old durable body must not be sent.
		fresh := sendDurableNotification(db, elem, retryMentionsPayload(""), server.URL+"/api/webhooks/webhook-1/synthetic-token", "", revision)
		if fresh.MessageID != "message-accepted" || !fresh.Durable || len(retryWebhookBodies(recorder)) != 2 {
			t.Fatalf("changed but freshly rendered intent should send current recipients: result=%+v posts=%d", fresh, len(retryWebhookBodies(recorder)))
		}
		var stored model.NotificationDeliveryEvent
		if err := db.Where("task_id = ?", "task-1").First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Disposition != model.DeliveryDelivered || stored.PayloadRedactedAt != nil {
			t.Fatalf("known-rejected intent did not converge to the freshly delivered event: disposition=%q", stored.Disposition)
		}
		bodies := retryWebhookBodies(recorder)
		var rejectedBody, sentBody Payload
		if err := json.Unmarshal(bodies[0], &rejectedBody); err != nil {
			t.Fatal(err)
		}
		if len(rejectedBody.AllowedMentions.Users) != 1 || rejectedBody.AllowedMentions.Users[0] != "discord-user-1" {
			t.Fatalf("fixture did not preserve the first rejected body: %+v", rejectedBody.AllowedMentions)
		}
		if err := json.Unmarshal(bodies[1], &sentBody); err != nil {
			t.Fatal(err)
		}
		if len(sentBody.AllowedMentions.Users) != 0 || strings.Contains(sentBody.Content, "discord-user-1") {
			t.Fatalf("retry delivered a stale revoked recipient: content=%q mentions=%+v", sentBody.Content, sentBody.AllowedMentions)
		}
	})

	t.Run("same freshly rendered intent remains safely replayable", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "delivery-retry.db")
		db := openRetryFileSQLiteAt(t, dbPath)
		server, recorder := newRetryWebhook(t, "webhook-1", "guild-1", "channel-1")
		setupRetryFileSQLite(t, db, server.URL+"/api/webhooks/webhook-1/synthetic-token")
		project := model.FindProjectByKitsuID(db, "prod-1")
		revision, err := model.ProductionNotificationRevisionWithError(db, project.ID, project.KitsuProjectID)
		if err != nil {
			t.Fatal(err)
		}
		elem := retryMessagePayload()
		payload := retryMentionsPayload("")
		first := sendDurableNotification(db, elem, payload, server.URL+"/api/webhooks/webhook-1/synthetic-token", "", revision)
		if !first.Retryable || len(retryWebhookBodies(recorder)) != 1 {
			t.Fatalf("first deterministic rejection was not recorded: result=%+v", first)
		}
		db = reopenRetryFileSQLite(t, db, dbPath)
		second := sendDurableNotification(db, elem, payload, server.URL+"/api/webhooks/webhook-1/synthetic-token", "", revision)
		if second.MessageID != "message-accepted" || !second.Durable || len(retryWebhookBodies(recorder)) != 2 {
			t.Fatalf("freshly re-rendered identical intent should replay safely: result=%+v posts=%d", second, len(retryWebhookBodies(recorder)))
		}
	})

	t.Run("reviewer payload and destination use the fresh authorized intent", func(t *testing.T) {
		for _, change := range []string{"reviewer", "payload", "destination"} {
			t.Run(change, func(t *testing.T) {
				dbPath := filepath.Join(t.TempDir(), "delivery-retry.db")
				db := openRetryFileSQLiteAt(t, dbPath)
				server1, recorder1 := newRetryWebhook(t, "webhook-1", "guild-1", "channel-1")
				project := setupRetryFileSQLite(t, db, server1.URL+"/api/webhooks/webhook-1/synthetic-token")
				server2, recorder2 := newRetryWebhookWithFirstStatus(t, "webhook-2", "guild-1", "channel-2", false)
				revision, err := model.ProductionNotificationRevisionWithError(db, project.ID, project.KitsuProjectID)
				if err != nil {
					t.Fatal(err)
				}
				elem := retryMessagePayload()
				initialPayload := retryMentionsPayload("reviewer-old")
				first := sendDurableNotification(db, elem, initialPayload, server1.URL+"/api/webhooks/webhook-1/synthetic-token", "", revision)
				if !first.Retryable || len(retryWebhookBodies(recorder1)) != 1 {
					t.Fatalf("first deterministic rejection was not recorded: result=%+v", first)
				}
				db = reopenRetryFileSQLite(t, db, dbPath)
				freshRevision := revision
				freshDestination := server1.URL + "/api/webhooks/webhook-1/synthetic-token"
				freshPayload := retryMentionsPayload("reviewer-new")
				if change == "payload" {
					freshPayload = retryMentionsPayload("reviewer-current")
				}
				if change == "reviewer" {
					if err := db.Create(&model.ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: model.ReviewerTargetUser, DiscordID: "reviewer-new"}).Error; err != nil {
						t.Fatal(err)
					}
					freshRevision, err = model.ProductionNotificationRevisionWithError(db, project.ID, project.KitsuProjectID)
					if err != nil || freshRevision == revision {
						t.Fatalf("reviewer change did not create a new revision: old=%q new=%q err=%v", revision, freshRevision, err)
					}
				} else if change == "destination" {
					if err := db.Model(&model.ProjectWebhook{}).Where("id = ?", 1).Update("discord_channel_id", "channel-2").Error; err != nil {
						t.Fatal(err)
					}
					if err := db.Model(&model.ProductionNotificationRoute{}).Where("production_id = ? AND task_type_id = ?", project.KitsuProjectID, "task-type-1").Updates(map[string]any{"destination_webhook_id": 2, "destination_channel_id": "channel-2"}).Error; err != nil {
						t.Fatal(err)
					}
					if err := db.Create(&model.ProjectWebhook{ID: 2, KitsuProjectID: project.KitsuProjectID, DiscordChannelID: "channel-2", WebhookURL: server2.URL + "/api/webhooks/webhook-2/synthetic-token"}).Error; err != nil {
						t.Fatal(err)
					}
					freshDestination = server2.URL + "/api/webhooks/webhook-2/synthetic-token"
					freshRevision, err = model.ProductionNotificationRevisionWithError(db, project.ID, project.KitsuProjectID)
					if err != nil || freshRevision == revision {
						t.Fatalf("destination change did not create a new revision: old=%q new=%q err=%v", revision, freshRevision, err)
					}
				}
				fresh := sendDurableNotification(db, elem, freshPayload, freshDestination, "", freshRevision)
				expectedOldPosts, expectedNewPosts := 2, 0
				if change == "destination" {
					expectedOldPosts, expectedNewPosts = 1, 1
				}
				if !fresh.Durable || fresh.MessageID != "message-accepted" || len(retryWebhookBodies(recorder1)) != expectedOldPosts || len(retryWebhookBodies(recorder2)) != expectedNewPosts {
					var debug model.NotificationDeliveryEvent
					_ = db.Where("task_id = ?", "task-1").First(&debug).Error
					t.Fatalf("fresh %s intent did not converge to current destination: result=%+v row=%+v oldPosts=%d newPosts=%d", change, fresh, debug, len(retryWebhookBodies(recorder1)), len(retryWebhookBodies(recorder2)))
				}
				var stored model.NotificationDeliveryEvent
				if err := db.Where("task_id = ?", "task-1").First(&stored).Error; err != nil {
					t.Fatal(err)
				}
				if stored.Disposition != model.DeliveryDelivered || stored.DestinationChannelID != map[bool]string{true: "channel-2", false: "channel-1"}[change == "destination"] {
					t.Fatalf("fresh %s intent did not replace only the known-rejected intent: row=%+v", change, stored)
				}
				var deliveredBody Payload
				bodies := retryWebhookBodies(map[bool]*retryWebhookRecorder{true: recorder2, false: recorder1}[change == "destination"])
				if err := json.Unmarshal(bodies[len(bodies)-1], &deliveredBody); err != nil {
					t.Fatal(err)
				}
				if len(deliveredBody.AllowedMentions.Users) != 1 || deliveredBody.AllowedMentions.Users[0] != freshPayload.AllowedMentions.Users[0] {
					t.Fatalf("delivery used stale Reviewer payload: %+v", deliveredBody.AllowedMentions)
				}
			})
		}
	})
}

func TestNotificationRetryFileFixtureUsesWALAndForeignKeys(t *testing.T) {
	db := openRetryFileSQLite(t)
	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil || journalMode != "wal" {
		t.Fatalf("expected file-backed WAL fixture, got mode=%q err=%v", journalMode, err)
	}
	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil || foreignKeys != 1 {
		t.Fatalf("expected foreign keys enabled, got %d err=%v", foreignKeys, err)
	}
}

func TestPreparedRetryCannotSendRefreshedIntentToPreviouslyValidatedWebhook(t *testing.T) {
	db := openRetryFileSQLite(t)
	server2, _ := newRetryWebhookWithFirstStatus(t, "webhook-2", "guild-1", "channel-2", false)
	var callbackErr error
	var postCount int
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if callbackErr == nil {
				callbackErr = db.Create(&model.ProjectWebhook{ID: 2, KitsuProjectID: "prod-1", DiscordChannelID: "channel-2", WebhookURL: server2.URL + "/api/webhooks/webhook-2/synthetic-token"}).Error
				if callbackErr == nil {
					callbackErr = db.Model(&model.ProductionNotificationRoute{}).Where("production_id = ? AND task_type_id = ?", "prod-1", "task-type-1").Updates(map[string]any{"destination_webhook_id": 2, "destination_channel_id": "channel-2"}).Error
				}
				var revision string
				if callbackErr == nil {
					project := model.FindProjectByKitsuID(db, "prod-1")
					revision, callbackErr = model.ProductionNotificationRevisionWithError(db, project.ID, project.KitsuProjectID)
				}
				if callbackErr == nil {
					fresh := retryMessagePayload()
					intent := model.NotificationDeliveryIntent{
						TaskID: fresh.Task.ID, ProductionID: fresh.Project.ID, TaskTypeID: fresh.TaskType.ID,
						TaskStatus: fresh.TaskStatus.ShortName, TaskUpdatedAt: fresh.Task.UpdatedAt,
						CommentID: fresh.LatestComment.Comment.ID, CommentUpdatedAt: fresh.LatestComment.Comment.UpdatedAt,
						Payload:      []byte(`{"content":"refreshed for destination B","allowed_mentions":{"parse":[]}}`),
						WebhookRowID: 2, GuildID: "guild-1", ChannelID: "channel-2", DiscordWebhookID: "webhook-2", NotificationRevision: revision,
					}
					_, callbackErr = model.PrepareNotificationDelivery(db, intent)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":"webhook-1","type":1,"guild_id":"guild-1","channel_id":"channel-1"}`)
		case http.MethodPost:
			postCount++
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":"unexpected-message"}`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server1.Close)
	setupRetryFileSQLite(t, db, server1.URL+"/api/webhooks/webhook-1/synthetic-token")
	project := model.FindProjectByKitsuID(db, "prod-1")
	revision, err := model.ProductionNotificationRevisionWithError(db, project.ID, project.KitsuProjectID)
	if err != nil {
		t.Fatal(err)
	}
	elem := retryMessagePayload()
	intent := model.NotificationDeliveryIntent{
		TaskID: elem.Task.ID, ProductionID: elem.Project.ID, TaskTypeID: elem.TaskType.ID,
		TaskStatus: elem.TaskStatus.ShortName, TaskUpdatedAt: elem.Task.UpdatedAt,
		CommentID: elem.LatestComment.Comment.ID, CommentUpdatedAt: elem.LatestComment.Comment.UpdatedAt,
		Payload:      []byte(`{"content":"original for destination A","allowed_mentions":{"parse":[]}}`),
		WebhookRowID: 1, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1", NotificationRevision: revision,
	}
	event, err := model.PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	attempt, started, err := model.BeginNotificationDelivery(db, event.EventID, time.Now().UTC())
	if err != nil || !started {
		t.Fatalf("begin initial known-rejection attempt: started=%v err=%v", started, err)
	}
	if err := model.SetNotificationDeliveryDisposition(db, *attempt, model.DeliveryRetryable, "rate_limited"); err != nil {
		t.Fatal(err)
	}

	result := dispatchPreparedNotification(db, *event, server1.URL+"/api/webhooks/webhook-1/synthetic-token")
	if callbackErr != nil {
		t.Fatalf("concurrent fresh-intent fixture failed: %v", callbackErr)
	}
	if result.MessageID != "" || postCount != 0 {
		t.Fatalf("refreshed destination B payload must not be sent to previously validated webhook A: result=%+v posts-to-A=%d", result, postCount)
	}
	var stored model.NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil || stored.DestinationChannelID != "channel-2" || stored.PayloadJSON != `{"content":"refreshed for destination B","allowed_mentions":{"parse":[]}}` || stored.Disposition != model.DeliveryPending {
		t.Fatalf("concurrent refresh should remain pending for a fresh destination-B dispatch: row=%+v err=%v", stored, err)
	}
}

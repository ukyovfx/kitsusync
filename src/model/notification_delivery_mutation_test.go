package model

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"gorm.io/gorm"
)

func TestAcquireProductionNotificationWriteLockGuardsAndRevisionMonotonicity(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	if err := AcquireProductionNotificationWriteLock(nil, "production-1"); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("acquire without a database must fail closed: %v", err)
	}
	if err := AcquireProductionNotificationWriteLock(db, "   "); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("acquire for a blank Production must fail closed: %v", err)
	}
	var count int64
	if err := db.Model(&ProductionNotificationWriteLock{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected acquisitions must not leave a lock row: count=%d err=%v", count, err)
	}
	for revision := 1; revision <= 3; revision++ {
		if err := AcquireProductionNotificationWriteLock(db, "production-1"); err != nil {
			t.Fatal(err)
		}
		var lock ProductionNotificationWriteLock
		if err := db.Where("production_id = ?", "production-1").First(&lock).Error; err != nil || lock.Revision != uint64(revision) {
			t.Fatalf("write lock revision must advance once per acquisition: want=%d lock=%+v err=%v", revision, lock, err)
		}
	}
	if err := db.Model(&ProductionNotificationWriteLock{}).Where("production_id = ?", "production-1").Updates(map[string]any{
		"mutation_state": NotificationMutationReordering, "mutation_token": "tok-1",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := AcquireProductionNotificationWriteLock(db, "production-1"); err != nil {
		t.Fatal(err)
	}
	var preserved ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", "production-1").First(&preserved).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.MutationState != NotificationMutationReordering || preserved.MutationToken != "tok-1" {
		t.Fatalf("acquiring the write lock must never reset an active mutation fence: %+v", preserved)
	}
	if preserved.Revision != 4 {
		t.Fatalf("acquiring over an existing fence must still advance the revision: %+v", preserved)
	}
	if err := AcquireProductionNotificationWriteLock(db, "production-2"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&ProductionNotificationWriteLock{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("each Production must own exactly one write lock row: count=%d err=%v", count, err)
	}
}

func TestAcquireProductionNotificationWriteLockRejectsLostRevisionUpdate(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	// The revision bump is what hands the fence out, so an update that reaches no row
	// must fail closed rather than silently succeed. Dropping the row right after the
	// upsert insert reproduces that lost-update shape without touching product code.
	if err := db.Exec("CREATE TRIGGER drop_lock_row_after_insert AFTER INSERT ON production_notification_write_locks BEGIN DELETE FROM production_notification_write_locks WHERE id = new.id; END").Error; err != nil {
		t.Fatal(err)
	}
	if err := AcquireProductionNotificationWriteLock(db, "production-1"); !errors.Is(err, ErrProductionNotificationRevisionConflict) {
		t.Fatalf("a revision update that affected no row must be reported as a conflict: %v", err)
	}
	var count int64
	if err := db.Model(&ProductionNotificationWriteLock{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("a lost revision update must not leave a half-acquired lock: count=%d err=%v", count, err)
	}
}

func TestAcquireProductionNotificationWriteLockSerializesConcurrentRevisions(t *testing.T) {
	db := notificationDeliveryFileDB(t, filepath.Join(t.TempDir(), "write-lock-race.db"))
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	const workersPerProduction = 4
	var wg sync.WaitGroup
	failures := make(chan error, workersPerProduction*2)
	for _, productionID := range []string{"production-1", "production-2"} {
		for worker := 0; worker < workersPerProduction; worker++ {
			wg.Add(1)
			go func(productionID string) {
				defer wg.Done()
				if err := AcquireProductionNotificationWriteLock(db, productionID); err != nil {
					failures <- err
				}
			}(productionID)
		}
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatalf("concurrent acquisition failed: %v", err)
	}
	var locks []ProductionNotificationWriteLock
	if err := db.Order("production_id asc").Find(&locks).Error; err != nil {
		t.Fatal(err)
	}
	if len(locks) != 2 {
		t.Fatalf("concurrent acquisitions must converge on one row per Production: %+v", locks)
	}
	for _, lock := range locks {
		if lock.Revision != workersPerProduction {
			t.Fatalf("concurrent acquisitions must not share or lose a revision: %+v", locks)
		}
	}
}

func TestProductionDeletionMutationGuardsAndProductionIsolation(t *testing.T) {
	t.Run("invalid requests", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		if _, err := BeginProductionDeletionMutation(nil, "production-1"); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("nil database must fail closed: %v", err)
		}
		if _, err := BeginProductionDeletionMutation(db, "  "); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("blank Production must fail closed: %v", err)
		}
	})

	t.Run("unresolved deliveries block the barrier", func(t *testing.T) {
		for _, disposition := range []string{DeliveryPending, DeliveryRetryable, DeliveryInFlight, DeliveryUnknown} {
			t.Run(disposition, func(t *testing.T) {
				db := notificationDeliveryTestDB(t)
				notificationDeliveryFixture(t, db)
				event, err := PrepareNotificationDelivery(db, notificationIntent())
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&NotificationDeliveryEvent{}).Where("event_id = ?", event.EventID).Update("disposition", disposition).Error; err != nil {
					t.Fatal(err)
				}
				if _, err := BeginProductionDeletionMutation(db, "production-1"); !errors.Is(err, ErrNotificationDeliveryInFlight) {
					t.Fatalf("%s delivery must block a deletion barrier: %v", disposition, err)
				}
				var fenced int64
				if err := db.Model(&ProductionNotificationWriteLock{}).Where("mutation_state <> ''").Count(&fenced).Error; err != nil || fenced != 0 {
					t.Fatalf("a refused barrier must leave no durable fence behind: count=%d err=%v", fenced, err)
				}
			})
		}
	})

	t.Run("duplicate and unrecovered states are rejected", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		token, err := BeginProductionDeletionMutation(db, "production-1")
		if err != nil || token == "" {
			t.Fatalf("deletion barrier did not start: token=%q err=%v", token, err)
		}
		if _, err := BeginProductionDeletionMutation(db, "production-1"); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
			t.Fatalf("a second deletion barrier must not stack on the active one: %v", err)
		}
		var active ProductionNotificationWriteLock
		if err := db.Where("production_id = ?", "production-1").First(&active).Error; err != nil || active.MutationState != NotificationMutationDeleting || active.MutationToken != token {
			t.Fatalf("the refused duplicate must not disturb the active fence: lock=%+v err=%v", active, err)
		}
		for _, state := range []string{NotificationMutationReordering, NotificationMutationReorderHelp} {
			if err := db.Exec("UPDATE production_notification_write_locks SET mutation_state = ?, mutation_token = ? WHERE production_id = ?", state, "tok-9", "production-1").Error; err != nil {
				t.Fatal(err)
			}
			if _, err := BeginProductionDeletionMutation(db, "production-1"); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
				t.Fatalf("%s must be resolved by its own recovery path, not by a deletion barrier: %v", state, err)
			}
		}
		if err := db.Exec("UPDATE production_notification_write_locks SET mutation_state = ?, mutation_token = '' WHERE production_id = ?", NotificationMutationDeletionHelp, "production-1").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := BeginProductionDeletionMutation(db, "production-1"); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
			t.Fatalf("a durable help state without its operation token must not resume: %v", err)
		}
	})

	t.Run("a fence stays scoped to its own Production", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		notificationDeliveryFixture(t, db)
		intent := notificationIntent()
		secondWebhook, secondIntent := seedSecondProductionDestination(t, db, intent)
		if _, err := BeginProductionDeletionMutation(db, "production-1"); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareNotificationDelivery(db, intent); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
			t.Fatalf("the fenced Production must stop accepting intents: %v", err)
		}
		if intent.WebhookRowID != 1 || secondWebhook.ID == 0 || secondIntent.WebhookRowID != secondWebhook.ID {
			t.Fatalf("fixture did not build an independent second Production: webhook=%+v intent=%+v", secondWebhook, secondIntent)
		}
		if _, err := PrepareNotificationDelivery(db, secondIntent); err != nil {
			t.Fatalf("another Production must keep dispatching while its neighbour is fenced: %v", err)
		}
	})
}

func TestFinishProductionDeletionMutationGuardsAndRollback(t *testing.T) {
	t.Run("invalid requests", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		if err := FinishProductionDeletionMutation(nil, "production-1", "token-1", false); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("nil database must fail closed: %v", err)
		}
		if err := FinishProductionDeletionMutation(db, "  ", "token-1", false); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("blank Production must fail closed: %v", err)
		}
		if err := FinishProductionDeletionMutation(db, "production-1", "   ", false); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("blank operation token must fail closed: %v", err)
		}
	})

	t.Run("a mismatched token leaves the fence intact", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		token, err := BeginProductionDeletionMutation(db, "production-1")
		if err != nil {
			t.Fatal(err)
		}
		if err := FinishProductionDeletionMutation(db, "production-1", "wrong-token", false); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
			t.Fatalf("only the exact operation token may release the barrier: %v", err)
		}
		var lock ProductionNotificationWriteLock
		if err := db.Where("production_id = ?", "production-1").First(&lock).Error; err != nil || lock.MutationState != NotificationMutationDeleting || lock.MutationToken != token {
			t.Fatalf("the rejected release must not weaken the active fence: lock=%+v err=%v", lock, err)
		}
	})

	t.Run("finishing without an active deletion rolls back", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		if err := FinishProductionDeletionMutation(db, "production-1", "token-1", false); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
			t.Fatalf("a release without a matching barrier must fail closed: %v", err)
		}
		var count int64
		if err := db.Model(&ProductionNotificationWriteLock{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("the refused release must roll back the lock row it created: count=%d err=%v", count, err)
		}
		token, err := BeginProductionDeletionMutation(db, "production-1")
		if err != nil || token == "" || token == "token-1" {
			t.Fatalf("no residual fence may survive the rolled back release: token=%q err=%v", token, err)
		}
	})

	t.Run("only an active deleting operation can be finished", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		if err := db.Create(&ProductionNotificationWriteLock{ProductionID: "production-1", MutationState: NotificationMutationReordering, MutationToken: "tok-9"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := FinishProductionDeletionMutation(db, "production-1", "tok-9", false); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
			t.Fatalf("a reorder fence must not be released as a deletion: %v", err)
		}
		var lock ProductionNotificationWriteLock
		if err := db.Where("production_id = ?", "production-1").First(&lock).Error; err != nil || lock.MutationState != NotificationMutationReordering {
			t.Fatalf("the unrelated fence must stay untouched: lock=%+v err=%v", lock, err)
		}
	})

	t.Run("an interrupted deletion retains its token without a mutation time", func(t *testing.T) {
		db := notificationDeliveryTestDB(t)
		token, err := BeginProductionDeletionMutation(db, "production-1")
		if err != nil {
			t.Fatal(err)
		}
		if err := FinishProductionDeletionMutation(db, "production-1", token, true); err != nil {
			t.Fatal(err)
		}
		var lock ProductionNotificationWriteLock
		if err := db.Where("production_id = ?", "production-1").First(&lock).Error; err != nil {
			t.Fatal(err)
		}
		if lock.MutationState != NotificationMutationDeletionHelp || lock.MutationToken != token {
			t.Fatalf("an interrupted deletion must keep its durable operation identity: lock=%+v", lock)
		}
		if lock.MutationAt != nil {
			t.Fatalf("a quarantined deletion is no longer in progress and must not keep a mutation time: %+v", lock)
		}
	})
}

// seedSecondProductionDestination wires a fully independent second Production and
// returns its webhook row together with a matching delivery intent.
func seedSecondProductionDestination(t *testing.T, db *gorm.DB, template NotificationDeliveryIntent) (ProjectWebhook, NotificationDeliveryIntent) {
	t.Helper()
	if err := db.Create(&Project{KitsuProjectID: "production-2", DiscordGuildID: "guild-2"}).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: "production-2", DiscordChannelID: "channel-2", WebhookURL: "https://discord.com/api/webhooks/webhook-2/token"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-2", TaskTypeID: "task-type-2", DestinationWebhookID: webhook.ID, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	intent := template
	intent.TaskID = "task-2"
	intent.ProductionID = "production-2"
	intent.TaskTypeID = "task-type-2"
	intent.CommentID = "comment-2"
	intent.TaskUpdatedAt = "2026-10-10T12:30:00Z"
	intent.CommentUpdatedAt = "2026-10-10T12:30:00Z"
	intent.WebhookRowID = webhook.ID
	intent.GuildID = "guild-2"
	intent.ChannelID = "channel-2"
	intent.DiscordWebhookID = "webhook-2"
	return webhook, intent
}

package model

import (
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func notificationDeliveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &Task{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProjectReviewerTarget{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func notificationIntent() NotificationDeliveryIntent {
	return NotificationDeliveryIntent{
		TaskID: "task-1", ProductionID: "production-1", TaskTypeID: "task-type-1", TaskStatus: "WFA",
		TaskUpdatedAt: "2026-10-10T12:00:00Z", CommentID: "comment-1", CommentUpdatedAt: "2026-10-10T12:00:00Z",
		Payload:      []byte(`{"content":"safe synthetic message","allowed_mentions":{"parse":[]}}`),
		WebhookRowID: 1, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1",
	}
}

func notificationDeliveryFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	project := Project{KitsuProjectID: "production-1", DiscordGuildID: "guild-1"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{ID: 1, KitsuProjectID: "production-1", DiscordChannelID: "channel-1", WebhookURL: "https://discord.com/api/webhooks/webhook-1/token"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-1", DestinationWebhookID: 1, DestinationChannelID: "channel-1"}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestNotificationDeliveryIntentIsUniqueAndDestinationBound(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	intent := notificationIntent()
	first, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareNotificationDelivery(db, intent)
	if err != nil || second.EventID != first.EventID {
		t.Fatalf("same event should return one durable intent: second=%+v err=%v", second, err)
	}
	var count int64
	if err := db.Model(&NotificationDeliveryEvent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected exactly one unique event row, count=%d err=%v", count, err)
	}
	intent.ChannelID = "channel-2"
	if _, err := PrepareNotificationDelivery(db, intent); !errors.Is(err, ErrDeliveryIdentityConflict) {
		t.Fatalf("route change must not rebind an existing event, got %v", err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", first.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryUnknown {
		t.Fatalf("identity conflict must quarantine the original event: row=%+v err=%v", stored, err)
	}
}

func TestResolveNotificationDestinationRequiresExactCurrentRouteAndRejectsTombstone(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	identity, err := ResolveNotificationDestination(db, "production-1", "task-type-1", "https://discord.com/api/webhooks/webhook-1/token", "webhook-1")
	if err != nil || identity.GuildID != "guild-1" || identity.ChannelID != "channel-1" || identity.WebhookID != "webhook-1" {
		t.Fatalf("expected exact current destination identity, got %+v err=%v", identity, err)
	}
	if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", "https://discord.com/api/webhooks/other/token", "other"); err == nil {
		t.Fatal("destination resolver accepted a webhook URL not bound to the current route")
	}
	secondWebhook := ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-2", WebhookURL: "https://discord.com/api/webhooks/webhook-2/token"}
	if err := db.Create(&secondWebhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-2", DestinationWebhookID: secondWebhook.ID, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := BeginManagedChannelDeletion(db, "production-1", 1, "guild-1", "channel-1", "webhook-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveNotificationDestination(db, "production-1", "task-type-1", "https://discord.com/api/webhooks/webhook-1/token", "webhook-1"); !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("tombstoned destination remained dispatchable: %v", err)
	}
}

func TestExpiredInFlightDeliveryIsQuarantinedAndNeverRetried(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	intent, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	started, ok, err := BeginNotificationDelivery(db, intent.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("initial attempt did not start: ok=%v err=%v", ok, err)
	}
	if _, ok, err := BeginNotificationDelivery(db, intent.EventID, time.Now().Add(3*time.Minute)); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("expired attempt must be quarantined rather than resent: ok=%v err=%v", ok, err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", started.Event.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryUnknown {
		t.Fatalf("expired in-flight state is not unknown: row=%+v err=%v", stored, err)
	}
}

func TestInFlightDeliveryBlocksConcurrentReviewerRouteAndChannelMutations(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	revision, err := ProductionNotificationRevisionWithError(db, project.ID, "production-1")
	if err != nil {
		t.Fatal(err)
	}
	intent := notificationIntent()
	intent.NotificationRevision = revision
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now()); err != nil || !ok {
		t.Fatalf("dispatch claim failed: ok=%v err=%v", ok, err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-type-1", "Synthetic", ReviewerTargetUser, "123456789012345678"); !errors.Is(err, ErrNotificationDeliveryInFlight) {
		t.Fatalf("direct Reviewer mutation during dispatch was not rejected: %v", err)
	}
	if _, err := ApplyProductionNotificationState(db, project.ID, "production-1", project.Name, revision, nil, nil); !errors.Is(err, ErrNotificationDeliveryInFlight) {
		t.Fatalf("route Apply during dispatch was not rejected: %v", err)
	}
	if err := BeginManagedChannelDeletion(db, "production-1", 1, "guild-1", "channel-1", "webhook-1", time.Now()); !errors.Is(err, ErrNotificationDeliveryInFlight) {
		t.Fatalf("managed channel deletion during dispatch was not rejected: %v", err)
	}
}

func TestProductionDeletionMutationFencesWritesAndRequiresExplicitRetryAfterInterruption(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	token, err := BeginProductionDeletionMutation(db, "production-1")
	if err != nil || token == "" {
		t.Fatalf("deletion barrier did not start: token=%q err=%v", token, err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-type-1", "Synthetic", ReviewerTargetUser, "123456789012345678"); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("Reviewer write crossed active deletion barrier: %v", err)
	}
	revision, err := ProductionNotificationRevisionWithError(db, project.ID, "production-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyProductionNotificationState(db, project.ID, "production-1", project.Name, revision, nil, nil); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("Apply crossed active deletion barrier: %v", err)
	}
	if _, err := PrepareNotificationDelivery(db, notificationIntent()); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("polling crossed active deletion barrier: %v", err)
	}
	if err := FinishProductionDeletionMutation(db, "production-1", token, true); err != nil {
		t.Fatal(err)
	}
	var lock ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", "production-1").First(&lock).Error; err != nil || lock.MutationState != "deletion_needs_help" {
		t.Fatalf("interrupted deletion was not retained for operator recovery: lock=%+v err=%v", lock, err)
	}
	retryToken, err := BeginProductionDeletionMutation(db, "production-1")
	if err != nil || retryToken != token {
		t.Fatalf("explicit deletion retry did not resume the same operation identity: token=%q original=%q err=%v", retryToken, token, err)
	}
	if err := FinishProductionDeletionMutation(db, "production-1", retryToken, false); err != nil {
		t.Fatal(err)
	}
}

func TestProductionDeletionMutationBarrierIsVisibleAcrossSQLiteConnections(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	secondDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	secondSQL, err := secondDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondSQL.Close() })
	token, err := BeginProductionDeletionMutation(db, "production-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareNotificationDelivery(secondDB, notificationIntent()); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("second SQLite connection did not observe the durable deletion barrier: %v", err)
	}
	if err := FinishProductionDeletionMutation(secondDB, "production-1", token, false); err != nil {
		t.Fatalf("second connection could not finalize the exact barrier token: %v", err)
	}
}

func TestNotificationRevisionChangeBeforeDispatchFailsClosed(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	revision, err := ProductionNotificationRevisionWithError(db, project.ID, "production-1")
	if err != nil {
		t.Fatal(err)
	}
	intent := notificationIntent()
	intent.NotificationRevision = revision
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: ReviewerTargetUser, DiscordID: "123456789012345678"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now()); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("stale recipient revision must not dispatch: ok=%v err=%v", ok, err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryTerminalFailure || stored.FailureClass != "notification_state_changed_before_dispatch" {
		t.Fatalf("stale event was not terminally blocked: row=%+v err=%v", stored, err)
	}
}

func TestRetryableDeliveryCanRestartButAcceptedSendPersistenceFailureCannot(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	intent, err := PrepareNotificationDelivery(db, notificationIntent())
	if err != nil {
		t.Fatal(err)
	}
	first, ok, err := BeginNotificationDelivery(db, intent.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("begin first attempt: ok=%v err=%v", ok, err)
	}
	if err := SetNotificationDeliveryDisposition(db, *first, DeliveryRetryable, "rate_limited"); err != nil {
		t.Fatal(err)
	}
	second, ok, err := BeginNotificationDelivery(db, intent.EventID, time.Now().Add(time.Second))
	if err != nil || !ok {
		t.Fatalf("known rejected retryable attempt should retry: ok=%v err=%v", ok, err)
	}
	if err := db.Exec("CREATE TRIGGER fail_task_checkpoint BEFORE INSERT ON tasks BEGIN SELECT RAISE(ABORT, 'injected checkpoint failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := CompleteNotificationDelivery(db, *second, "message-1", "", time.Now()); err == nil {
		t.Fatal("post-accept checkpoint failure must be surfaced")
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", intent.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryInFlight {
		t.Fatalf("failed SQLite commit must preserve the durable pre-send in-flight attempt: row=%+v err=%v", stored, err)
	}
	if err := db.Exec("DROP TRIGGER fail_task_checkpoint").Error; err != nil {
		t.Fatal(err)
	}
	if err := QuarantineAcceptedButUnpersisted(db, *second); err != nil {
		t.Fatalf("accepted-but-unpersisted attempt should be quarantined immediately: %v", err)
	}
	if _, ok, err := BeginNotificationDelivery(db, intent.EventID, time.Now().Add(time.Hour)); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("accepted-but-unpersisted event must not be blindly resent: ok=%v err=%v", ok, err)
	}
	if err := db.Where("event_id = ?", intent.EventID).First(&stored).Error; err != nil || stored.Disposition != DeliveryUnknown || stored.FailureClass != "discord_accepted_checkpoint_uncommitted" {
		t.Fatalf("accepted but uncommitted result needs durable reconciliation evidence: row=%+v err=%v", stored, err)
	}
}

func TestEarlierUnresolvedEventBlocksOutOfOrderDelivery(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	firstIntent := notificationIntent()
	first, err := PrepareNotificationDelivery(db, firstIntent)
	if err != nil {
		t.Fatal(err)
	}
	secondIntent := firstIntent
	secondIntent.TaskUpdatedAt = "2026-10-10T12:01:00Z"
	second, err := PrepareNotificationDelivery(db, secondIntent)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginNotificationDelivery(db, second.EventID, time.Now()); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("newer event must wait for the earlier pending event: ok=%v err=%v", ok, err)
	}
	firstAttempt, ok, err := BeginNotificationDelivery(db, first.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("earlier event should claim first: ok=%v err=%v", ok, err)
	}
	if err := SetNotificationDeliveryDisposition(db, *firstAttempt, DeliveryRetryable, "rate_limited"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginNotificationDelivery(db, second.EventID, time.Now().Add(time.Second)); ok || !errors.Is(err, ErrDeliveryUnavailable) {
		t.Fatalf("newer event must remain blocked behind retryable older event: ok=%v err=%v", ok, err)
	}
	retry, ok, err := BeginNotificationDelivery(db, first.EventID, time.Now().Add(2*time.Second))
	if err != nil || !ok {
		t.Fatalf("older retry should be eligible: ok=%v err=%v", ok, err)
	}
	if err := CompleteNotificationDelivery(db, *retry, "message-1", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := BeginNotificationDelivery(db, second.EventID, time.Now().Add(3*time.Second)); err != nil || !ok {
		t.Fatalf("newer event should proceed after prior commit: ok=%v err=%v", ok, err)
	}
}

func TestFailedPriorWebhookVerificationReleasesCleanupClaimForSafeRetry(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	intent := notificationIntent()
	intent.PriorMessageID = "old-message"
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("delivery attempt did not start: ok=%v err=%v", ok, err)
	}
	if err := CompleteNotificationDelivery(db, *attempt, "new-message", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := BeginPriorMessageCleanup(db, event.EventID)
	if err != nil || !ok || !claimed.PriorCleanupInFlight {
		t.Fatalf("prior-message cleanup was not claimed: event=%+v ok=%v err=%v", claimed, ok, err)
	}
	if err := RecordPriorMessageCleanup(db, event.EventID, false, "prior_webhook_verification_failed"); err != nil {
		t.Fatal(err)
	}
	var stored NotificationDeliveryEvent
	if err := db.Where("event_id = ?", event.EventID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.PriorCleanupPending || stored.PriorCleanupInFlight || stored.CleanupFailureClass != "prior_webhook_verification_failed" {
		t.Fatalf("failed verification should remain visible and retryable without a stuck claim: %+v", stored)
	}
}

func TestObservedSameStatusCommentEditDoesNotAdvanceDeliveryCheckpoint(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	legacy := Task{TaskID: "task-1", TaskStatus: "WFA", TaskUpdatedAt: "old-task-time", CommentID: "comment-old", CommentUpdatedAt: "old-comment-time"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	current := notificationIntent()
	if err := ObserveTaskStatus(db, current.TaskID, current.TaskUpdatedAt, current.TaskStatus, current.CommentID, current.CommentUpdatedAt); err != nil {
		t.Fatal(err)
	}
	var observed Task
	if err := db.Where("task_id = ?", current.TaskID).First(&observed).Error; err != nil {
		t.Fatal(err)
	}
	if observed.DeliveredTaskUpdatedAt != "old-task-time" || observed.DeliveredCommentUpdatedAt != "old-comment-time" {
		t.Fatalf("observation incorrectly consumed an undelivered same-status edit: %+v", observed)
	}
	event, err := PrepareNotificationDelivery(db, current)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("same-status comment edit should be dispatched: ok=%v err=%v", ok, err)
	}
	if err := SetNotificationDeliveryDisposition(db, *attempt, DeliveryRetryable, "rate_limited"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveTaskStatus(db, current.TaskID, current.TaskUpdatedAt, current.TaskStatus, current.CommentID, current.CommentUpdatedAt); err != nil {
		t.Fatal(err)
	}
	disposition, err := NotificationDeliveryDisposition(db, event.EventID)
	if err != nil || disposition != DeliveryRetryable {
		t.Fatalf("retryable outbox intent was consumed by repeat observation: disposition=%q err=%v", disposition, err)
	}
	if err := db.Where("task_id = ?", current.TaskID).First(&observed).Error; err != nil || observed.DeliveredCommentUpdatedAt != "old-comment-time" {
		t.Fatalf("failed same-status edit advanced its delivery checkpoint: task=%+v err=%v", observed, err)
	}
}

func TestExternalReorderMutationBarrierBlocksDispatchAndSurfacesRecovery(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	revision, err := ProductionNotificationRevisionWithError(db, project.ID, project.KitsuProjectID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewProductionNotificationMutationToken()
	if err != nil {
		t.Fatal(err)
	}
	routes := ListProductionNotificationRoutes(db, project.KitsuProjectID)
	if _, err := ApplyProductionNotificationStateForExternalReorder(db, project.ID, project.KitsuProjectID, project.Name, revision, routes, nil, token); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareNotificationDelivery(db, notificationIntent()); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("dispatch intent should be blocked during external reorder: %v", err)
	}
	if err := UpsertProjectReviewerTarget(db, project.ID, "task-type-1", "Synthetic", ReviewerTargetUser, "123456789012345678"); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("direct Reviewer mutation should wait for external reorder: %v", err)
	}
	diagnostics, err := ListUnresolvedNotificationDeliveries(db, 50)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].Disposition != "mutation_reordering" {
		t.Fatalf("external mutation should be visible in safe diagnostics: %#v err=%v", diagnostics, err)
	}
	if err := FinishProductionNotificationExternalMutation(db, project.KitsuProjectID, token, true); err != nil {
		t.Fatal(err)
	}
	var quarantined ProductionNotificationWriteLock
	if err := db.Where("production_id = ?", project.KitsuProjectID).First(&quarantined).Error; err != nil || quarantined.MutationState != NotificationMutationReorderHelp || quarantined.MutationToken != token {
		t.Fatalf("quarantined reorder must retain its durable attempt identity: lock=%+v err=%v", quarantined, err)
	}
	if _, err := PrepareNotificationDelivery(db, notificationIntent()); !errors.Is(err, ErrNotificationMutationRecoveryRequired) {
		t.Fatalf("failed compensation must keep dispatch quarantined: %v", err)
	}
	diagnostics, err = ListUnresolvedNotificationDeliveries(db, 50)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].Disposition != "mutation_reorder_needs_help" {
		t.Fatalf("unresolved recovery state not surfaced: %#v err=%v", diagnostics, err)
	}
	recoveryToken, err := BeginProductionNotificationOrderRecovery(db, project.KitsuProjectID)
	if err != nil || recoveryToken == "" || recoveryToken == token {
		t.Fatalf("explicit order reconciliation should claim the persisted recovery state with a fresh token: token=%q err=%v", recoveryToken, err)
	}
	if err := FinishProductionNotificationExternalMutation(db, project.KitsuProjectID, recoveryToken, false); err != nil {
		t.Fatalf("verified reconciliation should release its matching fence: %v", err)
	}
	if _, err := PrepareNotificationDelivery(db, notificationIntent()); err != nil {
		t.Fatalf("dispatch should resume only after explicit order reconciliation completes: %v", err)
	}
}

func TestDeletionBatchTombstonesAtomicallyAndBlocksAllRoutes(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	secondWebhook := ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-2", WebhookURL: "https://discord.com/api/webhooks/webhook-2/token"}
	if err := db.Create(&secondWebhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-2", DestinationWebhookID: secondWebhook.ID, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_second_tombstone BEFORE INSERT ON managed_channel_deletions WHEN NEW.webhook_row_id = 2 BEGIN SELECT RAISE(ABORT, 'injected tombstone failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	targets := []ManagedChannelDeletionTarget{{WebhookRowID: 1, GuildID: "guild-1", ChannelID: "channel-1", DiscordWebhookID: "webhook-1"}, {WebhookRowID: secondWebhook.ID, GuildID: "guild-1", ChannelID: "channel-2", DiscordWebhookID: "webhook-2"}}
	if err := BeginManagedChannelDeletionBatch(db, "production-1", targets, time.Now(), true); err == nil {
		t.Fatal("injected tombstone write failure should fail the whole batch")
	}
	var count int64
	if err := db.Model(&ManagedChannelDeletion{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed batch left a partial tombstone set: count=%d err=%v", count, err)
	}
	if err := db.Exec("DROP TRIGGER fail_second_tombstone").Error; err != nil {
		t.Fatal(err)
	}
	if err := BeginManagedChannelDeletionBatch(db, "production-1", targets, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	diagnostics, err := ListUnresolvedNotificationDeliveries(db, 50)
	if err != nil || len(diagnostics) != 2 || diagnostics[0].Disposition != "channel_pending" || diagnostics[1].Disposition != "channel_pending" {
		t.Fatalf("channel tombstones should be visible in safe diagnostics: %#v err=%v", diagnostics, err)
	}
	if _, err := PrepareNotificationDelivery(db, notificationIntent()); !errors.Is(err, ErrChannelDeletionConflict) {
		t.Fatalf("dispatch should stop while any Production deletion tombstone is unresolved: %v", err)
	}
}

func TestPriorMessageCleanupClaimSerializesWithManagedChannelDeletion(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	secondWebhook := ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-2", WebhookURL: "https://discord.com/api/webhooks/webhook-2/token"}
	if err := db.Create(&secondWebhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-2", DestinationWebhookID: secondWebhook.ID, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	intent := notificationIntent()
	intent.PriorMessageID = "prior-message-1"
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := BeginNotificationDelivery(db, event.EventID, time.Now())
	if err != nil || !ok {
		t.Fatalf("delivery claim failed: ok=%v err=%v", ok, err)
	}
	if err := CompleteNotificationDelivery(db, *attempt, "new-message-1", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := BeginPriorMessageCleanup(db, event.EventID); err != nil || !claimed {
		t.Fatalf("cleanup claim failed: claimed=%v err=%v", claimed, err)
	}
	if err := BeginManagedChannelDeletion(db, "production-1", 1, "guild-1", "channel-1", "webhook-1", time.Now()); !errors.Is(err, ErrChannelDeletionConflict) {
		t.Fatalf("channel deletion raced active/prior cleanup: %v", err)
	}
	if err := RecordPriorMessageCleanup(db, event.EventID, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := BeginManagedChannelDeletion(db, "production-1", 1, "guild-1", "channel-1", "webhook-1", time.Now()); err != nil {
		t.Fatalf("channel deletion should proceed after exact cleanup completes: %v", err)
	}
}

func TestManagedChannelDeletionTombstoneBlocksDispatchAndFinalizesAtomically(t *testing.T) {
	db := notificationDeliveryTestDB(t)
	notificationDeliveryFixture(t, db)
	project := FindProjectByKitsuID(db, "production-1")
	if err := db.Create(&ProjectReviewerTarget{ProjectID: project.ID, TaskTypeID: "task-type-1", TargetKind: ReviewerTargetUser, DiscordID: "discord-user-1"}).Error; err != nil {
		t.Fatal(err)
	}
	secondWebhook := ProjectWebhook{KitsuProjectID: "production-1", DiscordChannelID: "channel-2", WebhookURL: "https://discord.com/api/webhooks/webhook-2/token"}
	if err := db.Create(&secondWebhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ProductionNotificationRoute{ProductionID: "production-1", TaskTypeID: "task-type-2", DestinationWebhookID: secondWebhook.ID, DestinationChannelID: "channel-2"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := BeginManagedChannelDeletion(db, "production-1", 1, "guild-1", "channel-1", "webhook-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	intent, err := PrepareNotificationDelivery(db, notificationIntent())
	if err == nil || !errors.Is(err, ErrChannelDeletionConflict) || intent != nil {
		t.Fatalf("tombstoned destination must reject new delivery intent: intent=%+v err=%v", intent, err)
	}
	if err := db.Exec("CREATE TRIGGER fail_route_delete BEFORE DELETE ON production_notification_routes BEGIN SELECT RAISE(ABORT, 'injected finalize failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := FinalizeManagedChannelDeletion(db, 1, time.Now()); err == nil {
		t.Fatal("injected local-finalize failure must be reported")
	}
	if FindProjectWebhookByID(db, 1) == nil || len(ListProductionNotificationRoutes(db, "production-1")) != 2 || len(ListProjectReviewerTargets(db, project.ID)) != 1 {
		t.Fatal("failed finalization partially deleted local route state")
	}
	var tombstone ManagedChannelDeletion
	if err := db.Where("webhook_row_id = ?", 1).First(&tombstone).Error; err != nil || tombstone.Disposition != ChannelDeletionPending {
		t.Fatalf("failed finalization did not retain recoverable tombstone: row=%+v err=%v", tombstone, err)
	}
	if err := db.Exec("DROP TRIGGER fail_route_delete").Error; err != nil {
		t.Fatal(err)
	}
	if err := FinalizeManagedChannelDeletion(db, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	routes := ListProductionNotificationRoutes(db, "production-1")
	if FindProjectWebhookByID(db, 1) != nil || len(routes) != 1 || routes[0].TaskTypeID != "task-type-2" || len(ListProjectReviewerTargets(db, project.ID)) != 0 {
		t.Fatal("finalized deletion did not atomically remove local destination state")
	}
}

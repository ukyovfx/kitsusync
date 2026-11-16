package model

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func deliveryKeepTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &Task{}, &ProjectReviewerTarget{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func redactionSeed(t *testing.T, db *gorm.DB, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		row := NotificationDeliveryEvent{
			EventID: "ev-" + string(rune('a'+i)), TaskID: "task-1", ProductionID: "production-1",
			TaskTypeID: "tt", TaskStatus: "WFA", LaneType: "channel",
			PayloadJSON:  `{"content":"secret payload ` + string(rune('a'+i)) + `"}`,
			PayloadSHA256: "hash", Disposition: DeliveryDelivered,
			DeliveredAt: ptrTime(time.Now().Add(-time.Hour)),
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestRedactDeliveredNotificationPayloadsIsBounded(t *testing.T) {
	db := deliveryKeepTestDB(t)
	redactionSeed(t, db, 5)
	cutoff := time.Now()
	n, err := RedactDeliveredNotificationPayloads(db, cutoff, cutoff, 3)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if n != 3 {
		t.Fatalf("redacted = %d, want the 3-row batch limit", n)
	}
	var remaining int64
	db.Model(&NotificationDeliveryEvent{}).Where("payload_json <> ''").Count(&remaining)
	if remaining != 2 {
		t.Fatalf("remaining payload rows = %d, want 2", remaining)
	}
}

func TestRedactDeliveredNotificationPayloadsOnlyTouchesDelivered(t *testing.T) {
	db := deliveryKeepTestDB(t)
	row := NotificationDeliveryEvent{
		EventID: "ev-pending", TaskID: "task-1", ProductionID: "production-1",
		TaskTypeID: "tt", TaskStatus: "WFA", LaneType: "channel",
		PayloadJSON: `{"content":"keep"}`, PayloadSHA256: "hash", Disposition: DeliveryPending,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	n, err := RedactDeliveredNotificationPayloads(db, time.Now(), time.Now(), 10)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if n != 0 {
		t.Fatalf("redacted = %d, want 0 for a pending row", n)
	}
	var got NotificationDeliveryEvent
	if err := db.Where("event_id = ?", "ev-pending").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.PayloadJSON != `{"content":"keep"}` {
		t.Fatal("pending payload was modified")
	}
}

func TestProductionDeletionMutationFencesReviewerApply(t *testing.T) {
	// File-backed DB: the write lock must be visible across connections.
	path := filepath.Join(t.TempDir(), "deletion-mutation.db")
	db, err := gorm.Open(sqlite.Open("file:"+path+"?_busy_timeout=10000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectReviewerTarget{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	token, err := BeginProductionDeletionMutation(db, "production-1")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// While the deletion mutation is active, a second mutation must be refused.
	if err := EnsureProductionNotificationMutationIdle(db, "production-1", ""); err == nil {
		t.Fatal("expected the active deletion mutation to block other mutations")
	}
	if err := FinishProductionDeletionMutation(db, "production-1", token, false); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if err := EnsureProductionNotificationMutationIdle(db, "production-1", ""); err != nil {
		t.Fatalf("mutation still active after finish: %v", err)
	}
}

func TestConcurrentDeletionMutationsAreSerialised(t *testing.T) {
	db := deliveryKeepTestDB(t)
	var wg sync.WaitGroup
	wins := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := BeginProductionDeletionMutation(db, "production-1"); err == nil {
				wins <- 1
			} else {
				wins <- 0
			}
		}()
	}
	wg.Wait()
	close(wins)
	total := 0
	for w := range wins {
		total += w
	}
	if total != 1 {
		t.Fatalf("concurrent BeginProductionDeletionMutation winners = %d, want 1", total)
	}
}

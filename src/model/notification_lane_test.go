package model

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newNotificationLaneDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &Task{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &ProductionNotificationWriteLock{}, &NotificationDeliveryEvent{}, &ManagedChannelDeletion{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRecordNotificationOnlyDeliveryIsRecordedOnce(t *testing.T) {
	db := newNotificationLaneDB(t)
	if err := RecordNotificationOnlyDelivery(db, "task-1", "p1", "tt1", "WFA", "2026-11-13T00:00:00Z", "c1", "2026-11-13T00:00:01Z", false); err != nil {
		t.Fatal(err)
	}
	if err := RecordNotificationOnlyDelivery(db, "task-1", "p1", "tt1", "WFA", "2026-11-13T00:00:00Z", "c1", "2026-11-13T00:00:01Z", false); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&NotificationDeliveryEvent{}).Where("task_id = ?", "task-1").Count(&count)
	if count != 1 {
		t.Fatalf("record-only delivery rows = %d, want 1", count)
	}
	var row NotificationDeliveryEvent
	db.Where("task_id = ?", "task-1").First(&row)
	if row.LaneType != LaneRecordOnly || row.Disposition != DeliveryRecorded || row.DestinationTarget != "" {
		t.Fatalf("unexpected record-only row: lane=%q disposition=%q target=%q", row.LaneType, row.Disposition, row.DestinationTarget)
	}
}

func TestNotificationEventIdentitySeparatesLanes(t *testing.T) {
	base := NotificationDeliveryIntent{
		TaskID: "task-1", ProductionID: "p1", TaskTypeID: "tt1", TaskStatus: "WFA",
		TaskUpdatedAt: "2026-11-13T00:00:00Z", CommentID: "c1", CommentUpdatedAt: "2026-11-13T00:00:01Z",
	}
	dm := base
	dm.LaneType = LaneDM
	dm.DestinationTarget = "111111111111111111"
	dm.ChannelID = "dm-channel-1"
	if NotificationEventIdentity(base) == NotificationEventIdentity(dm) {
		t.Fatal("DM intent shares the event identity with the channel intent")
	}
}

func TestRetargetNotificationDeliveryMovesPendingIntent(t *testing.T) {
	db := newNotificationLaneDB(t)
	if err := db.Create(&Project{KitsuProjectID: "p1", Name: "P1", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: "p1", ChannelName: "comp", TaskType: "Comp", WebhookURL: "https://discord.com/api/webhooks/111111111111111111/token", DiscordChannelID: "channel-1"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	intent := NotificationDeliveryIntent{
		TaskID: "task-1", ProductionID: "p1", TaskTypeID: "tt1", TaskStatus: "WFA",
		TaskUpdatedAt: "2026-11-13T00:00:00Z", CommentID: "c1", CommentUpdatedAt: "2026-11-13T00:00:01Z",
		LaneType: LaneDM, DestinationTarget: "222222222222222222", ChannelID: "dm-channel-1",
		Payload: []byte(`{"content":"synthetic"}`),
	}
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	if event.LaneType != LaneDM || event.DestinationTarget != "222222222222222222" {
		t.Fatalf("prepared DM event lane/target = %q/%q", event.LaneType, event.DestinationTarget)
	}
	if err := RetargetNotificationDelivery(db, event.EventID, LaneChannel, "channel-1", webhook.ID, "guild-1", "channel-1", "111111111111111111"); err != nil {
		t.Fatal(err)
	}
	retargeted, err := FindNotificationDeliveryByID(db, event.EventID)
	if err != nil || retargeted == nil {
		t.Fatalf("retargeted event lookup failed: %v", err)
	}
	if retargeted.LaneType != LaneChannel || retargeted.DestinationChannelID != "channel-1" || retargeted.WebhookRowID != webhook.ID {
		t.Fatalf("unexpected retargeted destination: %+v", retargeted)
	}
	if retargeted.DestinationGuildID != "guild-1" || !strings.Contains(retargeted.DestinationFingerprint, "") {
		t.Fatalf("retargeted guild identity = %q", retargeted.DestinationGuildID)
	}
}

func TestRetargetNotificationDeliveryRejectsDeliveredEvent(t *testing.T) {
	db := newNotificationLaneDB(t)
	if err := db.Create(&Project{KitsuProjectID: "p1", Name: "P1", DiscordGuildID: "guild-1"}).Error; err != nil {
		t.Fatal(err)
	}
	webhook := ProjectWebhook{KitsuProjectID: "p1", ChannelName: "comp", TaskType: "Comp", WebhookURL: "https://discord.com/api/webhooks/111111111111111111/token", DiscordChannelID: "channel-1"}
	if err := db.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	intent := NotificationDeliveryIntent{
		TaskID: "task-2", ProductionID: "p1", TaskTypeID: "tt1", TaskStatus: "RETAKE",
		TaskUpdatedAt: "2026-11-13T00:00:00Z",
		LaneType:      LaneDM, DestinationTarget: "222222222222222222", ChannelID: "dm-channel-1",
		Payload: []byte(`{"content":"synthetic"}`),
	}
	event, err := PrepareNotificationDelivery(db, intent)
	if err != nil {
		t.Fatal(err)
	}
	delivered := event.Disposition
	_ = delivered
	// Simulate a delivered event: the retarget must be refused.
	if err := db.Model(&NotificationDeliveryEvent{}).Where("id = ?", event.ID).Updates(map[string]any{"disposition": DeliveryDelivered, "discord_message_id": "m-1", "delivered_at": event.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	if err := RetargetNotificationDelivery(db, event.EventID, LaneChannel, "channel-1", webhook.ID, "guild-1", "channel-1", "111111111111111111"); err == nil {
		t.Fatal("delivered event was retargeted")
	}
}

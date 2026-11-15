package model

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWriteAuditLogDoesNotPersistWebhookURL(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&AuditLog{}); err != nil {
		t.Fatal(err)
	}
	WriteAuditLog(db, AuditLog{TaskID: "task-1", WebhookURL: "https://discord.example/webhooks/secret", PreviousWebhookURL: "https://discord.example/old"})
	logs := ListAuditLogs(db, 1)
	if len(logs) != 1 {
		t.Fatalf("expected one audit row, got %d", len(logs))
	}
	if logs[0].WebhookURL != "" || logs[0].PreviousWebhookURL != "" {
		t.Fatal("audit log persisted webhook URL material")
	}
}

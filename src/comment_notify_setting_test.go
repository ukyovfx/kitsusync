package main

import (
	"testing"

	"app/src/model"
	"app/src/setup"
	"app/src/utils/config"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func commentNotifyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// commentNotifyAssignee prefers the stored setting; conf.toml only supplies
// the initial value while nothing is stored, and the default is false.
func TestCommentNotifyAssigneePrefersTheStoredValue(t *testing.T) {
	db := commentNotifyDB(t)
	conf := config.Config{}
	if commentNotifyAssignee(db, conf) {
		t.Fatal("unset DB fell back to true; want the default false")
	}
	conf.Notification.CommentNotifyAssignee = true
	if !commentNotifyAssignee(db, conf) {
		t.Fatal("unset DB must use the conf.toml value as the initial default")
	}
	model.SetSetting(db, setup.CommentNotifyAssigneeSettingKey, "false")
	if commentNotifyAssignee(db, conf) {
		t.Fatal("a stored false must win over conf.toml")
	}
	model.SetSetting(db, setup.CommentNotifyAssigneeSettingKey, "true")
	conf.Notification.CommentNotifyAssignee = false
	if !commentNotifyAssignee(db, conf) {
		t.Fatal("a stored true must win over conf.toml")
	}
}

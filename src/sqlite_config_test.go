package main

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestConfigureSQLiteEnablesForeignKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := configureSQLite(db)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var enabled int
	if err := sqlDB.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", enabled)
	}
}

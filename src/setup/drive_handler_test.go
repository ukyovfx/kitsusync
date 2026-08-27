package setup

import (
	"app/src/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDriveHandlerAcceptsAndPersistsValidatedURL(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:drive-handler-valid?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/drive", strings.NewReader("kitsu_project_id=p1&storage_url=https%3A%2F%2Fdrive.google.com%2Fdrive%2Ffolders%2F123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()
	DriveHandler(db)(res, req)
	if res.Code != http.StatusSeeOther || model.GetProjectStorageURL(db, "p1") != "https://drive.google.com/drive/folders/123" {
		t.Fatalf("valid Drive URL was not persisted: status=%d url=%q", res.Code, model.GetProjectStorageURL(db, "p1"))
	}
}

func TestDriveHandlerRejectsInvalidURLWithoutChangingStoredValue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:drive-handler-invalid?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	model.SetProjectStorageURL(db, "p1", "https://drive.google.com/drive/folders/old")
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/drive", strings.NewReader("kitsu_project_id=p1&storage_url=not-a-url"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()
	DriveHandler(db)(res, req)
	if res.Code != http.StatusBadRequest || model.GetProjectStorageURL(db, "p1") != "https://drive.google.com/drive/folders/old" {
		t.Fatalf("invalid Drive URL changed stored value: status=%d url=%q", res.Code, model.GetProjectStorageURL(db, "p1"))
	}
}

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
	var reloaded model.Project
	if err := db.Where("kitsu_project_id = ?", "p1").First(&reloaded).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.StorageURL != "https://drive.google.com/drive/folders/123" {
		t.Fatalf("reloaded project lost storage URL: %q", reloaded.StorageURL)
	}
}

func TestDriveHandlerKeepsStorageURLsScopedToTheirProduction(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:drive-handler-scope?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"p1", "p2"} {
		if err := model.CreateProject(db, id, id, "", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/drive", strings.NewReader("kitsu_project_id=p1&storage_url=https%3A%2F%2Fdrive.google.com%2Fdrive%2Ffolders%2Fp1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()
	DriveHandler(db)(res, req)
	if res.Code != http.StatusSeeOther || model.GetProjectStorageURL(db, "p1") == "" || model.GetProjectStorageURL(db, "p2") != "" {
		t.Fatalf("storage URL crossed Production boundary: status=%d p1=%q p2=%q", res.Code, model.GetProjectStorageURL(db, "p1"), model.GetProjectStorageURL(db, "p2"))
	}
}

func TestDriveHandlerDoesNotReturnSuccessForMissingProduction(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:drive-handler-missing?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/drive", strings.NewReader("kitsu_project_id=missing&storage_url=https%3A%2F%2Fdrive.google.com%2Fdrive%2Ffolders%2F123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()
	DriveHandler(db)(res, req)
	location := res.Header().Get("Location")
	if res.Code != http.StatusSeeOther || !strings.Contains(location, "drive_error=save") {
		t.Fatalf("missing Production did not return an actionable save error: status=%d location=%q", res.Code, location)
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

func TestDriveStoragePanelShowsLocalizedSaveFeedback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:drive-panel-feedback?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatal(err)
	}
	project := model.Project{KitsuProjectID: "p1", Name: "P"}
	for _, tc := range []struct {
		name, query, lang, want string
	}{
		{"success-ja", "drive_saved=1", "ja", "Drive設定を保存しました。"},
		{"success-en", "drive_saved=1", "en", "Drive settings saved."},
		{"error-ja", "drive_error=save", "ja", "Drive設定を保存できませんでした。"},
		{"error-en", "drive_error=readback", "en", "Drive settings were not confirmed after saving."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=p1&tab=storage-settings&lang="+tc.lang+"&"+tc.query, nil)
			body := renderSelectedProductionPanel(db, r, project, tc.lang, "storage-settings", "success", "Connected", "", "")
			if !strings.Contains(body, tc.want) {
				t.Fatalf("feedback %q missing from %s", tc.want, body)
			}
		})
	}
	body := renderSelectedProductionPanel(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=p1&tab=storage-settings&lang=ja", nil), project, "ja", "storage-settings", "success", "Connected", "", "")
	if !strings.Contains(body, "保存中...") || !strings.Contains(body, "drive-storage-form") {
		t.Fatal("Drive save form is missing localized saving feedback behavior")
	}
}

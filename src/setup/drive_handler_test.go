package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDriveHandlerRejectsLegacyWritesWithoutChangingStoredValue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:drive-handler-read-only?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	const existing = "https://drive.google.com/drive/folders/old"
	if err := model.SetProjectStorageURL(db, "p1", existing); err != nil {
		t.Fatal(err)
	}

	for _, submitted := range []string{
		"https://drive.google.com/drive/folders/new",
		"https://attacker.example/other-root",
		"",
	} {
		t.Run(submitted, func(t *testing.T) {
			body := "kitsu_project_id=p1&storage_url=" + submitted
			req := httptest.NewRequest(http.MethodPost, "/bot/admin/drive", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			res := httptest.NewRecorder()
			DriveHandler(db)(res, req)

			if res.Code != http.StatusMethodNotAllowed {
				t.Fatalf("legacy Drive writer status = %d, want %d", res.Code, http.StatusMethodNotAllowed)
			}
			if actual := model.GetProjectStorageURL(db, "p1"); actual != existing {
				t.Fatalf("legacy Drive writer changed stored notification link to %q", actual)
			}
		})
	}
}

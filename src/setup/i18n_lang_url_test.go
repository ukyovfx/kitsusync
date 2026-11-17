package setup

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// F3: the language toggle must keep page state and drop one-shot flash params.
func TestLangToggleKeepsPageStateAndDropsFlashParams(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=p1&tab=notifications&msg=saved&drive_saved=1&drive_error=save&validated_channels=1&danger_preview=1&lang=ja", nil)
	got := toggleLangURL(r)
	want := "/bot/admin/projects?lang=en&project=p1&tab=notifications"
	if got != want {
		t.Fatalf("language toggle URL = %q, want %q", got, want)
	}
	if plain := toggleLangURL(httptest.NewRequest(http.MethodGet, "/bot/admin/users?lang=en", nil)); plain != "/bot/admin/users?lang=ja" {
		t.Fatalf("language toggle URL without state = %q", plain)
	}
}

package setup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/model"
)

// The health page's exception queue (core v2 §6): an ambiguous status is
// listed with a classification form, and classifying it writes the studio
// status policy and clears the status's open exceptions.

func TestAdminExceptionsClassifyWritesPolicyAndClears(t *testing.T) {
	db := newIAViewDB(t)
	if err := db.AutoMigrate(&model.StudioStatusPolicy{}, &model.AdminException{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.StudioStatusPolicy{
		StatusID: "s-rejected", StatusName: "Rejected", Nature: model.StatusNatureAmbiguous,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.RaiseAdminException(db, model.ExceptionStatusAmbiguous, "s-rejected", "Rejected", "", "dictionary could not classify"); err != nil {
		t.Fatal(err)
	}
	open, err := model.OpenAdminExceptions(db)
	if err != nil || len(open) != 1 {
		t.Fatalf("open = %+v err=%v", open, err)
	}

	section := renderAdminExceptionsSection("en", db)
	if !strings.Contains(section, "Rejected") || !strings.Contains(section, `name="status_id" value="s-rejected"`) {
		t.Fatalf("exception not listed with a classify form: %s", section)
	}

	form := url.Values{
		"action":      {"exception_classify"},
		"exception_id": {fmt.Sprint(open[0].ID)},
		"status_id":   {"s-rejected"},
		"status_name": {"Rejected"},
		"nature":      {model.StatusNatureDone},
	}
	post := httptest.NewRequest(http.MethodPost, "/bot/admin/health?lang=en", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	if handled := handleAdminExceptionPost(db, rec, post); !handled {
		t.Fatal("classify post not handled")
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("classify post status = %d", rec.Code)
	}
	policies, err := model.LoadStudioStatusPolicies(db)
	if err != nil {
		t.Fatal(err)
	}
	policy := policies["s-rejected"]
	if policy.Nature != model.StatusNatureDone || !policy.Manual {
		t.Fatalf("policy after classify = %+v", policy)
	}
	open, err = model.OpenAdminExceptions(db)
	if err != nil || len(open) != 0 {
		t.Fatalf("open exceptions after classify = %+v err=%v", open, err)
	}
}

func TestAdminExceptionsResolveDismissesRow(t *testing.T) {
	db := newIAViewDB(t)
	if err := db.AutoMigrate(&model.AdminException{}); err != nil {
		t.Fatal(err)
	}
	if err := model.RaiseAdminException(db, model.ExceptionNoRoute, "tt-fx", "FX", "p1", ""); err != nil {
		t.Fatal(err)
	}
	open, err := model.OpenAdminExceptions(db)
	if err != nil || len(open) != 1 {
		t.Fatalf("open = %+v err=%v", open, err)
	}
	form := url.Values{
		"action":       {"exception_resolve"},
		"exception_id": {fmt.Sprint(open[0].ID)},
	}
	post := httptest.NewRequest(http.MethodPost, "/bot/admin/health?lang=en", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	if handled := handleAdminExceptionPost(db, rec, post); !handled {
		t.Fatal("resolve post not handled")
	}
	open, err = model.OpenAdminExceptions(db)
	if err != nil || len(open) != 0 {
		t.Fatalf("open exceptions after resolve = %+v err=%v", open, err)
	}
}

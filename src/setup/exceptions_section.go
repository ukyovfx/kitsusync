package setup

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"app/src/model"
)

// The admin exception queue (redesign core v2 §2.4): the few judgments the
// derivation model cannot make by itself. Everything else about
// notifications is derived; what lands here is only what a human must decide
// once — an unclassifiable status, a rename that would change a nature, a
// handoff whose recipients never resolved, a task type with no wiring, a
// possibly forgotten review.

func exceptionKindLabel(lang, kind string) string {
	switch kind {
	case model.ExceptionStatusAmbiguous:
		return t(lang, "分類できないステータス", "Unclassifiable status")
	case model.ExceptionStatusRenamed:
		return t(lang, "改名で性質が変わるステータス", "Rename changes the nature")
	case model.ExceptionRecipientUnresolved:
		return t(lang, "宛先が決まらなかった通知", "Recipients never resolved")
	case model.ExceptionNoRoute:
		return t(lang, "受け渡しの配線がないタスクタイプ", "Task type without handoff wiring")
	case model.ExceptionReviewStale:
		return t(lang, "レビュー出し忘れの可能性", "Possibly forgotten review")
	}
	return kind
}

// renderAdminExceptionsSection lists the open queue on the system status
// page. Status rows carry the classification form; the rest carry
// resolve/dismiss buttons.
func renderAdminExceptionsSection(lang string, db *gorm.DB) string {
	if db == nil {
		return ""
	}
	open, err := model.OpenAdminExceptions(db)
	if err != nil || len(open) == 0 {
		return ""
	}
	natureOptions := make([][2]string, 0, 5)
	for _, nature := range []string{model.StatusNatureReview, model.StatusNatureRetake, model.StatusNatureDone, model.StatusNatureWIP, model.StatusNatureNone} {
		natureOptions = append(natureOptions, [2]string{nature, statusNatureLabel(lang, nature)})
	}
	var rows strings.Builder
	for _, item := range open {
		rows.WriteString(`<tr><th scope="row" class="mobile-card-lead"><span class="mobile-card-lead-label">` + esc(exceptionKindLabel(lang, item.Kind)) + `</span></th>`)
		rows.WriteString(`<td>` + esc(item.SubjectName) + `</td>`)
		detail := item.Detail
		if item.SeenCount > 1 {
			detail += fmt.Sprintf(t(lang, "（%d回検出）", " (seen %d times)"), item.SeenCount)
		}
		rows.WriteString(`<td>` + esc(detail) + `</td><td>`)
		if item.Kind == model.ExceptionStatusAmbiguous || item.Kind == model.ExceptionStatusRenamed {
			rows.WriteString(`<form method="post" action="/bot/admin/health" class="inline-form">` +
				`<input type="hidden" name="action" value="exception_classify">` +
				`<input type="hidden" name="exception_id" value="` + fmt.Sprint(item.ID) + `">` +
				`<input type="hidden" name="status_id" value="` + esc(item.SubjectID) + `">` +
				`<input type="hidden" name="status_name" value="` + esc(item.SubjectName) + `">` +
				forumSelect("nature", "", natureOptions, t(lang, "（選択）", "(choose)")) +
				` <button type="submit" class="btn-ghost">` + esc(t(lang, "分類する", "Classify")) + `</button></form>`)
		} else {
			rows.WriteString(`<form method="post" action="/bot/admin/health" class="inline-form">` +
				`<input type="hidden" name="exception_id" value="` + fmt.Sprint(item.ID) + `">` +
				`<button type="submit" name="action" value="exception_resolve" class="btn-ghost">` + esc(t(lang, "解決", "Resolve")) + `</button> ` +
				`<button type="submit" name="action" value="exception_dismiss" class="btn-ghost">` + esc(t(lang, "却下", "Dismiss")) + `</button></form>`)
		}
		rows.WriteString(`</td></tr>`)
	}
	return `<section class="section-card glass admin-exceptions" aria-labelledby="admin-exceptions-title"><div class="page-heading"><div><h2 id="admin-exceptions-title">` + esc(t(lang, "判断待ち（例外キュー）", "Waiting for judgment (exception queue)")) + `</h2><p class="hint">` + esc(t(lang, "自動で決められないものだけがここに残ります。分類したステータスは次回から自動で扱われます。", "Only what cannot be derived automatically stays here. A classified status is handled automatically from then on.")) + `</p></div><span class="status-pill bad">` + esc(fmt.Sprint(len(open))) + `</span></div>` +
		`<table class="data-table mobile-card-table"><thead><tr><th scope="col">` + esc(t(lang, "種類", "Kind")) + `</th><th scope="col">` + esc(t(lang, "対象", "Subject")) + `</th><th scope="col">` + esc(t(lang, "詳細", "Detail")) + `</th><th scope="col">` + esc(t(lang, "操作", "Action")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></section>`
}

func statusNatureLabel(lang, nature string) string {
	switch nature {
	case model.StatusNatureReview:
		return t(lang, "レビュー待ち", "Review")
	case model.StatusNatureRetake:
		return t(lang, "リテイク", "Retake")
	case model.StatusNatureDone:
		return t(lang, "完了", "Done")
	case model.StatusNatureWIP:
		return t(lang, "作業中", "In progress")
	case model.StatusNatureNone:
		return t(lang, "通知なし", "No notification")
	}
	return nature
}

// handleAdminExceptionPost applies one exception-queue action. It reports
// whether the request was one (so the health handler can stop there).
func handleAdminExceptionPost(db *gorm.DB, w http.ResponseWriter, r *http.Request) bool {
	action := strings.TrimSpace(r.FormValue("action"))
	if action != "exception_resolve" && action != "exception_dismiss" && action != "exception_classify" {
		return false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(r.FormValue("exception_id")), 10, 0)
	if err != nil || id == 0 {
		http.Error(w, "invalid exception id", http.StatusBadRequest)
		return true
	}
	switch action {
	case "exception_resolve":
		err = model.ResolveAdminException(db, uint(id), false)
	case "exception_dismiss":
		err = model.ResolveAdminException(db, uint(id), true)
	case "exception_classify":
		nature := strings.TrimSpace(r.FormValue("nature"))
		statusID := strings.TrimSpace(r.FormValue("status_id"))
		statusName := strings.TrimSpace(r.FormValue("status_name"))
		if statusID == "" || nature == "" {
			http.Error(w, "invalid classification", http.StatusBadRequest)
			return true
		}
		if err := model.SetStudioStatusPolicyNature(db, statusID, statusName, nature); err != nil {
			http.Error(w, "classification failed", http.StatusInternalServerError)
			return true
		}
		_ = model.AutoResolveAdminExceptions(db, model.ExceptionStatusAmbiguous, statusID)
		_ = model.AutoResolveAdminExceptions(db, model.ExceptionStatusRenamed, statusID)
		err = model.ResolveAdminException(db, uint(id), false)
	}
	if err != nil {
		http.Error(w, "exception update failed", http.StatusInternalServerError)
		return true
	}
	http.Redirect(w, r, withLang("/bot/admin/health", r), http.StatusSeeOther)
	return true
}

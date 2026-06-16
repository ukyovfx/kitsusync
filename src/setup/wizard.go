package setup

import (
	"fmt"
	"net/http"

	"gorm.io/gorm"
)

// WizardHandler routes to entry, guided setup, or setup status based on ?mode=.
func WizardHandler(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Query().Get("mode") {
		case "guided":
			fmt.Fprint(w, RenderGuidedSetupPageStepwise(db, refreshCreds, r))
		case "quick":
			fmt.Fprint(w, RenderQuickSetupPage(db, refreshCreds, r))
		default:
			fmt.Fprint(w, RenderWizardEntryPage(db, refreshCreds, r))
		}
	}
}

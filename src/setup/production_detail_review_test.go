package setup

import (
	"net/http/httptest"
	"strings"
	"testing"

	"app/src/model"
)

func TestUnconfiguredProductionSharesConfiguredDetailShell(t *testing.T) {
	request := httptest.NewRequest("GET", "/bot/admin/projects?project=live-only&lang=en", nil)
	project := model.Project{KitsuProjectID: "live-only", Name: "Live-only Production", ReadOnlyPreview: true}
	body := renderIAUnconnectedProduction(request, project, "en")
	for _, marker := range []string{
		`class="production-context production-unconfigured"`,
		`class="production-identity"`,
		`class="eyebrow">Production</div>`,
		`<h1>Live-only Production</h1>`,
		`status-pill warning`,
		`class="production-unconfigured-state production-detail-surface"`,
		`/bot/setup?lang=en&amp;project=live-only`,
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("unconfigured Production is missing shared-shell element %q: %s", marker, body)
		}
	}
	for _, forbidden := range []string{`role="tablist"`, `production-notification-table`, `production-team-table`, `project-context-legacy`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("unconfigured Production fabricated configured-only content %q", forbidden)
		}
	}
}

func TestProductionStorageSaveSharesDesktopFieldRow(t *testing.T) {
	request := httptest.NewRequest("GET", "/bot/admin/projects?project=storage-row", nil)
	body := renderCurrentProductionStorage(request, model.Project{KitsuProjectID: "storage-row", StorageURL: "https://drive.example.invalid/project"}, "en")
	for _, marker := range []string{
		`class="form-stack drive-storage-form production-storage-form"`,
		`class="production-storage-field"`,
		`data-drive-save`,
		`production-storage-actions`,
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("Storage input/Save row is missing %q: %s", marker, body)
		}
	}
}

func TestProductionReviewControlsUseSharedSpacingAndControlTokens(t *testing.T) {
	for _, rule := range []string{
		`.production-context .production-notification-heading .status-pill,.production-context .production-routing-editor>.page-heading .status-pill,.production-context .production-notification-heading .btn-ghost{min-height:var(--control-height-dense,38px);`,
		`.production-context .production-routing-editor{gap:var(--space-section,24px);`,
		`.production-context .production-routing-editor-footer{margin-top:var(--space-2,8px);`,
		`.production-context .production-wfa-edit-groups{gap:var(--space-4,16px);`,
		`.production-context .production-settings-list{display:grid;gap:var(--space-2,8px);`,
		`.production-context .production-storage-form{display:grid;grid-template-columns:minmax(0,1fr) auto;`,
		`.production-context dialog select option:disabled{color:var(--muted);`,
		`.production-context .routing-row-menu-panel{position:fixed;`,
	} {
		if !strings.Contains(adminThemeCSS, rule) {
			t.Errorf("Production Detail review treatment is missing shared visual rule %q", rule)
		}
	}
}

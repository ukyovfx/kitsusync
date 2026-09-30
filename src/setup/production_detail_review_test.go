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
		`class="production-unconfigured-content"`,
		`production-unconfigured-actions"`,
		`class="eyebrow">Production</div>`,
		`<h1>Live-only Production</h1>`,
		`status-pill warning`,
		`class="production-settings-section production-unconfigured-state production-detail-surface"`,
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
		`class="production-storage-control-row"`,
		`data-drive-save`,
		`production-storage-actions`,
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("Storage input/Save row is missing %q: %s", marker, body)
		}
	}
}

func TestProductionDetailReviewControlsUseVisibleAlignedGroups(t *testing.T) {
	for _, rule := range []string{
		`.production-context .production-notification-actions{display:flex;align-items:center;gap:var(--space-2,8px)`,
		`.production-context .production-routing-editor [data-current-routing-form]{display:grid;row-gap:var(--space-6,32px)`,
		`.production-context .production-wfa-edit-panel{padding:0;border:0;border-top:1px solid var(--divider-color);`,
		`.production-context .production-settings-list{display:grid;gap:var(--space-1,4px);`,
		`.editorial-workbench .production-context .production-settings-disclosure-row>summary{min-height:var(--control-height-dense,38px);padding:0 var(--space-2,8px);`,
		`.production-context .routing-row-menu-panel{position:absolute;z-index:1100;right:0;top:calc(100% + var(--space-1,4px));`,
		`.production-context .production-unconfigured-content{display:grid;grid-template-columns:minmax(0,1fr);`,
		`.production-context .production-storage-control-row{display:grid;grid-template-columns:minmax(0,1fr) auto;`,
	} {
		if !strings.Contains(adminThemeCSS, rule) {
			t.Errorf("Production Detail visible layout rule is missing %q", rule)
		}
	}
}

func TestProductionReviewControlsUseSharedSpacingAndControlTokens(t *testing.T) {
	for _, rule := range []string{
		`.production-context .production-notification-actions .status-pill,.production-context .production-notification-actions .btn-ghost{box-sizing:border-box;display:inline-flex;align-items:center;justify-content:center;height:var(--control-height-dense,38px);`,
		`.production-context .production-routing-editor [data-current-routing-form]{display:grid;row-gap:var(--space-6,32px);`,
		`.production-context .production-routing-editor .production-routing-editor-footer{margin:0;padding-top:var(--space-4,16px)`,
		`.production-context .production-wfa-edit-panel{padding:0;border:0;border-top:1px solid var(--divider-color);`,
		`.production-context .production-settings-list{display:grid;gap:var(--space-1,4px);`,
		`.editorial-workbench .production-context #panel-settings>.production-settings-list>.production-settings-section{margin:0;padding:0;border:0;border-top:1px solid var(--divider-color);`,
		`.editorial-workbench .production-context .production-settings-disclosure-row>summary::before{content:"›";`,
		`.editorial-workbench .production-context .production-settings-disclosure-row[open]>.detail-list`,
		`.production-context .production-storage-control-row{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;`,
		`.production-context .production-storage-field{margin:0;color:var(--muted-2)`,
		`.production-context .production-storage-control-row input{box-sizing:border-box;width:100%;max-width:none;`,
		`.recipient-listbox{position:fixed;z-index:1110;`,
		`.production-context .production-routing-editor .wizard-plan-table{overflow:visible}`,
		`.production-context .routing-row-menu-panel{position:absolute;z-index:1100;right:0;top:calc(100% + var(--space-1,4px));min-width:200px;max-width:min(280px,calc(100vw - 16px));max-height:none;overflow:visible`,
		`.production-context .production-unconfigured-state{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:var(--space-5,24px);`,
		`.production-context .production-unconfigured-content{display:grid;grid-template-columns:minmax(0,1fr)`,
	} {
		if !strings.Contains(adminThemeCSS, rule) {
			t.Errorf("Production Detail review treatment is missing shared visual rule %q", rule)
		}
	}
}

package setup

import (
	"strings"
	"testing"
)

func TestBaseAdminJSGuardsUserUnlinkActions(t *testing.T) {
	for _, lang := range []string{"ja", "en"} {
		body := baseAdminJS(lang)
		for _, want := range []string{
			"remove_global_link",
			"remove_production_user",
			"remove_production_checker",
			"form.classList.add('delete-form')",
			"modal.setAttribute('aria-describedby', 'deleteModalText')",
			"modal.setAttribute('aria-hidden', 'false')",
			"else if(cancelBtn){ cancelBtn.focus(); }",
			"event.key === 'Escape'",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s confirmation script missing %q", lang, want)
			}
		}
	}
}

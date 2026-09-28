package setup

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBackgroundCanvasIsMountedOnlyOnLoginAndAdminSurfaces(t *testing.T) {
	login := loginPageHTML("en", "", "", false, httptest.NewRequest("GET", "/bot/login", nil))
	if !strings.Contains(login, `data-background="login-fabric"`) || !strings.Contains(login, `matchMedia('(prefers-reduced-motion: reduce)')`) {
		t.Fatal("login surface is missing the fabric canvas or its motion contract")
	}
	admin := appShell("KitsuSync", "", "en", nil, `<a href="/bot/admin">Admin</a>`, `<h1>Dashboard</h1>`)
	if !strings.Contains(admin, `data-background="app-dots"`) {
		t.Fatal("admin surface is missing the ambient dot canvas")
	}
	public := appShell("KitsuSync", "", "en", nil, "", `<h1>Public</h1>`)
	if strings.Contains(public, `data-background=`) {
		t.Fatal("non-login public surface unexpectedly received a background canvas")
	}
}

func TestBackgroundCanvasUsesReducedMotionAndNonRepellingPointerContracts(t *testing.T) {
	for _, want := range []string{
		`matchMedia('(prefers-reduced-motion: reduce)')`,
		`if(!reduced.matches)frame=requestAnimationFrame(tick)`,
		`pointer.target=1`,
		`pointer.target=0`,
		`sheetWidth=180`,
		`width=document.documentElement.clientWidth`,
		`const band=Math.min(sheetWidth,leftEdge,width-rightEdge)`,
		`const depthPhase=(inward*band*.012+seconds*flowSpeed)`,
		`const wavePhase=depthPhase+(v*6.2)+.9`,
		`const spacing=34`,
		`const local=Math.exp`,
	} {
		if !strings.Contains(backgroundCanvasScript, want) {
			t.Errorf("background script missing expected behavior %q", want)
		}
	}
	for _, forbidden := range []string{"Math.atan2", "repel", "createRadialGradient", "shadowBlur", "xBase*.012", "xBase*.075", "field.direction*.9"} {
		if strings.Contains(backgroundCanvasScript, forbidden) {
			t.Errorf("background script contains forbidden effect %q", forbidden)
		}
	}
}

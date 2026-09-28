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
		`width=document.documentElement.clientWidth`,
		`const maxHalfHeight=Math.min(height*(mobile?.04:.14),mobile?30:92)`,
		`const columns=mobile?Math.max(24,Math.ceil(width/12)):Math.max(80,Math.ceil(width/8))`,
		`const ribbonLayers=mobile?2:3`,
		`const alpha=(.075+depth*.19+local*.09+organic)*edgeTaper*cardAttenuation*(mobile?.58:1)`,
		`const xBase=u*width`,
		`const halfHeight=maxHalfHeight*edgeTaper*centerNarrowing`,
		`const wavePhase=u*5.2+seconds*flowSpeed+layer*.71`,
		`const cardAttenuation=insideCard?.78:1`,
		`const spacing=34`,
		`const local=Math.exp`,
	} {
		if !strings.Contains(backgroundCanvasScript, want) {
			t.Errorf("background script missing expected behavior %q", want)
		}
	}
	for _, forbidden := range []string{"Math.atan2", "repel", "createRadialGradient", "shadowBlur", "sheetWidth=180", "const fields=[", "field.direction"} {
		if strings.Contains(backgroundCanvasScript, forbidden) {
			t.Errorf("background script contains forbidden effect %q", forbidden)
		}
	}
}

func TestLoginFabricUsesTheWholeViewportAsAHorizontallyTaperedRibbon(t *testing.T) {
	for _, want := range []string{
		`const xBase=u*width`,
		`const edgeTaper=smoothstep(0,.08,u)*smoothstep(0,.08,1-u)`,
		`const centerNarrowing=1-.18*Math.exp(-Math.pow((u-.5)/.18,2))`,
		`const ribbonLayers=mobile?2:3`,
		`const slowWave=Math.sin(wavePhase+Math.sin(seconds*.12+u*2+q)*.6)*slowAmplitude`,
		`const flutter=Math.sin(u*width*(mobile?.055:.075)+q*17+seconds*1.7+layer*.83)*(mobile?1:1.5)`,
	} {
		if !strings.Contains(backgroundCanvasScript, want) {
			t.Errorf("horizontal login ribbon is missing %q", want)
		}
	}
	for _, forbidden := range []string{"leftEdge-band", "rightEdge+band", "const fields=[", "const band=Math.min"} {
		if strings.Contains(backgroundCanvasScript, forbidden) {
			t.Errorf("login fabric still renders as two side curtains (%q)", forbidden)
		}
	}
}

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
		`const mobile=width<600`,
		`const rowCount=mobile?13:20`,
		`cardCenterY=mobile?rect.top-42:height*.5`,
		`const spacingX=mobile?15:Math.max(8,width>1700?10:9)`,
		`const cursorGustAmp=1+gust*cursorGust*pointer.amount`,
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

func TestLoginFabricPortsLayeredSilkSurfaceModel(t *testing.T) {
	for _, want := range []string{
		`const evaluateSideClothProfile=(side,colX,seconds,u)=>{`,
		`const clothBuckets=Array.from({length:10},()=>({coords:new Float32Array(maxDotsPerTier*3),count:0}))`,
		`const travelTime=seconds*.36*flowDir`,
		`const mirroredX=isLeft?centerX-colX:colX-centerX`,
		`const phiM1=mobile?warpedX*.00205-travelTime*.65+phaseOffset:isLeft?warpedX*.00205-travelTime*.65:warpedX*.00225+travelTime*.62+2.4`,
		`const effectiveWidth=sheetWidth*widthFunnelRatio*outerTaperFactor`,
		`const profileWave=(stokes*42+stokesSub*20)*waveHeightEnv*silhouetteScale`,
		`const totalRoll=obliqueAngle+dynamicTwist`,
		`const collectSurfaceVertices=(seconds)=>{`,
		`const catenaryZ=(1-v*v*.85)*(halfW*.32)*drapeCamber`,
		`const fineDepthZ=(Math.sin(depthWavePhase)*22+Math.cos(depthWavePhase*1.8+.5)*10)*fineDepthScale*profile.verticalFunnelEnvelope`,
		`const worldZ=profile.spineZ+deltaZ`,
		`const scale=focalLength/(focalLength+worldZ)`,
		`const cardDissolve=quintic(cardNorm)`,
		`const mobile=width<600`,
		`const rowCount=mobile?13:20`,
		`const spacingX=mobile?15:Math.max(8,width>1700?10:9)`,
		`const cursorGustAmp=1+gust*cursorGust*pointer.amount`,
		`const gustScale=1+profile.gust*pointer.amount*(mobile?.18:.35)`,
	} {
		if !strings.Contains(backgroundCanvasScript, want) {
			t.Errorf("horizontal login ribbon is missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`const columns=mobile?Math.max(28,Math.ceil(width/11)):Math.max(80,Math.ceil(width/8))`,
		`const wavePhase=u*5.2+seconds*flowSpeed+layer*.71`,
		`const ribbonLayers=mobile?2:3`,
		`leftEdge-band`, `rightEdge+band`, `const fields=[`, `const band=Math.min`,
	} {
		if strings.Contains(backgroundCanvasScript, forbidden) {
			t.Errorf("login fabric still uses the superseded flat-ribbon renderer (%q)", forbidden)
		}
	}
}

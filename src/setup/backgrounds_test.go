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

func TestLoginForegroundSurfacesAreOpaque(t *testing.T) {
	for _, want := range []string{
		`.login-card{margin:0!important}`,
		`body.login-surface main:has(.login-card[style*="520px"]) .login-card{background:#111214}`,
		`.login-card .section-card{background-color:#151619}`,
	} {
		if !strings.Contains(adminThemeCSS, want) {
			t.Errorf("login foreground is missing its opaque surface rule %q", want)
		}
	}
}

func TestBackgroundCanvasUsesReducedMotionAndNonRepellingPointerContracts(t *testing.T) {
	for _, want := range []string{
		`matchMedia('(prefers-reduced-motion: reduce)')`,
		`mode==='login-fabric'||!pointerSettled`,
		`mode==='login-fabric'||Math.abs(pointer.amount-pointer.target)>=.002`,
		`pointer.target=mode==='app-dots'&&reduced.matches?0:1`,
		`pointer.target=0`,
		`width=document.documentElement.clientWidth`,
		`const mobile=width<600`,
		`const meshRows=mobile?13:params.meshRows`,
		`centerY=mobile?rect.top-42:height*.5`,
		`pointer.targetX-pointer.x)*.10`,
		`Math.pow(.94,elapsed*60)`,
		`const spacing=34`,
		`const local=Math.exp`,
		`const alpha=.04+local*.034`,
		`const radius=.92+local*.18`,
		`rgba(218,126,82,alpha)`,
		`if(mode==='app-dots'&&!reduced.matches&&!frame&&!document.hidden)`,
	} {
		if !strings.Contains(backgroundCanvasScript, want) {
			t.Errorf("background script missing expected behavior %q", want)
		}
	}
	for _, forbidden := range []string{"Math.atan2", "repel", "createRadialGradient", "shadowBlur", "const fields=[", "field.direction", `const slow=Math.sin(x*.003+y*.004+seconds*.09)`, `local*Math.sin`, `local*Math.cos`, `ctx.translate(`} {
		if strings.Contains(backgroundCanvasScript, forbidden) {
			t.Errorf("background script contains forbidden effect %q", forbidden)
		}
	}
}

func TestAuthenticatedDotGridIsStaticAndViewportAnchored(t *testing.T) {
	start := strings.Index(backgroundCanvasScript, `const drawDots=()=>{`)
	end := strings.Index(backgroundCanvasScript[start:], `const draw=seconds=>`)
	if start < 0 || end < 0 {
		t.Fatal("authenticated dot-grid renderer is missing")
	}
	dots := backgroundCanvasScript[start : start+end]
	for _, want := range []string{`const spacing=34`, `for(let y=18;y<height;y+=spacing)`, `for(let x=18;x<width;x+=spacing)`, `ctx.arc(x,y,radius,0,Math.PI*2)`} {
		if !strings.Contains(dots, want) {
			t.Errorf("authenticated dot grid is missing fixed viewport geometry %q", want)
		}
	}
	for _, forbidden := range []string{"seconds", "Math.sin", "Math.cos", "translate", "centerX", "centerY"} {
		if strings.Contains(dots, forbidden) {
			t.Errorf("authenticated dot-grid positions or appearance depend on global motion/card geometry (%q)", forbidden)
		}
	}
}

func TestLoginFabricPortsLayeredSilkSurfaceModel(t *testing.T) {
	for _, want := range []string{
		`const desktopBaseline={meshRows:26,meshSpacingX:7.0,sheetWidth:180,centerMinWidth:36`,
		`convergenceStrength:1.00,outerTaper:.95,obliqueAngleDeg:20.0,dotSize:.85,depthContrast:5.00,backgroundSparsity:0.00,foldBrightness:1.20`,
		`microFlutter:1.70,flowSpeed:.30,silhouetteAmp:1.15,heightEnvelope:1.25,intervalWarp:1.20,rollTorsion:1.25,macroTwist:1.15,fineDepthWave:1.20,edgeFlutter:1.15,drapeCamber:.88`,
		`cursorGust:.35,cursorRadius:220,cursorRecovery:.06`,
		`const tierColors=[`,
		`rgba(18,10,8,.08)`, `rgba(46,16,6,.18)`, `rgba(82,26,8,.34)`, `rgba(118,38,10,.50)`,
		`rgba(156,52,14,.68)`, `rgba(196,68,18,.82)`, `rgba(228,84,21,.92)`, `rgba(248,102,26,.97)`,
		`rgba(255,126,34,1)`, `rgba(255,150,48,1)`,
		`const maxDotsPerTier=36000`,
		`const evaluateSideClothProfile=(side,colX,seconds,u,pointerGust)=>{`,
		`const flowDir=isLeft?1:-1`,
		`const travelTime=seconds*.36*flowDir`,
		`const streamDist=seconds*52*params.flowSpeed`,
		`const staggerX=row%2===0?0:.5*spacingX`,
		`xWarp=(Math.sin(colX*.00118+travelTime*.30)*56+Math.cos(colX*.00270-travelTime*.20)*24)*params.intervalWarp`,
		`xWarp=(Math.cos(colX*.00128+travelTime*.26+1.7)*52+Math.sin(colX*.00290-travelTime*.16)*26)*params.intervalWarp`,
		`phiM1=warpedX*.00205-travelTime*.65`,
		`phiM1=warpedX*.00225+travelTime*.62+2.4`,
		`const pointerDistance=Math.sqrt((dx/params.cursorRadius)**2+(dy/verticalRadius)**2)`,
		`const pointerGust=smoothstep(1,0,pointerDistance)*pointer.amount`,
		`const verticalRadius=mobile?clamp(base.effectiveWidth*.48,24,58):clamp(base.effectiveWidth*.55,70,120)`,
		`canvas.dataset.meshRows=String(meshRows)`,
		`canvas.dataset.meshSpacingX=String(spacingX)`,
		`canvas.dataset.streamDistance=String(streamDist)`,
		`const fineDepthZ=(Math.sin(depthWavePhase)*22+Math.cos(depthWavePhase*1.8+.5)*10)*params.fineDepthWave*profile.verticalFunnelEnvelope`,
		`const sizeMultiplier=.65+Math.pow(prominence,2.6)*(params.depthContrast*.52)`,
	} {
		if !strings.Contains(backgroundCanvasScript, want) {
			t.Errorf("horizontal login ribbon is missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`const rowCount=mobile?13:20`,
		`const spacingX=mobile?15:Math.max(8,width>1700?10:9)`,
		`const maxDotsPerTier=12000`,
		`const clothColors=[`,
		`cardDissolve`,
		`projectedX>=rect.left`,
		`Math.abs(colX-pointer.x)`,
	} {
		if strings.Contains(backgroundCanvasScript, forbidden) {
			t.Errorf("login fabric still uses a simplified renderer or card moat (%q)", forbidden)
		}
	}
}

# Runtime Agent D — Motion audit

Date: 2026-08-28 JST
Target: `C:\Users\mynti\Documents\KitsuSync-clean`
Scope: runtime motion for `particleDrift`, `riseIn`, and CSS transitions under baseline/no-preference and emulated `prefers-reduced-motion: reduce`.

## Runtime boundary and evidence

The available runtime evidence is the unauthenticated Login renderer only. It was captured with Playwright Chromium at CSS viewports 375×812, 768×1024, 1024×768, and 1440×900, in both Japanese and English. The 1440×900 state has top and full-page captures. The recorded runtime identity is container `kitsusync-8090-current`, image `kitsusync:v0.4.4-current`, with worktree/image correspondence not cryptographically proven.

Source references below are to the current worktree; runtime facts are taken from `docs/UI-UX-RUNTIME-EVIDENCE.md` and the supplied images under `docs/audits/runtime-evidence/`.

## Confirmed

### D-M01 — `particleDrift` is active on Login in baseline/no-preference and remains active under reduce

- Status: CONFIRMED
- Exact evidence: `src/setup/ui.go:45-56` defines a fixed, viewport-wide `body::before` dot field with `opacity:.22` and `animation:particleDrift 18s linear infinite`; `src/setup/ui.go:67-70` moves the first background layer from `0 0` to `34px 68px`.
- Runtime evidence: `docs/UI-UX-RUNTIME-EVIDENCE.md:38` records `body::before` with `animation-name: particleDrift` and `animation-duration: 18s` in the Login runtime, and records the same animation under emulated `prefers-reduced-motion: reduce`.
- Viewport: Login JP/EN at 375×812, 768×1024, 1024×768, and 1440×900; top/full at 1440×900. The runtime style observation is not viewport-specific beyond this supplied Login matrix.
- Assessment: This is continuous decorative motion, not task feedback. It may compete with reading and status inspection, especially during longer operational sessions.
- Severity: P2 / High accessibility and attention risk under reduce; P3 / Medium visual distraction in baseline.
- Confidence: High for the source rule and recorded reduce result; medium-high for baseline/no-preference because the evidence document records the computed style but does not include the raw browser trace.

### D-M02 — `riseIn` is applied to representative page surfaces and remains active under reduce

- Status: CONFIRMED
- Exact evidence: `src/setup/ui.go:71-74` defines `riseIn` as `opacity:0` plus `translateY(10px)` to `opacity:1` and `translateY(0)`; `src/setup/ui.go:285-286` applies `.42s ease both` to `.tile`, `.section-card`, and `.page-card`.
- Runtime evidence: `docs/UI-UX-RUNTIME-EVIDENCE.md:38` records representative elements retaining `riseIn` with duration `0.42s` under emulated `prefers-reduced-motion: reduce`.
- Viewport: Login JP/EN across the supplied 375×812, 768×1024, 1024×768, and 1440×900 captures. No authenticated surface was runtime-reached.
- Assessment: The short entrance movement is low amplitude, but it is still motion that remains enabled for users who requested reduction. Multiple cards on protected pages are not runtime-confirmed here.
- Severity: P2 / Medium accessibility defect under reduce; P3 / Low baseline impact.
- Confidence: High for the source declaration and recorded reduce result; medium for the perceptual impact because no frame recording or timing trace was supplied.

### D-M03 — Functional CSS transitions are present and remain active under reduce

- Status: CONFIRMED
- Exact evidence: `src/setup/ui.go:218-228` sets `transition:all .18s ease` on navigation/action links; `src/setup/ui.go:251-262` transitions the language thumb with `left .18s ease`; `src/setup/ui.go:285` transitions tile transform/border; `src/setup/ui.go:343` transitions form border, shadow, and background; `src/setup/ui.go:351-352` transitions button transform/opacity/shadow; `src/setup/ui.go:396-397` transitions accordion-caret rotation.
- Runtime evidence: `docs/UI-UX-RUNTIME-EVIDENCE.md:38` records representative elements retaining `transition: all` under emulated `prefers-reduced-motion: reduce`.
- Viewport: Login JP/EN at all supplied Login viewports. The language-thumb transition is source-confirmed but its activation was not recorded as an interaction in the supplied captures.
- Assessment: These are generally short, low-amplitude interaction cues. Their presence under reduce is nevertheless unhandled because no reduced-motion policy is present.
- Severity: P2 / Medium for the unhandled reduce preference; P3 / Low for baseline interaction impact.
- Confidence: High for source presence and recorded computed-style result; low-to-medium for actual transition perception because no pointer/keyboard interaction trace was supplied.

### D-M04 — No reduced-motion override exists in the inspected UI source

- Status: CONFIRMED
- Exact evidence: `rg` over `src/setup/ui.go`, `src/setup/ia_views.go`, `src/setup/admin.go`, and `site.jsx` found no `prefers-reduced-motion` rule. The motion declarations are at `src/setup/ui.go:56`, `:228`, `:260`, `:285-286`, `:343`, `:351`, and `:396`.
- Viewport: Source-level finding; runtime coverage is limited to Login at 375×812, 768×1024, 1024×768, and 1440×900.
- Assessment: The absence explains why the recorded reduce run retained the same animation and transition styles.
- Severity: P2 / High because a user preference is demonstrably not honored for the tested Login renderer.
- Confidence: High.

## Rejected

No scoped motion finding is rejected by the available evidence. The evidence does not demonstrate that any of `particleDrift`, `riseIn`, or the listed transitions are disabled under reduce, so none can be downgraded to “rejected.”

The claim that these rules affect every authenticated route is not rejected; it is untested because the protected renderer was not reached. The source suggests shared UI coverage, but this report does not convert that into runtime confirmation.

## Untested

### D-U01 — Authenticated route motion

- Status: UNTESTED
- Viewport: All authenticated JP/EN states at 375×812, 768×1024, 1024×768, and 1440×900.
- Evidence gap: `docs/UI-UX-RUNTIME-EVIDENCE.md:21-32` states that only unauthenticated Login was represented; Dashboard, Productions, Connections, User Linking, System Status, Audit Log, Setup, diagnostics, and dialogs were not runtime-confirmed.
- Severity: Not assigned; route-specific defect severity cannot be established from Login evidence.
- Confidence: High that these states are absent from the supplied runtime evidence.

### D-U02 — Actual baseline/no-preference versus reduce animation behavior over time

- Status: UNTESTED
- Evidence gap: No video, frame trace, computed `animationPlayState`, sampled transform values, or before/after timing record is supplied. The evidence document records computed styles, not observed frame motion.
- Viewport: The supplied Login matrix only; no intermediate widths, zoom levels, low-performance devices, or long-duration sessions.
- Severity: Not assigned beyond the confirmed style-level reduce defect.
- Confidence: High that the perceptual comparison was not performed.

### D-U03 — System Status refresh and chart updates

- Status: UNTESTED
- Exact source evidence: `src/setup/ia_views.go:347-348` and `:1355-1357` contain a five-second polling loop and replace status/details/chart markup. No motion-preference branch is visible in the inspected script.
- Evidence gap: System Status was not runtime-reached; actual chart-change frequency, visual update behavior, and error/recovery transitions were not observed.
- Viewport: All protected viewports and both languages.
- Severity: Not assigned; functional refresh must not be conflated with decorative animation without runtime observation.
- Confidence: High for untested runtime coverage; high for the source-level polling declaration.

### D-U04 — Interaction-triggered transition activation

- Status: UNTESTED
- Evidence gap: The screenshots show initial rendered states only. No hover, focus, click, keyboard, accordion, locale-switch, drag, or resize trace is included.
- Viewport: Login supplied viewports; protected interaction surfaces not reached.
- Severity: Not assigned.
- Confidence: High.

## Evidence index

Runtime screenshots inspected:

- `docs/audits/runtime-evidence/login_ja_375x812_top.png`
- `docs/audits/runtime-evidence/login_ja_768x1024_top.png`
- `docs/audits/runtime-evidence/login_ja_1024x768_top.png`
- `docs/audits/runtime-evidence/login_ja_1440x900_top.png`
- `docs/audits/runtime-evidence/login_ja_1440x900_full.png`
- `docs/audits/runtime-evidence/login_en_375x812_top.png`
- `docs/audits/runtime-evidence/login_en_768x1024_top.png`
- `docs/audits/runtime-evidence/login_en_1024x768_top.png`
- `docs/audits/runtime-evidence/login_en_1440x900_top.png`
- `docs/audits/runtime-evidence/login_en_1440x900_full.png`

Primary written evidence: `docs/UI-UX-RUNTIME-EVIDENCE.md:19-38, 53-75, 83-104`.
Primary source evidence: `src/setup/ui.go:45-74, 218-228, 251-262, 285-286, 343-352, 396-397`; `src/setup/ia_views.go:347-348, 1355-1357`.

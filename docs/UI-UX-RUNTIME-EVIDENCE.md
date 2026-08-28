# KitsuSync UI/UX Runtime Evidence

Date: 2026-08-28 JST
Target: `C:\Users\mynti\Documents\KitsuSync-clean`

## Runtime boundary and identity

| Item | Exact evidence |
|---|---|
| Active renderer reached | Unauthenticated Login renderer for `/bot/login?lang=ja` and `/bot/login?lang=en`; protected renderers were not reached. `/bot/admin` and protected aliases redirected to Login. |
| Branch | `codex/v0.4.4-notification-card` |
| HEAD/build SHA | `1a070c403a0bddeabe63086f037c6bb4f02fd8ca` |
| Container/image | `kitsusync-8090-current` / `kitsusync:v0.4.4-current`, image ID `sha256:53b5a58007f3839bdf3ec4712f800df478bd548047a3c31d49ec25aad2b4c005` |
| Binding/health | `127.0.0.1:8090`; `GET /health = 200` |
| Renderer/build caveat | Image labels report revision `unknown`; worktree-to-image correspondence is not cryptographically proven. The image was already running and was not rebuilt for this pass. |
| Browser | Playwright Chromium, explicit CSS viewport, `deviceScaleFactor: 1`; unauthenticated session; no credentials, secrets, cookies, tokens, POST mutations, or external state changes. |

The repository was already dirty. Existing changes were preserved. This reconciliation changes only this evidence document.

## Exact runtime coverage

The supplied artifacts contain eight locale×viewport Login states and ten PNG files because the 1440×900 state has both `top` and `full` captures. PNG headers were checked against the filenames; all dimensions match.

| State | Viewport | Screenshot evidence |
|---|---:|---|
| Login, ja | 375×812 | `docs/audits/runtime-evidence/login_ja_375x812_top.png` |
| Login, ja | 768×1024 | `docs/audits/runtime-evidence/login_ja_768x1024_top.png` |
| Login, ja | 1024×768 | `docs/audits/runtime-evidence/login_ja_1024x768_top.png` |
| Login, ja | 1440×900 | `docs/audits/runtime-evidence/login_ja_1440x900_top.png`, `login_ja_1440x900_full.png` |
| Login, en | 375×812 | `docs/audits/runtime-evidence/login_en_375x812_top.png` |
| Login, en | 768×1024 | `docs/audits/runtime-evidence/login_en_768x1024_top.png` |
| Login, en | 1024×768 | `docs/audits/runtime-evidence/login_en_1024x768_top.png` |
| Login, en | 1440×900 | `docs/audits/runtime-evidence/login_en_1440x900_top.png`, `login_en_1440x900_full.png` |

| Screen/state | ja | en | Runtime verdict |
|---|---:|---:|---|
| Login | 4 viewports | 4 viewports | Captured; 10 screenshots |
| Dashboard | no | no | `UNTESTED`; protected route redirected |
| Productions list/detail | no | no | `UNTESTED`; protected route redirected |
| Connections read/edit | no | no | `UNTESTED`; protected route redirected |
| User Linking | no | no | `UNTESTED`; protected route redirected |
| System Status | no | no | `UNTESTED`; protected route redirected |
| Audit Log | no | no | `UNTESTED`; protected route redirected |
| Setup/diagnostics/dialogs | no | no | `UNTESTED`; no safe authenticated state was available |

## Direct Login observations

These are runtime observations for the active Login renderer only:

- All eight locale×viewport states had `clientWidth == scrollWidth == viewport width`; no horizontal overflow, visible clipping, or failed requests was observed. No console errors or warnings were observed in the recorded pass.
- Inputs and the Login/ログイン button rendered at 44px height. The screenshots show the email control with an orange outline; a screenshot alone does not prove that it is keyboard focus.
- The page exposed `main` and `form` landmarks and one `h1` (`KitsuSync`) in the recorded runtime inspection. Visible labels and Login controls were present in both locales.
- At 375×812, Japanese instructional copy wrapped to two lines inside the panel; the fields and CTA remained visible. English remained one line. This is a reflow observation, not a defect by itself.
- At 1440×900, the `full` captures showed no additional Login content below the visible form and no unexpected vertical overflow.
- The locale control was below the brand at 375px and at the upper right at 768px, 1024px, and 1440px. Both options remained contained.
- Computed body font was `Outfit, "Noto Sans JP", sans-serif`; body letter-spacing was `0.13px`.
- `body::before` had `animation-name: particleDrift` and `animation-duration: 18s`. Under emulated `prefers-reduced-motion: reduce`, `particleDrift` remained active; representative elements retained `riseIn` (`0.42s`) and `transition: all`.

## Reconciliation of previous findings

Status meanings: `CONFIRM` = the runtime evidence supports the finding as stated for the reached renderer; `REJECT` = the supplied runtime evidence contradicts the finding; `MODIFY` = only a narrower/different claim is supported; `UNTESTED` = the relevant renderer or interaction was not reached.

| Previous finding(s) | Runtime evidence and contradiction note | Decision | Final runtime verdict |
|---|---|---|---|
| Global dot field / ambient particles (Agents 2, 7, 8, 11, 12) | Login screenshots show repeated dots at all eight states; computed Login `body::before` is active with 18s `particleDrift`. Protected-route application is not runtime-proven. | `MODIFY` | Confirmed as Login decoration; protected operational-page impact `UNTESTED`. |
| Gradient, glow, and glass treatment (Agents 2, 8, 11; Agent A) | Warm gradient/glow and layered dark panels are visible in Login. Agent A rejects “confirmed glassmorphism” from screenshots because blur/translucency cannot be proven. Source-level `.glass` claims for protected pages were not rendered in this pass. | `MODIFY` | Login has visible layered panels and warm emphasis; frosted glass and protected-page scope `UNTESTED`, not a screenshot-confirmed runtime fact. |
| Stacked/nested cards and anti-card concern (Agents 2, 8, 12; Agent A A-01) | Login shows an inner lighter rounded form panel inside a darker rounded shell at all sampled sizes. No authenticated card hierarchy was captured. | `MODIFY` | Login card-on-card appearance `CONFIRM`; protected nested-card severity `UNTESTED`. |
| Login dead space (Agent A A-05) | JP/EN 768×1024 and 1440×900 captures show a compact task with a large uninterrupted lower field; 1440 full captures add no content. | `CONFIRM` | Login composition observation confirmed, P2 visual concern; no claim about protected pages. |
| Login warm orange/brown gradient and action glow (Agent A A-04) | Visible in JP/EN captures, strongest around upper-left/form area and primary action. | `CONFIRM` | Login visual observation confirmed, P3; product intent remains outside runtime evidence. |
| Dashboard hierarchy / repeated metrics and CTA (Agents 1, 2, 8, 11) | Dashboard was not reached; `/bot/admin` redirected to Login. | `UNTESTED` | No runtime verdict. Static source findings remain hypotheses for a future authenticated pass. |
| Setup Step 4 density and execution/recovery flow (Agents 0, 1, 4, 9) | `/bot/setup` and setup execution were not authenticated or submitted. | `UNTESTED` | No runtime verdict; no mutation or destructive action was attempted. |
| Production detail IA, tabs, storage context, routing/diagnosis (Agents 0, 3, 9, 10) | Protected Production routes were not reached. | `UNTESTED` | No runtime verdict. |
| Connections: independent Kitsu/Discord states (Agent 1 COG-001) versus separate named sections (Agents 0, 8; Current IA/spec) | This is a genuine prior-report contradiction: Agent 1 says the current rendered state is a single status-list that does not independently expose Kitsu/Discord; Agent 0/8 cite `admin.go` separate named sections, while the Current IA spec expects independent cards/badges. `/bot/admin/bot` redirected to Login, so no active Connections DOM or screenshot can adjudicate the contradiction. | `UNTESTED` | Do not accept or reject either structure from this pass. Authenticated read and `?edit=1` captures are required. |
| Connection Map feasibility/scope (Agents 10, 12; Distributed Audit) | No Connection Map route or protected renderer was reached. Static reports agree it should not be assumed as top-level or in-graph editor, but this is not runtime evidence. | `UNTESTED` | Runtime cannot validate need, density, mobile fallback, or visual feasibility. |
| User Linking and Production-local association (Agents 0, 3, 9) | Protected User Linking and Production detail were not reached. | `UNTESTED` | Scope relationship remains source/spec evidence only. |
| System Status, SVG charts, polling, diagnostics/test notification (Agents 0, 1, 5, 7, 9) | `/bot/admin/health` was not reached. No chart, refresh, SVG, test-send, error, or focus state was observed. | `UNTESTED` | No runtime verdict; functional refresh must not be treated as observed decorative motion. |
| Audit Log and activity surfaces (Agents 0, 3, 7, 9) | Protected Audit Log/Activity states were not reached. | `UNTESTED` | No runtime verdict. |
| Form labels and accessible names (Agents 4, 5; Agent C) | Login has visible localized labels; the recorded Login runtime inspection reported `main`/`form` and named controls. Agent C correctly notes that screenshots alone cannot prove programmatic label association. Protected edit forms were not reached. | `MODIFY` | Login visible/recorded control naming is supported; DOM association, keyboard behavior, and all protected forms `UNTESTED`. |
| Language selector accessible naming (Agent 5 A5-008; Agent C A11Y-C-002) | JP/EN state is visibly indicated in every Login screenshot, but the recorded evidence does not include keyboard activation, `aria-current`, locale-specific accessible naming, or announcement behavior. | `UNTESTED` | Visual selected-state evidence only; semantic and assistive-technology behavior is untested. |
| Dialog naming/description and destructive confirmation variants (Agents 4, 5) | No protected dialog was opened; no POST or destructive action was performed. | `UNTESTED` | No runtime verdict. |
| SVG non-text alternatives (Agent 5) | No System Status SVG was rendered. | `UNTESTED` | No runtime verdict. |
| Target sizes (Agents 4, 5; Distributed Audit) | Login inputs and button were measured at 44px height. Compact admin controls were not rendered or measured. | `MODIFY` | Login target-height concern not reproduced; admin target sizes `UNTESTED`. |
| Login responsive overflow/wrapping (Agent B R-01/R-03; Agent E E-TYPO-002/E-TYPO-004; Agent C A11Y-C-004) | Exact PNG dimensions match 375×812, 768×1024, 1024×768, and 1440×900. No horizontal overflow or clipping was observed. JP copy wraps at 375px without overlap; CTA remains visible. | `REJECT` for defect claim; `CONFIRM` for observed wrap | Supplied Login overflow defect not reproduced. Zoom, text-spacing, intermediate widths, errors, and protected data remain `UNTESTED`. |
| Login locale-control responsive behavior (Agent B R-02) | Position changes at 375px versus wider sampled viewports; both JP/EN controls remain contained. | `CONFIRM` observation | Responsive repositioning confirmed; exact breakpoint and keyboard semantics `UNTESTED`. |
| Typography: Outfit body stack (Agents 6, 11; Agent E E-TYPO-001) | Login computed style is `Outfit, "Noto Sans JP", sans-serif` across the supplied locale/viewport matrix. | `CONFIRM` | Login body stack confirmed; font-file loading is not proven by family string alone. |
| Typography: Space Grotesk actual load (Agent E E-TYPO-006) | No computed family or font-resource status for `.eyebrow`, `.lang-option`, labels, buttons, or table headers was recorded. | `UNTESTED` | No runtime verdict. |
| Typography: actual Noto Sans JP glyph fallback, cross-platform behavior, FOUT/FOIT/offline (Agents 6, 11; Agent E E-TYPO-005/007/009) | Runtime records the family list only; no `document.fonts` result, glyph provenance, request status, fallback screenshot, or forced network failure. | `UNTESTED` | Do not state that Noto Sans JP loaded or supplied the glyphs. |
| Reduced motion (Agents 5, 7, 8, 11, 12; Agent D D-M01–D-M04) | Login computed styles retain `particleDrift`, `riseIn`, and representative `transition: all` under emulated `prefers-reduced-motion: reduce`; source has no override. | `CONFIRM` | Login reduced-motion handling defect confirmed, High/P2. Authenticated route impact and actual frame-by-frame motion remain `UNTESTED`. |
| Interaction transitions/hover/focus/accordion/drag (Agents 4, 7; Agent D D-M03/D-U04) | Initial Login screenshots only; no pointer, keyboard, locale-switch, accordion, drag, resize, or timing trace. | `MODIFY` | Style declarations are recorded for Login; activation and protected interaction behavior `UNTESTED`. |
| Mobile navigation duplication and responsive admin density (Agents 0, 3, 5, 8, 11) | Login has no admin navigation. No authenticated 375/768/1024/1440 admin screen was captured. | `UNTESTED` | No runtime verdict on mobile nav, tables, tabs, or protected density. |
| Operational findings OPS-01–OPS-07 (Agent 9) | Setup execution, partial rollback, Production repair, workflow diagnosis, test notification, pause/resume, and result redirect were not exercised in an authenticated runtime. | `UNTESTED` | Source-supported operational risks remain unverified runtime claims; no external action was taken. |

## Final verdicts

| Topic | Verdict after reconciliation |
|---|---|
| Login viewport containment | `CONFIRMED` for the eight sampled locale×viewport states; no Login overflow defect reproduced. |
| Login visual treatment | Dot texture, warm gradient/glow, card-on-card layering, and lower dead space are runtime-observed; “glassmorphism” is not proven from screenshots. |
| Login typography | Outfit family list and JP/EN copy behavior confirmed; actual webfont loading and glyph fallback unproven. |
| Login accessibility | Visible labels, landmarks, and a recorded named-control result are limited positive evidence; association, keyboard, screen reader, zoom, contrast, and error states are not fully proven. |
| Login motion | Reduced-motion defect confirmed from computed styles; no claim is made about protected pages or frame-level perceptual severity. |
| Protected/current IA screens | `UNTESTED` across Dashboard, Productions, Connections, User Linking, System Status, Audit Log, Setup, diagnostics, dialogs, and authenticated JP/EN states. |
| Connections contradiction | `UNTESTED`; no report-side assertion is promoted to runtime truth. |
| Design readiness | `KITSUSYNC_UIUX_MORE_EVIDENCE_REQUIRED`. Do not begin DESIGN.md or infer protected-screen acceptance from Login screenshots. |

## Remaining evidence gate

The remaining gate is an authenticated, read-only runtime pass using the same active renderer/build identity, with exact screenshots and recorded DOM/accessibility/computed-style evidence for JP and EN at 375×812, 768×1024, 1024×768, and 1440×900. It must reach representative Dashboard, Production list/detail, Connections read and `?edit=1`, User Linking, System Status, Audit Log, Setup review/blocked/error states, and dialogs without submitting destructive operations. It must specifically resolve the Connections contradiction, inspect font resource/fallback status, keyboard/focus and dialog semantics, reduced-motion behavior on protected screens, responsive admin navigation/tables, and representative empty/error/connected data states.

Source documents reconciled: `docs/UI-UX-DISTRIBUTED-AUDIT.md`; `docs/audits/uiux-agent0-inventory.md` through `uiux-agent12-adversarial.md`; `docs/audits/uiux-runtime-agentA-visual.md` through `uiux-runtime-agentE-typography.md`; `docs/audits/runtime-evidence/`; and the runtime/source references recorded in those documents. Primary implementation references include `src/main.go:859-940`, `src/setup/middleware.go`, `src/setup/ui.go:34-74,197-203,218-228,251-262,285-287,343-352,396-397`, `src/setup/ia_views.go`, and `src/setup/admin.go`.

## Authenticated runtime follow-up

Date: 2026-08-28 JST. The same running target was reached through the user's authenticated Chrome session. Runtime identity remained branch `codex/v0.4.4-notification-card`, HEAD/build SHA `1a070c403a0bddeabe63086f037c6bb4f02fd8ca`, container `kitsusync-8090-current`, image `kitsusync:v0.4.4-current`; the image revision label remains `unknown`, so worktree-to-image correspondence is still not cryptographically proven.

### Authenticated coverage

Authenticated live DOM and screenshot coverage reached Dashboard, Production list, Production detail empty/disconnected state, Connections read, User Linking, System Status, Audit Log, and Setup through the non-submitting Channel Plan and Review states. JP and EN were exercised across requested 375×812, 768×1024, 1024×768, and 1440×900 viewport overrides. The browser backend reports scaled effective CSS dimensions (for example requested 375×812 reported `innerWidth=416`, `innerHeight=902`); this is recorded as a backend viewport-scaling caveat, not application overflow.

Screenshot index: `docs/audits/runtime-evidence/auth_dashboard_ja_1440x900_top.png`, `auth_dashboard_ja_1440x900_full.png`, `auth_connections_en_1440x900_top.png`, `auth_connections_en_1440x900_full.png`, `auth_setup_ja_375x812_top.png`, plus the complete `auth_<screen>_<ja|en>_<width>x<height>_top.png` matrix for Dashboard, Productions, User Linking, System Status, Audit Log, and Setup, and 1440×900 `full` captures for that matrix.

### Authenticated findings and decisions

| Finding | Runtime evidence | Decision / severity / confidence / implication |
|---|---|---|
| Connections contradiction | `/bot/admin/bot?lang=ja|en` reached the active `BotHandlerWithRuntime` GET path, which calls `renderConnectionsPageSafeWithRuntime`; the rendered DOM and screenshots contain separate `Kitsu connection`/`Kitsu接続` and `Discord Bot connection`/`Discord Bot接続` sections. | `CONFIRMED` for the named-section renderer; P2 static contradiction resolved, High confidence. The separate sections are the current runtime truth. |
| Connections edit | `/bot/admin/bot?edit=1` redirected to `/bot/login` for step-up authentication. The edit renderer was not reached without another login step. | `UNTESTED` for edit-form layout/semantics; High confidence. Do not infer edit-form acceptance from source alone. |
| Sidebar/navigation | At 1440 the horizontal primary navigation is visible. At the effective narrow mobile state the header changes to a `メニュー`/`Menu` disclosure with a plus control; no duplicate desktop navigation was visible in the captured Setup state. | `ADOPT WITH LIMITS`; P2, Medium confidence. Keep the current responsive disclosure pattern; do not add a second persistent mobile sidebar without evidence. |
| Connection Map | No graph/map renderer or real routing visualization was present in reached screens. Setup Channel Plan is a tabular ordered plan, not a graph. | `DEFER`; High confidence. Current evidence does not establish a need or safe density for a map. |
| Serif/mincho typography | Authenticated pages computed the same body family list: `Outfit, "Noto Sans JP", sans-serif`. No serif/mincho renderer was observed. | `REJECT` as a current-runtime need; High confidence for reached pages. Do not introduce serif typography from the audit alone. |
| Dot/particle treatment | Authenticated screenshots show the same ambient dotted field/background treatment as Login. | `ADOPT WITH LIMITS`; Medium confidence. Treat it as ambient decoration; preserve content contrast and do not use it as information encoding. |
| Motion / reduced motion | Normal authenticated screenshots show no evidence of motion failure, but reduced-motion emulation was not available in the connected Chrome pass. Login previously retained `particleDrift`, `riseIn`, and `transition: all` under reduced motion. | `CONFIRMED` only for the previously reached Login defect; authenticated-page impact `UNTESTED`, High confidence in the boundary. |
| Responsive overflow | The authenticated route matrix reported `scrollWidth <= clientWidth` at all requested size/locale samples. Long Setup content produced vertical scrolling, not horizontal overflow. | `REJECT` for a reproduced horizontal-overflow defect; High confidence for reached states. Backend effective viewport scaling is documented above. |
| JP/EN parity | Dashboard, Productions, User Linking, System Status, Audit Log, Connections, and Setup all rendered localized headings/navigation and were reached in both locales where applicable. | `CONFIRMED` for basic locale rendering; Medium confidence for interaction-level parity. Setup review showed a URL language-state inconsistency after a non-submitting transition and requires a focused follow-up. |
| Destructive confirmation | The visible User Linking `解除` control executed immediately and redirected with `msg=saved`; no confirmation dialog opened. The original `Super Admin → サマータイムレンダリング` mapping was restored and re-verified. | `CONFIRMED BUG`, P1 safety/P2 UX, High confidence. A destructive unlink action lacks the requested confirmation barrier. No further destructive action was attempted. |
| Console errors | Chrome dev logs contained three extension message-channel errors during the pass; they were emitted by the Chrome extension context, not an application stack trace. | `MODIFY`, Low application attribution confidence. Do not classify as KitsuSync product errors without a clean non-extension console capture. |

### Authenticated final gate

Dashboard and empty/disconnected operational states are now runtime-supported. Production detail has no connected data in this target, so Overview/Notifications/Users tabs remain unavailable; Connections edit is gated by step-up auth; no real notification, Discord mutation, destructive deletion, or Setup execution was performed. System Status charts and read-only status cards were rendered; Audit Log was rendered empty. Design acceptance remains blocked by the missing connected-production detail state, edit-mode state, and reduced-motion/keyboard confirmation evidence.

Final marker: `KITSUSYNC_UIUX_RUNTIME_EVIDENCE_READY` is not issued until the remaining authenticated states are available. Current design readiness: `DESIGN_MD_READY = NO`.

## Final authenticated runtime gate — post-fix

Date: 2026-08-28 JST. The target was rebuilt from `KitsuSync-clean` at build/revision SHA `1a070c403a0bddeabe63086f037c6bb4f02fd8ca`, with image labels `org.opencontainers.image.revision=1a070c403a0bddeabe63086f037c6bb4f02fd8ca`, `org.opencontainers.image.source-id=KitsuSync-clean`, and `org.opencontainers.image.dirty=true`. The existing `C:\Users\mynti\Documents\Kitsu Sync\conf.toml` and `data` bind mounts were retained to preserve the current local runtime state; no secret contents were read or recorded. Container health returned HTTP 200.

### Remaining-gate results

| Gate | Result | Evidence / boundary |
|---|---|---|
| Unlink/remove confirmation | `FIXED / CONFIRMED` | Global User Linking and equivalent removal actions now enter the canonical confirmation dialog. The dialog includes the localized target name, `role=dialog`, `aria-labelledby`, `aria-describedby`, Cancel/Continue controls, default focus on Cancel, Escape/Cancel close, and focus return to the triggering control. EN and JP were exercised without submitting; no immediate mutation occurred after the fix. |
| Japanese destructive wording | `NO MOJIBAKE OBSERVED; CONNECTED PRODUCTION DIALOG DEFERRED` | Active User Linking JP copy rendered `Kitsuユーザー「Super Admin」とDiscordユーザー「サマータイムレンダリング」の紐づけを解除します。`. The connected Production destructive dialog could not be exercised because the retained runtime has no connected Production; no claim is made for that unavailable state. |
| Connections edit / step-up | `CONFIRMED` | Authenticated `?edit=1` renderer reached in EN and JP at requested 1440×900 and JP 375×812; 768×1024 and 1024×768 were also inspected with no horizontal overflow. Kitsu and Discord remain separate named sections. Evidence includes `docs/audits/runtime-evidence/auth_connections_edit_en_1440x900_top_postfix.png`, `auth_connections_edit_en_1440x900_full_postfix.png`, `auth_connections_edit_ja_1440x900_top_postfix.png`, `auth_connections_edit_ja_375x812_top_postfix.png`, `auth_connections_edit_en_768x1024_top_postfix.png`, and `auth_connections_edit_en_1024x768_top_postfix.png`. Secret fields remained undisplayed behind the existing Change token flow. |
| Connected Production detail | `CONNECTED_PRODUCTION_RUNTIME_EVIDENCE = DEFERRED` | The available Production is disconnected and has no connected detail tabs. Creating or mutating an external Discord resource was out of scope for this evidence pass. |
| Reduced motion | `SOURCE FIXED; LOGIN BASELINE CONFIRMED; REDUCED-MOTION EMULATION DEFERRED` | The shared theme now disables ambient particle animation, card entrance animation, nonessential transitions, and hover transforms under `prefers-reduced-motion: reduce`. Normal authenticated Connections computed styles remain active (`particleDrift`, `riseIn`, normal button transition), and the CSS rule is present in the rendered stylesheet. The connected Chrome control surface cannot emulate the media preference, so protected-page computed values under an emulated reduce profile are not claimed. |
| Keyboard / accessibility | `CONFIRMED FOR REACHED STATES` | Connections edit at the narrow state exposed skip link, home, Menu, language toggle, labeled form controls, actions, and navigation in sequence. The unlink dialog focus, accessible name/description, Escape behavior, and focus return were verified in EN and JP. Authenticated pages reported one main landmark and one h1 with no horizontal overflow in the sampled matrix. |
| Typography | `PARTIAL / CONFIRMED STACK` | Authenticated Connections edit reported `document.fonts.status=loaded` and computed `Outfit, "Noto Sans JP", sans-serif`; the FontFaceSet contained no inspectable font faces and no font resource provenance was recorded. Actual glyph provenance and offline fallback remain `DEFERRED`. |

The earlier pre-fix unlink click briefly removed `Super Admin → サマータイムレンダリング`; it was restored through the existing User Linking UI and re-verified before the post-fix pass. No later destructive action was submitted.

### Final design evidence decision

The evidence is sufficient to start a future design specification for the reached navigation, hierarchy, responsive behavior, form controls, typography stack, accessibility baseline, and motion policy. The unavailable connected-Production state and protected reduced-motion emulation are explicitly deferred optional/runtime-boundary evidence, not silent assumptions. Connection Map remains `DEFER`; Serif/mincho remains `REJECT` for current runtime need; Sidebar and Dot remain `ADOPT WITH LIMITS`.

Final marker: `KITSUSYNC_UIUX_EVIDENCE_GATE_COMPLETE`.
`DESIGN_MD_READY = YES`

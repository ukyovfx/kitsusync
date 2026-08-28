# Runtime accessibility audit — Agent C

Scope: login screenshots and artifact inventory under `docs/audits/runtime-evidence` only. This is an independent runtime-evidence audit; no application source, DOM dump, accessibility tree, browser interaction log, or other agent report was used.

## Executive result

The supplied screenshots show a visually simple login surface in Japanese and English at 375×812, 768×1024, 1024×768, and 1440×900. Visible labels, a visible focus-like outline on the email field, a two-option language control, and responsive text wrapping are observable. No accessibility defect can be confirmed from screenshots alone for DOM semantics, keyboard behavior, screen-reader output, language metadata, error announcement, reduced motion, or actual color/target-size conformance.

## Evidence register

All ten supplied images were inspected:

- English: `login_en_375x812_top.png`, `login_en_768x1024_top.png`, `login_en_1024x768_top.png`, `login_en_1440x900_top.png`, `login_en_1440x900_full.png`.
- Japanese: `login_ja_375x812_top.png`, `login_ja_768x1024_top.png`, `login_ja_1024x768_top.png`, `login_ja_1440x900_top.png`, `login_ja_1440x900_full.png`.

Evidence paths are relative to `docs/audits/runtime-evidence/`.

## Findings and verification gaps

### A11Y-C-001 — Visible form labels are present; programmatic association is unverified

- Criterion: WCAG 1.3.1 and 3.3.2 are relevant, but no failure is established.
- Page/state: anonymous login, JP and EN, all supplied viewports.
- User/task: keyboard and screen-reader user entering email and password.
- Evidence: the screenshots visibly show `EMAIL` / `PASSWORD` in English and `メールアドレス` / `パスワード` in Japanese above the two inputs. See `login_en_1440x900_full.png` and `login_ja_1440x900_full.png`; the same pattern is visible at the other supplied sizes.
- Inference/untested: screenshots cannot establish whether each text label is associated with its input via `label`, `aria-labelledby`, or an equivalent accessible name, nor whether the password input has the correct password semantics.
- Impact: assistive-technology users may receive no or an incorrect field name if the visible text is not programmatically bound.
- Recommendation: inspect the accessibility tree and DOM; verify each input has one stable accessible name, the email type/input purpose is correct, and the password field is exposed as a password input.
- Action class: implementation verification.
- Severity: P2 if the association is absent; otherwise no issue.
- Confidence: confirmed visible labels — high; defect — untested.

### A11Y-C-002 — Language selector state and names are visually indicated; operability and semantics are unverified

- Criterion: WCAG 4.1.2 and 2.1.1 are relevant, but no failure is established.
- Page/state: anonymous login, JP and EN, all supplied viewports.
- User/task: keyboard or screen-reader user switching language.
- Evidence: a two-option `JP` / `EN` control is visible in every screenshot; the active option is shown with an orange filled pill. JP is active in Japanese screenshots and EN in English screenshots. See `login_ja_1440x900_full.png` and `login_en_1440x900_full.png`.
- Inference/untested: the images do not prove whether the control is a native button/radio/tab pattern, whether its accessible name exposes the current/available state, whether it is reachable in the tab order, or whether activation works without a pointer.
- Impact: users who cannot use a pointer may be unable to change locale or may not know which locale is active.
- Recommendation: verify native interactive semantics, visible and programmatic selected state, keyboard activation, focus visibility, and an accessible name for each option.
- Action class: implementation verification.
- Severity: P2 if keyboard/state semantics fail; otherwise no issue.
- Confidence: visual state — high; defect — untested.

### A11Y-C-003 — Email outline is visible in captures; keyboard focus behavior is unverified

- Criterion: WCAG 2.4.7 and 2.4.11 are relevant, but no failure is established.
- Page/state: initial login screen, JP and EN, all supplied viewports.
- User/task: keyboard user moving focus through language control, email, password, and Login.
- Evidence: the email input has a distinct orange/brown outline in the supplied screenshots, including `login_en_1440x900_full.png` and `login_ja_1440x900_full.png`.
- Inference/untested: a static image cannot prove that this is the actual keyboard focus indicator rather than a default, validation, or styling state. No focus sequence, focus order, focus-visible behavior, or focused-button evidence is supplied.
- Impact: keyboard users may lose location awareness or encounter an unexpected tab sequence.
- Recommendation: run a keyboard-only pass and record focus on every interactive element at each locale; verify the indicator is persistent, not obscured, and has sufficient contrast against adjacent colors.
- Action class: runtime verification.
- Severity: P1 if focus is missing or trapped; P2 if visibility/order is materially deficient; otherwise no issue.
- Confidence: visible outline — medium-high; focus defect — untested.

### A11Y-C-004 — Japanese copy wraps at the narrow viewport without an observed overlap

- Criterion: WCAG 1.4.10 is relevant only if content is clipped or requires two-dimensional scrolling; no failure is established.
- Page/state: Japanese login at 375×812.
- User/task: low-vision or zoomed user reading the login instructions and completing the form.
- Evidence: `login_ja_375x812_top.png` shows the instructional sentence wrapping to two lines inside the card; the fields and Login button remain visible, with no obvious overlap in the captured viewport.
- Inference/untested: this does not test browser zoom, reflow at 400% zoom, text spacing overrides, dynamic error text, or all supported widths. Screenshot cropping also limits certainty about the full page.
- Impact: longer localized or user-agent-enlarged text could still clip or displace controls outside the tested capture.
- Recommendation: verify 200%/400% zoom and text-spacing overrides in both locales, including error and loading states.
- Action class: runtime verification.
- Severity: P2 if reflow fails; otherwise no issue.
- Confidence: observed screenshot state — high; broader reflow behavior — untested.

## Confirmed visual inventory (not conformance claims)

- One apparent primary login form is visible, with two text-entry controls and one Login/ログイン action.
- The visible form labels are localized in the JP and EN captures.
- The language control visibly communicates a selected option through fill color and position.
- The email control has a distinct outline in all inspected captures.
- The desktop and mobile captures show the form remains within the viewport; the narrow Japanese instruction wraps rather than visibly overlapping.
- The dark background, dark inputs, and orange controls require measured color sampling or computed styles before any contrast conclusion. No contrast pass is claimed.

## Not tested / unavailable evidence

- DOM landmarks (`header`, `main`, `form`, navigation), landmark names, heading hierarchy, and document outline.
- `lang` and `dir` attributes, locale changes announced to assistive technology, and language metadata on mixed-language text.
- Accessible names, roles, states, descriptions, autocomplete/input-purpose tokens, and native versus custom control semantics.
- Keyboard tab order, Enter submission, Escape behavior, focus persistence, focus trapping, and focus visibility after interaction.
- Screen-reader output, virtual cursor order, and announcements.
- Empty-submit, invalid-credentials, server-error, loading, disabled, and recovery/error association states; no secret or credential was entered.
- Motion, animation, transition, blinking, vestibular impact, and `prefers-reduced-motion` behavior.
- Computed contrast, non-text contrast, color-only communication under color-vision differences, exact CSS target dimensions, touch spacing, browser zoom, text spacing, high-contrast/forced-colors modes, and assistive technology/browser combinations.
- Authenticated or post-login pages.

## Limitations and source support

This audit is screenshot-based and therefore records rendered appearance, not semantic or interactive accessibility. A screenshot showing a control or outline is evidence that pixels were rendered; it is not evidence of a DOM landmark, accessible name, focus state, keyboard reachability, or WCAG conformance. Severity values above are conditional triage levels for the corresponding untested failure, not confirmed defects.

Primary sources used: the ten supplied image artifacts listed above. No external research or external write was used. WCAG criterion references are verification targets only and should be confirmed against the implementation and an actual keyboard/accessibility-tree test before remediation is prioritized.

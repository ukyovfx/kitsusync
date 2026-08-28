# Responsive runtime audit — Agent B

Date: 2026-08-28
Target: `C:\Users\mynti\Documents\KitsuSync-clean`
Scope: login screenshots only, at 375×812, 768×1024, 1024×768, and 1440×900 in JP and EN.

## Result

One observable responsive text behavior was found: the Japanese login description wraps to two lines at 375px. It remains readable and the form CTA remains visible. No visible horizontal clipping, broken navigation state, or unusable form state was found in the supplied captures.

## Findings

### R-01 — Japanese login description wraps at 375px

- Evidence: [`login_ja_375x812_top.png`](runtime-evidence/login_ja_375x812_top.png) shows `Kitsu の manager / admin アカウントでログインしてく` on the first line and `ださい。` on the second.
- The Japanese description is one line at 768px, 1024px, and 1440px; the English description is one line at all supplied widths.
- The wrapped text is inside the card, is not visibly clipped or overlapped, and the login button remains fully visible.
- Severity: Low (observable reflow; no usability break established).
- Confidence: High for the supplied screenshots; medium for intermediate widths because they were not supplied.
- Evidence vs inference: The line break and preserved CTA are evidence. Calling the wrap a defect would be an inference; this audit does not establish it as a defect.

### R-02 — Locale control changes position across sampled widths

- Evidence: At 375px, the JP/EN pill is below the brand at the upper left in [`login_en_375x812_top.png`](runtime-evidence/login_en_375x812_top.png) and [`login_ja_375x812_top.png`](runtime-evidence/login_ja_375x812_top.png). At 768px, 1024px, and 1440px it is at the upper right in the corresponding JP/EN captures.
- The control remains visually contained at every sampled width; neither label is visibly cut off.
- No hamburger, primary navigation menu, or intermediate broken navigation mode is visible in the supplied login captures.
- Severity: None observed.
- Confidence: High for the sampled widths; low for the exact breakpoint because only four widths were captured.
- Evidence vs inference: The position change and containment are evidence. The exact CSS breakpoint and whether the control is semantically navigation cannot be determined from screenshots alone.

### R-03 — Login form remains visible and contained at all sampled widths

- Evidence: Both fields and the Login/ログイン button are visible in all ten supplied captures. The 1440px full-page captures show no additional form content below the visible card: [`login_en_1440x900_full.png`](runtime-evidence/login_en_1440x900_full.png), [`login_ja_1440x900_full.png`](runtime-evidence/login_ja_1440x900_full.png).
- The email field has the orange focus outline in each capture; this is a captured visual state, not a tested interaction result.
- No visible field overflow, label collision, validation message collision, disabled CTA, or button clipping is present.
- Severity: None observed.
- Confidence: High for visual containment; low for actual keyboard, submit, validation, and error behavior because no form interaction was performed.
- Evidence vs inference: Field/button visibility and focus styling are evidence. Whether focus is intentional initial focus or a screenshot harness artifact is unknown.

## Limitations

- This is a static screenshot audit. No typing, submit, authentication, keyboard, viewport resize, scrolling, or browser zoom was performed.
- Evidence covers only the ten files in `docs/audits/runtime-evidence` matching the supplied JP/EN login widths. Intermediate breakpoints, landscape mobile, and narrow widths below 375px were not assessed.
- A screenshot cannot prove the absence of an off-screen scrollable element; conclusions are limited to visible clipping and containment.
- No other agent reports or application source were used.

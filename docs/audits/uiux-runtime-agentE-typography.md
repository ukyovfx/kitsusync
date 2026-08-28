# Runtime Agent E — Typography Audit

対象: `C:\Users\mynti\Documents\KitsuSync-clean`
監査日: 2026-08-28 JST
対象: Typography（Outfit、Space Grotesk、日本語フォールバック、折返し、フォント証拠）
実装変更: なし

## 結論

ログイン画面では、実行時の `body` computed font stack が `Outfit, "Noto Sans JP", sans-serif` であること、英日とも4 viewportで横方向のoverflowがないこと、日本語説明文が375×812で自然に2行へ折り返されることを確認した。Outfitの実使用はruntime-confirmedだが、Space Groteskが実際にロード・適用されたこと、日本語glyphがNoto Sans JPへfallbackしたこと、font requestの成功状態は未証明である。認証後のDashboard、Production、Connections、System Status、Setup等はruntime未到達のため、全管理画面へ一般化しない。

## Runtime evidence

### 実行条件

- Playwright Chromium、明示CSS viewport、`deviceScaleFactor: 1`。
- 対象は未認証 `/bot/login?lang=ja|en`。認証情報は使用していない。
- build SHA: `1a070c403a0bddeabe63086f037c6bb4f02fd8ca`。
- 根拠: `docs/UI-UX-RUNTIME-EVIDENCE.md:21-32, 40-58`。

### 確認済みのviewport

| locale | viewport | screenshot evidence | 判定 |
|---|---:|---|---|
| ja | 375×812 | `docs/audits/runtime-evidence/login_ja_375x812_top.png` | 確認 |
| ja | 768×1024 | `docs/audits/runtime-evidence/login_ja_768x1024_top.png` | 確認 |
| ja | 1024×768 | `docs/audits/runtime-evidence/login_ja_1024x768_top.png` | 確認 |
| ja | 1440×900 | `docs/audits/runtime-evidence/login_ja_1440x900_top.png`, `_full.png` | 確認 |
| en | 375×812 | `docs/audits/runtime-evidence/login_en_375x812_top.png` | 確認 |
| en | 768×1024 | `docs/audits/runtime-evidence/login_en_768x1024_top.png` | 確認 |
| en | 1024×768 | `docs/audits/runtime-evidence/login_en_1024x768_top.png` | 確認 |
| en | 1440×900 | `docs/audits/runtime-evidence/login_en_1440x900_top.png`, `_full.png` | 確認 |

## Confirmed

### E-TYPO-001 — Outfit stack is computed at runtime

- 判定: CONFIRMED
- Severity: Medium
- Confidence: High
- viewport/state: Login、ja/en、375×812 / 768×1024 / 1024×768 / 1440×900
- Exact evidence: `docs/UI-UX-RUNTIME-EVIDENCE.md:38` records computed `body` font as `Outfit, "Noto Sans JP", sans-serif` and body letter-spacing as `0.13px`.
- Source corroboration: `src/setup/ui.go:34-43` sets `font-family:"Outfit","Noto Sans JP",sans-serif`, `font-size:13px`, `letter-spacing:.01em`.
- Meaning: 少なくともbodyのfamily指定は実DOM/CSS計算値として確認できる。これはOutfitのfont fileが正常ロードされた証明ではない。

### E-TYPO-002 — Login wrapping is stable at supplied widths

- 判定: CONFIRMED
- Severity: Low
- Confidence: High
- viewport/state: Login、ja/en、全8 viewport
- Exact evidence: `docs/UI-UX-RUNTIME-EVIDENCE.md:36` records `clientWidth == scrollWidth == viewport width` for all eight captures and no horizontal overflow.
- Screenshot evidence: At `login_ja_375x812_top.png`, the Japanese description `Kitsu の manager / admin アカウントでログインしてください。` wraps to two lines inside the card without visible clipping or overlap. At `login_en_375x812_top.png`, the English description remains on one line. At the supplied desktop screenshots both forms remain within the viewport.
- Source corroboration: `src/setup/middleware.go:550` emits the localized description; `src/setup/ui.go:282` sets heading paragraph `line-height:1.6`; `src/setup/ui.go:33` sets `overflow-x:hidden` on `html,body`.
- Limitation: 200%/400% zoom, user text-spacing overrides, long production names, table cells, errors, and authenticated screens were not tested.

### E-TYPO-003 — JP and EN locale content is visibly differentiated without changing the base stack

- 判定: CONFIRMED
- Severity: Low
- Confidence: High
- viewport/state: Login、ja/en、全8 viewport
- Exact evidence: `docs/audits/runtime-evidence/login_ja_1440x900_full.png` shows Japanese labels `メールアドレス`, `パスワード`, `ログイン`; `login_en_1440x900_full.png` shows `EMAIL`, `PASSWORD`, `Login`. The same distinction is visible in the 375px captures.
- Source corroboration: `src/setup/i18n_catalog.go:106-110, 209-213`; `src/setup/middleware.go:509-527` emits locale-specific strings; `src/setup/ui.go:729` emits `<html lang="%s">`.
- Meaning: locale content and document language are wired, but this does not prove a language-specific glyph/font selection.

## Rejected / not reproduced

### E-TYPO-004 — Login has a confirmed viewport typography overflow defect

- 判定: REJECTED / NOT REPRODUCED
- Severity: None for supplied runtime; residual risk Medium outside coverage
- Confidence: High for supplied Login captures
- viewport/state: Login、ja/en、375×812 / 768×1024 / 1024×768 / 1440×900
- Exact evidence: `docs/UI-UX-RUNTIME-EVIDENCE.md:36` reports no horizontal overflow in all eight captures. The supplied 375px screenshots show JP description wrapping rather than clipping; inputs and button remain inside the card.
- Boundary: This rejects a claim that the supplied Login runtime already demonstrates a wrapping/overflow failure. It does not reject the source-level risk for protected screens with longer data.

### E-TYPO-005 — Actual Noto Sans JP fallback is confirmed by the screenshots

- 判定: REJECTED as a confirmed claim
- Severity: Medium residual risk
- Confidence: High
- viewport/state: Login、ja/en、all supplied viewports
- Exact evidence: Runtime records only the computed family list (`docs/UI-UX-RUNTIME-EVIDENCE.md:38`); it does not record `document.fonts.check`, loaded font faces, rendered glyph provenance, or font resource response status. Source only places `"Noto Sans JP"` in the stack at `src/setup/ui.go:37`.
- Meaning: The screenshot appearance cannot distinguish a loaded Noto Sans JP face from an OS/browser generic sans fallback. Treating JP fallback as proven would overstate the evidence.

## Untested / cannot determine

### E-TYPO-006 — Space Grotesk actual load and applied face

- 判定: UNTESTED
- Severity: Medium
- Confidence: High
- viewport/state: Login screenshots show the eyebrow/language labels, but no runtime computed `font-family` was recorded for those elements.
- Exact source evidence: `src/setup/ui.go:9` imports `Outfit` and `Space Grotesk` from Google Fonts; `src/setup/ui.go:164-170` assigns Space Grotesk to `.eyebrow`; `src/setup/ui.go:265-276` assigns it to `.lang-option`; `src/setup/ui.go:342` assigns it to labels; `src/setup/ui.go:375` assigns it to table headers; `src/setup/ui.go:351` assigns it to buttons.
- Missing evidence: network/resource success, `document.fonts` status, computed family for `.eyebrow`, `.lang-option`, `label`, and `.btn`, and screenshot comparison with the font unavailable.

### E-TYPO-007 — Noto Sans JP glyph fallback and cross-platform consistency

- 判定: UNTESTED
- Severity: Major
- Confidence: High
- viewport/state: Login ja captures only; Chromium renderer/environment is not sufficient to establish Windows/macOS/Linux or browser-wide behavior.
- Exact source evidence: `src/setup/ui.go:9` imports only Outfit and Space Grotesk; `src/setup/ui.go:37` references `Noto Sans JP` but has no local `@font-face` or explicit Noto Sans JP import. Repository font asset search found no `.woff`, `.woff2`, `.ttf`, or `.otf` font artifact under the target repository.
- Missing evidence: actual loaded face/glyph coverage, fallback chain, baseline/weight/width comparison, and behavior when Google Fonts is unavailable. `docs/UI-UX-RUNTIME-EVIDENCE.md:73` also explicitly says font request failure was not forced.

### E-TYPO-008 — Authenticated screen wrapping and type rhythm

- 判定: UNTESTED
- Severity: Major
- Confidence: High
- viewport/state: Dashboard, Productions/list/detail, Connections read/edit, User Linking, System Status, Audit Log, Setup/diagnostics/dialog — all locales and widths.
- Exact evidence: `docs/UI-UX-RUNTIME-EVIDENCE.md:26-32` marks all protected screen states as not captured. `docs/UI-UX-RUNTIME-EVIDENCE.md:21` states the authenticated session was unavailable without credential entry.
- Source risk evidence: `src/setup/ui.go:281-282` defines page heading size/line height; `src/setup/ui.go:342` applies uppercase and `.16em` letter spacing to all labels; `src/setup/ui.go:375` applies the same pattern to table headers; `src/setup/ui.go:382-384` applies uppercase and `.14em` to metric labels and `word-break:break-all` to host values. These rules require runtime validation with real JP/EN data before classifying visual impact.

### E-TYPO-009 — Font loading failure, FOUT/FOIT, and offline/CSP behavior

- 判定: UNTESTED
- Severity: Major
- Confidence: High
- viewport/state: all
- Exact evidence: `src/setup/ui.go:9` uses an external Google Fonts `@import`; `docs/UI-UX-RUNTIME-EVIDENCE.md:73` says no network failure was forced.
- Missing evidence: request timing/status, computed font before/after load, fallback screenshot, CSP interaction, and layout shift measurement.

## Evidence-based assessment

| Topic | Status | Evidence strength | Severity |
|---|---|---|---|
| Outfit in body computed style | Confirmed on Login | High | Medium residual risk |
| Space Grotesk loaded/applied | Untested | High that it is unproven | Medium |
| `Noto Sans JP` actual glyph fallback | Untested | High | Major |
| JP/EN content and `html lang` | Confirmed | High | Low |
| Login wrapping/no horizontal overflow | Confirmed; defect not reproduced | High | Low in tested state |
| Authenticated wrapping/type rhythm | Untested | High | Major |
| Font request failure/FOUT/FOIT | Untested | High | Major |

## Scope boundary

このファイルは指定されたruntime artifactと対象ソースのTypography確認だけを記録する。認証情報の取得、外部通信、実装、Git操作は行っていない。

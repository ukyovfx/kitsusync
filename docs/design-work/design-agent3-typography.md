# Agent 3 — Typography and JP/EN

対象: `C:\Users\mynti\Documents\KitsuSync-clean`
範囲: Outfit / Noto Sans JP runtime、Space Grotesk source usage、JP/EN文字組み、技術用語、長い名称、折返し、fallback、外部フォント失敗時の方針。
変更: 本レポートのみ作成。UIコード、route、behavior、`DESIGN.md` は変更していない。

## 結論

現行の意図は、本文を `Outfit`、日本語候補を `Noto Sans JP`、短い UI chrome を `Space Grotesk` で補う構成である。source 上はこの役割分担が明示されているが、`Noto Sans JP` は stack に名前があるだけで、リポジトリ内の font asset または import は確認できない。Google Fonts の外部 `@import` が失敗した場合の実フォント、FOUT/FOIT、JP glyph の由来は未証明である。

推奨は「役割と見た目は維持し、供給経路を self-hosted に移す」である。短期の design decision は current stack を stay/system-compatible として固定し、実装段階で Outfit、Space Grotesk、必要な Noto Sans JP subset を self-host する。フォントを別の無料書体へ later に交換する判断は、実データでの幅・字形・アクセシビリティ比較後に限定する。

## 観測された事実

| 事項 | 観測 | 根拠 |
|---|---|---|
| 外部供給 | Google Fonts の `@import` が Outfit と Space Grotesk を取得する | `src/setup/ui.go:8-10` |
| 本文 | `body` は `"Outfit","Noto Sans JP",sans-serif`、13px、letter-spacing `.01em` | `src/setup/ui.go:34-43` |
| Space Grotesk | eyebrow、言語切替、label、button、table header、accordion trigger、workflow badge に明示 | `src/setup/ui.go:170-175`, `:269-283`, `:348`, `:357`, `:380-381`, `:401`, `:453` |
| 見出し | 通常 page heading は 26px / 1.03、Dashboard は 28px / 1.1、System Status は 32px。Connections には 24px/18px/16px の局所指定がある | `src/setup/ui.go:285-288`, `:307`, `:323-325`, `:337-340`, `:385`, `:515-523` |
| 英語向け処理 | label / table header / metric label は uppercase と広い letter-spacing を持つ | `src/setup/ui.go:348`, `:381`, `:388`; metric `.14em` |
| 長い値 | table cell は `overflow-wrap:anywhere; word-break:break-word`、connection row も anywhere。host 値だけ `word-break:break-all` | `src/setup/ui.go:150-151`, `:372`, `:378-381`, `:390-391` |
| 短い状態値 | status pill は `white-space:nowrap`。狭幅では一部 status row / button が normal wrapping に変わる | `src/setup/ui.go:365-370`, `:158`, `:484-486` |
| JP/EN state | renderer は `<html lang="ja|en">` を出し、Current IA は JP/EN を semantic equivalent と定義する | `src/setup/ui.go:729`; `docs/CURRENT-IA-UI-SPEC.md:113-116` |
| Runtime | Login の computed body family は `Outfit, "Noto Sans JP", sans-serif`。Connections edit では `document.fonts.status=loaded` と同じ computed stack が記録されたが、font face/resource provenance は未記録 | `docs/UI-UX-RUNTIME-EVIDENCE.md:38`; `docs/UI-UX-RUNTIME-EVIDENCE.md` の post-fix typography gate |
| 未確認 | 実際に Space Grotesk が適用されたこと、JP glyph が Noto Sans JP 由来であること、offline/CSP/外部 request 失敗時の表示 | `docs/audits/uiux-runtime-agentE-typography.md:E-TYPO-005–009`; `docs/audits/uiux-agent6-typography.md:T-01–T-03` |
| asset | 対象 repository 内に `.woff/.woff2/.ttf/.otf` は確認できない | `docs/audits/uiux-runtime-agentE-typography.md:E-TYPO-007` |

Runtimeで確認できたのは family list と、指定 viewport の Login の折返し／横 overflow がないことまでである。これは font file のロード成功や glyph provenance の証明ではない。認証後画面については post-fix pass で stack は部分確認されたが、offline fallback は未確認である。

## Type roles（推奨契約）

以下は現行 source を基礎にした design-level recommendation であり、現在の CSS が完全にこの scale を実装済みという意味ではない。

| Role | WHAT | 推奨 | WHY |
|---|---|---|---|
| Display / page title | route の主題を示す h1 | Outfit + JP fallback、weight 600–700、32px desktop / 22–28px narrow、line-height 1.15–1.3 | 現行は page/Dashboard/System Status で 26/28/32px に分散しているため、semantic role を先に固定する |
| Section title | h2/h3、card title | Outfit + JP fallback、weight 600、18–24px、line-height 1.3–1.45 | Connections と System Status の階層を連続閲覧で安定させる |
| Body / helper | 説明、診断詳細、form help | Outfit + JP fallback、14–15px、line-height 1.6–1.75 | JP の縦方向の可読性と EN の密度の妥協点。Current IA は helper/body 15px を要求している (`docs/CURRENT-IA-UI-SPEC.md:103`) |
| UI label / table header | field name、table column、small metadata | JP は通常 case・狭い tracking、EN のみ必要に応じ uppercase / Space Grotesk | `.16em` を JP にも適用すると語が分断されるため |
| Metric / status | count、latency、Connected 等 | 数値は tabular numerals、status は短く明示、pill の nowrap は短い既知値だけ | 数値の比較と status のスキャン性を守り、可変長 status の clipping を避ける |
| Technical identifier | ID、host、channel、Task Type 名 | body/JP fallback を基本に、code-like value のみ等幅を later decision とする。自然な wrap を優先 | identifier は表示価値があるため、装飾用 uppercase / tracking で意味を壊さない |

### Rules

1. WHAT: semantic role ごとに size、weight、line-height を定義する。 WHY: route ごとの局所 override が JP の行高と折返しを不安定にする。 WHEN: design token または新しい画面を定義するとき。 WHEN NOT: 実測なしに、既存画面の個別値を一括置換しない。
2. WHAT: display と body は Outfit を第一候補、日本語 glyph は Noto Sans JP を第一 fallback とする。 WHY: 現在の brand/runtime evidence を保ち、mincho 導入の根拠がない。 WHEN: 管理 UI と docs の共通 typographic contract を定義するとき。 WHEN NOT: Noto Sans JP が実際に供給されていない状態を「ロード済み」と扱わない。
3. WHAT: Space Grotesk は短い英語 UI chrome（eyebrow、EN nav、button、compact label）に限定する。 WHY: source 上の既存役割を維持し、JP本文の字面・glyph coverageをこの書体に依存させない。 WHEN: 英語の短い分類ラベルや操作語を視覚的に区別するとき。 WHEN NOT: 日本語本文、長い Production 名、ユーザー入力、診断 detail の主フォントにしない。

## JP/EN casing、spacing、line-height

### Rules

1. WHAT: JP は `text-transform: none` を原則とし、letter-spacing は 0〜.02em 程度の狭い範囲にする。 WHY: Japanese に uppercase の情報価値がなく、全角文字の間隔を英語基準で広げるとラベルのまとまりが崩れる。 WHEN: JP label、table header、metric label、badge text を表示するとき。 WHEN NOT: ロゴや意図的な editorial display など、別途 visual rationale と検証がある場合。
2. WHAT: EN の uppercase は eyebrow、短い metric label、navigation のカテゴリなど短い語だけに使い、tracking は role-specific にする。 WHY: 現行 source の uppercase/`.14em`/`.16em` を全言語共通ルールへ拡大しないため。 WHEN: 1〜3語程度の短い英語ラベルで scan cue が有効なとき。 WHEN NOT: sentence、長い technical term、ユーザー入力、mixed JP/EN string。
3. WHAT: JP body/helper は line-height 1.6–1.75、EN body は 1.5–1.7 を基準にし、同一 semantic role で過度に別サイズへ分岐させない。 WHY: JP は glyph box が大きく、同じ px 値でも密度が高く見える。 WHEN: localized pair を同じ card/row に置くとき。 WHEN NOT: chart axis や one-line status のように compactness が要件で、別途 clipping を検証済みの場合。
4. WHAT: punctuation は各 locale の文面を保ち、JP の句読点・括弧と EN の ASCII punctuation を混在させる翻訳を避ける。 WHY: line break と視覚的 rhythm を予測可能にする。 WHEN: localized copy、helper、error を作成するとき。 WHEN NOT: source identifier、API 名、コード、Discord channel 名など canonical value を表示するとき。

## Technical terms and casing

Observed source/spec has intentional product and integration terms such as `Production`, `Task Type`, `Discord Channel`, `Kitsu API`, `Discord API`, `User Linking`, `Reviewer / Checker`, `Production ID` and `Discord Server ID` (`docs/CURRENT-IA-UI-SPEC.md:10-17, 124-128`; `src/setup/current_routing.go:277-295`). It also contains raw diagnostic labels such as `Entity scope`, `Current routing`, `Stable ID`, `Classification`, `Short name`, `Semantic flags`, and `Would notify` (`docs/audits/uiux-agent6-typography.md:T-04`).

### Rules

1. WHAT: maintain one glossary for product nouns and integration names; choose either a localized label or an intentional canonical English term per term, not ad hoc mixing. WHY: JP screen currently has translated prose beside raw English technical labels. WHEN: adding or revising label/caption/diagnostic output. WHEN NOT: never translate stable API values, IDs, code, Task Type names, channel names, or user-provided names.
2. WHAT: preserve canonical casing exactly for `Kitsu`, `Discord`, `API`, `ID`, `Task Type`, `Production`, `User Linking`, `Reviewer / Checker`; use sentence case for explanatory English. WHY: these strings are identity-bearing terms and inconsistent casing harms search and recognition. WHEN: labels, headings, table columns, diagnostics. WHEN NOT: do not uppercase a canonical value merely because the surrounding English label uses uppercase styling.
3. WHAT: in JP, attach Japanese grammar outside the canonical term (`Kitsu API の状態`, `Production の接続`) and do not insert decorative tracking inside the term. WHY: preserves recognizability and JP reading rhythm. WHEN: mixed JP/EN sentences. WHEN NOT: do not re-case, split, or translate a value that is copied from Kitsu/Discord.
4. WHAT: retain the exact Current IA chart labels: JP `60秒`, `30秒`, `今`, `5分`, `2分30秒`; EN `60s`, `30s`, `Now`, `5m`, `2m30s`. WHY: these are an explicit content contract. WHEN: System Status charts. WHEN NOT: do not substitute abbreviated Japanese or localized clock formats (`docs/CURRENT-IA-UI-SPEC.md:100,105,155`).

## Long names, wrapping, truncation

Long values are expected for Production names, channel names, hostnames, IDs, Task Type names, and linked user names. Source already uses `overflow-wrap:anywhere` / `word-break:break-word` on tables and connection data, while host values use the more aggressive `break-all` (`src/setup/ui.go:150-151, 372, 378-381, 390-391, 408`). The narrow media rules let buttons wrap and transform project channel tables into stacked rows (`src/setup/ui.go:158, 484-486`). Login evidence confirms no horizontal overflow at the supplied eight locale×viewport samples, but authenticated long-data coverage is not equivalent to that Login result (`docs/audits/uiux-runtime-agentE-typography.md:E-TYPO-002, E-TYPO-008`).

### Rules

1. WHAT: wrap human-readable names at word boundaries where possible, then allow `overflow-wrap:anywhere` as the safety net for unbroken IDs/channel names. WHY: preserve readability before using emergency breaks. WHEN: Production, channel, Task Type, user, diagnostic values. WHEN NOT: do not use `word-break:break-all` for ordinary prose or JP/EN labels.
2. WHAT: use `break-all` only for opaque values whose complete visibility is more important than word shape (for example a host or fixed-width ID field), and pair it with a copy/read affordance if the value is operationally important. WHY: current host rule prevents overflow but can make values hard to scan (`src/setup/ui.go:390`). WHEN: code-like, non-linguistic values. WHEN NOT: names, sentences, button text, or technical terms that users read as words.
3. WHAT: never silently truncate a value that is needed to identify a Production, channel, user, route, or diagnostic cause. WHY: truncation can make two resources appear identical and can conceal the reason for a blocked action. WHEN: data tables, cards, confirmation dialogs, audit/diagnostic details. WHEN NOT: decorative previews only; if truncation is unavoidable, expose the full value via accessible name, details, copy, or a visible expansion.
4. WHAT: status pills and compact controls may remain one line only for bounded, known labels; variable localized status text must wrap or grow. WHY: current `.status-pill{white-space:nowrap}` is safe for short English values but not automatically for JP or future translations (`src/setup/ui.go:365-370`). WHEN: `Connected`, `Error`, `接続済`, and equivalent bounded labels. WHEN NOT: diagnostic explanations, `Needs review`-style variable text, or user content.
5. WHAT: test the longest realistic JP/EN values at 375×812, 768×1024, 1024×768, and 1440×900, including mixed Latin/Japanese and CJK punctuation. WHY: font fallback changes width and line count even when CSS family lists match. WHEN: before accepting typography for a connected Production state or a new localization. WHEN NOT: do not infer protected-screen behavior from Login screenshots alone.

## Fallback and external-font failure policy

### Recommendation

Use self-hosted current fonts as the target policy: Outfit for Latin body/display, Space Grotesk for the limited English UI-chrome role, and a deliberately selected Noto Sans JP subset for Japanese. Keep system sans fallbacks after the named faces so the UI remains usable if a face cannot load. This is a supply-chain and layout-stability recommendation, not a claim that the current implementation already self-hosts these fonts.

Rationale: the current source depends on an external Google Fonts import (`src/setup/ui.go:9`), while the runtime evidence proves only computed family lists and not request/resource provenance (`docs/audits/uiux-runtime-agentE-typography.md:E-TYPO-005–009`). Self-hosting makes the selected JP glyph coverage, caching, version, and failure behavior testable, and avoids making network availability a prerequisite for the admin UI. A later free-font substitution should be deferred: changing to another font without measuring JP glyph coverage, mixed-script baselines, line breaks, and long-name density would trade a known stack for an unverified one.

### Rules

1. WHAT: production fallback order is self-hosted named face → compatible system sans → generic `sans-serif`; keep a JP-capable fallback in the JP path. WHY: the UI must remain readable and structurally safe when font delivery fails. WHEN: CSS/font packaging is implemented. WHEN NOT: do not claim a named face is available solely because it appears in `font-family`.
2. WHAT: external-font failure is a degraded typography state, not a content or auth failure. WHY: users must still be able to log in, inspect diagnostics, and recover operations. WHEN: request timeout, offline mode, CSP block, server error, or `document.fonts` failure occurs. WHEN NOT: do not hide text, block the route, or replace technical values with fabricated placeholders.
3. WHAT: define `font-display` and test the before/after layout with fonts unavailable. WHY: prevents unmeasured FOIT/FOUT and layout shift. WHEN: selecting the self-hosted delivery configuration. WHEN NOT: do not choose a loading policy based only on visual preference; verify Login and representative authenticated screens.
4. WHAT: use the same fallback policy in admin UI and `/bot/docs/` where the same identity is intended. WHY: source currently has separate document typography definitions (`docs.html:11,79,83-97`; `docs/audits/uiux-agent6-typography.md:T-06`). WHEN: consolidating the service editorial identity. WHEN NOT: do not modify `docs.html` as part of this report or assume its current stack is automatically equivalent to `src/setup/ui.go`.
5. WHAT: if self-hosting is not yet approved, retain the current stack and system fallback, instrument/record font resource status, and gate typography acceptance on offline/CSP/fallback evidence. WHY: this is the smallest safe interim posture. WHEN: implementation authority or packaging decision is pending. WHEN NOT: do not introduce a different free font merely to remove the external request.

## Acceptance evidence still required

- `document.fonts` and network/resource evidence for Outfit, Space Grotesk, and Noto Sans JP separately; computed family alone is insufficient.
- Forced offline/CSP/request-failure captures, recording FOIT/FOUT, layout shift, fallback line breaks, and recovery behavior.
- JP/EN screenshots and DOM/computed style for representative authenticated Dashboard, Production detail, Connections edit, System Status, User Linking, Audit Log, and Setup states with long names.
- Cross-platform/browser comparison of JP glyph coverage, weight, baseline, punctuation, and mixed Latin/Japanese wrapping.
- Verification that any truncation exposes the complete value accessibly and that status pills do not clip localized labels.

## Source index

- Runtime theme and typography source: `src/setup/ui.go:8-10, 34-43, 170-175, 269-283, 285-288, 307-318, 348-381, 388-401, 408, 484-486, 515-523`.
- Locale/document language: `src/setup/ui.go:729`; `src/setup/i18n.go:8-17`.
- Current IA content contract: `docs/CURRENT-IA-UI-SPEC.md:69-80, 100-116, 124-155`.
- Existing static typography audit: `docs/audits/uiux-agent6-typography.md:T-01–T-06`.
- Runtime typography evidence and boundaries: `docs/audits/uiux-runtime-agentE-typography.md:E-TYPO-001–009`; consolidated runtime evidence `docs/UI-UX-RUNTIME-EVIDENCE.md:36-38` and post-fix typography gate.
- Technical-term source examples: `src/setup/current_routing.go:277-295`; `src/setup/workflow_diagnosis.go:453,469,490`.

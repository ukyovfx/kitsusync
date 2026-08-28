# KitsuSync Distributed UI/UX Audit

対象: `C:\Users\mynti\Documents\KitsuSync-clean` の現行v0.4.4相当コード/UI。
監査は設計監査のみ。UI/CSS/renderer/routes/product behavior、外部サーバー、Gitは変更していない。認証済みブラウザと実データ状態はこの実行環境で確認できなかったため、静的コード、既存テスト、`docs/CURRENT-IA-UI-SPEC.md`を証拠として扱い、未確認事項を分離した。

## 1. Executive Summary

結論: Editorial Systems Interface は ACCEPT ではなく REFINE。KitsuSyncの差別化はProduction-first、Kitsu→Discord routing、fail-closed、preview-before-send、日英同等性に置く。静かな運用面を基礎にし、serif/mincho・dot・motion・Connection Mapは限定的な補助要素に留める。

最も重要な問題は、グローバルな装飾（dot/gradient/glass）、カードとstatusの反復、Dashboard/diagnosticsの同等表示、Wizard/route編集の操作モデル、モバイル/キーボード未検証、そしてUser LinkingとProduction associationの記憶負荷である。

## 2. Current UI Inventory

主要ルートは `/bot/login`、`/bot/setup`、`/bot/admin`、`/bot/admin/projects`（list/detail）、`/bot/admin/users`、`/bot/admin/bot`（read/edit）、`/bot/admin/health`、`/bot/admin/audit`、`/bot/admin/workflow-diagnosis`。`/bot/admin/checkers`は互換入口。詳細は [Agent 0 inventory](audits/uiux-agent0-inventory.md)。

## 3. Strong Existing Elements to Preserve

- Productionを選択した後にrouting/users/troubleshootingを扱うスコープ。
- Discord書き込み前のchannel plan/reviewとnetwork-free dry-run。
- connection-only removalとDiscord resource deletionの分離。
- exact-name confirmation、fail-closedなownership/permission検証。
- Kitsu/Discord接続のpeer表示、JP/EN切替、skip link、visible focus、status/live-regionの基礎。
- System Statusの診断詳細を通常表示から分離する方向。

## 4. Highest-Severity Problems

1. Accessibility risk: edit-form labels未関連付け、削除dialogの名前/説明不足、SVG履歴の非テキスト情報不足、mobile nav重複、wizard drag操作、reduced-motion未対応。
2. グローバルなparticle driftと背景gradientが、密度の高い情報をfigure/ground上で競合させる。
3. `section-card glass`、metric card、nested surface、status pillの反復で意味のある境界が埋もれる。
4. Dashboardのmetrics、attention、CTA、management menuが同等の視覚重みを持ち、次の行動が弱い。
5. Workflow diagnosisがraw IDs、scope、matching、status分類などを通常の修復導線に近い形で露出する。
6. Setup/route編集のdrag affordanceとkeyboard modelの同等性が不確実。
7. User Linking（global）とProduction users/roles（local）の二段階が記憶依存。
8. 解除/削除確認モーダルが画面間で不統一で、stronger deleteの日本語確認語に文字化けがある。
9. Production detailのprimary/diagnostic/danger同列化と、Storage保存後のglobal `/bot/admin/drive`遷移でcontextが落ちる。
10. Connection Mapを広域・常設化すると、関係理解より視覚ノイズが勝つ。

## 5. Cognitive / Perceptual Findings

認知負荷スコア（0 none–3 significant）: Login 1、Setup 3、Dashboard 2、Production detail 2、Connections 1、System Status 2、Audit Log 1、Workflow diagnosis 3、User Linking 2。主要因は同一状態のbadge/explanation/action反復、等価でないmetricsの均等提示、技術詳細の早期露出。

知覚上の主要所見: 背景のdot/gradient/透明surface、nested card、uniform grid、wide tableがcontent hierarchyを弱める。解決原則は装飾追加ではなく、semantic regionを減らし、Primary decision→supporting explanation→advanced detailsの順にすること。

## 6. Navigation / IA Findings

推奨案A（順位1）: Productions、Connections、User Linking、System Status、Audit Logをdesktopのtop-level。`/bot/setup`は目立つcontextual CTA。Production配下はOverview/Notifications/Users/Troubleshooting。通常の深さはtop-level→selected Production→subviewの2段まで。

案B: Productions、Operations（Status/Audit）、Settings（Connections/User Linking）。noviceには軽いが、頻繁な設定入口を隠す。

案C: Overview、Productions、Connections、Operations、Advanced。generic dashboard化と重複集約のリスクが高い。

Sidebar verdict: desktopでは有利。ただしmobileで狭いrailにせず、Production tabsを重複させないこと。Connection Mapはtop-levelに置かない。

## 7. Interaction Findings

44pxの入力/ボタン高さ、focus outline、staged apply、dry-run、exact confirmationは保持する。高リスクは、drag-onlyに見えるwizard、32px以下の補助操作、row menu内のremove/delete、複数のconnection deletion variants、壊れたJP確認語、disabled confirmationの理由伝達。破壊操作は実行せず、ブラウザでキーボード順序・dialog focus・announceを検証してから設計判断する。

## 8. Accessibility / Responsive Findings

Critical: 現時点で証拠十分なcriticalはなし。High: A5-001 edit-form label未関連付け、A5-002/003 dialog naming/description不足、A5-005 SVG履歴の非テキスト情報不足、mobile navのDOM重複、wizard/routing dragとkeyboard parity未検証。Medium: hard-coded English、mixed status classes、wide table overflow、global continuous motion。Low: 28–32px visual controls（実効target未計測）。

未テスト: authenticated JP/EN runtime、screen reader、contrastの実ピクセル、375/768/1024/1440のスクリーンショット、zoom/reflow、reduced-motion、tablet。`docs/audits/uiux-agent5-accessibility.md`参照。

## 9. Typography Findings

Serif/mincho verdict: ADOPT WITH LIMITS。Brandとpage title、まれなeditorial momentに限る。navigation/body/forms/tables/logs/statuses/metadataはsans。Production名への常用適用は、日英の文字幅・ユーザーデータ長で不安定になるため避ける。最安全案はsans everywhere、次点がsans operational + serif identity/title。現行のOutfit/Space Grotesk + `Noto Sans JP` fallbackは、実環境でのfont availabilityを検証する。

## 10. Motion / Spatial Findings

Dot/motion verdict: DOT MOTIF = ADOPT WITH LIMITS、MOTION = ADOPT WITH LIMITS。normal operational pagesは静的またはごく subtle。Login/loadingは低コントラストのbrand用途、Connection Mapはfocus/highlight用途のみ。global `particleDrift`はdecorativeからdistractingになり得るため、reduced-motionでは無効化必須。continuous movement、strong parallax、glow/particle spectacleは不採用。

## 11. Anti-AI Findings

AI-like and harmful: repeated glass/nested cards、global gradients/glow、card-based dashboard、mechanicsを言い直すhelper copy。AI-like but acceptable: status pills、dot motif、uniform four-peer Production summary（decision valueが確認できる場合）。Not actually a problem: decision-linked counts、実際の安全説明、surfaceをownership/riskのために使うこと。Anti-AI自体を別の定型テンプレートにしない。

## 12. Connection Map Decision

ADOPT WITH LIMITS。1 ProductionのTask Type→Discord destinationを中心とするbounded diagnosticに限る。推奨配置は `/bot/admin/projects?project=<id>&tab=notifications` の既存routing summaryの隣接secondary view。Kitsuを左、Discordを右など固定semantic regions、filter、highlight、inspector、list/table fallbackを必須とする。編集はcanonical formへ遷移させ、graph内編集はしない。高密度・cross-Production・共有channelが増えたらlist modeへ戻す。top-level defaultには置かない。

## 13. Proposed Design Identity

Editorial Systems Interface: REFINE。Sidebar = ADOPT WITH LIMITS。借りるのは task focus、explicit hierarchy、restrained developer-tool density、editorial typographyの限定利用。避けるのは referenceのコピー、glassmorphism、neon/glow、巨大rounded panel、decorative particles、generic metrics。固有性はKitsu/Discordの運用モデルと安全境界から出す。

## 14. Decisions With Strong Consensus

| Decision | Support | Oppose | Evidence / confidence |
|---|---:|---:|---|
| Production contextをprimary scopeにする | 6 | 0 | IA/operations/adversarial/spec。High |
| preview-before-sendとfail-closedを保持 | 6 | 0 | interaction/operations/destructive/spec。High |
| global motion/dotを縮小 | 6 | 0 | perception/motion/accessibility/anti-AI/adversarial。High |
| raw diagnosisはadvancedへ | 5 | 0 | cognitive/operations/progressive-disclosure/IA。High |
| serifはbrand/title限定 | 4 | 1 | typography/direction/adversarial。Medium-High |
| Connection Mapは限定付き | 4 | 1 | map/IA/operations/adversarial。Medium |
| desktop sidebarは有利 | 3 | 1 | IA/direction/adversarial。Medium |
| anti-cardを絶対化しない | 3 | 0 | perception/anti-AI/adversarial。High |

最終判定: SIDEBAR = ADOPT WITH LIMITS、SERIF-MINCHO = ADOPT WITH LIMITS、DOT MOTIF = ADOPT WITH LIMITS、MOTION = ADOPT WITH LIMITS、CONNECTION MAP = ADOPT WITH LIMITS、EDITORIAL SYSTEMS INTERFACE = REFINE。

## 15. Contested Decisions

- Production overviewの4-card equal grid: spec/perceptionはpeerとして許容するが、cognitive/anti-AIはdecision valueの低いmetricが同等化される可能性を指摘。実 operator task testで決める。
- Dashboard metrics: operationsはreadiness/countを有用とし、cognitive/perceptionはattention/next actionより前面に出すことに反対。metricsは残しても主行動の従属情報にするのが安全。
- Connection Map:関係異常の把握価値はあるが、対象数が増えると視覚ノイズ化する。bounded diagnostic以外は採用しない。
- External font: typographyは自由なweb font候補を許容するが、accessibilityはoffline/fallbackをHighリスク扱い。実runtimeで再判定する。

## 16. Things Explicitly Rejected

常時表示のparticle/graph motion、generic AI dashboard、Bento/card soup、glass/glow overload、serif operational UI、Connection Mapのtop-level常設、graph内での直接編集、statusを色だけで伝える設計、wizardのdrag-only操作、装飾を増やしてhierarchyを解決すること。

## 17. Screen-by-Screen Scores

1=弱い/高リスク、5=強い/低リスク。静的証拠による暫定値。

| Screen | Hier. | Cog. | Nav. | Space | Type | Inter. | State | Resp. | A11Y | Coherence | AI risk | Individuality |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Login | 4 | 4 | 4 | 4 | 3 | 4 | 4 | 3 | 3 | 3 | 3 | 3 |
| Setup wizard | 3 | 2 | 3 | 3 | 3 | 3 | 4 | 2 | 2 | 3 | 3 | 3 |
| Dashboard | 2 | 2 | 4 | 3 | 3 | 3 | 3 | 3 | 3 | 2 | 2 | 3 |
| Production list | 4 | 4 | 4 | 4 | 3 | 4 | 4 | 3 | 3 | 4 | 3 | 3 |
| Production detail | 3 | 3 | 4 | 3 | 3 | 3 | 4 | 3 | 3 | 3 | 2 | 3 |
| User Linking | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 2 | 3 | 3 | 3 | 2 |
| Connections | 4 | 4 | 4 | 4 | 3 | 4 | 4 | 4 | 3 | 4 | 3 | 3 |
| System Status | 3 | 2 | 4 | 3 | 4 | 3 | 3 | 3 | 3 | 3 | 2 | 3 |
| Audit Log | 4 | 4 | 4 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 3 | 2 |
| Workflow diagnosis | 2 | 1 | 2 | 2 | 2 | 2 | 3 | 1 | 2 | 2 | 1 | 2 |

## 18. Top 20 UI Debts

1. Global animated dot field
2. Global radial gradients competing with content
3. `glass`/nested card overuse
4. Dashboard hierarchy between issue, next action, metrics, CTA
5. Repeated status labels with different scopes
6. Seven-step wizard decision density
7. Wizard drag/keyboard parity uncertainty
8. Wide wizard/diagnosis tables at narrow widths
9. Mobile nav duplicate DOM
10. No observed reduced-motion rule
11. Edit-form labels not consistently associated
12. Dialog name/description not consistently associated
13. SVG chart history lacks equivalent nonvisual detail
14. Global User Linking vs local Production association memory cost
15. Raw IDs/scope/counts in workflow diagnosis
16. Mixed `ok/warn/bad` vs semantic class vocabulary
17. Hard-coded English in bilingual surfaces
18. Multiple deletion/removal renderer variants and broken JP confirmation text
19. Production Storage context loss and detail tab scope mixing
20. Real runtime evidence absent for authenticated JP/EN and required widths

## 19. Recommended Design Principles

1. Task outcome before atmosphere.
2. Production is the durable context.
3. One canonical state, one next action.
4. Surface only semantic boundaries: ownership, risk, or independent task.
5. Keep technical identity in diagnostics unless it is necessary for the decision.
6. Preview and consequence before external mutation.
7. Equal visual weight requires equal operational value.
8. JP/EN must match in meaning, order, density, and accessibility.
9. Motion must explain a state change or focus; otherwise it is absent.
10. Graphs supplement exact lists/forms; they do not replace them.
11. Keyboard and reduced-motion behavior are design requirements, not polish.
12. Use orange/dark as restrained identity, not as continuous visual noise.

## 20. Recommended Next Step Before DESIGN.md

Do not start DESIGN.md yet. First run a bounded evidence pass on the actual authenticated v0.4.4 runtime: JP/EN at 375/768/1024/1440, keyboard-only setup/routing/destructive dialog, screen-reader landmarks/announcements, reduced-motion, and representative empty/error/connected states. Then conduct operator task tests for (a) first Production connection, (b) route correction, (c) User Linking→Production association, and (d) diagnosis/recovery. Use those results to resolve the contested Dashboard grid, mobile nav, font loading, and Connection Map scope decisions.

## Traceability and scope

Primary reports: `docs/audits/uiux-agent0-inventory.md` through `uiux-agent12-adversarial.md`。13/13 specialist scopes were completed in waves, with completed handles released before the next wave; Agent 6 required one bounded retry after a stalled run. The initial concurrency-limit error was handled by wave scheduling as requested. The reports were generated in the permitted audit directory; no application files were changed by this audit.

主要ソース: `src/main.go:860-936`; `src/setup/ui.go:1-75,320-370,537-765`; `src/setup/ia_views.go:1-160`; `src/setup/admin.go:40-240,375-440,775-920,1086-1130,2120-2340`; `src/setup/channel_plan.go:112-170`; `src/setup/workflow_diagnosis.go:411-505`; `src/setup/current_routing.go`; `docs/CURRENT-IA-UI-SPEC.md`。

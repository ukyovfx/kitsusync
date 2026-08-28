# Agent 7 — Motion / Dot / Spatial audit

結論: 現状のmotionは、操作フィードバックとしては概ね低強度だが、全管理画面に常時適用される粒子背景と初期表示アニメーションが、業務用の状態確認画面では装飾過多になり得る。最大の未対応事項は `prefers-reduced-motion` の明示的な扱いがソース上確認できないこと。ドットはSystem Statusの自動更新を補助する小さな静的インジケーターだけで、点滅・pulseは確認できない。

## Scope and method

- 対象: `C:\Users\mynti\Documents\KitsuSync-clean`
- 対象画面: `/bot/login`、`/bot/setup`、`/bot/admin`、`/bot/admin/bot`、`/bot/admin/projects`（一覧/詳細タブ）、`/bot/admin/users`、`/bot/admin/health`、`/bot/admin/audit`、`/bot/docs/`
- 対象外: UI/CSS/routes/behaviorの変更、実行時の外部接続、他Agentの監査文書、Git操作。
- 判定根拠: サーバー側HTML生成と共通CSSの静的読解。ブラウザ実機での知覚評価ではない。
- 強度尺度: 0=なし、1=低（補助的/視認負荷が小さい）、2=中（画面の注意を継続的に引く可能性）、3=高（主作業を遮る/強い点滅・移動）。

証拠と推論を分離する。`Evidence` はソースに直接存在する事実、`Inference` はその事実からのUX評価である。

## Candidate classification

| Candidate | Classification | Evidence | Inference | Severity / confidence |
|---|---|---|---|---|
| 全画面の粒子パターン移動 | DECORATIVE; 条件次第で DISTRACTING | `body::before` が全viewportに固定され、粒子背景を持ち、`opacity:.22` と `animation:particleDrift 18s linear infinite` を指定している（`src/setup/ui.go:45-56`）。`particleDrift` は背景位置を18秒で移動させる（`src/setup/ui.go:67-70`）。 | 業務状態・ログを読む画面では、意味を持たない継続運動が視線競合になる。特に長時間滞在、低輝度環境、前庭/注意過敏では装飾より負荷が先に立つ可能性がある。 | Major / high |
| カード/タイルの初期表示 | DECORATIVE | `.tile,.section-card,.page-card{animation:riseIn .42s ease both;}`（`src/setup/ui.go:285-286`）、`riseIn` は透明状態から10px上へ移動して表示する（`src/setup/ui.go:71-74`）。 | 初回の状態把握を遅らせる程度は小さいが、一覧・診断・ログで要素数が多い場合は連続的な出現感が増える。 | Minor / high |
| リンク、ボタン、入力、ナビのhover/focus遷移 | FUNCTIONAL | ナビ等は `transition:all .18s ease` とhover時の `translateY(-1px)`（`src/setup/ui.go:218-234`）、入力は境界/影/背景を `.18s` で遷移（`src/setup/ui.go:343-346`）、ボタンもtransform等を `.18s` で遷移する（`src/setup/ui.go:350-353`）。 | 操作対象と現在のポインター/フォーカス位置を示すフィードバック。短時間かつ低振幅で、通常は主作業を邪魔しない。 | Enhancement / high |
| 言語切替のthumb移動 | FUNCTIONAL | `.lang-thumb` が `left .18s ease` で移動し、`data-lang="en"` 時に位置が変わる（`src/setup/ui.go:251-262`）。 | 変更後の選択状態を空間的に理解しやすくする。画面遷移を伴うため、実際の知覚効果は実機未確認。 | Minor / high |
| Production routing / wizard plan の行移動 | FUNCTIONAL | routing行とwizard行は `draggable="true"`。drag中は `opacity:.45` と背景、drag-overは内側の下線で示す（`src/setup/current_routing.go:275,308`、`src/setup/ia_views.go:2689,2699,2707,2756`、`src/setup/ui.go:150`）。 | これは装飾ではなく、並び替え対象・drop位置の状態表示。情報量が高い編集画面でも目的に結びつく。キーボードのAlt+Arrow操作も実装されているため、motion依存は限定的。 | Minor / high |
| System StatusのAPI観測グラフ更新 | FUNCTIONAL | `/bot/admin/health` では `interval=5000` の周期処理があり、5秒ごとに観測値を取得し、status pillとSVG bar chartを置き換える（`src/setup/ia_views.go:347-349,1355-1357`）。 | データ更新は機能的。ただし更新時の視覚変化に専用のreduced-motion分岐は見当たらないため、変動の大きい環境では注意を奪う可能性がある。 | Major / medium |
| System Statusのライブdot | FUNCTIONAL | `.system-live-indicator i` は7pxの円形、緑背景、3px相当の静的ring。markupは `aria-hidden="true"` のiと「Auto-refresh」ラベル（`src/setup/ui.go:485`、`src/setup/ia_views.go:1410-1411`）。pulse/keyframesは確認できない。 | dot単体は補助的な状態記号で、意味は隣接テキストが担う。静的であるため、点滅による注意喚起や誤った「リアルタイム保証」の印象は抑えられている。 | Enhancement / high |
| accordion caret回転 | FUNCTIONAL | `.accordion-caret` に `.18s` transition、open時に `rotate(180deg)`（`src/setup/ui.go:386-397`）。 | 開閉方向の状態を空間的に伝える小さなフィードバック。 | Enhancement / high |

## Screen-by-screen motion and dot strength

| Route / screen | Motion candidates | Dot strength | Assessment |
|---|---|---:|---|
| `/bot/login` | 共通のparticleDrift、page-cardのriseIn、入力/ボタンのfocus・hover transition | 0 | 画面固有のdot markupは確認できない。ブランド/装飾の比率が機能より高く、背景移動はDECORATIVE、場合によってDISTRACTING。 |
| `/bot/setup` steps 1–3 | 共通背景/カード登場、入力・ボタン遷移。ステップ状態は `setup-step` と `aria-current="step"` の静的表示（`src/setup/ia_views.go:2451-2474`）。 | 0 | ステップ表示は状態説明でありdotではない。画面遷移のmotionではなくページ再描画。 |
| `/bot/setup` steps 4–5 plan/review | 共通背景/カード登場、入力/ボタン遷移、drag/dropとdrag-over。 | 0 | 行移動はFUNCTIONALで強度1。操作対象のopacity/下線が明確。 |
| `/bot/setup` steps 6–7 execute/complete | 共通背景/カード登場、結果の `role="status" aria-live="polite"`（`src/setup/ia_views.go:2484-2487,2857`）。 | 0 | 完了/実行状態はテキストとstatusで伝え、dot/pulseなし。装飾背景は強度2相当。 |
| `/bot/admin` Dashboard | 共通背景/カード登場、nav/button/card hover、status pill・attention count（`src/setup/ia_views.go:319-323`）。 | 0 | status pillはdotではなく文字付き機能状態。hoverはFUNCTIONAL強度1、背景はDECORATIVE強度2。 |
| `/bot/admin/bot` Connections | 共通背景/カード登場、入力/ボタン遷移、Kitsu/Discord status pill（`src/setup/admin.go:2914-2933,3039-3040`）。 | 0 | 接続状態はpillの文字と色で示され、独立dotは確認できない。点滅なし。 |
| `/bot/admin/projects` list | 共通背景/カード登場、button hover、production status pill（`src/setup/ia_views.go:476-492`）。 | 0 | 一覧の主要作業は比較・選択なので、背景移動の相対注意強度が上がる。dotは0。 |
| `/bot/admin/projects?project=…` detail tabs/overview/users/notifications/activity/troubleshooting | 共通背景/カード登場、tab/button/input transition、accordion caret、routing編集時のdrag state。 | 0 | 静的status pillが中心。routing editの行移動だけFUNCTIONAL強度1。詳細/診断の読解中はparticleDriftがDISTRACTING寄り。 |
| `/bot/admin/users` User Linking | 共通背景/カード登場、button/input transition、status pill（`src/setup/ia_views.go:1959-1968`）。 | 0 | dot/pulseの証拠なし。反復的な紐づけ作業では常時背景運動が不要な注意負荷になり得る。 |
| `/bot/admin/health` System Status | 共通背景/カード登場、5秒ごとのAPI観測値/graph更新、accordion等の小遷移、static live dot。 | 1 | dotは補助記号として低強度。機能的motionは強度1–2。reduced-motion時も周期更新そのものは意味があるが、視覚的な差分の抑制が必要。 |
| `/bot/admin/audit` Audit Log | 共通背景/カード登場、button/table interaction、ローカル時刻へのテキスト置換（`src/setup/ia_views.go:1934-1956`）。 | 0 | ログは比較・精読中心。motion固有の追加証拠はなく、背景運動が最も相対的に目立ちやすい画面。 |
| `/bot/docs/` | `site.jsx` の画面構造上、motion/animation/transition/prefers-reduced-motionの実装証拠は確認できない。 | 0 | 静的ドキュメントとして扱われている。サーバー管理画面の共通CSSとは別実装のため、同じ評価を自動適用しない。 |

## Reduced-motion audit

Evidence: `src/setup/ui.go` には `@keyframes particleDrift`、`@keyframes riseIn`、複数のtransition、drag stateがあるが、`@media (prefers-reduced-motion: reduce)` の記述は確認できない（`src/setup/ui.go:56,67-74,228,260,286,343,351,396`）。JS側のSystem Status周期更新も、ユーザーのmotion preferenceを読む分岐は確認できない（`src/setup/ia_views.go:347-349,1355-1357`）。

Inference: motionを減らしたいユーザーに対し、全画面粒子移動、初期riseIn、hover/accordion/lang-thumb/dragの視覚変化がそのまま残る可能性が高い。特に粒子背景は意味を持たないため、reduced-motion時に最初に無効化すべき候補。周期的なhealth更新は機能なので停止と同一視せず、表示のアニメーションだけを抑える設計が適切。

Finding M1 — Major / confidence high: reduced-motion contract is absent in the shared admin theme. Scope is all screens using `adminThemeCSS`; the most direct evidence is the always-running `particleDrift` and `riseIn`. Acceptance evidence for a future fix should verify computed styles and screen-reader-independent visual behavior with both `no-preference` and `reduce`.

Finding M2 — Major / confidence medium: System Status has a functional 5-second refresh but no evidence of motion-preference-aware visual update policy. A rapidly changing bar chart can become visually noisy even when the user needs the data refresh. The audit cannot determine the actual perceptual severity without runtime observation and representative API latency data.

Finding M3 — Minor / confidence high: the static live dot is appropriately restrained, but its green color/ring is a secondary cue only. The adjacent text and status semantics must remain the source of truth; the dot must not become the only indication of refresh state.

## Untested scope

- ブラウザ実機、画面録画、フレームレート、初回表示時の要素数別の知覚評価は未実施。
- OS/browserの `prefers-reduced-motion` 設定下でのcomputed style、キーボード操作、drag/dropの実機挙動は未確認。
- `/bot/admin/health` の実データ量、5秒更新時のチャート変動、失敗/復旧時の差分は未確認。
- 低スペック端末、ズーム、長時間滞在、前庭障害・光過敏等のユーザーテストは未実施。
- `site.jsx` の最終ブラウザレンダリングと配信時CSSは未確認。静的ソース上で固有motion証拠が見つからないことだけを記録している。

## Source references

- `src/setup/ui.go:45-74` — global fixed particle background and keyframes.
- `src/setup/ui.go:218-234,251-262,285-287,343-353,386-397,485-486` — interaction transitions, card entry, accordion rotation, live dot.
- `src/setup/ia_views.go:2451-2474,2689-2756` — wizard progress and plan row movement.
- `src/setup/ia_views.go:319-323,476-492` — dashboard and Production list status presentation.
- `src/setup/ia_views.go:347-349,1338-1411` — System Status refresh and live indicator.
- `src/setup/ia_views.go:1934-1956` — Audit Log rendering.
- `src/setup/admin.go:2914-2933,3039-3040` — Connections status presentation.

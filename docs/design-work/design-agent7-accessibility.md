# Agent 7 — Accessibility contract

対象: `C:\Users\mynti\Documents\KitsuSync-clean`
範囲: WAVE 2 のデザインシステム契約。UIコード、route、behavior、`DESIGN.md` は変更していない。

## 結論

アクセシビリティは後付けの検査項目ではなく、すべての画面・状態・JP/EN の設計入力とする。必須契約は、意味的なランドマークと見出し、安定した名前/ラベル、状態変化の通知、ダイアログの名前付けと focus/return、十分な操作領域、色に依存しない状態表現、ズーム/reflow/text spacing、reduced motion、非テキストデータの代替、破壊操作の確認である。

現状はログインの runtime で `main`/`form`/`h1`、44px 高さ、JP/EN の折返しが確認され、共通 shell には skip link、`main`、polite live region、focus-visible がある。一方、認証後画面・実ダイアログ・キーボード実操作・スクリーンリーダー・200/400% zoom は runtime 正本で未確認である。したがって、以下は「現在すでに満たす」とは限らない、将来の必須 design-system rule として扱う。

## 現在確認できる事実と境界

| 現在の事実 | 根拠 | 判定の境界 |
|---|---|---|
| 共通 shell は `<html lang>`, skip link, `main#main-content`, `#ui-status[aria-live=polite][aria-atomic=true]` を出力する | `src/setup/ui.go:760-790` | live region の各更新内容・全 route の到達性は未確認 |
| 共通 primary navigation は `nav` の中にデスクトップ用リンクと mobile `details/summary` を持つ | `src/setup/ui.go:755-759` | runtime の認証後ナビ、重複読み上げ、開閉後の focus は未確認 |
| ログインの hostname/email/password は label と `for`/`id` を持ち、エラーは assertive alert | `src/setup/middleware.go:537-550`; `src/setup/accessibility_markup_test.go:8-27` | protected edit form 全体ではない |
| Current IA の Production tabs は tablist/tab/tabpanel、選択状態、矢印/Home/End を持つ | `src/setup/ia_views.go:562-576` | 実ブラウザの focus/order と各 locale は未確認 |
| 共通 focus-visible は 3px outline、主要 input/select/button は概ね 44px 高さ | `src/setup/ui.go:348-358`; `src/setup/responsive_accessibility_test.go:10-21` | 28–32px の compact controls、実ターゲット間隔、contrast は未確認 |
| 現行 source には reduced-motion の上書きがある | `src/setup/ui.go:75-80, 524-529` | `docs/UI-UX-RUNTIME-EVIDENCE.md:38` と `docs/audits/uiux-runtime-agentD-motion.md:D-M01–D-M04` は、別時点の runtime で抑制されなかった記録。現行 source と runtime の対応関係を再検証する必要がある |
| runtime は Login の JP/EN 8 viewport のみ。認証後画面、dialogs、System Status は未到達 | `docs/UI-UX-RUNTIME-EVIDENCE.md:20-43, 106-142` | Login の画像から protected UI の適合を推論しない |

## Mandatory contract

### A11Y-01 — Landmarks and page structure

- WHAT: 各ページは一つの主 `main`、目的が識別できる `nav`、必要な場合の header/region/form を意味的に使い、同じページに同じ目的の主要 landmark を重複させない。`main` には一意な参照先を持つ skip link を置く。
- WHY: 支援技術がページの入口・主要ナビ・作業領域を短絡して移動でき、desktop/mobile の表示差が読み上げ上の重複を生まないようにする。現行 shell には `primary-nav` の desktop と mobile の両方が同じ `nav` 内に出るため、重複レンダリングは特に確認対象である (`src/setup/ui.go:755-759`)。
- WHEN: 新規 route、layout、responsive navigation、モーダル外の主領域を設計・変更するとき。JP/EN と anonymous/authenticated の各 state で確認する。
- WHEN NOT: 見た目だけの wrapper に landmark role を追加しない。`div` を `region`/`navigation` に昇格させ、目的名のない landmark を増やさない。

### A11Y-02 — Heading hierarchy

- WHAT: 画面の主題は一つの `h1`、主要 section は `h2`、その下位 section は `h3` という連続した階層で表し、見た目の大きさだけで見出しを表現しない。見出しが region/dialog/tab panel を命名する場合は一意な `id` と `aria-labelledby` を結ぶ。
- WHY: スクリーンリーダーの見出し一覧と region navigation が、画面の作業順と一致するため。現行には shell/test の一つの main heading を確認するテストがある (`src/setup/responsive_accessibility_test.go:16-21`) が、全 protected route の hierarchy は未確認である。
- WHEN: page title、card/section、wizard step、dialog、tab panel を追加・再編するとき。
- WHEN NOT: eyebrow、status、装飾テキストを heading にしない。階層を飛ばしてサイズ調整で意味を補わない。

### A11Y-03 — Names, labels, and localized identity

- WHAT: すべての非 hidden form control は一つの安定した accessible name を持ち、visible label を `for`/`id` または明示的な `aria-labelledby` で関連付ける。placeholder は label の代替にしない。icon-only control は対象と作用を name に含める。JP/EN の name、label、error、help は同じ locale source から生成する。
- WHY: 視覚的に近い文字だけでは、支援技術が入力目的・対象行・遷移先を保証できない。現行の一部編集フォームには `label` と input の関連がない (`src/setup/admin.go:1006-1013, 2615-2619, 3443-3451`)。一方 login と wizard channel input は関連付け済み (`src/setup/middleware.go:548-550`; `src/setup/ia_views.go:2689, 2707`)。
- WHEN: form、table row action、language switch、menu、status、dynamic error を作るとき。翻訳変更と同時に accessible name を確認する。
- WHEN NOT: `aria-label` で visible label を無言で置換しない。`aria-hidden` を操作可能な要素や唯一の説明に付けない。言語切替を英語固定の抽象名 `Toggle language` にしない (`src/setup/ui.go:552-555`)。

### A11Y-04 — Live regions and status communication

- WHAT: 非同期の成功・失敗・保存中・接続状態・polling 結果は、変更された情報だけを適切な `role=status`/polite live region または緊急エラーの alert で通知する。通知文は対象、結果、次の操作を含め、色やアイコンだけにしない。
- WHY: 視覚的な toast、status pill、chart 更新を見られない利用者にも、作業の完了・停止・回復を伝えるため。現行には `#ui-status`、wizard の status/alert、login alert がある (`src/setup/ui.go:785-787`; `src/setup/first_time_connection.go:270-281`; `src/setup/middleware.go:538-540`) が、全 route の一貫性は未確認である。
- WHEN: POST 結果、保存中、test notification、5秒 polling、validation/error、dialog の内容が変わるとき。重複通知を抑え、atomicity を必要な範囲に限定する。
- WHEN NOT: 静的な全画面を assertive にしない。頻繁な polling や hover 状態を毎回読み上げさせない。success/error を背景色・dot・pill 色だけで表さない。

### A11Y-05 — Dialog semantics, keyboard focus, and return

- WHAT: dialog は一意な title を `aria-labelledby`、必要な説明を `aria-describedby` に結び、開いたら目的に応じた最初の focus（確認語が必要なら入力、そうでなければ安全な Cancel）へ移す。open 中は背後へ Tab が抜けず、Escape/Cancel で閉じ、元の trigger へ focus を返す。破壊操作の対象と作用範囲を dialog 内で読めるようにする。
- WHY: 操作対象・不可逆性・確認条件を失わず、キーボード利用者が modal の外へ迷子にならないため。共通 delete modal は title、description、focus trap、Escape、return を実装している (`src/setup/admin.go:4080-4084`; `src/setup/ui.go:581-654, 697-719`)。routing delete dialog は title に `id`/`aria-labelledby` がなく、現行 source の要改善点である (`src/setup/current_routing.go:270-278`)。
- WHEN: native dialog、custom modal、confirmation、編集 drawer、エラー dialog を設計するとき。実 browser で open/close、Tab/Shift+Tab、Escape、Cancel、submission failure、focus return を検証する。
- WHEN NOT: dialog を単なる視覚 overlay として出し、背後を操作可能なままにしない。`aria-modal`、`aria-hidden`、CSS visibility の状態を不一致にしない。説明を title だけに詰め込まない。

### A11Y-06 — Keyboard order and visible focus

- WHAT: すべての操作は pointer なしで到達・実行でき、DOM/tab order は画面の作業順と一致する。focus indicator は常時視認でき、背景・境界・隣接色に埋もれない。drag/reorder は keyboard の同等操作を持つ。
- WHY: 現行には全体 focus-visible、tab keyboard 操作、routing/wizard の Alt+Arrow がある (`src/setup/ui.go:351-352`; `src/setup/ia_views.go:575`; `src/setup/current_routing.go:307-308`) が、実操作の runtime evidence はないため、設計段階で契約化する必要がある。
- WHEN: 新しい link/button/menu/tab/disclosure/drag/reorder、responsive DOM order、validation focus を追加するとき。JP/EN、375px、zoom state を含める。
- WHEN NOT: click handler だけの `div`、hover-only affordance、視覚順と DOM 順が異なる grid、focus を `outline:none` で消す実装を採用しない。

### A11Y-07 — Touch targets and spacing

- WHAT: primary action、navigation、form control、解除/削除入口を少なくとも 44×44 CSS px の実操作領域にする。24×24 CSS px は WCAG 2.2 Target Size (Minimum) の下限確認であり、design-system の通常目標にはしない。隣接 target は押し分けられる間隔を持つ。
- WHY: 現行の主要 button/input/nav は概ね 44px だが、wizard move/exclude は 28px、routing menu summary は 32px (`src/setup/ui.go:218, 226, 279, 350, 357, 537-549`; `src/setup/ia_views.go:2689, 2707`)。小さい icon のまま hit area を拡大すれば、視覚密度を保ちながら誤操作を減らせる。
- WHEN: touch、pen、低精度 pointer、375px 幅、compact row action を設計・受入するとき。
- WHEN NOT: 44px を単に icon の大きさへ強制しない。隣接 target を重ねたり、scroll container の端に押し付けたりしない。サイズを満たしたことだけで keyboard/name/contrast の合格とみなさない。

### A11Y-08 — Status, charts, and SVG alternatives

- WHAT: status は text と shape/position/icon など複数の手掛かりで表し、成功/失敗/blocked/warning を色だけにしない。SVG/chart は補助表示とし、近接する table、一覧、または十分な visually-hidden description で全体の要点・時刻・値・成功失敗を提供する。個別 interactive mark にも accessible name を付ける。
- WHY: 現行 health chart は `role="img"` と observations/期間の `aria-label` を持つが、旧描画では棒の success/failure が色 class と ms title 中心で、一覧性が不足する (`src/setup/ia_views.go:1505-1524, 1699-1709`; `docs/audits/uiux-agent5-accessibility.md:A5-005`)。System Status runtime 自体は未到達 (`docs/UI-UX-RUNTIME-EVIDENCE.md:29-32, 122-142`)。
- WHEN: metric、status pill、health graph、sparkline、icon legend、empty/error state を設計・更新するとき。データ更新時も text alternative を同じ snapshot から更新する。
- WHEN NOT: `role="img"` と短い aria-label だけで detailed chart data の代替としない。赤/緑、塗り/未塗り、点滅、位置だけを唯一の状態伝達にしない。

### A11Y-09 — Reflow, zoom, and text spacing

- WHAT: content は 200% zoom、400%相当 reflow、text spacing override、JP/EN の長い error/help/identifier でも information loss、horizontal two-dimensional scrolling、overlap、clipped focus、hidden action を起こさない。長い人間可読名は自然に wrap し、opaque ID は完全値への access を持つ。
- WHY: Login の 375×812 では JP instruction が wrap し、8つの Login viewport に水平 overflow はなかったが、これは protected UI、zoom、text spacing、dynamic error を証明しない (`docs/UI-UX-RUNTIME-EVIDENCE.md:20-43`; `docs/audits/uiux-runtime-agentC-accessibility.md:A11Y-C-004`)。現行 CSS は `overflow-wrap:anywhere` などを使う一方、host に `break-all` もある (`src/setup/ui.go:145-151, 372, 378-391`)。
- WHEN: 新 route、new locale copy、table/card、error/loading、dialog、chart label を受入するとき。375/768/1024/1440px と 200/400% zoom、text spacing を組み合わせる。
- WHEN NOT: Login screenshot の無 overflowだけから authenticated state の合格を推測しない。重要な resource 名・原因・確認語を省略記号だけで隠さない。

### A11Y-10 — Reduced motion

- WHAT: `prefers-reduced-motion: reduce` では無限 decorative motion、非必須の entrance/transform、transition を停止または instant 化し、状態変化に必要な最小限の視覚変化だけ残す。機能的 polling と decorative animation を別々に扱う。
- WHY: 現行 source には `particleDrift`、`riseIn`、複数 transition とそれらの reduce override がある (`src/setup/ui.go:45-80, 218-228, 251-262, 285-286, 343-358, 396-397, 524-529`)。ただし前回 runtime は reduce 下でも motion style が残った (`docs/UI-UX-RUNTIME-EVIDENCE.md:38`; `docs/audits/uiux-runtime-agentD-motion.md:D-M01–D-M04`)ため、source/build/runtime の整合性を acceptance gate にする。
- WHEN: motion を追加・変更するとき、または shared CSS/build artifact を更新するとき。JP/EN Login と代表的 protected route で no-preference/reduce を比較する。
- WHEN NOT: 全 animation を無条件に消して、loading、progress、状態変化の理解を失わせない。CSS override の存在だけで runtime の適合と宣言しない。

### A11Y-11 — Destructive confirmation and recovery

- WHAT: delete、unlink、remove、disconnect、resource cleanup は安全な初期状態、対象名、影響範囲、不可逆性、Cancel、実行 action を明示し、必要なら exact-name/phrase confirmation を要求する。確認後の結果と未完了 cleanup は status と recovery path で通知する。
- WHY: 現行の stronger delete と routing channel delete は対象・件数・範囲の review、exact channel name、disabled initial submit を持つ (`src/setup/admin.go:1328-1435, 1511-1618`; `src/setup/current_routing.go:270-278`; `src/setup/ui.go:606-654`)。しかし同じ解除 action でも plain POST と確認 modal が混在する (`docs/audits/uiux-agent4-interaction.md:14-60`)。安全契約は action 名ではなく全入口へ適用する。
- WHEN: Production connection、Discord resource、route、user/checker link、global link の解除・削除・cleanup を設計するとき。失敗、権限不足、部分完了、再試行可能性も同じ確認/結果モデルで表す。
- WHEN NOT: 破壊操作を click/Enter 一回で即時送信しない。対象名を省略した generic `Confirm action` だけにしない。確認語を locale 間・表示と server expectation で不一致にしない。視覚的な赤色だけで destructive scope を伝えない。

## Acceptance evidence gate

次の evidence が揃うまで、アクセシビリティ contract の実装 PASS や `DESIGN.md` 反映を宣言しない。

- JP/EN の Login、Dashboard、Productions、Production detail、Connections edit、User Linking、System Status、Audit Log、Setup、diagnostics、dialogs を実際に render し、landmark、heading、accessible name、live region、focus order を記録する。
- keyboard-only で全 interactive element、tab/tabpanel、disclosure、reorder、dialog open/close、Escape、Cancel、focus return、validation/error recovery を記録する。スクリーンリーダーまたは accessibility tree で name/role/state/description を確認する。
- 375×812、768×1024、1024×768、1440×900、200%/400% zoom、text-spacing override、長い JP/EN copy、opaque identifiers、error/loading state を確認する。
- 主要 target と compact action の実測、focus indicator、非色 status、forced-colors/high-contrast、chart text alternative を確認する。
- no-preference/reduced-motion の computed style だけでなく、現行 build の runtime behavior を比較し、`docs/UI-UX-RUNTIME-EVIDENCE.md:38` の旧記録との対応関係を解消する。
- destructive action は送信せずに fixture/static inspection で確認経路を検証し、実環境での delete/unlink は安全な認証済みテスト計画なしに行わない。

## Sources

- Current shell/forms/dialog source: `src/setup/ui.go`; `src/setup/middleware.go`; `src/setup/admin.go`
- Current IA/tabs/routing/chart source: `src/setup/ia_views.go`; `src/setup/current_routing.go`
- Current accessibility tests: `src/setup/accessibility_markup_test.go`; `src/setup/responsive_accessibility_test.go`; `src/setup/ui_header_test.go`
- Evidence source of truth: `docs/UI-UX-RUNTIME-EVIDENCE.md`; `docs/audits/runtime-evidence/`; `docs/audits/uiux-runtime-agentC-accessibility.md`; `docs/audits/uiux-runtime-agentD-motion.md`
- Prior accessibility/interaction audits: `docs/audits/uiux-agent5-accessibility.md`; `docs/audits/uiux-agent4-interaction.md`
- Design/IA context: `docs/CURRENT-IA-UI-SPEC.md`; `docs/design-work/design-agent4-navigation.md`; `docs/design-work/design-agent3-typography.md`
- External criteria referenced by the evidence: [WCAG 2.2](https://www.w3.org/TR/WCAG22/), especially 1.1.1, 1.3.1, 1.4.10, 2.1.1, 2.4.3, 2.4.7, 2.5.8, 3.3.2, 4.1.2.

# Agent 5 — Accessibility Audit (v0.4.4)

対象: `C:\Users\mynti\Documents\KitsuSync-clean` の v0.4.4 現行コード。JP/EN の匿名ログインと、コード上で確認できる Current IA 管理画面・セットアップ画面を対象に、WCAG/HIG aligned の観点で監査した。設計監査のみで、アプリ実装・UI/CSS・routes・runtime・外部サーバー・Git は変更していない。

## 判定の読み方

- `Evidence` はコードまたは実行で直接確認した事実。
- `Inference` は、その事実から利用者影響を推論したもの。
- Severity は依頼指定の `CRITICAL/HIGH/MEDIUM/LOW`。Action class は修正優先度の補助として `P0–P3` を付した。
- WCAG の criterion は、証拠が直接支える場合だけ記載した。HIG は Apple の操作・ターゲットサイズ・Reduced Motion の指針を補助基準にした。

## Findings

### A5-001 — 編集フォームの多数のラベルが入力コントロールに関連付いていない

- Severity: HIGH / Action class: P1
- Confidence: High
- Criterion: WCAG 1.3.1 Info and Relationships, 3.3.2 Labels or Instructions, 4.1.2 Name/Role/Value
- Page/state: `/bot/admin/bot?edit=1&lang=ja|en`; `/bot/admin/users` の production/reviewer 編集状態; 旧 setup 編集フォームが到達可能な状態
- User/task: スクリーンリーダー利用者が接続情報、ユーザー紐づけ、Reviewer/Checker、Guild ID を編集する。
- Evidence: `src/setup/admin.go:1008-1009` は `<label>Discord Guild ID</label>` に `for` がなく、入力にも `id` がない。`src/setup/admin.go:2616-2618` は person、Discord ID、表示名を同様に出力している。`src/setup/admin.go:3445-3450` は Kitsu hostname、Discord Bot Token、Runtime email/password を同様に出力している。反対に、ログインと Current IA wizard の一部は `for`/`id` を持つため、画面全体に一貫した実装ではない。
- Inference: 支援技術がフィールド名を自動的に確実に読み上げられず、同一画面で複数の入力を区別しにくい。通常の視覚利用者には見えないが、キーボード・スクリーンリーダー利用者の編集タスクを阻害する。
- Recommendation: すべての非 hidden コントロールに一意な `id` を付け、対応する `<label for>` または明示的な `aria-labelledby` を付ける。placeholder をラベルの代替にしない。JP/EN の表示ラベルと accessible name を同じ翻訳カタログから生成する。

### A5-002 — Current IA のルーティング削除ダイアログにアクセシブルな名前がない

- Severity: MEDIUM / Action class: P2
- Confidence: High
- Criterion: WCAG 4.1.2 Name, Role, Value; 1.3.1 Info and Relationships
- Page/state: `/bot/admin/projects?project=<id>&tab=notifications&edit_routing=1&lang=ja|en` の「Delete Discord channel」操作
- User/task: キーボードまたはスクリーンリーダーでルーティング行の削除確認を行う。
- Evidence: `src/setup/current_routing.go:270-272` は `<dialog class="routing-delete-dialog">` 内に `<h4>` を出力するが、`aria-labelledby`、`aria-label`、見出しの `id` のいずれも付けていない。なお確認入力は label で包まれているが、dialog 自体の名前付けとは別である。
- Inference: dialog に入ったとき支援技術が「何の確認ダイアログか」を安定して取得できず、複数行の削除操作では対象チャンネルの文脈も失いやすい。
- Recommendation: dialog ごとに一意な heading `id` を生成して `aria-labelledby` を設定し、説明文にも `aria-describedby` を設定する。対象チャンネル名を heading または説明に含める。実ブラウザで dialog open/close、Escape、Cancel、入力フォーカスを再テストする。

### A5-003 — 共通削除確認モーダルの説明が読み上げツリーに関連付いていない

- Severity: MEDIUM / Action class: P2
- Confidence: High
- Criterion: WCAG 1.3.1, 4.1.2
- Page/state: `adminPage` が生成する各管理画面の destructive action 実行時
- User/task: スクリーンリーダー利用者が削除・連携解除の確認内容と必要な確認語を理解する。
- Evidence: `src/setup/admin.go:4080-4084` は `role="dialog" aria-modal="true" aria-labelledby="deleteModalTitle"` を出力する一方、本文 `#deleteModalText`、補助説明 `#deleteModalHelper`、確認語の説明は `aria-describedby` で dialog に関連付けていない。`src/setup/ui.go:600-623` で JavaScript が本文を後から設定するが、accessible description の参照関係は追加していない。
- Inference: dialog の名前だけが読み上げられ、対象操作・不可逆性・入力すべき確認語が自動で読まれない可能性がある。確認語入力が必要なケースでは、視覚的には表示される重要情報が非視覚利用者に伝わりにくい。
- Recommendation: `aria-describedby="deleteModalText deleteModalHelper"` を常設または状態に応じて設定し、helper が非表示のときは参照から外す。確認語入力にも明確な説明を関連付ける。モーダル表示時に状態が更新されたことを live region または適切な dialog description として検証する。

### A5-005 — System Status の SVG グラフは失敗/成功の分布と各値を非視覚的に提供しない

- Severity: MEDIUM / Action class: P2
- Confidence: High
- Criterion: WCAG 1.1.1 Non-text Content, 1.3.1 Info and Relationships
- Page/state: `/bot/admin/health?lang=ja|en` の External API health グラフ（初期描画および 5 秒ごとの更新）
- User/task: スクリーンリーダー利用者が API 観測の推移と失敗箇所を確認する。
- Evidence: `src/setup/ia_views.go:1524` および `src/setup/ia_views.go:1709` の SVG は `role="img"` と `aria-label="<件数> observations, <期間>"` だけを持つ。棒の success/failure は `bar-success`/`bar-failure` の色クラスに分けられ、各棒の `<title>` は ms 値だけで、成功/失敗、時刻、全観測の一覧を accessible name/description に含めていない。JavaScript 更新も `src/setup/ia_views.go:348` 付近で同じ `role="img"` と色クラスを再生成する。
- Inference: status pill と現在値は読めても、グラフの履歴にある失敗数・該当時刻・個別値を視覚なしでは取得できない。色だけに依存する履歴情報が残る。
- Recommendation: グラフの近くに成功/失敗件数、最終失敗時刻、各観測値を表形式または visually hidden の一覧として提供し、SVG は補助表示にする。成功/失敗を色以外のテキストでも表現する。

### A5-006 — 非常に短い操作ターゲットが HIG の標準的なタップサイズを下回る

- Severity: LOW / Action class: P3
- Confidence: High
- Criterion: WCAG 2.5.8 Target Size (Minimum) は 24×24 CSS px の最低要件を考慮。HIG aligned の推奨を満たさない観察。
- Page/state: Current IA wizard の並べ替え/除外操作、Current IA routing editor の行メニュー
- User/task: 低視力・運動障害のある利用者がタッチまたは低精度ポインタで行操作を行う。
- Evidence: `src/setup/ui.go:448` 付近の `.wizard-move` は `min-width:28px; min-height:28px`、`.wizard-exclude` は `width:28px;height:28px`。`src/setup/ui.go:150` 付近の `.routing-row-menu summary` は `width:32px;height:32px`。一方、共通の主要ボタンは `src/setup/ui.go:351` で `min-height:44px`。
- Inference: WCAG 2.5.8 の 24 px 最低値には抵触しない可能性があるが、同一 UI 内の主要操作より小さく、タッチ誤操作の余地が増える。HIG の一般的な 44 pt タッチターゲット整合性が不足する。
- Recommendation: アイコン自体を大きくする必要はなく、周囲の padding/クリック領域を少なくとも 44 px に拡大し、隣接ターゲットとの間隔を確保する。実機/375 px 幅で確認する。

### A5-007 — 動きを減らす設定を尊重する CSS がない

- Severity: LOW / Action class: P3
- Confidence: High
- Criterion: WCAG 2.3.3 Animation from Interactions (AAA; AA 不適合とは断定しない)。HIG aligned reduced-motion behavior
- Page/state: 管理画面・ログイン・セットアップを含む共通 shell
- User/task: 前庭障害、注意・認知特性、または motion sensitivity のある利用者が画面を閲覧する。
- Evidence: `src/setup/ui.go:56` は `body::before` に 18 秒の無限 `particleDrift` を設定し、`src/setup/ui.go:286` は `.tile,.section-card,.page-card` に `riseIn` animation を設定している。`src/setup/ui.go:228,260,285,351,396` 付近には複数の transition もあるが、ファイル全体に `@media (prefers-reduced-motion: reduce)` の上書きがない。
- Inference: OS の reduced-motion 設定を有効にしても、背景の無限移動とページ要素の出現アニメーションが抑制されない可能性がある。影響は個人差が大きいため LOW とした。
- Recommendation: `prefers-reduced-motion: reduce` 時は無限背景アニメーションと非必須の出現/移動 transition を停止し、状態変化に必要な最小限の視覚変化だけ残す。ブラウザの設定を変えた JP/EN 両方で再確認する。

### A5-008 — 言語切替リンクの accessible name が英語固定かつ遷移先を示さない

- Severity: LOW / Action class: P3
- Confidence: High
- Criterion: WCAG 2.4.6 Headings and Labels, 3.3.2 Labels or Instructions; HIG clear labeling
- Page/state: `appShell` を使う全画面のヘッダー。JP/EN 切替
- User/task: スクリーンリーダー利用者が現在言語からもう一方へ切り替える。
- Evidence: `src/setup/ui.go:540-547` は visible な `JP`/`EN` を持つ `<a>` に `aria-label="Toggle language"` を設定している。ラベルは `lang == "ja"` でも英語固定で、`Switch to English` / `日本語に切り替え` のような遷移先の具体名、`aria-current`、現在言語の明示はない。ログインの input labels や `<html lang>` は別途実装されている（`src/setup/ui.go:729`、`src/setup/middleware.go:548-550`）。
- Inference: 日本語利用者にも英語の抽象的な名称だけが読み上げられ、どちらへ切り替わるかを予測しにくい。visible JP/EN があるため重大な操作不能ではなく LOW とした。
- Recommendation: 現在状態に応じて accessible name を「英語に切り替え」/「日本語に切り替え」にローカライズし、必要なら現在選択中の言語を `aria-current="true"` 等で示す。リンクとして遷移することも name/description で明確にする。

## Positive evidence

- 共通 shell は `src/setup/ui.go:729` で `<html lang="ja|en">`、`src/setup/ui.go:738` で skip link、`src/setup/ui.go:753-754` で main landmark と polite live region を出力する。
- ログインは `src/setup/middleware.go:548-550` で email/password/hostname の `label` と `for`/`id`、エラーの `role="alert" aria-live="assertive"` を持つ。既存テスト `src/setup/accessibility_markup_test.go:8-27` もこれを確認している。
- Current IA の Production tabs は `src/setup/ia_views.go:562-575` で `role="tablist"`、`role="tab"`、`aria-selected`、`aria-controls`、`role="tabpanel"` を出力し、左右/Home/End のキー操作を実装している。
- 共通 CSS は `src/setup/ui.go:346-347` で主要な interactive element に `:focus-visible` の 3 px outline、主要ボタン/入力には概ね 44 px 高さを設定している。既存テスト `src/setup/responsive_accessibility_test.go:8-21` も一部を確認している。
- Current IA wizard の channel input は `src/setup/ia_views.go:2707` で per-field `label for`/`id`、除外ボタンには `aria-label` を付けている。

## 未確認範囲 / 実行証拠

- ブラウザの実 DOM を使った全 route のキーボード操作、実際のフォーカス可視性、スクリーンリーダー（VoiceOver/NVDA）、axe 等の自動検査は未実施。
- 認証後の実データを用いた `/bot/admin/*` の rendered screenshot、375 px/640 px/960 px の全画面比較、ズーム 200%、High Contrast/forced-colors、OS の reduced-motion 切替は未確認。
- Current IA の destructive dialog はコード静的確認のみで、`showModal()` の実ブラウザ挙動、Safari/Firefox 差、Escape/背景クリック時の focus return は未確認。
- ローカル 8090 の `http://127.0.0.1:8090/bot/login?lang=en` に匿名 GET を行い HTTP 200 と `<html lang="en">`、ログイン画面の冒頭を確認した。認証、外部 Kitsu/Discord 操作、外部サーバー変更は行っていない。稼働コンテナ名は `kitsusync-8090-current` だったが、対象ワークツリーのビルドと同一であることは証明していないため、コード監査の証拠とは分離した。
- `go test ./src/setup -count=1 -timeout=120s` は実行したが、現環境が `CGO_ENABLED=0` のため `go-sqlite3 requires cgo to work` で多数の SQLite 依存テストが失敗した。アクセシビリティの PASS 根拠には使用していない。
- 既存 worktree には監査開始前から変更があった（`CHANGELOG.md`、`README.md`、`RELEASE_NOTES_v0.4.4.md`、`docs/QUICK_START.md`、`docs/SETUP_FOR_STUDIOS.md`、`src/setup/ia_views.go`、`src/setup/ia_views_test.go`、`src/setup/ui.go`、`.github/dependabot.yml`）。これらは変更していない。

## 参照ソース

- 対象コード: [src/setup/ui.go](../../src/setup/ui.go), [src/setup/admin.go](../../src/setup/admin.go), [src/setup/ia_views.go](../../src/setup/ia_views.go), [src/setup/current_routing.go](../../src/setup/current_routing.go), [src/setup/middleware.go](../../src/setup/middleware.go), [src/main.go](../../src/main.go)
- 対象リリース: [RELEASE_NOTES_v0.4.4.md](../../RELEASE_NOTES_v0.4.4.md)
- 対象ルート/IA: [docs/CURRENT-IA-UI-SPEC.md](../CURRENT-IA-UI-SPEC.md)
- 既存アクセシビリティテスト: [src/setup/accessibility_markup_test.go](../../src/setup/accessibility_markup_test.go), [src/setup/responsive_accessibility_test.go](../../src/setup/responsive_accessibility_test.go), [src/setup/ui_header_test.go](../../src/setup/ui_header_test.go)
- WCAG 2.2: [1.1.1 Non-text Content](https://www.w3.org/TR/WCAG22/#non-text-content), [1.3.1 Info and Relationships](https://www.w3.org/TR/WCAG22/#info-and-relationships), [2.3.3 Animation from Interactions](https://www.w3.org/TR/WCAG22/#animation-from-interactions), [2.4.3 Focus Order](https://www.w3.org/TR/WCAG22/#focus-order), [2.4.6 Headings and Labels](https://www.w3.org/TR/WCAG22/#headings-and-labels), [2.4.7 Focus Visible](https://www.w3.org/TR/WCAG22/#focus-visible), [2.5.8 Target Size (Minimum)](https://www.w3.org/TR/WCAG22/#target-size-minimum), [3.3.2 Labels or Instructions](https://www.w3.org/TR/WCAG22/#labels-or-instructions), [4.1.2 Name, Role, Value](https://www.w3.org/TR/WCAG22/#name-role-value)
- HIG accessibility reference: [Apple Human Interface Guidelines — Accessibility](https://developer.apple.com/design/human-interface-guidelines/accessibility)

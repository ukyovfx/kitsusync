# Agent 6 — Typography / Editorial Identity Audit

対象: `C:\Users\mynti\Documents\KitsuSync-clean`

監査範囲: 管理画面と公式ドキュメントのタイポグラフィ、見出し階層、serif/mincho の扱い、JP/EN の文字組み parity、フォント読み込みとフォールバック性能。実装変更・CSS変更・route/behavior 変更は行っていない。

判定方法: 「Evidence」はリポジトリから直接確認できる事実、「Inference」はその事実から想定される表示・運用上の影響である。実ブラウザでの表示結果を取得していない項目は、未テストとして明記する。

## 結論

現状は「日本語 UI に英語向けの幾何学的 sans を重ねる」方向で統一され、serif/mincho の無制限な混在はない。一方、日本語用フォントが実際には配信されず、JP/ENで同じ CSS の英語的な字間・uppercase 指定が適用されるため、環境依存の字形差と日本語の不自然な文字組みが残る。最優先はフォント供給経路と日本語フォールバックの確定、次に JP/EN で異なる見出し・ラベルの字間ルールを持つことである。

## Findings

| # | Severity | Confidence | Route / screen | Finding | Evidence | Inference / impact |
|---|---|---|---|---|---|---|
| T-01 | Major | High | 全管理画面 (`/bot/admin*`, `/bot/setup`) | Web Font の可用性と初期表示が外部 Google Fonts に依存している。 | `src/setup/ui.go:9` の `@import url('https://fonts.googleapis.com/...')` が Outfit と Space Grotesk を読み込む。`body` は `"Outfit","Noto Sans JP",sans-serif` (`src/setup/ui.go:37`) だが、Noto Sans JP 自体を読み込む指定はない。 | ネットワーク遮断、CSP、Google Fonts の遅延・失敗時に、英語と数字の幅・改行・見出し幅が変わる。日本語は利用端末の generic sans に落ちるため、FOUT/FOIT と環境差を制御できない。これは performance と editorial identity の両方に影響する。 |
| T-02 | Major | Medium | `/bot/admin` Dashboard、`/bot/admin/projects` Production details、`/bot/setup` wizard、`/bot/admin/health` System Status | 日本語の本文と見出しに対する明示的な日本語書体設計がない。serif/mincho は使っていないが、sans の代替が実行環境任せである。 | `body` の font stack は Outfit → Noto Sans JP → generic sans。管理 UI の主要見出しは `.page-heading h1` 26px、Dashboard は `.dashboard-intro h1` 28px、接続画面の一部は 24px (`src/setup/ui.go:281`, `src/setup/ui.go:317`, `src/setup/ui.go:332`, `src/setup/ui.go:379`)。 | mincho を避ける方針自体は視認性・運用 UI に整合するが、JP glyph の実体が一定しないため、同じ画面でも OS/ブラウザごとに字面、太さ、ベースライン、折返し位置が変わり得る。JP/EN を同一ブランドとして見せる根拠が弱い。 |
| T-03 | Major | High | 全管理画面の form label、table header、status/metric labels | 英語向けの uppercase と広い letter-spacing が日本語にも一律適用される。 | `label` は `text-transform:uppercase; letter-spacing:.16em; font-family:"Space Grotesk","Outfit",sans-serif`、`th` も同じ `.16em` と Space Grotesk、`.metric-label` は `.14em` (`src/setup/ui.go:342`, `src/setup/ui.go:375`, `src/setup/ui.go:382`)。 | 日本語には uppercase の利点がなく、かな・漢字間に英語基準の空きが入る。短い日本語ラベルの視認的なまとまりが崩れ、英語よりラベルが間延びする可能性が高い。特に `/bot/admin/bot` の接続フォーム、`/bot/admin/health` の指標、wizard の表で階層のリズムが JP/EN 非対称になる。 |
| T-04 | Moderate | High | `/bot/admin/projects` routing editor / summary、`/bot/admin/health` Workflow Diagnosis | JP表示でも、意味階層に属する見出し・表列名が英語のまま出力される箇所がある。 | `src/setup/current_routing.go:278` と `:295` は `Kitsu Task Type` を raw text で出力する。`src/setup/workflow_diagnosis.go:453`, `:469`, `:490` には `Entity scope`, `Current routing`, `Stable ID`, `Classification`, `cg template reference`, `Short name`, `Semantic flags`, `Would notify` などの raw English がある。周辺の説明文は `t`/`tr` で JP/EN 切替される。 | プロダクト用語として英語を残す判断はあり得るが、翻訳済みラベルと混在するため、JP画面の editorial hierarchy が不均一になる。英語を意図的な technical term とするなら、全 route で同じ語彙・書体・表記規則を明示的に揃える必要がある。 |
| T-05 | Moderate | Medium | `/bot/admin` Dashboard、各 admin detail screen | 見出しサイズはページ／コンポーネント単位で上書きされ、共有の段階的な type scale がない。 | `.page-heading h1` は 26px、Dashboard h1 は 28px、System Status は `main:has(.system-status-sections) .page-heading h1{font-size:32px}`、section h3 は 16px、connections h2 は 18px/16px と複数の局所 override がある (`src/setup/ui.go:281`, `:379`, `:509-513`)。 | 画面の情報量に応じた調整は見えるが、JPは英語より横幅を使うため、同じ階層でも折返しと高さが変わる。Dashboard、Connections、Health のページタイトルが同じ「管理画面の主見出し」として連続閲覧した際に、編集履歴由来の差に見えやすい。 |
| T-06 | Minor | High | `/bot/docs/` official documentation | 公式ドキュメントは管理画面と別 stylesheet／別の typography identity を持つ。 | `docs.html:11` は system sans を指定し、後段の `docs.html:79` では `"Outfit","Noto Sans JP",sans-serif`、`docs.html:83` 以降では brand/nav/headings に Space Grotesk を指定する。本文、nav、hero のサイズと hierarchy は admin `ui.go` と別定義。 | ドキュメント単体では成立しているが、管理画面から `/bot/docs/` に移ると、見出しの幅・字間・本文密度が変わる。サービス全体の editorial identity を一つにする場合は、フォント供給と JP 規則を共有できる設計が必要になる。 |

## Positive evidence

- 管理 UI の `body`、主要見出し、button、label、table header で font family の候補が明示されており、無秩序な serif/mincho の混在は確認できない (`src/setup/ui.go:37`, `:169`, `:276`, `:342`, `:351`, `:375`)。
- `appShell` は `<html lang="%s">` を出力し、`currentLang` は `ja` と `en` を明示的に切り替える (`src/setup/ui.go:729`, `src/setup/i18n.go:8-17`)。JP/EN parity を作る基盤はある。
- 見出しの主階層は実際の semantic heading (`h1` / `h2` / `h3`) と CSS class の両方で実装されている。少なくとも typography の起点を追跡できる (`src/setup/admin.go:4070-4084`, `src/setup/ui.go:280-282`)。

## Recommended direction (audit-level, no implementation performed)

1. フォント供給経路を一つに決める。外部 Web Font を使うなら失敗時の許容範囲を測定し、自己ホストするなら JP glyph を含む配布物・subset・font-display を決める。`Noto Sans JP` を stack に書くだけでは供給にならない。
2. JP/EN 共通の semantic type scale を定義し、h1/h2/h3、body、supporting text、label、table header、status の役割ごとにサイズ・weight・line-height を固定する。画面固有 override は差分理由があるものだけにする。
3. 日本語では `text-transform: uppercase` と広い letter-spacing を解除または縮小する。英語の label/header だけに uppercase/Space Grotesk を適用し、JPの技術用語は意図的に英語のまま残すか、画面ごとに翻訳するかを統一する。
4. `/bot/docs/` と admin shell の font stack、本文サイズ、heading scale を同じ基準から生成できる状態にする。serif/mincho を導入する必要はないが、導入する場合は本文全体ではなく引用・説明的な editorial surface に限定する。

## Untested scope

- 実ブラウザでの font request、FOIT/FOUT、CSP、ネットワーク失敗時の挙動は未確認。
- Windows/macOS/Linux、主要ブラウザ、OSごとの日本語 glyph fallback、太さ・ベースライン・字幅の差は未測定。
- 実データを入れた `/bot/admin/projects`、`/bot/admin/health`、wizard の長い日本語・英語名称で、改行・表の列幅・ボタン内の clipping は未確認。
- 視覚的な screenshot 比較、実際の font file の glyph coverage、読み込み byte 数／LCP は未測定。
- 通知本文を表示する Discord 側の JP/EN typography は本監査の対象外。管理画面の通知 preview markup は source 上の書体指定のみ確認し、レンダリング結果は未確認。

## Evidence / inference summary

この監査の主要な事実根拠は `src/setup/ui.go`、`src/setup/i18n.go`、`src/setup/admin.go`、`src/setup/current_routing.go`、`src/setup/workflow_diagnosis.go`、`docs.html` の source inspection である。フォントの実際の見え方、性能、端末差についての記述は、上記 source evidence に基づく inference であり、ブラウザ実測ではない。

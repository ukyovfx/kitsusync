# Agent 12 — Adversarial UI/UX design audit

対象: `C:\Users\mynti\Documents\KitsuSync-clean`
監査範囲: 実装から確認できる管理 UI の構造・視覚言語。対象画面は `/bot/admin` Dashboard、`/bot/admin/bot` Connections、`/bot/admin/projects` Production list/detail、`/bot/admin/users` User Linking、`/bot/admin/health` System Status、`/bot/admin/audit` Audit Log、`/bot/setup` New Production Connection。
独立性: 他エージェントの監査文書・結論は読まず、Go の UI レンダリング／CSS／ルート登録だけを根拠にした。UI/CSS/route/behavior は変更していない。

## 結論

残すべき判断は「暗色を基調に、オレンジを限定的な主アクション／ブランドアクセントとして使う」「カードを全面廃止せず、実際の境界が必要な単位だけに使う」「常設の大きなサイドバーや Connection Map、セリフ体、背景の装飾ドットを新たな標準にしない」である。

最も強い反証は、現在の UI がすでに `section-card`、`metric-card`、`tile`、`dashboard-menu-card` を重ね、背景ドットと常時アニメーションまで持つことだ。見た目の方向性を足すより、情報の境界・状態・操作の優先順位を視覚効果から分離する必要がある。

## 評価方法と確度

「証拠」はリポジトリ中の実装上の観測、「推論」はそこから利用時のリスクを推定したものとして分けた。Severity は修正優先度、Confidence はこのコード根拠に対する確信であり、ユーザーテストの確信ではない。

| 論点 | Severity | Confidence | 判定 |
|---|---:|---:|---|
| Sidebar | Medium | High | 大きな常設サイドバーは不採用。現行の上部ナビは暫定的に残せるが、画面幅別検証が必要 |
| Serif | Low | High | 不採用。セリフ体を導入する根拠がない |
| Dot | Medium | High | 背景ドットは不採用。意味を持つ状態インジケータだけ残す |
| Connection Map | Medium | High | 独立画面として不採用。安定したタスクがない |
| Orange / dark | Medium | High | 方向性は残すが、オレンジを状態・装飾・操作に横断使用しない |
| Motion | High | High | 背景ドリフトと一律 entrance は標準から外す。短い操作フィードバックだけ残す |
| Anti-card | Medium | High | カード廃止ではなく、入れ子と同格化を削る |

## Adversarial findings

### 1. Sidebar は解決策ではない

Evidence: `src/setup/ia_views.go:23-34` は5項目の `nav-chip` を生成し、`src/setup/ui.go:714-759` はそれをトップバーの `primary-nav` に置く。`src/setup/ui.go:481-483` では 960px 以下で横スクロール、760px 以下で `<details>` の Mobile Menu に切り替える。独立した sidebar の DOM／route は確認できない。

Inference: 5つ程度の管理領域に常時幅を消費するサイドバーを足すと、Production detail の表・診断グラフ・編集フォームに幅を奪う。逆に現行の横並びチップは中間幅で「見えていない項目がある」ことを伝えにくく、モバイルではメニューを開く追加操作が生じる。したがって「sidebar を採用」が問題解決という判断は落第する。

Surviving decision: 上部ナビのコンパクトさは残せる。ただし selected state、横スクロールの発見性、戻り先の文脈は実ブラウザで確認する。常設 sidebar は追加しない。

### 2. Serif は差別化ではなく、現行の役割分担を壊す

Evidence: `src/setup/ui.go:9` は `Outfit` と `Space Grotesk` を読み込み、`src/setup/ui.go:37` は本文を Outfit、`src/setup/ui.go:169` と `276` は補助的な見出し／言語切替を Space Grotesk にしている。セリフ体の指定はない。

Inference: 接続状態、ID、Task Type、時刻、診断値を多く扱う管理画面で、見出しだけセリフにすると「編集可能なプロダクト UI」と「読み物」の信号が分裂する。日本語フォールバックも別の見え方になり、ブランド差別化より密度・整列の不安定さを増やす可能性が高い。

Surviving decision: セリフ導入はしない。差別化は書体追加ではなく、既存2書体のサイズ、太さ、余白、状態表現で検証する。

### 3. Dot は「状態」と「雰囲気」を混同する

Evidence: `src/setup/ui.go:45-70` は全画面固定の粒子背景を `particleDrift` で移動させる。実状態を示す小さな点も `src/setup/ui.go:485` の `.system-live-indicator i` にあるが、同じく丸い形状である。

Inference: 背景ドットは操作対象でも状態でもないため、情報密度の高い Health／Audit 画面では視線ノイズになる。ライブ状態の点はラベルと併置されているため意味を持つが、色だけで判断されると失敗状態・停止状態との区別が弱い。

Surviving decision: 背景ドットは残さない。状態インジケータは「ラベル＋色＋必要ならアイコン」の実データに限定する。点をブランド記号として全画面へ拡張しない。

### 4. Connection Map は「見せたい関係」だけでは画面にならない

Evidence: `src/main.go:893-940` に登録される管理 route は admin、users、bot、projects、production-routing、workflow-diagnosis、audit、health、provenance、diagnostics であり、Connection Map route はない。`src/setup/ia_views.go:455-473` の Dashboard menu も5つの管理領域へのリンクで、map view の実装は確認できない。

Inference: Kitsu、Production、Task Type、Discord Channel、User Linking の関係はデータとして存在するが、ノード図がなければ理解できないという証拠はない。図は route・凡例・状態・編集単位・モバイル代替を新たに要求し、現在の「どの作業をどこで行うか」という入口を増やす。監視・修復の具体的タスクがない map は、意味のある可視化ではなく装飾になる。

Surviving decision: 独立 Connection Map は作らない。関係が必要な場面では、Production detail の routing table、Connections のサービス別状態、Health の診断へ、作業単位の局所表示として置く。将来追加する場合も、実際の user task と失敗時の次操作を先に示せることを受入条件にする。

### 5. Orange / dark は残るが、オレンジを意味にしない

Evidence: `src/setup/ui.go:11-26` は黒系の背景・半透明 panel・オレンジ2色を定義する。`src/setup/ui.go:39-66`、`235-259`、`285-293`、`453-459` では背景グロー、active、tile、workflow の境界や影にもオレンジが使われる。一方、成功・警告・危険には `--success`、黄色系、`--danger` が別に定義されている（`src/setup/ui.go:24-25`、`484-485`）。

Inference: 暗色は映像／運用コンソールの集中感と相性がよいが、panel が半透明で背景グローも強いと、境界と階層が発光量で決まる。オレンジを active、CTA、境界、背景、workflow emphasis に横断すると、重要度とブランド色が競合する。暗色の低輝度テキストも、コード上の数値だけでは実レンダリング時のコントラストを保証できない。

Surviving decision: dark + restrained orange は残す。オレンジは主アクションまたは選択状態のどちらかに絞り、成功・警告・危険は意味専用色で表す。surface はまず不透明度・境界・余白で分け、グローを階層の代用品にしない。

### 6. Motion は存在感より制御可能性を優先する

Evidence: `src/setup/ui.go:56-74` は背景の18秒無限ドリフトと `riseIn` を定義し、`src/setup/ui.go:285-287` では tile に hover transform と全 tile／section-card／page-card の entrance animation を適用する。`prefers-reduced-motion` のメディアクエリは確認できない。

Inference: これは視覚的には「生きている」ように見えるが、Dashboard の再訪、Health の自動更新、画面読み込みで同じ動きが繰り返され、運用中の注意を奪う。利用者が減速を要求してもコード上の代替がないため、アクセシビリティ上のリスクは High とする。

Surviving decision: 100–200ms程度の hover/focus の状態遷移は affordance として残せる。背景の無限ドリフトと全カードの entrance は既定から外し、少なくとも `prefers-reduced-motion: reduce` で無効化する設計を受入条件にする。自動更新そのものを motion と誤認しないよう、データ更新は明確な live status で伝える。

### 7. Anti-card は「カード禁止」ではない

Evidence: `src/setup/ui.go:197-203` の `.glass`、`279-293` の `.page-card`／`.tile`／`.section-card`、`296-329` の Connections 内カード、`src/setup/ia_views.go:318-323` の Dashboard sections、`src/setup/admin.go` の多数の `section-card glass` が同一画面・入れ子構造に使われる。

Inference: すべてをカードにすることは、同格でない情報を同格に見せ、境界の数を増やす。特に Connections と Production detail は「概要」「編集」「診断」「危険操作」が同じ角丸・半透明語彙に乗るため、利用者は見た目でなく見出しを読み続ける必要がある。しかしカードを全面廃止すると、サービス間、操作区域、危険区域の境界まで失う。

Surviving decision: カードは実際の責務境界がある場合だけ残す。ページ全体は一枚の強い surface、内部は見出し・罫線・余白で区切り、入れ子カードは例外にする。Dashboard の5入口はカードのままでもよいが、数値・説明・状態チップを詰め込みすぎない。カード数を減らすこと自体を KPI にしない。

## 対象別の未検証範囲

- 実ブラウザでのスクリーンショット、実データ、JP/EN の長文、未接続／エラー／空状態は未確認。
- 320px、375px、768px、960px、1440px の実 rendered width、横スクロールナビの発見性、表の横 overflow は未測定。
- WCAG のコントラスト比、色覚差、OS の高コントラスト設定、`prefers-reduced-motion` の実挙動は未検証。
- キーボードだけで nav、details、drag 操作、dialog、Health の自動更新を操作する確認は未実施。
- 体感速度、初回表示時の motion、連続更新時の視線負荷はユーザーテストしていない。
- Connection Map の必要性は、運用者が実際にどの障害を関係図で解くのかというタスク証拠がないため判定不能であり、不要と断定するユーザーデータもない。

## 受入に残す判断

1. 上部ナビを維持し、sidebar／Connection Map／serif は追加しない。
2. dark + orange は維持するが、orange はブランド／主アクションまたは選択の限定用途にする。
3. 意味のある状態点以外の背景ドットを除外する。
4. 無限背景ドリフトと一律 entrance を標準にせず、reduced-motion を明示的に扱う。
5. カードは責務境界に限定し、入れ子・同格化・装飾のための glass を減らす。

## 参照したソース

- `src/setup/ui.go:8-74, 197-330, 453-518` — テーマ、書体、ドット、motion、nav、カード、responsive 規則。
- `src/setup/ui.go:714-759` — 共通 shell、topbar、primary navigation、mobile menu。
- `src/setup/ia_views.go:23-34, 318-323, 455-473` — ナビゲーション、Dashboard、管理入口。
- `src/main.go:819-940` — HTTP route 登録と管理画面の route 範囲。

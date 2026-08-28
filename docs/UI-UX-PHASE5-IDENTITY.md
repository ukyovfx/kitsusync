# KitsuSync UI/UX Phase 5 — Brand / Identity Polish

Date: 2026-08-28 JST
Workspace: `C:\Users\mynti\Documents\KitsuSync-clean`

## Verdict

- `TYPOGRAPHY_IDENTITY = KEEP_SANS`
- `SHIPPorI_MINCHO = KEEP_SANS`
- `LOADING_IDENTITY = KEEP_CURRENT`
- `DOT = KEEP`
- `MICROCOPY_CLEANUP = PASS`
- `UI_RELEASE_READY = YES`

The production UI remains the existing Sans-only implementation. No route, behavior, notification semantics, polling, palette, navigation structure, external Kitsu/Discord write, Connected Production state, Connection Map, merge, or release action was changed.

## Typography A/B

The candidates considered were:

| Candidate | Fit | Decision |
|---|---|---|
| Noto Serif JP | Strong JP coverage and OFL 1.1; used for the browser B prototype | Rejected for the current tool identity |
| Source Serif 4 | High-quality open-source serif, OFL; good Latin pairing but requires a separate JP strategy | Rejected because JP/EN pairing would be less coherent |
| IBM Plex Serif | OFL family with technical credibility; Japanese support is clearer in Plex Sans than Plex Serif | Rejected because the serif accent did not improve the KitsuSync hierarchy |

The B prototype applied Noto Serif JP only to the KitsuSync mark, Login heading, page headings, and safely available Production headings. Navigation, body, buttons, forms, tables, statuses, logs, and diagnostics stayed Sans. At both 1440×900 and 375×812, the serif treatment added editorial weight without a clear identity or readability gain; it also weakened the technical-console rhythm. The temporary CSS and font import were removed after capture.

Sources: [Noto fonts / OFL reference](https://github.com/notofonts/noto-fonts/blob/main/NEWS.md), [Source Serif readme and OFL terms](https://github.com/adobe-fonts/source-serif/wiki/Source-Serif-Readme), [IBM Plex repository and OFL license](https://github.com/IBM/plex).

## Loading identity

`KEEP_CURRENT`. The current reachable screens load without a meaningful wait that needs a branded interstitial. No splash, artificial delay, WebGL, or new loading motion was added. The existing reduced-motion contract remains authoritative, and operational state continues to explain actual status rather than decorative progress.

## Dot identity

`KEEP`. Login/public surfaces retain the more visible restrained dot texture. Admin surfaces keep the static, low-contrast treatment. No pointer-follow, glow spectacle, or particle system was introduced.

## Fresh review lenses

| Lens | Result | Finding |
|---|---|---|
| Brand individuality | KEEP Sans | Existing dark/orange identity is distinctive enough; serif made the mark feel editorial rather than operational. |
| Readability / typography | KEEP Sans | Sans-only preserves JP/EN balance and stable mobile wrapping. |
| Anti-AI | KEEP Sans | The current restrained system avoids a generic font-pairing flourish; no extra decorative identity was needed. |
| Accessibility / responsive | KEEP | No new font-loading dependency, overflow risk, focus issue, or motion behavior was introduced. |

No bounded REFINE or REVERT remains within Phase 5 scope.

## Browser evidence

Screenshots were captured from the real Chrome session at 1440×900 and 375×812 for Login, Dashboard, Connections, and System Status in JP/EN. `sans` is the production baseline; `serif-accent` is the temporary comparison prototype and is not deployed.

Desktop 1440×900:

- [Login Sans EN](audits/runtime-evidence/phase5_login_sans_en_1440x900.png) / [Serif A/B](audits/runtime-evidence/phase5_login_serif-accent_en_1440x900.png)
- [Dashboard Sans EN](audits/runtime-evidence/phase5_dashboard_sans_en_1440x900.png) / [Serif A/B](audits/runtime-evidence/phase5_dashboard_serif-accent_en_1440x900.png)
- [Connections Sans EN](audits/runtime-evidence/phase5_connections_sans_en_1440x900.png) / [Serif A/B](audits/runtime-evidence/phase5_connections_serif-accent_en_1440x900.png)
- [System Status Sans EN](audits/runtime-evidence/phase5_system-status_sans_en_1440x900.png) / [Serif A/B](audits/runtime-evidence/phase5_system-status_serif-accent_en_1440x900.png)

Japanese desktop and both mobile language sets use the same filename pattern: `phase5_<screen>_<sans|serif-accent>_<en|ja>_<1440x900|375x812>.png`. The post-cleanup final Sans captures use `phase5_final_<screen>_<en|ja>_<1440x900|375x812>.png`.

Login evidence was captured after clearing the visible email and password fields; credential contents are not retained in the Phase 5 evidence.

## Accessibility / JP-EN

- Final sampled pages retain one `main` and one `h1`.
- Existing visible `aria-current`, labels, dialog naming, focus styling, 44px primary targets, text status labels, and reduced-motion source rules were preserved.
- Final browser console error check returned no application errors.
- JP/EN heading, navigation, form, status, and diagnostic copy remained coherent; no new English-only leakage, mojibake, or font-dependent wrapping issue was introduced.
- Mobile smoke at 375×812 retained the single Menu disclosure and no observed horizontal document overflow.

## Validation

- `gofmt -d src/setup/ia_views.go src/setup/ui.go src/setup/middleware.go`: clean.
- `git diff --check`: passed; existing line-ending warnings only.
- CGO-enabled affected tests passed in Go 1.21 Bookworm: `go test ./src/setup -run "TestDashboard|TestHeader|TestConfirmation|TestResponsive|TestAccessibility|TestIA" -count=1 -timeout=240s`.
- `CGO_ENABLED=1 go vet ./src/setup`: passed.
- `docker compose config --quiet`: passed; existing unset `FB_USERNAME` / `FB_PASSWORD` warnings remain.
- Final Sans image was rebuilt and the existing runtime container was recreated once after the prototype was removed.
- `GET /health`: HTTP 200.
- Browser smoke reached Dashboard, Productions, Connections, User Linking, System Status, Audit Log, Setup, and Login.
- Phase 5 evidence secret-name sanity scan: zero matches; no credential contents retained.

## Remaining deferred scope

Connected Production detail remains deferred because no connected Production runtime evidence exists. Connection Map remains prohibited/deferred. Sidebar, palette redesign, hierarchy restructuring, external Kitsu/Discord writes, route changes, notification/polling redesign, merge, and release publishing remain out of scope.

Acceptance marker: `KITSUSYNC_UIUX_PHASE5_IDENTITY_READY`

## Shippori Mincho追加検証

Phase 5決定後の追加検証として、ユーザー指定のShippori MinchoだけをSansとA/B比較した。

- `SHIPPorI_MINCHO = KEEP_SANS`
- 適用範囲はブランド名、Login見出し、ページh1、利用可能なProduction見出しだけ
- Navigation、body、button、form、table、status、logs、diagnostics、helper textはSansのまま
- JP/EN、1440×900／375×812、Login／Dashboard／Connections／System Statusを実Chromeで比較

Shippori MinchoはJPの個性とLoginの静かな編集性は増したが、英語見出しとのリズム差が大きく、運用コンソールとしての技術的信頼感とモバイルの見出し密度を明確には改善しなかった。装飾的・AI生成的な印象を増やすほどではないが、採用を正当化する優位性もないため、試作CSSとフォントimportは撤去した。

Shippori証跡：`audits/runtime-evidence/phase5_shippori_<screen>_<en|ja>_<1440x900|375x812>.png`。Login証跡は保存済み入力を読み取らず、空欄化して保存した。

参考：[Shippori Mincho公式リポジトリ](https://github.com/fontdasu/ShipporiMincho)、[Shippori Minchoの説明とOFL 1.1](https://github.com/fontdasu/ShipporiMincho/blob/master/DESCRIPTION.en_us.html)。

## Microcopy / layout cleanup

限定的な可視文言整理を実施した。

| 変更 | 分類 | 意図 |
|---|---|---|
| Dashboard CTAの「新しいプロダクションを接続します。」相当の補助文を削除 | `REDUNDANT` | 見出しと開始ボタンが既に意図を示していたため |
| Dashboard Production一覧カードの英語説明から注意件数の重複説明を削除 | `TOO_VERBOSE` / `LAYOUT_ALIGNMENT` | 注意件数は状態チップが示すため、JP/ENのカード高さを揃えた |
| 空の監査ログ／ユーザー紐づけの短い状態文3箇所から末尾句点を削除 | `PUNCTUATION_NOISE` | コンパクトな空状態ラベルとして整えた |

接続設定、通知停止、権限、復旧、削除確認、Production／Kitsu／Discordの区別を伝える安全・結果・前提説明は変更していない。最終ブラウザ確認では全8画面でJP/ENの意味同値、Dashboardの重複文消失、モバイルMenu、1 main／1 h1を確認した。横溢れは目視証跡で発生なし。

`MICROCOPY_CLEANUP = PASS`

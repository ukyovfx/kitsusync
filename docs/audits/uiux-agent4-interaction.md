# Agent 4 — Interaction audit (v0.4.4)

対象: `C:\Users\mynti\Documents\KitsuSync-clean`
監査日: 2026-08-28 (Asia/Tokyo)
担当: Agent 4 (forms / interaction / Fitts / error prevention / feedback / keyboard)
判定: 観察のみ。破壊操作、フォーム送信、外部リソース変更、Git操作は実行していない。

## 結論

破壊・解除系の確認導線は主要な Production danger zone と routing channel delete には実装されているが、Productionユーザー/Reviewer・Checker解除と Global User Linking の解除には同じ確認がなく、同じ操作名でも安全策が不統一である。加えて、stronger delete の日本語確認語がソース上壊れており、日本語UIでは意図した確認文字を入力できない可能性が高い。キーボード代替操作は routing editor と wizard plan の一部に実装されているが、wizard polished plan の除外操作と routing menu/delete のポインタ領域は小さい。

## Finding register

### AG4-INT-001 — 解除操作の確認モーダルが画面間で不統一

- action: Production membership / Reviewer・Checker assignment / Global User Linking の解除
- affected screen / route:
  - `/bot/admin/projects?project=<id>&tab=users` — `renderCurrentProductionUserSettings` の Production user `remove_production_user`、Reviewer/Checker `remove_production_checker`
  - 同 route の scalable user details — `remove_production_user` と `remove_production_checker`
  - `/bot/admin/users` — `renderGlobalUserLinking` の `remove_global_link`
- severity: P2
- confidence: high
- evidence:
  - `src/setup/ia_views.go:785` と `:811` は、`method="post"` の form と `action="remove_production_user|remove_production_checker"` を直接送信する。`class="delete-form"`、`data-confirm`、`data-require-text` はない。
  - `src/setup/ia_views.go:917`、`:975`、`:992` にも同じく解除用の plain POST form がある。
  - `src/setup/ia_views.go:2307` の Global User Linking の `remove_global_link` も plain POST form である。
  - `src/setup/ui.go:600-623` の確認モーダルは `form.delete-form` にだけ submit interception を付けるため、上記 form はモーダル対象外になる。
  - `src/setup/admin.go:2355-2365` は `remove_global_link` を受けると、DB上のDiscord identityとguild IDを空にして `/bot/admin/users?msg=saved` へ redirect する。
  - 対照的に `src/setup/ia_views.go:2188` の別の Global User Linking 表示では同じ `remove_global_link` に `delete-form`、確認文、`data-require-text="REMOVE"` が付いている。
- inference: 同一の解除系 action が、ある画面では確認文字必須、別の画面ではクリック/Enterだけで即時POSTされる。誤操作時にProduction membership、Reviewer/Checker設定、またはGlobal identity linkがその場で解除され、ユーザーは再設定を要する。外部Discordユーザー自体は変更しないが、状態変更の可逆性と意図確認が画面ごとに揃っていない。
- required safeguard: すべての `remove_production_user`、`remove_production_checker`、`remove_global_link` を同じ確認コンポーネントに通し、対象名と「Discord側は変更しない」等の作用範囲を表示する。確認語は action ごとに固定し、確認成功後だけPOSTする。

### AG4-INT-002 — stronger delete の日本語確認語が壊れている

- action: 連携情報・Discord channel・Discord categoryをまとめて削除する stronger delete の最終確認
- affected screen / route: `/bot/admin/projects?project=<id>` の connected Production delete review。`renderUnifiedConnectedProductionChannelDeleteAction`。
- severity: P2
- confidence: high
- evidence:
  - `src/setup/admin.go:1622-1632` は、確認文を `data-confirm` に設定し、`data-require-text` を `t(lang, "蜑企勁", "delete")` としている。
  - 同じ関数の `:1626` は日本語で「確認文字の入力が必要」と説明し、`src/setup/ui.go:606-612` は `data-require-text` の値を確認モーダルの期待値として表示する。
  - stronger delete の別の旧実装コメント部分 `src/setup/admin.go:1645-1647` と通常の削除処理 `src/setup/handler.go:612-614` では日本語確認語が `削除` になっている。
- inference: 日本語でstronger deleteを選ぶと、画面の確認欄に意図した `削除` ではない文字列が期待値として出る。ユーザーが正常な日本語確認語を入力しても承認できず、確認語を知らないユーザーには意味不明な入力を要求する状態である。これは破壊操作を誤って通す問題ではなく、正当な操作をブロック/混乱させるerror-preventionとfeedbackの欠陥である。
- required safeguard: active code path の日本語確認語を `削除` に統一し、英語 `delete`、日本語 `削除` の両方を生成結果と handler の期待値で一致検証する。v0.4.4の日本語レンダリングを実機または fixture で確認する。

### AG4-INT-003 — 小さい操作ターゲットが誤操作余地を増やす

- action: wizard planからのTask Type除外、connected Production routing rowの操作メニュー
- affected screen / route:
  - `/bot/admin/projects?project=<id>&tab=notifications&edit_routing=1` — routing row menu / channel delete entry
  - `/bot/setup` の polished wizard plan — Task Type除外 `×`
- severity: P3
- confidence: high
- evidence:
  - `src/setup/ui.go:537` の `.routing-row-menu summary` は `width:32px;height:32px`。これは行操作を開く唯一の視覚的ターゲットである。
  - `src/setup/ui.go:537` の `.routing-row-menu-panel button` は `padding:8px 10px` だが、最小高さ指定がない。
  - `src/setup/ia_views.go:2707` の `.wizard-exclude` は `src/setup/ui.go:537` のCSSで `width:28px;height:28px`。
  - `src/setup/ui.go:478` の640px以下 media ruleではボタン全体を広げる指定はあるが、`.wizard-exclude` の28px指定を上書きする規則は確認できない。
  - `src/setup/ia_views.go:2707` は除外ボタンに accessible name を付けている。一方、routing menu summaryにも `aria-label` はある。
- inference: 28–32pxの小さいアイコン/三点メニューは、特に狭い画面やポインタ精度の低い利用状況で、隣接する入力・行操作を誤って開く/押す余地を増やす。accessible nameとキーボード到達性は補っているが、Fitts上の操作距離・ターゲット面積は弱い。
- required safeguard: 破壊/解除に至る入口とwizard除外を、少なくとも実画面上で押し分けやすいサイズへ統一し、モバイル幅でも隣接操作との誤タップを確認する。routing menu内の破壊項目はメニューを開いた後も対象channel名を表示する。

## 確認できた安全策・良好な実装

- `src/setup/ui.go:600-687`: `delete-form` は確認モーダルへ集約され、確認文字が一致するまでconfirm buttonをdisabledにし、Escape、Cancel、Tab trap、元のtriggerへのfocus復帰を実装している。破壊操作の実行はしなかった。
- `src/setup/ia_views.go:1314-1320`: Production danger zoneは validation-only / read-only preview では変更・削除不可と明示し、通常の連携解除とDiscord resource削除を別formに分離している。
- `src/setup/admin.go:1328-1435`、`:1511-1618`: stronger delete reviewはProduction、Project ID、削除対象数、未確認対象、削除する/しない範囲を先に表示する。
- `src/setup/current_routing.go:272-312`: routing channel deleteはdialog内でexact channel nameを要求し、初期submit buttonをdisabledにする。server側の `src/setup/current_routing.go:22-25` 以降もactionを分けている。
- `src/setup/ia_views.go:575`、`src/setup/current_routing.go:308`、`src/setup/ia_views.go:2756`: tab移動、routing rowのAlt+Arrow、wizard plan rowのAlt+Arrowというkeyboard代替操作を確認した。
- `src/setup/handler.go:1363-1370` と `src/setup/ui.go:626-641`: 通常formの二重送信抑止と保存中ラベルへの切替がある。`delete-form` は確認モーダル側で別処理される。
- `src/setup/admin.go:2943-2954`、`src/setup/ia_views.go:2949`: 保存/削除結果の一部には `role="status"` / `aria-live` があり、結果をページ内で伝える実装がある。ただし全routeの一貫性は本監査の未確認範囲に残る。

## 未確認範囲・検証結果

- `/bot/login` は既存localhost runtime `http://127.0.0.1:8090/bot/login` でGET 200を確認した。ログイン画面にはlabel、`required`、`autofocus`、`aria-live="polite"` のstatus領域があった。
- `/bot/setup`、`/bot/admin`、`/bot/admin/projects`、`/bot/admin/health` は同runtimeでGETすると認証リダイレクト (303) となり、認証済みの実画面、フォーカス遷移、実際のモーダル表示、モバイルでの押しやすさはruntimeでは確認できなかった。
- リポジトリ内に `screenshots/*.png` はなく、既存スクリーンショットによる視覚確認はできなかった。
- `go test ./src/setup -count=1 -timeout=120s` は実行したが、環境の `CGO_ENABLED=0` により `go-sqlite3 requires cgo to work` でDB依存テストが失敗した。監査レポート以外のファイルは変更していない。
- 外部Discord/Kitsuへの接続、フォーム送信、削除、解除、作成、再試行は実行していない。

## 参照ソース

- [AGENTS.md](C:\Users\mynti\Documents\KitsuSync-clean\AGENTS.md)
- [RELEASE_NOTES_v0.4.4.md](C:\Users\mynti\Documents\KitsuSync-clean\RELEASE_NOTES_v0.4.4.md)
- [src/setup/ui.go](C:\Users\mynti\Documents\KitsuSync-clean\src\setup\ui.go)
- [src/setup/ia_views.go](C:\Users\mynti\Documents\KitsuSync-clean\src\setup\ia_views.go)
- [src/setup/admin.go](C:\Users\mynti\Documents\KitsuSync-clean\src\setup\admin.go)
- [src/setup/current_routing.go](C:\Users\mynti\Documents\KitsuSync-clean\src\setup\current_routing.go)
- [src/setup/handler.go](C:\Users\mynti\Documents\KitsuSync-clean\src\setup\handler.go)
- [src/setup/current_routing_test.go](C:\Users\mynti\Documents\KitsuSync-clean\src\setup\current_routing_test.go)
- [src/setup/ia_views_test.go](C:\Users\mynti\Documents\KitsuSync-clean\src\setup\ia_views_test.go)

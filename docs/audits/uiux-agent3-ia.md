# Agent 3 — Information Architecture Audit

対象: KitsuSync v0.4.4 candidate（`C:\Users\mynti\Documents\KitsuSync-clean`）
範囲: navigation、route、階層、オブジェクト所有範囲、selected Production context、競合する入口。スタイリング、文言、アクセシビリティ、feature necessity、destructive safeguards は対象外。

## 結論

現行の主軸は `Dashboard → Connected Productions → selected Production` で、通知 routing と User Linking を Production/global に分ける方向は妥当です。主なIA課題は、(1) Production detailの同一タブ列に日常管理・診断・詳細・危険操作が同列で並ぶこと、(2) Production-scoped Storageの保存後にglobal `/bot/admin/drive`へ遷移してcontextが失われることです。

## 方法・証拠の扱い

- 現行コードとして `src/main.go`、`src/setup/ia_views.go`、`src/setup/admin.go`、`src/setup/production_routing.go`、`src/setup/root_route.go` を確認。
- `docs/CURRENT-IA-UI-SPEC.md`、`docs/notes/KitsuSync-CURRENT-STATE.md`、`README.md`、`RELEASE_NOTES_v0.4.4.md` は補助資料。設計資料の記述は現行実装の証拠とは扱わない。
- ローカル `127.0.0.1:8090` にGETのみ実施。認証、POST、外部サーバー操作は未実施。
- 秘密値を含み得る `.env*` とruntime stateの内容は開示・証拠利用していない。

## 現行 sitemap / route map

| Route | 用途 | 所有範囲 / 備考 |
|---|---|---|
| `/bot/` | 認証後の振り分け | global。未設定時 `/bot/setup`、ready時 `/bot/admin`（`src/setup/root_route.go:9-35`） |
| `/bot/login` | 管理ログイン | global。`/login` と同じhandler（`src/main.go:851-861`） |
| `/bot/admin` | runtime summary、management navigation | global。通常はCurrent IA dashboard、`legacy=1`で旧renderer（`src/setup/admin.go:19-27`） |
| `/bot/admin/projects` | Production list | Production collection。`project=<id>`でdetail（`src/setup/ia_views.go:476-499`） |
| `/bot/admin/projects?project=<id>` | Production detail | selected Production。`tab`でsection切替（`src/setup/ia_views.go:549-576`） |
| `/bot/admin/bot` | Kitsu / Discord connection | global。edit stateは `?edit=1`（`src/main.go:906-910`） |
| `/bot/admin/users` | Kitsu user ↔ Discord user linking | global。Production-local associationとは別物（`src/setup/ia_views.go:1934-1974`） |
| `/bot/admin/health` | System Status、API/runtime diagnostics | diagnostic/global（`src/setup/ia_views.go:1338-1358`） |
| `/bot/admin/audit` | operation / notification history | diagnostic/global（`src/main.go:932-933`） |
| `/bot/setup` | New Production connection wizard | Production onboarding（`src/main.go:866-868`; `src/setup/ia_views.go:2318-2346`） |
| `/bot/admin/production-routing` | 旧 routing入口 | compatibility。GETはProjectsへredirect、POST handlerは残存（`src/main.go:915-921`; `src/setup/production_routing.go:51-73`） |
| `/bot/admin/workflow-diagnosis` | 旧 diagnosis入口 | compatibility。GETはProjectsへredirect（`src/main.go:922-931`; `src/setup/production_routing.go:63-73`） |
| `/bot/admin/checkers` | 旧 checker入口 | compatibility。User Linkingへredirect（`src/setup/admin.go:2749-2753`） |
| `/bot/admin/drive` | Storage handler | global GET renderer + Production POST target（`src/setup/ia_views.go:650-654`; `src/setup/admin.go:2755-2778`） |
| `/login`, `/setup`, `/docs`, `/docs/` | root aliases | `/bot` prefixと並行登録。docsは `/docs` と `/bot/docs` が入口（`src/main.go:859-868`; `src/docs_routes.go:13-32`） |

## selected-context map

| 操作 | 正しい範囲 | 現行位置 |
|---|---|---|
| 接続状態、routing、Production users、Storage、activity、troubleshooting、details、danger | selected Production | detail tabs（`src/setup/ia_views.go:562-670`）。Storage保存だけglobal routeへ出る（IA-002）。 |
| Kitsu / Discord credentials | global | `/bot/admin/bot` |
| Kitsu user ↔ Discord identity link | global | `/bot/admin/users` |
| API/runtime health | diagnostic/global | `/bot/admin/health` |
| operation / notification history | diagnostic/global | `/bot/admin/audit` |
| connection解除・Discord resource削除 | dangerous / selected Production | detail内 `danger-zone`。安全性の妥当性は本監査対象外。 |

## Findings

### IA-001 — Production detailでprimary/secondary/dangerの階層が混在

- severity: P2
- confidence: high
- affected user/task: 管理者がselected Productionを設定・調査・詳細確認・解除するtask
- affected route: `/bot/admin/projects?project=<id>`

Evidence:

- `renderIASelectedProduction`は1つの`role="tablist"`に `overview`、`notifications`、`users`、`storage-settings`、`activity`、`troubleshooting`、`advanced`、`danger-zone` の8項目を同列で生成する（`src/setup/ia_views.go:562-568`）。
- 同じswitchで `notifications` / `users` / `storage-settings` は管理対象、`activity` / `troubleshooting` は診断・履歴、`advanced` は詳細、`danger-zone` は危険操作として処理される（`src/setup/ia_views.go:638-670`, `1314-1321`）。

Inference:

- selected Production context自体は維持されるが、日常管理と診断・メタデータ・dangerous actionの親子関係がroute/navigation上で表現されない。ユーザーは同じ重みの8入口から、設定と調査・解除の違いを判断する必要がある。
- これは視覚的な見た目ではなく、Production object内のprimary managementとsecondary/diagnostic/dangerous scopeの情報設計問題である。

Recommended action class: product-level IA restructuring。primary workflowとsecondary/operationsを明示的に分ける。

### IA-002 — Production-scoped Storageの保存後にglobal `/bot/admin/drive`へcontextが落ちる

- severity: P2
- confidence: high
- affected user/task: selected Productionの保存先リンクを編集・保存するtask
- affected route: `/bot/admin/projects?project=<id>&tab=storage-settings` → `/bot/admin/drive`

Evidence:

- selected ProductionのStorage panelは、そのProductionの`storage_url`と`kitsu_project_id`を持つformを生成する（`src/setup/ia_views.go:650-654`）。
- formのPOST先は`/bot/admin/drive`で、保存成功後も`/bot/admin/drive?msg=saved`へredirectする（`src/setup/admin.go:2755-2773`）。
- `/bot/admin/drive`のGET rendererは`model.ListProjects(db)`をloopして全projectのStorage formを描画するglobal list surfaceである（`src/setup/admin.go:2776-2778`）。
- Current IAの通常nav `iaNav`にはStorageのglobal入口はない（`src/setup/ia_views.go:23-34`）。

Inference:

- 編集開始時はselected Productionの子画面なのに、保存完了時は全Productionを対象にする別global surfaceへ移る。保存自体は可能でも、完了後のcontextと次の行動が不連続になり、Production ownershipの説明と画面階層が一致しない。
- `/bot/admin/drive`は通常top navから隠れているため、保存後にだけ現れる競合入口になる。

Recommended action class: route ownership correction。Storageをselected Production配下に保ち、保存後は同じProduction detail stateへ戻す。

### IA-003 — root aliasesとcompatibility aliasesの境界が運用上曖昧

- severity: P3
- confidence: high
- affected user/task: bookmark、運用手順、外部リンクから管理画面へ入るtask
- affected routes: `/login` ↔ `/bot/login`、`/setup` ↔ `/bot/setup`、`/docs` ↔ `/bot/docs`、旧admin routes

Evidence:

- rootと`/bot`のlogin/setupが並行登録され、docsも`/docs`と`/bot/docs`を処理する（`src/main.go:859-868`; `src/docs_routes.go:13-32`）。
- adminはroot prefixと`/bot` prefixの両方に登録される（`src/main.go:892-943`）。
- 旧routing/diagnosisは既存bookmark向けcompatibility handler、旧checkerはUser Linking redirectである（`src/setup/production_routing.go:51-73`; `src/setup/admin.go:2749-2753`）。READMEもProjects detailを通常管理面、旧routingをcompatibility-onlyと説明する（`README.md:272-274`）。

Inference:

- compatibility維持の意図は確認でき、直ちに通常workflowの重複とは断定しない。ただしcanonical routeとcompatibility routeを運用文書で分けない限り、入口の所有者が不明確になる。
- P3とした理由は、旧routeがCurrent IAへredirectし、通常操作を直接分岐させる証拠がないためである。

Recommended action class: route governance/documentation。`/bot/*`をcanonical user-facing sitemapとし、aliasはredirect-only compatibility surfaceとして扱う。

## Ranked product-level IA options

### 1. Recommended — Production workspace with primary / secondary grouping

```text
Dashboard
├─ New Production setup
├─ Connected Productions
│  └─ Selected Production
│     ├─ Overview
│     ├─ Notifications
│     ├─ Users
│     ├─ Storage
│     └─ More / Operations
│        ├─ Activity
│        ├─ Troubleshooting
│        ├─ Details
│        └─ Danger zone
├─ Connections
├─ User Linking
├─ System Status
└─ Audit Log
```

selected Productionを全Production-owned surfaceで維持し、診断・詳細・dangerous surfaceをsecondary/operations groupへ置く。CurrentのProduction-centered modelを最も保ち、ユーザーの再定位も小さい。

### 2. Production detailをsingle workspaceにし、tabをdeep-link専用にする

`/bot/admin/projects?project=<id>`をcanonical detail routeとし、Notifications、Users、Storage、Operationsを1つのProduction workspace内のsectionにする。object hierarchyは最も明瞭だが、route/renderer統合とback/refresh設計の変更量が大きい。

### 3. 現行tabsを維持し、route ownershipだけ修正する

`/bot/admin/drive`をselected Productionへ戻す、`/bot/*`をcanonicalと明記、compatibility endpointをredirect-onlyとして文書化する。最小変更だが、primary・diagnostic・dangerousの同列問題（IA-001）は残る。

## Not findings / accepted boundaries

- `iaNav`はProductions、User Linking、Connections、System Status、Audit Logを通常入口として持ち、旧routing/diagnosisを通常入口にしていない（`src/setup/ia_views.go:23-34`）。compatibility handlerもCurrent IAへredirectするため、競合normal workflowとは数えない。
- global User LinkingとProduction-local associationは意図的に分離されている。Production detailからglobal linkingへ誘導し、local association/role assignmentはProduction-scopedに残る（`src/setup/ia_views.go:1098-1125`, `1934-1974`）。

## Runtime / screenshot coverage

- local runtimeの`127.0.0.1:8090`はlisten中。GET-only probeは `/bot/` 303、`/bot/login` 200、`/bot/admin` 303、`/bot/admin/projects` 303、`/bot/admin/production-routing` 303、`/bot/admin/workflow-diagnosis` 303、`/bot/admin/checkers` 303、`/bot/admin/diagnostics` 303。redirectは追従せず、form送信もしていない。
- authenticated runtime screenはsession不在のため未確認。repositoryの`screenshots/`には`CAPTURE_GUIDE.md`と`README.md`のみで、画像証拠はなかった。
- 外部researchは未使用。findingは現行repository codeとlocal GET statusに基づく。

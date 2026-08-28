# Agent 1 — Design intent and anti-AI principles

判定: Editorial Systems Interface は `REFINE`。この名前は装飾様式ではなく、Productionを中心に、状態・原因・次の操作を静かに読ませる運用インターフェースの性格を指す。

## 1. Scope and evidence boundary

本レポートは、分散UI監査、提供済みruntime evidence、Current IA contract、KitsuSyncの実装・運用文書を読み、既存の意図を設計原則へ整理したもの。コード、route、挙動、`DESIGN.md` は変更していない。

`docs/UI-UX-DISTRIBUTED-AUDIT.md:8,18-23,70-82` は Production-first、Kitsu→Discord routing、fail-closed、preview-before-send、JP/EN同等性を差別化の核とし、静かな運用面を推奨している。`docs/UI-UX-RUNTIME-EVIDENCE.md:21-44,48-58,65-80` によれば、実行時に確認できたのは未認証 Login のJP/EN、375/768/1024/1440pxだけで、保護されたDashboard、Production、Connections、System Status、Setup、診断、dialogは未到達である。したがって、以下では「観測」と「推奨」を明確に分け、Loginの見た目を保護画面へ一般化しない。

## 2. Observed facts

### Runtime-confirmed

- Loginの8 locale×viewport状態で、横方向overflowやvisible clippingは確認されず、`main`、`form`、`h1`、localized labels/controlsが記録されている（`docs/UI-UX-RUNTIME-EVIDENCE.md:48-56`）。
- Loginでは、bodyのcomputed font stackが `Outfit, "Noto Sans JP", sans-serif`、letter-spacingが `0.13px` と記録されている（`docs/UI-UX-RUNTIME-EVIDENCE.md:56`）。JPの説明文は375pxで2行に折り返されるが、これは不具合ではない（同:53、`docs/audits/runtime-evidence/login_ja_375x812_top.png`）。
- Loginでは、暗いパネル、暖色のgradient/glow、内側パネル、固定dot fieldがJP/ENと全viewportで見える（`docs/UI-UX-RUNTIME-EVIDENCE.md:65-69`）。これはLoginの事実であり、保護画面での同じ効果は未確認（同:70-77）。
- 提供されたruntime evidenceでは、emulated `prefers-reduced-motion: reduce` 下でも `particleDrift` と代表要素の `riseIn` がcomputed style上残っていた（`docs/UI-UX-RUNTIME-EVIDENCE.md:56-57`）。ただし現行ソースには `@media (prefers-reduced-motion: reduce)` が存在し、dot、カードentry、主要transitionを抑制している（`src/setup/ui.go:75-80`、追加の共通override `src/setup/ui.go:524-528`）。これはruntime artifactとcurrent sourceの時点差またはbuild差を示すため、現行sourceだけでruntime PASSとはしない。

### Source/spec-confirmed

- Current IAは、Dashboardを runtime summary → next action → attention → New Production Connection CTA → management menu の順に定義している（`docs/CURRENT-IA-UI-SPEC.md:21-35`）。Production detailは一つのProductionを durable context として connection/routing/resource stateを扱う（同:37-42）。
- Setupは7段階の明示的な流れ（Prerequisites、Production、Discord Server、Channel Plan、Review、Execute、Complete）で、Discord write前にcreate/reuse/conflict planを表示し、Reviewとdry-runを経る（`docs/SETUP_WIZARD.md:18-32,52-54,106-114`）。
- Notification readinessはKitsu、Discord、Production/Guild、Task Type channels、routingなど独立した条件の合成であり、connected Productionだけではreadyではない（`docs/SETUP_WIZARD.md:50-54`）。ConnectionsではKitsu/Discordを独立表示し、secretは保存済みでも固定長maskだけを表示する契約である（`docs/CURRENT-IA-UI-SPEC.md:43-63`）。
- Notificationsのnormal viewはread-only previewで、Task Type、destination、Production notification language、mention behavior、deterministic rendered message/embedを示すが送信しない（`docs/CURRENT-IA-UI-SPEC.md:120-128,130-141`）。安全でない・曖昧なownershipやstale mappingはfail closedしてreviewへ回す（`docs/SETUP_WIZARD.md:52-54`、`docs/NOTIFICATION_SAFETY.md:3-7`）。
- System StatusはOverall system health → API response status → KitsuSync operational status → recent system issuesの順で、Kitsu/Discord APIをpeerとして扱い、詳細はexpandableなsafe diagnosticsに隠す契約である（`docs/CURRENT-IA-UI-SPEC.md:91-111,147-155`）。
- 現行sourceは共通shellにOutfit/Space Grotesk、near-black/orange token、radial background、fixed dot field、`.glass`、大きなradius、カードgradientを持つ（`src/setup/ui.go:8-70,162-208,285-299`）。Dashboardはpeer metricsとmanagement destinationsを生成し、System Statusは5秒ごとのread-only snapshot refreshを行う（`src/setup/ia_views.go:317-323,1338-1358`）。これは「存在」の観測であり、良い階層や実際のtask performanceの証明ではない。

## 3. Editorial Systems Interface character

### Core statement

KitsuSyncの画面は「AI dashboard」でも「decorative dark mode」でもなく、Productionの現在地を編集・確認・復旧するための小さな運用編集室である。Editorial性はserifやカードの見た目ではなく、情報を編集上の順序に並べ、primary decisionを先に、理由を次に、technical identityを最後に置くことで成立する。

### Hierarchy

1. `Object`: 選択中のProduction、または未選択時に選ぶProduction。
2. `State`: Connected/Disconnected、Healthy/Needs review/Error、routing readinessなど、人が判断できる短いsemantic state。
3. `Decision`: 今できる、または今必要な一つの操作。
4. `Reason`: なぜそのstateなのか、失敗したら何を確認するか。
5. `Evidence`: IDs、scope、match、observation、timestamps、raw-ish diagnostics。必要時だけ開く。

WHAT: 各画面でこの順序を視覚・文言・DOM順の基本にする。WHY: オペレーターは装飾を解読するためでなく、どのProductionに何が起き、何をすべきかを確認するために来る。WHEN: Dashboard、Production detail、Connections、System Status、Setup、Audit Logの通常状態。WHEN NOT: 完全な診断・監査画面ではEvidenceが主目的になり得るが、それでも人間向けの見出しとstateを先に置く。

### Density

WHAT: densityは「情報量を最大化」ではなく「判断に必要な情報を近接させる」ことで調整する。Production detail、routing、Audit Logは restrained developer-tool density、Setup Review/Executeは確認と結果が読める余白、Login/empty/completeは短い説明と単一CTAにする。WHY: 分散監査は、uniform grid、wide table、nested card、同一stateのbadge/explanation/action反復が認知負荷を増やすと指摘している（`docs/UI-UX-DISTRIBUTED-AUDIT.md:38-42`）。WHEN: operational dataが多い画面ほど表・規則線・compact metadataを使う。WHEN NOT:密度を理由に、label、原因、次の操作、keyboard path、JP/EN parityを省略しない。

### Surfaces

WHAT: surfaceは semantic boundary のためだけに使う。境界の根拠は独立task、ownership、risk、または明確なpeer関係。通常の表・フォーム・ログはflat region、rule、table sectionを基本にし、elevation/glassは一画面に意図した一層まで。WHY: 現行の`.glass`はtranslucency、18px blur、inset highlight、large shadowを組み合わせ、`adminPage`のpage shellと子sectionで反復される（`src/setup/ui.go:203-208`、`src/setup/admin.go:4080-4082`、`docs/audits/uiux-agent8-anti-ai.md`）。反復された外観が意味境界を埋める。WHEN: destructive zone、modal、または本当に別レイヤーのpreviewなど、ownership/riskが視覚的に分かれる必要がある場合。WHEN NOT: 各metric、各table、各helper copyを別カードにしない。anti-cardを新しい絶対規則にはしない。`docs/UI-UX-DISTRIBUTED-AUDIT.md:94-97,106-108` もこの限定的な立場を採る。

### State language

WHAT: stateは短いlabel/badge、具体的なvalue、supporting explanation、next actionの組み合わせで示し、色は補助に留める。独立したKitsu/Discord status、Production state、notification readiness、issue countを混同しない。WHY: Current IAはconnection stateを独立定義し、JP/ENの同じ順序・意味・densityを要求する（`docs/CURRENT-IA-UI-SPEC.md:37-39,65-80,113-118`）。WHEN: 接続、routing、health、delivery、empty/error/recoveryの全状態。WHEN NOT: full sentenceをbadgeに入れない、色だけでstateを伝えない、connected countをKitsu/Discord badgeの代用にしない。

### Interaction and consequence

WHAT: interactionは予測可能で、外部mutationの前にplan/preview/consequenceを見せる。Setupはstage→review→execute→complete、routing editorはcanonical form、System Statusのrefreshはread-only、destructive actionは対象と境界を明記する。WHY: setupの安全境界は「no write during dry-run/preview」「exact valid matchのみreuse」「ambiguous ownershipはfail closed」である（`docs/TROUBLESHOOTING.md:7`、`docs/SETUP_WIZARD.md:54,106-114`）。WHEN: Discord channel creation、routing変更、notification test、remove/delete、credential save。WHEN NOT: previewをsendボタンに見せない、graph内編集を許可しない、曖昧なresourceを推測で再利用・削除しない。

### Progressive disclosure

WHAT: normal operator viewは人間向けstateと次の行動を含む最小情報にし、Details/diagnosticsで必要な証拠を段階的に開く。Dashboardはattention/next actionをmetricsより先に、Production detailはselected Production配下に、Workflow Diagnosisのraw IDs・scope・matching・classificationはadvanced側に置く。WHY: 分散監査は「Primary decision → supporting explanation → advanced details」を推奨し、Workflow Diagnosisの技術詳細早期露出を問題にした（`docs/UI-UX-DISTRIBUTED-AUDIT.md:40-42,150-161`）。WHEN: System Status、Troubleshooting、Workflow Diagnosis、Audit Log、Connections metadata。WHEN NOT: technical detailが安全判断に不可欠なときは隠してはならない。例えばpreviewのdestinationやmention behavior、confirm対象、failure reasonは通常確認面に残す（`docs/CURRENT-IA-UI-SPEC.md:111,124-128`）。

## 4. Explicit anti-AI rules

各ルールは実装仕様ではなく、将来の設計判断を判定できる監査規則である。

| Rule | WHAT | WHY | WHEN | WHEN NOT |
|---|---|---|---|---|
| A1. Task outcome before atmosphere | primary object/state/actionを先に読み取れるようにし、ambient decorationを背景に置かない | 運用画面の目的はProduction状態・原因・操作で、雰囲気ではない（`docs/UI-UX-DISTRIBUTED-AUDIT.md:152-160`） | 全通常画面、特にDashboard/Health/Audit/Setup | Loginのbrand cueやcomplete stateに、静的で低コントラストのidentity treatmentを限定使用する場合 |
| A2. No global decorative theater | fixed particle、continuous drift、radial glow、global gradientはsemantic stateに結び付かない限り使わない | 現行sourceのdot/gradientは全shellに適用される（`src/setup/ui.go:34-70`）。runtimeではLoginで確認、保護画面は未確認（`docs/UI-UX-RUNTIME-EVIDENCE.md:65-77`） | operational page、長時間監視、精読中の表・ログ |意味のあるfocus/highlight、静的brand、機能的refresh表示。motionはstate change/focusを説明するときだけ |
| A3. One surface, one meaning | card/glass/rounded/elevationをsemantic boundaryごとに一度だけ使う | repeated glass/nested cardはcard soupとなり、status/action/dataの境界を埋める（`docs/audits/uiux-agent8-anti-ai.md`） | page shell、section、metric、table、formを設計するとき | modal、danger zone、previewなど独立risk/layerを表す必要がある場合 |
| A4. Accent is scarce | orange/glow/strong contrastはprimary actionまたは意味のあるstateに限定する | tile、section、button、CTAへの同じgradient/glow反復は優先順位を平坦化する（`src/setup/ui.go:291-295,351-358,378-380`） | action hierarchy、warning/danger、selected context | statusに文字/valueがあり、accentが補助的な場合。装飾目的だけのglowは不可 |
| A5. Real metrics only | 数値は実際の運用判断に結び付く名称・value・次の行動を持つものだけ表示する | fabricated proof barやgeneric AI dashboardは信頼を損なう。現行Dashboardのcountsはdecision-linkedなら許容される（`docs/audits/uiux-agent8-anti-ai.md`） | Dashboard、Health、Production overview | “premium”感を出すためのinvented metric、意味のないsample/score、閾値未定義のpseudo-metric |
| A6. Editorial type is a role, not a costume | editorial typographyはbrand/title/まれな説明的momentに限定し、forms/tables/logs/status/metadataはreadable sansを基本にする | serif/minchoをoperational UI全体へ適用するとJP/EN幅と可読性が不安定になる（`docs/UI-UX-DISTRIBUTED-AUDIT.md:64-66`）。現行font供給とJP glyph fallbackも未検証（`docs/audits/uiux-runtime-agentE-typography.md:89-123`） | title、context、empty/completeのidentity | Production名、long labels、table headers、status、technical IDsを装飾書体にする場合 |
| A7. JP/EN are one interface | locale間でstate、order、action、density、accessible meaningを一致させ、英語のuppercase/letter-spacingを日本語へ機械適用しない | Current IAはsemantic equivalenceを要求し、typography監査はJPへのuppercase/広いletter-spacingの非対称を指摘する（`docs/CURRENT-IA-UI-SPEC.md:113-118`、`docs/audits/uiux-agent6-typography.md`） | すべてのroute/state、特にwizard/table/form | 意図的な製品固有technical termを残す場合。ただし表記規則と範囲を明示する |
| A8. Motion must explain | motionはfocus、selection、drag position、accordion、refresh stateなど意味がある場合だけ短く使い、reduced-motionではdecorative motionを無効化する | 現行sourceにはparticleDrift/riseIn/transitionとreduced-motion overrideがあり、runtime artifactには時点差のある残存記録がある（`src/setup/ui.go:67-80,524-528`、`docs/UI-UX-RUNTIME-EVIDENCE.md:56-57`） | interaction feedback、bounded status refresh、keyboard/focus | continuous particle drift、parallax、bounce、pulse、celebratory toast、機能refreshを理由に視覚変化を誇張する場合 |
| A9. Progressive disclosure is anti-spectacle | raw IDs、scope、matching、chart metadata、diagnostic causesは判断に必要な時だけ開く。safe consequenceは隠さない | 技術詳細の早期露出がWorkflow Diagnosisの負荷を上げる（`src/setup/workflow_diagnosis.go:444-502`、`docs/UI-UX-DISTRIBUTED-AUDIT.md:40-42`） | health details、troubleshooting、audit、workflow diagnosis | confirm対象、destination、failure reason、secret masking、preview内容など、判断に必須な情報 |
| A10. Do not template the anti-AI look | glassをflatにしただけ、暗色を別色にしただけ、均一なdashboard templateへ戻すだけを「個性」としない | anti-AIを別の定型テンプレートに置換すると、Kitsu/Discordの固有性を失う（`docs/UI-UX-DISTRIBUTED-AUDIT.md:70-74,82`） | 新規画面、redesign、component追加 | Production-centered workflow、fail-closed、preview-before-send、独立service stateなど、実際の運用モデルから形が決まる場合 |

## 5. Screen intent map

| Surface | Reading order | Density/surface intent | Explicit restraint |
|---|---|---|---|
| Login | identity → sign-in task → locale | compact, calm, one task | Login decoration must not become a precedent for protected screens; runtime support is only for Login (`docs/UI-UX-RUNTIME-EVIDENCE.md:44-58`) |
| Dashboard | attention/next action → runtime summary → CTA → management destinations | peer cards only when peer value is real; action priority must remain visible | no generic metric theater; current five-card parity is a source/spec fact, but its operator priority remains a contested item (`docs/UI-UX-DISTRIBUTED-AUDIT.md:101-103`) |
| Production list/detail | Production → connection/routing state → scoped action → details | selected Production is durable context; local associations and routing stay local | do not mix global User Linking with Production-local association; do not duplicate stale resources (`docs/CURRENT-IA-UI-SPEC.md:120-141`) |
| Connections | Kitsu state → Discord state → safe metadata → per-service action | two independent peer groups; masked secrets; flat/readable metadata | never expose secret contents or turn supporting mask guidance into a health badge (`docs/CURRENT-IA-UI-SPEC.md:43-63`) |
| Setup | current step → required input/plan → consequence → execute/result | breathing room at Review/Execute; dense but scannable plan table | no write before review/confirmation; no drag-only model; no inferred reuse (`docs/SETUP_WIZARD.md:18-32,106-114`; `src/setup/ia_views.go:2689-2707`) |
| System Status | overall state → API peers → operational state → issues → details | exact current value first, bounded chart second, diagnostics expandable | no invented latency threshold, no raw response bodies, no secret/ID leakage (`docs/CURRENT-IA-UI-SPEC.md:91-111,147-155`) |
| Audit/Workflow Diagnosis | outcome/filter → human-readable event → advanced evidence | log/table density is appropriate; details are read-only | raw IDs/classification/scope cannot outrank the operator conclusion (`src/setup/workflow_diagnosis.go:444-502`) |

## 6. Acceptance and evidence gaps

The following are design acceptance requirements, not claims that the current UI already passes them:

- Authenticated JP/EN at 375/768/1024/1440px, including empty, connected, error, stale, and recovery states, remains untested in the supplied evidence (`docs/UI-UX-RUNTIME-EVIDENCE.md:40-44,70-80`).
- Keyboard-only Setup/routing/destructive dialog, screen-reader landmarks/announcements, reduced-motion runtime, font loading/fallback, and the actual Connection Map scope remain open evidence needs (`docs/UI-UX-DISTRIBUTED-AUDIT.md:60-62,167-168`; `docs/audits/uiux-runtime-agentD-motion.md:60-92`; `docs/audits/uiux-runtime-agentE-typography.md:87-123`).
- A future visual decision must test operator tasks: first Production connection, route correction, User Linking → Production association, and diagnosis/recovery. These are the correct tests for resolving dashboard grid, disclosure depth, density, and map scope—not aesthetic preference alone (`docs/UI-UX-DISTRIBUTED-AUDIT.md:101-104,167-168`).
- Any acceptance review must preserve the security boundary: no credentials, Authorization headers, JWTs, webhook URLs, response bodies, or secret keys in rendered UI, logs, telemetry, or evidence (`docs/CURRENT-IA-UI-SPEC.md:111-118`). This report intentionally names no secret values.

## 7. Source index

- `docs/UI-UX-DISTRIBUTED-AUDIT.md:8-10,18-23,38-42,56,64-82,101-108,150-168` — consolidated intent, findings, disputed decisions, rejections, and next evidence pass.
- `docs/UI-UX-RUNTIME-EVIDENCE.md:21-44,48-80,83-104` — runtime boundary, Login observations, and untested protected surfaces.
- `docs/CURRENT-IA-UI-SPEC.md:5-19,21-35,37-63,65-89,91-118,120-155` — route roles, hierarchy, state vocabulary, responsive contract, disclosure, safety, and observability.
- `docs/SETUP_WIZARD.md:18-32,36-54,92-114` — setup stages, runtime states, readiness, fail-closed channel planning, and System Status role.
- `docs/NOTIFICATION_SAFETY.md:3-7` — fail-closed notification behavior and explicit recovery boundary.
- `src/setup/ui.go:8-80,162-208,218-234,285-299,342-397,524-528` — current shared theme, surfaces, typography, controls, cards, motion, and reduced-motion overrides.
- `src/setup/ia_views.go:2451-2474,2689-2707,2864-2895,1338-1358` — wizard progress/plan, Dashboard menu, and System Status refresh/markup.
- `src/setup/workflow_diagnosis.go:411-502` — current diagnosis summary and technical-detail sections.
- `src/setup/current_routing.go:275-295` — routing row, stable IDs, channel mapping, and keyboard/drag affordance source.
- `docs/audits/uiux-agent8-anti-ai.md`, `docs/audits/uiux-agent11-direction.md`, `docs/audits/uiux-runtime-agentD-motion.md`, `docs/audits/uiux-runtime-agentE-typography.md` — specialist evidence used with the line-cited consolidated records above.

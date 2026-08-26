# KitsuSync UI/UX HIG Audit — 2026-08-26

This audit uses Apple Human Interface Guidelines as a quality framework only. KitsuSync's existing visual language and product semantics remain authoritative.

## Evidence and scope

- Repository: `C:/Users/mynti/Documents/KitsuSync-clean`
- Branch: `feature/readiness-routing-ui`
- HEAD at audit start: `81be1af`
- Runtime reviewed: authenticated `http://127.0.0.1:8090`
- Audited locales: Japanese and English where the runtime permitted; desktop layout; mobile behavior reviewed from the responsive source rules.
- No Kitsu, Discord, Production, notification, or database writes were performed by this audit.

## Findings

| Route / screen | Issue | Evidence | Principle | Severity | Recommended fix |
|---|---|---|---|---|---|
| `/bot/admin/projects?tab=overview` | The overview can present a full-width `現在の問題 / Current issues` row after the four summary cards, making a healthy zero state feel detached from the dashboard card system. | `renderProductionPanelMarkup` promotes the first four rows to cards and leaves the fifth row as `production-current-issues`; the corresponding CSS makes it a separate full-width row. | Hierarchy; grouping; glanceable status | P2 | Keep one truthful issue summary, but style it as an intentional fifth summary item with the same surface, padding, and status treatment. |
| `/bot/admin/checkers` and docs Routes | `/bot/admin/checkers` is a compatibility redirect to User Linking, while the public route inventory describes it as an independent role-assignment page. | `CheckersHandler` redirects to `/bot/admin/users`; `site.jsx` lists `/bot/admin/checkers` as a role-assignment destination. | Predictability; consistency | P2 | Document the compatibility redirect accurately and point operators to the Production Users flow for Production-scoped assignments. |
| `/bot/docs/` on 8090 | The running 8090 image is not the target `KitsuSync-clean` source and serves older documentation content/branding. | Container mounts `C:\Users\mynti\Documents\KitsuSync`, image label `docs-product-content`; observed content included pre-release/codebase wording and older version text. | Continuity; product consistency | P1 runtime blocker | Rebuild/recreate 8090 from `KitsuSync-clean` only after preserving and explicitly validating the runtime data/config mounts. |
| `/bot/setup?wizard_step=2..7` without wizard state | Direct URLs without the required selected-project/guild state render the Step 2 selection surface rather than a contextual error or safe redirect. | Browser review of direct step URLs without query state. | Error prevention; feedback | P3 | Keep behavior unless product requirements call for deep-link recovery; add a clear state explanation if changed later. |
| `/bot/admin/health` | System Status uses dense operational content and technical labels, but its hierarchy and responsive source rules are consistent with the accepted current design. | Browser review: readable response values, compact labels, no overflow or app console errors. | Legibility; information density | None | No change. |
| `/bot/admin/bot` and `/bot/admin/users` | Connection cards and simple user-association forms are grouped, independently labelled, and preserve secret masking and scoped actions. | Browser review and current renderer/tests. | Consistency; progressive disclosure | None | No change. |
| Production detail Notifications / Users / Danger Zone | Current source uses read-only routing summary, explicit edit flow, staged actions, truthful empty states, and separated danger controls. | Current renderer and focused tests. | Error prevention; progressive disclosure | None | No change. |
| `/bot/login` | Login purpose, fields, and primary action are immediately identifiable. | Browser review; no overflow. | Clarity; hierarchy | None | No change. |

## Implemented safe repairs

- Preserved the existing accepted Japanese terminology changes in the dirty worktree (`プロダクション` in the current Dashboard, Production list, wizard, and current diagnostics strings).
- Updated the public route inventory so `/bot/admin/checkers` is described as a compatibility URL that leads to User Linking; Production-scoped Reviewer / Checker assignment remains documented under Production Users.
- Adjusted the current Production overview issue summary to remain a single intentional summary item with the same card treatment as the other overview metrics. No status calculation or product behavior changed.
- Corrected the current Notifications summary/editor headers so Japanese renders `Discordチャンネル` while English remains `Discord Channel`.
- Rewrote the public Preview documentation to describe the current read-only Notifications and Audit Log workflow; no obsolete preview control is claimed.

## Deliberately unchanged

- Accepted Dashboard and System Status behavior.
- Notifications, Storage, Activity, Danger Zone, Kitsu authentication, Discord integration, routing semantics, telemetry, and persistence behavior.
- Direct deep-link behavior for incomplete setup URLs (P3; no safe product-semantic change was required).
- The target 8090 runtime was rebuilt from this checkout with the existing data and configuration mounts preserved; health and readiness were rechecked.

## Validation record

- Focused source inspection completed across login, Dashboard, Connections, setup wizard, Production detail tabs, health, docs, User Linking, and compatibility routes.
- `gofmt` completed before this audit.
- `git diff --check` passed before this audit.
- Focused `go test ./src/setup`, `gofmt`, `git diff --check`, and the target Docker build passed after the final edits.
- Browser desktop review found no horizontal overflow across the major JP/EN routes and Production tabs; the final Notifications terminology and docs sections were rechecked. The only browser warning was the external Babel CDN transformer warning; no app-origin errors or warnings were observed.

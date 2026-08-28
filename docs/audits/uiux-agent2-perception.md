# Agent 2 — Gestalt / visual hierarchy / perception

| Finding | Observation | Perceptual consequence | Structural principle |
|---|---|---|---|
| P2-01 | Base shell combines radial gradients, translucent surfaces, borders, shadows, dot field, and animation (`ui.go:1-75`). | Figure/ground is busy before content is parsed; content and atmosphere compete. | Establish a quiet content plane; reserve motif and contrast for identity/important state. |
| P2-02 | `section-card glass` is reused for page sections, metrics, forms, warnings, results, and nested sections. | Common-region grouping loses semantic meaning; card boundaries become visual noise. | Use surfaces only for ownership boundaries or high-risk actions. |
| P2-03 | Dashboard exposes metrics, attention queue, CTA, and management menu with similar surface treatment. | Scanning does not yield one obvious focal point. | Order by operator decision: blocking issue → next action → management entry. |
| P2-04 | Status pills/badges appear at dashboard, Production, service, table, and diagnostic levels. | Similarity implies equivalence even when states differ in scope. | Use a shared vocabulary but vary placement/weight by scope; do not repeat the same state in adjacent regions. |
| P2-05 | Wizard plan and workflow diagnosis use wide tables with many columns. | Continuity breaks at narrow widths and row-level meaning is hard to scan. | Keep the primary decision columns adjacent; disclose identity/reference columns. |
| P2-06 | Production overview deliberately uses equal four-card grid plus full-width issue card. | Strong structure and alignment, but equal weight may overstate low-value summary metrics. | Equal grids are acceptable only when four states are genuinely peer decisions. |

The current dark/orange palette is coherent and the production summary/routing rows have useful alignment. The dominant visual risk is layering and repetition, not lack of decoration.

Evidence: `src/setup/ui.go:1-160, 320-370, 430-470`; `src/setup/admin.go:435-440`; `src/setup/workflow_diagnosis.go:444-502`; `docs/CURRENT-IA-UI-SPEC.md`.

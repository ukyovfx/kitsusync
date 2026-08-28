# Agent 10 — Final DESIGN.md adversarial review

Date: 2026-08-28 JST. Scope: `DESIGN.md` only. No UI code, routes, behavior, or implementation files were reviewed or changed.

## Result

The design direction is KEEP. The first synthesis required refinement before becoming an implementation contract. The following issues were identified and resolved in the final `DESIGN.md`:

- Desktop navigation now states that the current horizontal navigation is the standard; a sidebar is a future candidate and must replace, not coexist with, it.
- IA now distinguishes top-level routes, the Dashboard attention surface, and provisional Production-local candidates.
- Connected Production screens are explicitly provisional and excluded from acceptance until runtime evidence exists.
- Warning/info values are grounded in observed source values, and component, size, density, and status-label tokens are explicit.
- The 38px dense exception is restricted to read-only, non-primary connection summaries and requires keyboard, spacing, zoom, and text-spacing checks.
- JP/EN canonical terminology and mixed-language/wrapping rules are explicit.
- Motion has a component/trigger/default/reduced-motion/completion contract and prohibits `transition: all`.
- Serif/mincho is removed from the canonical system rather than left as an implementation branch.
- Anti-AI rules are measurable: one primary action, semantic surfaces only, text-backed status, no decorative state encoding, and no unexplained duplicate signals.

## KEEP

Editorial Systems Interface, state-first hierarchy, consequence-first safety language, list-first/card-minimal information structure, Kitsu/Discord peer comparison, durable Production context, restrained warmth, and the accessibility contract are coherent and evidence-grounded.

## Remaining deferred decisions

Connected Production runtime evidence, actual font glyph provenance/offline fallback, and protected-page reduced-motion emulation remain explicit runtime or delivery boundaries. They are not silently promoted to design facts. Connection Map remains deferred and is not a core requirement.

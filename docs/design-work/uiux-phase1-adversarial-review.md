# Phase 1 post-implementation adversarial review

Four fresh independent reviews were run after the foundation implementation. Reviewers did not edit files.

## A — Visual hierarchy / Anti-AI

KEEP: removing shared blur/shadow and page/card entrance defaults, static low-contrast admin dots, and preserving the current horizontal navigation.
REFINE: remaining nested cards, dashboard CTA/metric competition, and repeated orange emphasis are screen-specific follow-up items. They were not expanded into a screen redesign.

## B — JP/EN typography

Two Phase 1 issues were repaired: JP Connections labels now use localized technical wording (`Kitsu Bot APIトークン`, `Discord Botトークン`), and edit-form controls no longer use the 38px dense size. The final runtime reports normal JP transform/tracking and 44px edit controls. The existing 38px rule remains for read-only connection summaries only.

## C — Accessibility / responsive

KEEP: landmarks, skip link, route-owned `aria-current`, native mobile disclosure, text-backed statuses, no page overflow in the final matrix, and the existing confirmation dialog contract.
REFINE: the locale switch is a single action rather than a selected tab; compact legacy controls outside the representative set remain future audit items. Reduced-motion emulation on protected pages remains unavailable in the connected Chrome surface.

## D — Existing workflows / regression

KEEP: Productions, User Linking, System Status, Audit Log, Setup, route ownership, and `/health` remained functional in the reachable smoke matrix. The only console errors observed were Chrome-extension message-channel errors, not application stack traces.
DEFER: connected Production detail and the previously recorded Setup URL-language inconsistency require separate evidence; neither was caused by the foundation batch.

## Final disposition

No Phase 1 blocker remained after repair. Screen-specific hierarchy, connected Production detail, and legacy compact-control normalization belong to later bounded work.

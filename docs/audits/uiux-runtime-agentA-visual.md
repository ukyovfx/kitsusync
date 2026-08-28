# Runtime visual audit — Agent A

## Scope and method

Read-only inspection of the supplied rendered login screenshots. Scope is visual hierarchy and anti-AI concerns only: card treatment, glass treatment, dot texture, gradient treatment, and dead space. No application code, runtime state, secrets, external sources, or other agents' reports were used.

Tested exact screenshot sizes: 375×812, 768×1024, 1024×768, and 1440×900, in both Japanese and English. The 1440×900 `top` and `full` captures were also checked; the full captures do not expose additional login content beyond the visible page.

## Findings

| ID | Finding / verdict | Route and evidence | Evidence vs inference | Severity | Confidence | Recommendation / action class |
|---|---|---|---|---|---|---|
| A-01 | Confirmed: stacked card treatment | Login, JP/EN, all tested sizes. The form sits inside a lighter rounded panel, which is itself enclosed by a second dark rounded shell with its own border and inset spacing. Examples: [JP 375×812](runtime-evidence/login_ja_375x812_top.png), [EN 1440×900](runtime-evidence/login_en_1440x900_top.png). | Evidence: two visually distinct rounded containers surround the same login task. Inference: the outer shell adds a card-on-card appearance without adding task hierarchy, making the composition read as template-like. | P2 | High | Reduce the visual nesting to one primary form surface, or make the outer boundary clearly structural rather than another card. Design review / implementation follow-up; no change made here. |
| A-02 | Rejected as a confirmed issue: glassmorphism | Login, JP/EN, all tested sizes. [JP 1024×768](runtime-evidence/login_ja_1024x768_top.png) and [EN 1024×768](runtime-evidence/login_en_1024x768_top.png) show dark surfaces with solid-looking fills and borders; the background texture remains visually separate. | Evidence: no visible frosted blur, background distortion, or readable content seen through the panel. Inference cannot establish CSS opacity or `backdrop-filter` from a static screenshot. The screenshots support “dark layered panel,” not a confirmed glass treatment. | P3 / rejected | Medium-high | Do not carry forward a “glass” finding based on these captures alone. Inspect computed styles only if implementation-level verification is required. |
| A-03 | Confirmed: dotted background texture | Login, JP/EN, all tested sizes. The dark background contains a repeated, evenly spaced dot field, most apparent in the lower half of [JP 1440×900 full](runtime-evidence/login_ja_1440x900_full.png) and [EN 1440×900 full](runtime-evidence/login_en_1440x900_full.png). | Evidence: repeated dots are visible across the viewport outside the form. Inference: the texture adds a decorative layer to an otherwise low-information screen and can read as a generic tech/AI motif. | P3 | High | Keep only if the texture has a deliberate product role; otherwise reduce contrast or remove it so the login task owns the visual hierarchy. Visual polish / design decision. |
| A-04 | Confirmed: warm orange/brown gradient or glow | Login, JP/EN, all tested sizes. A warm field is strongest behind the upper-left branding/form area and fades toward near-black, visible in [JP 768×1024](runtime-evidence/login_ja_768x1024_top.png) and [EN 1440×900](runtime-evidence/login_en_1440x900_top.png). Orange button glow is also visible around the primary action. | Evidence: background luminance/color visibly transitions from warm upper-left to dark elsewhere; the button has a warm halo. Inference: the duplicated warm emphasis competes with the form boundary and contributes to a familiar “AI dashboard” visual language. | P3 | High | Keep one intentional source of warm emphasis. Prefer the action/form focus to win over a large ambient glow; verify against brand intent before altering. Visual polish / design decision. |
| A-05 | Confirmed: lower-page dead space weakens hierarchy | Login, JP/EN, all sizes, strongest at 768×1024 and 1440×900. In [JP 1440×900 full](runtime-evidence/login_ja_1440x900_full.png), the compact login group occupies the upper-middle region while a large uninterrupted field remains below it. The same pattern is visible in [EN 768×1024](runtime-evidence/login_en_768x1024_top.png). | Evidence: after the form and header controls, a large majority of the viewport contains no task content. Inference: the page gives decorative background treatment comparable visual territory to the only required task, making the login feel visually undersized and less intentional. | P2 | High | Rebalance vertical composition so the login group is the clear page anchor: center it within the usable viewport or intentionally use the space for meaningful context. Do not add decorative filler. Layout / design follow-up. |

## Cross-size notes

- 375×812: the form is readable and fits without clipping, but the outer shell plus inner panel consumes most of the horizontal composition while the lower viewport remains unused. JP description wraps to two lines; EN remains one line. This is observed wrapping, not a wording audit.
- 768×1024: the form is a small centered island in a large textured field; dead space is more pronounced than at 375px.
- 1024×768 and 1440×900: the form remains compact relative to the viewport. The header branding at upper-left and language control at upper-right create a wide frame around a small central task.
- JP and EN use the same visual hierarchy. No language-specific card, glass, dot, gradient, or dead-space verdict was rejected because of localization differences.

## Limitations

This is a screenshot-only audit. It cannot prove DOM structure, CSS opacity, blur filters, stacking contexts, focus behavior, scroll behavior outside the supplied captures, or the intended brand rationale. Coordinates and dimensions of visual regions were assessed from the exact-size images, but no implementation geometry or computed-style inspection was performed. No external research was used; therefore there is no external-research support to cite.

## Evidence index

- `docs/audits/runtime-evidence/login_ja_375x812_top.png`
- `docs/audits/runtime-evidence/login_en_375x812_top.png`
- `docs/audits/runtime-evidence/login_ja_768x1024_top.png`
- `docs/audits/runtime-evidence/login_en_768x1024_top.png`
- `docs/audits/runtime-evidence/login_ja_1024x768_top.png`
- `docs/audits/runtime-evidence/login_en_1024x768_top.png`
- `docs/audits/runtime-evidence/login_ja_1440x900_top.png`
- `docs/audits/runtime-evidence/login_en_1440x900_top.png`
- `docs/audits/runtime-evidence/login_ja_1440x900_full.png`
- `docs/audits/runtime-evidence/login_en_1440x900_full.png`

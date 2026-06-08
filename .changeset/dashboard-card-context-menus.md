---
"dashboard": patch
---

Add right-click (context-menu) support to dashboard cards. Right-clicking a card opens a menu of the same actions shown by the card's visible `⋯` button, driven by one shared `Action[]` so the two never drift. Applied to every card that already exposes an action menu — Plugin, Source, Environment, Assistant, Custom Tool, Prompt, and Resource cards — via a reusable `CardContextMenu` primitive. The right-click menu honors the same RBAC gating as the `⋯` menu (e.g. the Environment "Clone" action stays hidden without `environment:write`).

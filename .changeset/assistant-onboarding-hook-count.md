---
"dashboard": patch
---

Loading the assistant onboarding page directly — a fresh navigation or a browser refresh on `/assistants/new` or `/assistants/:id` — no longer crashes the page with "Rendered more hooks than during the previous render". `FrontendTools` called each tool component inline instead of rendering it, so every tool's hooks ran inside `FrontendTools`' own hook list. The onboarding tool set is gated on RBAC grants and `hasScope` returns false until those grants load, so the first render after a hard load saw a trimmed set and a later render saw the full one — growing the component's hook count and tripping React's hook-order invariant. Reaching the page by in-app navigation happened to work because the grants query was already cached. Each tool now renders as its own keyed element, giving it its own hook list.

---
"dashboard": minor
"@gram-ai/elements": minor
---

Make the Project Assistant dock a continuous experience across the dashboard. The dock stays expanded across page navigation and swaps in the new page's suggestions; every suggestion set is colocated in one route-keyed object with question-phrased titles and per-subject icons, and chips animate in on route change. The expanded composer gets a Granola-style grey tray with a bordered inner input, the Cmd+/ hint moves to the breadcrumb bar, and the chat panel opens as an extension of the pill — including a matching slim composer.

Elements: add `theme.customCss` to `ElementsConfig` — extra CSS injected into the Elements shadow root after the built-in stylesheet, the supported escape hatch for embedders restyling the stable `aui-*` class hooks (host-page CSS cannot reach into the shadow DOM).

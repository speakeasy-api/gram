---
"server": patch
"dashboard": patch
---

Gate the "click to reveal" secret action in Risk Events behind the `chat:read` scope. Users without `chat:read` now see flagged secret values as a non-interactive "Hidden" placeholder (with an explanatory tooltip) instead of a reveal control, and the page-level "Reveal all" toggle is hidden for them. The `chat:read` scope description in the role editor is updated to note that the grant also controls unmasking flagged secrets in Risk Events.

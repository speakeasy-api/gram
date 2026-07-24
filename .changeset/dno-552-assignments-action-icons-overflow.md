---
"dashboard": patch
---

Fix the plugin assignments multi-select trigger overflowing its container. Long selected-principal labels (e.g. email principals) now truncate with an ellipsis instead of bleeding past the right edge, and the clear/chevron controls stay pinned to the top-right aligned with the first badge row rather than floating in the vertical middle of a tall wrapped stack.

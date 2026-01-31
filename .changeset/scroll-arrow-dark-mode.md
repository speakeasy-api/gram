---
"@gram-ai/elements": patch
---

Fix thread list and tool approval UI for small containers and dark mode:
- Fix scroll-to-bottom arrow invisible in dark mode
- Make tool approval Deny/Approve buttons responsive with container queries
- Fix popover toggle race condition using composedPath() for Shadow DOM support
- Fix popover and tooltip z-index ordering
- Fix thread list item title text wrapping
- Resize welcome suggestions layout for small containers

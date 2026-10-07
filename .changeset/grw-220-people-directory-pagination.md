---
"server": patch
---

Fix the people directory repeating a person on every page and never reaching the rest of the list when that person's most recent activity was Gram-hosted (excluded) usage. Next-page cursors now carry the exact activity timestamp the page ended on; cursors issued before this change keep working.

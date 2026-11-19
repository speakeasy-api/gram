---
"server": patch
---

The sessions list now loads even when risk counts are slow or unavailable. The risk count read gives up after five seconds, and the list then omits the counts instead of hanging or failing.

---
"server": patch
---

Retry chat completions when the upstream model returns an empty response, and report the upstream details when it still fails, reducing transient playground and chat errors.

---
"server": patch
---

Disable HTTP keep-alives on function-runner calls and give that path its own timeout, so retries dial fresh connections instead of reusing pooled connections to Fly machines that were autostopped mid-flight (which surfaced as instant EOFs). The function-runner timeout now sits above the runner's 5-minute execution budget so long tool calls are no longer cancelled by the caller.

---
"server": patch
---

Per-user telemetry surfaces (employee page tiles, overview, employees list) no longer count Gram-hosted inference — risk-analysis judges and other platform-side completions logged under the session owner's identity — as the employee's usage. External-user surfaces are unaffected: an external user's hosted-chat completions are their usage, and an explicit hook_source filter still returns Gram-hosted rows when asked for by name.

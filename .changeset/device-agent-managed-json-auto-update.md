---
"dashboard": patch
---

The example `managed.json` in the Device Agent setup now sets `auto_update` to `"automatic"` for macOS and Linux, so fleets that copy it get self-updating agents, matching the install steps and the docs. Windows keeps `"notify"` until its self-update is validated, and the onboarding MDM card, which covers every platform, says so.

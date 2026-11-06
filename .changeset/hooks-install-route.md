---
"server": patch
---

Generated hooks plugins record how they were installed (marketplace, download, agent install) in `speakeasy.json`, and the hooks binary reports it on every request so it shows up on hook telemetry. `speakeasy-hooks install` marks its plugins as `hooks_install`, and `SPEAKEASY_AI_HOOKS_INSTALL_ROUTE` overrides the value for managed installs.

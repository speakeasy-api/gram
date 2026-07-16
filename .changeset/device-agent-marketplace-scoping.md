---
"server": patch
---

Scope the device agent's managed marketplaces to the org's default project plus any project the caller has an assignment in. `agent.getPlugins` previously returned every published marketplace in the org — each synthesizing its always-on observability plugin independent of assignments — so an org with many published projects flooded the device agent with one `speakeasy-observability` per project. The default project still always surfaces as the org-wide baseline; a non-default project now appears only when the caller has a matching plugin assignment there.

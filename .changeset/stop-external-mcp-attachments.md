---
"server": minor
---

Deployments no longer accept or carry external MCP servers. The `external_mcps` field on deployment creation and the `upsert_external_mcps` field on evolve are removed, and evolving or redeploying a deployment produces one without the external MCP servers its source had. `exclude_external_mcps` is still accepted but has nothing left to exclude. Deployment processing no longer creates tools for external MCP servers; older deployments that already have them keep serving them.

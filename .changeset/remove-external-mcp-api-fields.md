---
"server": patch
"dashboard": patch
"admin": patch
---

Remove the external MCP fields from the API. Deployments no longer list external MCP sources, evolving a deployment ignores `exclude_external_mcps`, toolsets no longer carry external MCP header definitions, and the `externalmcp` tool type is no longer a valid tool filter. The deployment external MCP counts stay, always 0, so older clients keep working. The dashboard drops its external MCP views, warnings and OAuth auto-setup.

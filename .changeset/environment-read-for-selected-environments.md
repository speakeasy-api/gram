---
"server": patch
---

Selecting an environment whose values reach tool code now requires `environment:read` on that environment, not only project, MCP server or assistant write access. This applies to direct tool invocation, environment links on sources and toolsets, a toolset's default environment, an MCP server's environment, and an assistant's per-server environments. A project-wide grant covers every environment in the project, and a grant on one environment covers only that one. Existing bindings keep working, and legacy API keys are unchanged.

---
"cli": patch
---

The `install` commands with `--toolset` now read the MCP server URL from the server's platform endpoint when the toolset does not report an MCP slug, and keep the toolset's MCP slug URL otherwise. Toolsets with no MCP URL now report an error instead of writing an unroutable organization/project/environment URL or crashing when the toolset has no default environment.

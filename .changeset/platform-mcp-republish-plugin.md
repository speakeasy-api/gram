---
"server": patch
---

Organization administrators can now republish a stale plugin from the Platform MCP with `republish_plugin`. When `get_plugin` reports that a plugin's published package is out of date, an agent can request a publish of the project's plugin packages right away instead of waiting for the hourly refresh. The tool asks for confirmation first, because the publish covers every plugin in the project. It does nothing when the package is already current and refuses with a dashboard link when no package repository is connected. The publish runs in the background, and `get_plugin` reports when it has landed.

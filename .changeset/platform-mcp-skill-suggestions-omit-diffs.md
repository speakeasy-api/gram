---
"server": patch
---

`list_skill_suggestions` on the Platform MCP accepts `omit_diffs` to return each proposed change's ID, rationale and evidence counts without its diff, so an agent can triage a large suggestion queue before reading individual changes. Diffs are still returned by default.

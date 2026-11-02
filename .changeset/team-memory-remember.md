---
"server": patch
---

The Platform MCP has a new `remember` tool, behind the `team-memory` flag. It saves a decision, convention or gotcha to a project's team memory. Detected secrets are masked before the memo is stored, the memo is attributed to whoever saved it, and every save is audited. It needs an explicit `memory:write` grant on the project.

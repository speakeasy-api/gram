---
"server": patch
---

Add two Platform MCP tools for skill insights: `list_skill_insights` ranks a project's skills by activations, sampled efficacy, session cost, and estimated time saved, and `compare_skill_versions` breaks those same numbers down across one skill's versions. Deployments without ClickHouse keep both tools registered as stubs.

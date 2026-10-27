---
"server": minor
---

The Platform MCP can now serve a small discovery surface to hosts with tight tool budgets, such as Codex Agent Plugins. Callers in the treatment see `search_platform_tools`, `describe_platform_tool` and `call_platform_tool`, plus `execute` (Code Mode) when the organization has Code Mode. Every Platform tool stays reachable through them with its usual authorization, confirmation and audit rules. A PostHog multivariate flag picks the surface per harness; callers outside it keep the full catalogue.

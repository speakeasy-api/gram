---
"server": patch
---

Add managed-assistant provisioning: `EnableManagedAssistant` / `DisableManagedAssistant` / `GetManagedAssistant` toggle a project's platform-managed assistant (AI Insights sidebar). Enabling creates the assistant with the ported Insights prompt and all MCP-reachable project toolsets attached and records the `project_managed_assistants` mapping; disabling tears both down. Idempotent and race-safe. Foundation for AGE-2631.

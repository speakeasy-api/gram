---
"server": minor
---

Per-MCP-server scoping for plugin-published API keys, enforced via RBAC.
Each server in a published plugin now carries its own bearer token bound
to that server's toolset through a `mcp:connect` grant on a new
`api_key` principal — the same RBAC engine call that already gates
session-authenticated MCP requests does the work for plugin-key requests
too. A leak from one plugin server only grants access to that single
toolset, not the whole plugin or org. Audit log entries for these keys
carry `plugin_id` and `toolset_id` metadata, so an admin can answer
"which plugin/server was this credential tied to?" from history alone.
Plugin-minted keys are tagged `system_managed` and hidden from the
dashboard's keys page so they don't crowd out user-managed credentials.

Existing org-wide API keys (CLI, producer, hooks-download, dashboard-
created consumer/chat keys) keep their historic behavior — RBAC is
bypassed when an api-key request has no per-key grants. Plugin-scoped
enforcement runs for every org regardless of account type or RBAC
feature flag, since key scoping is a security primitive rather than a
tier feature.

Republish accumulates keys (does not auto-revoke); cleanup is deferred
to a follow-up reaper / explicit admin rotation flow.

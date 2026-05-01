---
"server": minor
---

Per-MCP-server scoped API keys for generated plugin manifests. Each server in
a published plugin now carries its own bearer token, bound to that server's
toolset. A leak from one plugin server only grants access to that single
toolset — not the whole plugin, and not the org. Audit log entries for these
keys carry `plugin_id` and `toolset_id` metadata, so an admin can answer
"which plugin/server was this credential tied to?" from history alone.
Plugin-minted keys are tagged `system_managed` and hidden from the dashboard's
keys page so they don't crowd out user-managed credentials. Republish
accumulates keys (does not auto-revoke); cleanup is deferred to a follow-up
reaper / explicit admin rotation flow.

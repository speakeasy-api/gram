---
"server": patch
---

Enabling an MCP server no longer fails when the project's Default plugin already lists a server under the same display name. The Default-plugin attach now picks the first available display name — the requested one, then a backend-id-suffixed variant — instead of letting the `(plugin_id, display_name)` unique index abort the enclosing transaction, so a same-named toolset attachment or a stale row can't block enablement. Deleting an MCP server also detaches it from its plugins (recording a `plugin:server_remove` audit event per detachment), releasing the display name for a replacement server.

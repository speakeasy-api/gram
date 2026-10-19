---
"server": minor
"dashboard": minor
---

A project's first enabled approval policy now gives it an approvals server, the MCP server agents call `request_approval` on. Speakeasy creates it at `<org>-<project>-approvals`, adds it to the Default plugin as `speakeasy-approvals` and republishes the plugin, so agents that use the Default plugin can request approval without extra setup. A held call names this server's URL. The policy editor says where the server went, the server shows as Built-in on the MCP and plugin pages, and policies and gateways cannot include it, because the gateway serves it before any policy runs.

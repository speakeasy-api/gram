---
"server": minor
"dashboard": minor
---

Support tool-level RBAC for MCP servers. Grants now use typed selectors with `resource_kind`, `resource_id`, `disposition`, and `tool` fields instead of untyped string maps. The dashboard scope picker stores toolset UUIDs (not slugs) as resource identifiers, fixing a bug where grants created via the UI never matched backend authorization checks. Public MCP servers correctly skip per-tool RBAC enforcement.

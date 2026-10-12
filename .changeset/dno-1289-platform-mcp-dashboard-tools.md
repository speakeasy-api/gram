---
"server": patch
---

Agents can list, read, create, lay out, filter, duplicate and delete dashboards through the Speakeasy Platform MCP. `list_dashboards` and `get_dashboard` read a project's dashboards and the Speakeasy-built ones; `save_dashboard`, `duplicate_dashboard`, `delete_dashboard`, `edit_dashboard_layout` and `save_dashboard_filters` preview first, then commit once confirmed against the version last read, with the same permissions, grid rules and audit trail as the Dashboards page. Speakeasy-built dashboards stay read-only: duplicate one to change it.

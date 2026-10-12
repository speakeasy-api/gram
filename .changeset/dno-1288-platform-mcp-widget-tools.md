---
"server": patch
---

Agents can list, read, save, duplicate and delete a project's Explore widgets through the Speakeasy Platform MCP. `list_widgets` and `get_widget` show each widget's query, chart, whether it still works and the dashboards it is on. `save_widget`, `duplicate_widget` and `delete_widget` preview first and commit only on confirmation, naming the version they read, so an edit made in the dashboard meanwhile is never overwritten; a delete reports which dashboards lost the widget. The tools need Explore switched on for the organization and write access to the project for changes, and serve external users only.

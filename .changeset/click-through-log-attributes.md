---
"dashboard": patch
---

Add click-to-filter on attribute rows in the MCP logs detail sheet. Click any attribute to filter by equals, exclude, contains, or copy its value. Also fixes attribute filters returning too few results due to a hardcoded event_source filter that didn't account for attributes being spread across multiple log entries per trace.

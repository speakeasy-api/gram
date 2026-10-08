---
"server": patch
---

The Platform MCP exposes the analytics query API: `describe_analytics_catalog` reads the datasets, fields, grains and limits a project's agent session data is queried through, `list_analytics_dimension_values` lists what a dimension holds in a window, and `run_analytics_query` runs a query in that vocabulary, so an agent can ask the questions Explore answers. The three tools are switched on by the same flag as Explore and refuse readably until it is.

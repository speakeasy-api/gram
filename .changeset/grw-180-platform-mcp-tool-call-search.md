---
"server": patch
---

Add `search_tool_calls` and `list_attribute_keys` to the Platform MCP. The search lists one project's tool calls across every MCP server over up to 30 days, narrowed by tool name text, error text, outcome, configured MCP server, person reference, and custom attribute filters, with opaque cursors, masked identities, and no tool inputs or outputs. Attribute key discovery lists the custom (@-prefixed) and filterable system keys present in the window. Attributes that carry tool content, an HTTP header, or a person's identity are refused as filters and withheld from key discovery, and paging holds the observation window fixed at the interval the first page read.

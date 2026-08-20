---
"dashboard": patch
---

The command palette's "MCP Servers" group now searches `mcp_servers`-backed servers as well as toolset-backed ones. Previously the group was built entirely from toolsets, so a remote-, tunneled-, or unproxied-backed server never appeared in ⌘K and could only be reached by navigating to the MCP list by hand. Both kinds are listed under the one heading, matching how the MCP list page presents them, and each row still matches on name and slug.

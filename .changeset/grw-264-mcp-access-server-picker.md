---
"dashboard": minor
---

The role editor's MCP access tab is now a server picker. It opens with a Specific servers / All servers choice; Specific servers lists every MCP server grouped by project, with fuzzy search, collapsible projects that count their chosen servers, and Select all / Clear all. Each server row has a pencil that limits its tools to all tools, a hand-picked list, or tools carrying chosen annotations, edited in a side sheet, or forbids the server. Forbidden servers sit in their own section with Unblock. Servers the role already administers through `mcp:read` or `mcp:write` show as ticked and locked, with a hover card that links to Platform access. Rules the picker cannot show, such as access to a whole project, are kept as stored, and the editor now notices when one server is swapped for another.

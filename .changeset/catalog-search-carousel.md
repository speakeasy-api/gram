---
"server": minor
---

Platform MCP catalog search results now render as a carousel of server cards in MCP Apps clients. Each card shows the server's logo, name and description. A server not yet in the project has an **Add to project** button that asks the agent to add it, and a server already in the project has an **Open** link to its dashboard page. `search_mcp_catalog` takes an optional `project_slug`, which defaults to the organization's Default project, to mark servers already added. The text and structured results are unchanged.

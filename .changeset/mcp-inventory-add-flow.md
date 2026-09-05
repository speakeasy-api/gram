---
"dashboard": minor
"@gram-ai/functions": patch
---

Unify how MCP servers are added: every way in starts from the MCP page, with the catalog, remote and tunneled servers, and an Advanced group covering OpenAPI documents, functions, and building a server from a source the project already has. Remote servers must verify connectivity before they can be saved.

Sources move under MCP rather than going away: a shelf at `/mcp/sources` and a page per source showing its file, the tools it produced, the deployments it is versioned by, and a download. Deploying a function now offers the flow that builds a server from it, scoped to the right project, instead of the dashboard root.

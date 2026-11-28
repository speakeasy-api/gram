---
"server": patch
"dashboard": patch
---

MCP clients configured with a URL that serves no MCP server now get JSON errors they can read instead of a parse error. The "mcp server not found" message says how to find the server's current URL in the dashboard. Requests from MCP clients to MCP URLs that match no route, such as a server URL with an extra path segment, answer with a JSON or JSON-RPC error instead of a plain text page. Root OAuth and OpenID discovery documents and `POST /register` on the dashboard answer with a JSON 404 instead of the dashboard page. Stream and session termination requests for an unknown MCP server answer not found instead of 405. A disabled MCP server now answers metadata, registration, stream, and session termination requests exactly as a missing one does.

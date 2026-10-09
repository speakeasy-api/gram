---
"tunnel": minor
"dashboard": minor
---

The tunnel agent can serve a stdio MCP server. Set `TUNNEL_LOCAL_MCP_COMMAND` instead of `TUNNEL_LOCAL_MCP_URL` and the agent starts one server process per MCP session, bridging Streamable HTTP onto its stdin and stdout.

The tunneled MCP setup snippets have a **Transport** choice for existing servers: **Stdio** asks for the server command and shows a Dockerfile that adds the tunnel agent to a base image with the server's runtime. Every snippet now pins the tunnel agent image to the release the dashboard was built with instead of `latest`.

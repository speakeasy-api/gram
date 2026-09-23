---
"tunnel": patch
---

The tunnel agent now forwards a tunneled OAuth call, such as a token exchange, to the request's own path on the host pinned by `TUNNEL_LOCAL_MCP_URL`, instead of appending that path to the pinned URL's path. With a `TUNNEL_LOCAL_MCP_URL` that carries a path, for example a Snowflake-managed MCP server URL, the old behavior sent the token request to a URL that does not exist, so logins through an identity provider bound to the tunnel failed with an empty 404. Any query pinned in `TUNNEL_LOCAL_MCP_URL` is kept on these forwards.
  
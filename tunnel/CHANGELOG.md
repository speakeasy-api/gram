# tunnel

## 0.1.1

### Patch Changes

- 3509e3e: The tunnel agent now forwards a tunneled OAuth call, such as a token exchange, to the request's own path on the host pinned by `TUNNEL_LOCAL_MCP_URL`, instead of appending that path to the pinned URL's path. With a `TUNNEL_LOCAL_MCP_URL` that carries a path, for example a Snowflake-managed MCP server URL, the old behavior sent the token request to a URL that does not exist, so logins through an identity provider bound to the tunnel failed with an empty 404. Any query pinned in `TUNNEL_LOCAL_MCP_URL` is kept on these forwards.

## 0.1.0

### Minor Changes

- 3e492c4: The Gram tunnel agent is now available as a public Docker image (linux/amd64 and linux/arm64):

  ```
  docker pull ghcr.io/speakeasy-api/gram-tunnel-agent:latest
  ```

  This is the image the Docker and Kubernetes setup instructions on the tunneled MCP server page use.

---
"server": minor
"dashboard": minor
---

Apps and workloads that sign in without a browser can now get sessions on MCP servers that offer several upstream credentials. A token request can name the credential for each server with `upstream_auth_client=<server id>:<credential id>` (or `:none`). Without one, the session uses the credential an admin chose for that app in the server's **Upstream credentials** settings, or the only credential the caller can use. When several credentials qualify and none was chosen, the request fails with `selection_required`; when the credential still needs setup, with `setup_required`. Applies to organizations where typed upstream credentials are enforced.

---
"server": patch
---

When an identity provider refuses the code exchange at the end of an MCP connection login, the error now names why: the provider's standard OAuth error code (for example `invalid_grant` or `invalid_client`), including when the provider reports the error under HTTP 200, or otherwise the HTTP status of a failed response, noting when it came through an MCP tunnel. The provider's own error description is still kept to server logs. Expired or reused authorization codes now log as warnings rather than errors.

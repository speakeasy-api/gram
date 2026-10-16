---
"server": patch
---

Sign-in to a remote MCP server no longer fails when its OAuth server rejects unrecognised authorize parameters. The `nonce` parameter is now sent only when the request asks for the `openid` scope.

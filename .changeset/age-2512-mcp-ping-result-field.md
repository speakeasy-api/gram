---
"server": patch
---

Always emit the `result` field in JSON-RPC success responses from the MCP server. Empty-result handlers (notably `ping`) previously sent `{"jsonrpc":"2.0","id":N}`, which violates JSON-RPC 2.0 and the MCP spec. Cursor's MCP SDK rejected those frames with `invalid_union` zod errors and dropped the transport to a failed state after each keep-alive ping.

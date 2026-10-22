---
"server": patch
---

Tool Logs now marks a failed tool call on a remote or tunneled MCP server as failed. A tool result with `isError` set is recorded with status 500 when the server answered with HTTP 2xx, as a JSON-RPC error already was. A call that gets no tool result at all, because the server could not be reached, answered with an error status or an unreadable body, or ended its stream early, now writes a failed row instead of none, carrying the server's error status or 502 when there was none.

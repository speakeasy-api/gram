---
"@gram-ai/functions": patch
---

fix: stop leaking stack traces in `ctx.fail()` errors and intercept errors in dev mode

`ctx.fail()` no longer includes a stack trace in the user-facing error body — only the supplied message is returned. The dev-mode MCP server now intercepts thrown failures (and unexpected JavaScript errors) and surfaces them as normal `isError` tool results instead of an opaque "Internal Error" in clients like the MCP Inspector.

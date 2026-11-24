---
"server": patch
---

Project Assistant turns can now be traced end to end. The assistant runner joins the trace of the backend request that dispatched the turn, adds spans for the turn, each tool call (including calls inside compose scripts), MCP connects and reconnects, bootstrap and turn-end compaction, and marks the first streamed model token. Its outbound bootstrap, inference and MCP tool-call requests carry W3C trace context. Local runner containers now export spans to the local collector when the server has tracing enabled.

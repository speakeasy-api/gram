---
"server": patch
---

Public MCP and OAuth routes now start a fresh server-side trace per request and record the inbound W3C trace context as a span link, instead of adopting the client-supplied `traceparent` as the span parent. This stops third parties from merging unrelated requests into one trace or steering our trace ids, and drops client-supplied `baggage` on those routes before it reaches handlers. The trusted `/rpc` and `/admin` surfaces keep end-to-end parent-child trace continuity and their inbound baggage unchanged.

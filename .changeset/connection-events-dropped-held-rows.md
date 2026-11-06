---
"server": patch
---

The `mcp.connection.events.dropped` metric now counts only connection events that were lost. Single-request rows held back by the rate limit or the queue share are written later as a repeat count, so they no longer count as `rate_limited` or `queue_reserved`. A held-back row lost when its key is evicted counts once, as `repeats_lost`, and a lost row that carries repeats counts every event it stood for.

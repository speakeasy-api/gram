---
"dashboard": patch
---

Make the server-assistant transport poll adaptively for replies: poll quickly for the first few iterations (so short turns surface within a few hundred milliseconds instead of waiting a full fixed interval), then back off geometrically to the steady-state interval for long, tool-heavy turns. Reduces the perceived latency of the project assistant relative to the old streaming AI assistant.

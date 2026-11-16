---
"server": patch
---

The tailnet gateway's certificate lifecycle now runs as a statechart, checked by exhaustive exploration and a TLA+ model of all of a connection's slots. A renewal that returns a certificate no later than the one held is now treated as failed instead of replacing it.

---
"server": minor
"dashboard": minor
---

A gateway's Debug tab charts its connections over the shared window: credentials created and revoked during each interval, and how many were active (unexpired, with no recorded revocation) at each interval's end, with a summary. A refresh continues a connection rather than counting as a new one, and older records without lineage are left out with a note. A new `userSessions.getTrend` endpoint and the Platform MCP tool `get_gateway_connection_trend` return the same counts. Revoking an access token now records the end of a connection that cannot refresh.

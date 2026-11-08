---
"server": minor
---

Long-lived SSE responses now end before the load balancer's response deadline. Remote and tunneled MCP listen streams, and POST streams whose response has been relayed, close with an SSE `retry` hint at a configurable maximum age (`GRAM_STREAM_MAX_AGE`, default 55 minutes) so clients reconnect cleanly; `subscriptions/listen` streams receive their completion result first. The chat turn stream sends a keepalive comment every 25 seconds and rotates the same way, and the dashboard resumes it from its cursor. A new `gram.http.stream.duration` histogram records how every long-lived response ended, by route, method and end reason.

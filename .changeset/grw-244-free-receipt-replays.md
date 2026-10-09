---
"server": patch
---

Retrying a Platform MCP write that already committed now returns its stored result without spending the caller's rate limit, and expired idempotency receipts are swept hourly instead of being kept forever.

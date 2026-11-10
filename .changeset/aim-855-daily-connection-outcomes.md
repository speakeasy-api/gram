---
"server": minor
---

Add a daily job that keeps MCP connection outcomes for 13 months: how each attempt ended and what fixed failures, per known provider, app and client, with no organization or person. A failure only an admin can fix counts an admin's change anywhere in the organization that can fix its reason, and is stored as `unattributed` when no such change or retry explains the fix. The first run backfills every whole day since connection events began.

---
"server": patch
---

fix(hooks): harden hook ingest against transient connection resets. Plugin hook senders now retry a dropped request with backoff instead of blocking the tool call or silently losing the event, and the server de-duplicates redelivered events so a retry is recorded exactly once across all coding assistants.

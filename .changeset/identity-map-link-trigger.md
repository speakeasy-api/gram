---
"server": patch
---

Trigger an immediate identity map sync when an account link gains attribution
during ingest or a WorkOS directory membership changes, instead of waiting out
the sync schedule. Triggers are throttled per process and go through the
schedule's overlap-skip policy, so chatty ingest coalesces and concurrent
requests are safe; a lost trigger degrades to the next scheduled tick.

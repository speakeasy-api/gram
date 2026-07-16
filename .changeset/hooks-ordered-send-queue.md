---
"hooks": patch
---

Preserve conversation order across offline-spool recovery (DNO-536). Previously the first live event after an outage was persisted before the spooled backlog it triggered draining of, so the newest message sorted ahead of older ones in chat_messages. The spool now doubles as an ordered send queue: observe-only events arriving while backlog exists append behind it (and kick the drain) instead of overtaking it, and gating events flush the backlog synchronously under a small budget before their live verdict send. Enforcement is unaffected — gating events always send live for the authoritative verdict, the budget keeps a slow backlog from stalling the user, and a drain already holding the lock is never waited on. The drain also re-lists after productive passes so entries queued mid-flush deliver in the same run.

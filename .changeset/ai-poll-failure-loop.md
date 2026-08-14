---
"server": patch
---

Stop a chronically failing AI integration sync from ringing failure monitors forever, and make its failure diagnostics actionable without exposing provider payloads to organization members. Poll failures now back off exponentially — each consecutive final failure doubles the delay before the next run, bounded by a 6h ceiling and anchored on recording time so long runs don't erase the early rounds — and a success, a config save, or re-enabling the schedule resets the streak and makes it due again immediately. Pollers retain normal diagnostic error chains for Temporal and worker logs, while the activity boundary separately derives the safe error persisted to `last_poll_error`. Codex JSON decode failures include the offending log record internally, but customer-visible status includes only the log id and decoder error.

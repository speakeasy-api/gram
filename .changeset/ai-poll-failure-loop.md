---
"server": patch
---

Stop a chronically failing AI integration sync from ringing failure monitors forever, and make its stored error actually say what broke. Poll failures now back off exponentially — each consecutive final failure doubles the delay before the next run, capped at 64x the schedule's base interval, and a success resets the streak — so a deterministic failure (e.g. a poisoned Codex compliance log file) decays to a slow cadence instead of retrying at full speed indefinitely. The Temporal failure message now expands the oops wrapper's cause chain instead of truncating at its public text, and the Codex cost importer's content errors carry the offending log file id and their underlying cause through to `last_poll_error`.

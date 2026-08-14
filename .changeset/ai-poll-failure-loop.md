---
"server": patch
---

Stop a chronically failing AI integration sync from ringing failure monitors forever, and make its recorded error actually say what broke. Poll failures now back off exponentially — each consecutive final failure doubles the delay before the next run, bounded by a 6h ceiling and anchored on recording time so long runs don't erase the early rounds — and a success, a config save, or re-enabling the schedule resets the streak and makes it due again immediately. Internal failure surfaces (Temporal messages, worker logs) render the full error chain via a new `oops.Detail` helper instead of truncating at an oops wrapper's public text, and the Codex cost importer's content errors carry the offending log file id and their underlying cause through to `last_poll_error` — while infrastructure error text stays behind an oops boundary and out of tenant-visible surfaces.

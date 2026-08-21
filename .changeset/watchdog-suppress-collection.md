---
"dashboard": patch
"server": patch
---

Watchdog suppression now collects a signal's findings without the page's time window, fixing the silent no-op when a signal contained findings whose messages predate the window (signals exist by scan time, the listing filters by message event time). An empty collection now shows an error toast instead of doing nothing. The signal drawer labels its window-scoped stats and unwindowed latest-evidence list so their counts can't read as contradictory, and the top-affected-users list no longer shows an "Unknown user" row for findings with no user attribution (matching the Users count).

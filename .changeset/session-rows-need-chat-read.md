---
"server": minor
---

Session lists now follow `chat:read`. `telemetry.listSessions` lists per-session rows only for projects the caller holds `chat:read` on and otherwise reports the slice's totals alone, flagged with `scoped: true`. `telemetry.searchChats` narrows to the caller's own sessions and those of agents they own instead of listing every member's.

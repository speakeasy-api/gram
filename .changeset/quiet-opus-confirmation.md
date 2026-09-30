---
"server": patch
---

Migrate prompt injection scanning to a cascade that sends Jev candidates at or above 50% probability to Opus 5.5 for confirmation using up to five conversation messages. When Opus 5.5's safety classifier refuses a candidate, Opus 4.8 confirms it instead.

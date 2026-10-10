---
"server": patch
---

Migrate prompt injection scanning to a cascade that sends Jev candidates at or above 50% probability to Sonnet 5.5 for confirmation using up to five conversation messages. Role-play and "act as" requests count as content requests unless they also try to override the agent. When Sonnet 5.5's safety classifier refuses a candidate, Opus 4.8 confirms it instead.

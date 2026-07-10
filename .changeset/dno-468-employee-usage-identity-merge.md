---
"dashboard": patch
"server": patch
---

The enrollment page no longer shows 0 tokens and a stale last activity for employees whose telemetry rows split across identity keys: usage rows carrying a user id but no email now merge into the employee's email-keyed summary, linked AI accounts attach to that merged summary, and role breakdowns resolve those users instead of bucketing them as Unassigned. The employees and agents tables also render their pagination footer flush against the table instead of floating below a gap.

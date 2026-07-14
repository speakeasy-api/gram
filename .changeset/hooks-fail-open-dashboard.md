---
"dashboard": minor
---

Add a "Fail Open During Outages" toggle to the org Logging & Telemetry page (DNO-497). Org admins can choose to let agent tool calls proceed when Speakeasy is unreachable instead of blocking them, with copy spelling out the trade-off: blocking policies are not enforced during the outage, events are still recorded and scanned after recovery, and broken credentials always block regardless.

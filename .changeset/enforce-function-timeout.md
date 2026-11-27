---
"server": minor
---

Speakeasy Function tool calls and resource reads now stop after the organization's function timeout, which defaults to 60 seconds instead of the previous fixed 5 minutes. Staff can raise or lower it per organization from 30 to 600 seconds in the admin dashboard. A limit above 5 minutes takes effect for a project once it is redeployed, because earlier function runners cap calls at 5 minutes.

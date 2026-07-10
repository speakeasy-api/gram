---
"server": minor
---

Forward each organization's tokens-under-management usage to PostHog (AGE-2289): hourly group properties on the organization group (current/previous cycle tokens, contracted allowance, utilization) plus a once-per-day organization_token_usage event, emitted from the billing usage refresh workflow.

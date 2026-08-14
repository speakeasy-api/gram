---
"server": patch
---

Cost analytics email filters and group-bys can fold one employee's directory,
personal, and case-variant emails into one canonical identity via the
ClickHouse identity map, gated by a PostHog rollout flag with a shadow-compare
mode that validates the fold on live traffic before serving it. Off by
default; literal matching is unchanged.

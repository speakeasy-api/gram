---
"server": patch
---

Add an `exempt_rule_ids text[]` column to `risk_policies`: custom detection rules
attached here act as exemptions (a match short-circuits the whole policy for
that message — an allowlist), disjoint from the detector `custom_rule_ids`.
Schema-only.

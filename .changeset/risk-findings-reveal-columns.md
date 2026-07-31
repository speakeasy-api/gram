---
"server": patch
---

Add reveal metadata columns to the ClickHouse `risk_findings` table:
`surface`, `field`, `path` and `tool_call_id` record which text a finding's
byte offsets index, so the raw match can be reconstructed and verified from
the original chat data at reveal time. Also redefine the `match_redacted`
column comment for the upcoming partial-mask display format.

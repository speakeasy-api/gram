---
"server": patch
---

Record MCP connection attempts in ClickHouse: one row per step, with a stable reason code, who fixes it and a plain-language message per code. Attempts carry through Speakeasy and provider sign-in. `/authorize` and the provider callback are recorded.

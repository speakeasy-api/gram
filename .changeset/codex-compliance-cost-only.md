---
"server": patch
---

Codex-product compliance COSTS rows (`codex:usage:metrics`) now meter cost only. Their token counts previously rode on `gen_ai.usage.*` keys, which the ClickHouse agent-usage predicates sum on top of the Codex OTEL stream — the token source of truth — double counting token metering for orgs running both feeds. The raw counts are preserved under new `codex.compliance.*_tokens` attributes (summed by nothing) because the compliance feed also covers surfaces OTEL never sees (cloud-delegated tasks, GitHub code review); a future surface-partitioned metering pass can promote them. Parked non-Codex rows (`chatgpt:usage:metrics`) keep their `gen_ai.usage.*` token counts since the compliance feed is ChatGPT/Work's only usage source.

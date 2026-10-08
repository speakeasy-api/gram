---
"server": patch
---

Prompt-injection and prompt-policy (llm_judge) findings from the batch risk scan are now published to the shared findings topic, so they reach the ClickHouse `risk_findings` store that Risk Events, the Watchdog and the Platform MCP read from. Previously only the async shadow-scan sample of those findings reached ClickHouse; every other organization's judge findings were written to Postgres alone and never appeared in those views.

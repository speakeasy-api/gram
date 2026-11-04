---
"server": patch
---

Record refused and failed MCP request outcomes with client and server attribution, including gateway member failures, credential gaps, and hosted access refusals. Successful requests do not create connection outcome rows.

Gateway recovery guidance uses fixed, slug-free text; the affected server remains identified separately in structured results. Routine access-token expiry and HTTP-successful tool errors do not create connection-failure rows.

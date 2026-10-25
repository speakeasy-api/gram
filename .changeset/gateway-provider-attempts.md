---
"server": minor
"dashboard": minor
---

Gateway connection health now records provider sign-in callbacks and the provider refreshes that requests to a configured gateway cause, for the same person, registered client and provider. A provider failure is told apart from the gateway's own faults. Each callback or refresh is one logical attempt: a saved credential does not show that the person returned to their client, and a reused refresh does not show that the provider was contacted. Scheduled and manual refreshes and requests to provider endpoints are listed as not counted. Gateway Debug and the Platform MCP connection health tool read both stages.

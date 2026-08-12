---
"server": minor
---

Add an internal admin pricing tracker: `GET /admin/organizations.pricingTracker` returns, per organization, the pay-as-you-go price at the current rate card (derived from observed tokens under management) alongside Gram-hosted inference spend over a trailing window, so staff can watch customer pricing exposure when adjusting spend limits. Inference spend is summed from Gram-server-run completion surfaces (playground, elements, risk analysis, assistants, and other platform-run completions) and is reported as zero when the admin service is started without a ClickHouse connection.

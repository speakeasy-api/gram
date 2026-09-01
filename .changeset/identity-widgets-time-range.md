---
"dashboard": patch
"server": patch
---

Identity page widgets now honour the selected time range. The audit trail, authorization challenges and per-person shadow MCP servers were reading their whole history regardless of the picker, so a 7-day view could report 233 findings alongside a device's worth of activity from a year ago and give no sign the two were counted over different periods. `auditlogs.list`, `access.listChallenges` and `access.listShadowMCPInventoryServersForUser` take optional `from`/`to` bounds — half-open, and absent bounds still return the whole history for every existing caller — and the Overview, Activity, Access and Security tabs pass the window they are showing.

Managed devices deliberately stay outside the range: it is the current MDM inventory rather than a stream of events, and a machine that has been quietly missing its agent for a month is exactly the one worth seeing. The panel and the Overview tile now say so instead of leaving the reader to assume the picker applied.

Each widget also shows a skeleton shaped like what it is loading rather than its empty state, so a panel that has not answered yet no longer claims there is nothing to report, and the detail page keeps room below the last panel instead of ending flush with the viewport.

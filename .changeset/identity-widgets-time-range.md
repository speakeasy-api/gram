---
"dashboard": patch
"server": patch
---

Identity page widgets now honour the selected time range. The audit trail, authorization challenges and per-person shadow MCP servers were reading their whole history regardless of the picker, so a 7-day view could report 233 findings alongside a device's worth of activity from a year ago and give no sign the two were counted over different periods. `auditlogs.list`, `access.listChallenges` and `access.listShadowMCPInventoryServersForUser` take optional `from`/`to` bounds — half-open, and absent bounds still return the whole history for every existing caller — and the Overview, Activity, Access and Security tabs pass the window they are showing.

Managed devices deliberately stay outside the range: it is the current MDM inventory rather than a stream of events, and a machine that has been quietly missing its agent for a month is exactly the one worth seeing. The panel and the Overview tile now say so instead of leaving the reader to assume the picker applied.

Each widget also shows a skeleton shaped like what it is loading rather than its empty state, so a panel that has not answered yet no longer claims there is nothing to report, and the detail page keeps room below the last panel instead of ending flush with the viewport.

A failed read is now told apart from a quiet window everywhere on these pages. Panels whose request errored say so and offer a retry instead of rendering "no roles assigned", "no managed device assigned" or "not enrolled" off data that never arrived, and the Cost and Usage tiles show a dash rather than `$0` when the metrics request fails or the identity carries no identifier the endpoint can key on. Two narrower cases go with it: an address claimed by more than one member no longer resolves to whichever came first, so the Access panels cannot show a stranger's roles; and the shadow-MCP lookup deduplicates and bounds the identifiers it sends, since an over-long list was rejected outright and read on screen as "no shadow servers".

Audit actor names resolve through the reading organization's memberships rather than straight at the global directory, so an actor id that never belonged to the organization falls back to the stored value instead of naming someone from another tenant. Soft-deleted memberships still resolve — a departed member is exactly the actor whose name the feed is meant to keep.

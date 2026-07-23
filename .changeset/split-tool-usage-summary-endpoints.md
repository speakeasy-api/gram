---
"server": minor
"dashboard": minor
---

Split the tool-usage summary into per-panel endpoints so the MCP & Tools dashboard streams in each card as its data arrives instead of blocking on the slowest aggregate (INC-417).

`getToolUsageSummary` now has seven sibling endpoints — `getToolUsageTotals`, `getToolUsageTargets`, `getToolUsageUsers`, `getToolUsageTargetTimeSeries`, `getToolUsageUserTimeSeries`, `getToolUsageUsersByTarget`, and `getToolUsageTargetToolBreakdown` — each returning one section of the summary from the same shared query helpers and filter payload. The aggregate endpoint is unchanged for the platform agent tool that wants everything in one call. The MCP & Tools page fetches the seven sections in parallel (the cheap totals query gates the page shell; each panel shows its own loading skeleton and, if its section query fails, its own error state rather than a misleading empty chart), and the MCP overview "Top users" table now fetches only the users section it needs.

---
"server": patch
"dashboard": patch
---

The Watchdog page now shows when risk analysis last ran. A badge beside the time range picker reads "Analyzing now" while a run is in flight, "Last analyzed 4m ago" once it finishes, or "No recent analysis" when no run is visible, so an administrator can tell at a glance whether an empty page means no findings or no analysis. The same status is available through the management API as `risk.getAnalysisStatus` and through the Platform MCP server as the `get_risk_analysis_status` tool, which also explains that analysis is event-driven: it starts within about 30 seconds of new chat traffic rather than on a timer.

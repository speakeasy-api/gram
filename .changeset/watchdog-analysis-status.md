---
"server": patch
"dashboard": patch
---

The Watchdog page now shows when risk analysis last ran: "Analyzing now", "Last analyzed 4m ago", or "No recent analysis". The same status is available as `risk.getAnalysisStatus` in the management API and as the `get_risk_analysis_status` Platform MCP tool.

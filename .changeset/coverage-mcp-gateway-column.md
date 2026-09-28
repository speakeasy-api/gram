---
"server": minor
"admin": minor
---

Add an MCP Gateway column to the observed coverage matrix on each organization's record in the staff admin app.

Every other column is an agent surface folded from a `hook_source`. The gateway is Gram itself, so its evidence is read from the traffic it served rather than from what an agent reported: session activity counts non-hook `trace_summaries` traces carrying a toolset slug or a meta MCP server id, matching how the tool-usage reads classify hosted and gateway MCP servers; identity counts the calls bound to a person, stating calls bound only to a managed agent separately; policy enforcement counts distinct mediated executions a risk policy stopped, read from `risk_findings` rows with a non-empty `mediation_surface`.

Two cells can never report for the gateway — it brokers tool calls rather than model calls, and shadow servers are by definition reached around it — so cells carry a new `na` status that reads as inapplicable rather than as a gap an integration could close. Cells also carry a `unit`, letting a column name what it counts when the capability's own unit does not fit: the gateway is measured in tool calls where an agent surface is measured in sessions.

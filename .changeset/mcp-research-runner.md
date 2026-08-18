---
"server": minor
"dashboard": minor
---

The MCP research agent goes live end to end. A new
`mcpApproval.startResearch` endpoint (decide-scoped) opens a report row and
enqueues a Temporal workflow that runs a bounded tool-calling loop over the
research web tools — search and page fetch — with the untrusted-content
posture pinned in its prompt, then extracts a schema-held report: summary,
independent-coverage level, and tiered claims where every web-sourced claim
carries its citations or is dropped. Reports land on `mcp_research_reports`
with model, prompt version, and per-run spend metadata; re-runs are additive
and at most one run per request is in flight. The approval page's research
section gains a Run Research button, polls while a run is live, and renders
the report with its coverage callout, tier chips, citation links, and run
footer.

# Platform MCP parity: catalogue source selection

- Target: reviewed catalogue discovery and detail inspection under the temporary organization feature flag.
- Actor: an authorized project-scoped Platform MCP caller (external OAuth or managed assistant), not staff administration.
- Existing tools: catalogue search/inspection and registration already use the shared catalogue facade; dashboard detail enrichment does not introduce a separate caller outcome.
- Decision: retain the existing tools, audiences, authorization, and schemas. No new tool or shipped workflow is needed for source selection. Registration continuation is handled separately.
- Success evidence: catalogue selection tests cover flag-based admission; the dashboard SSE regression test preserves native last-SSE and Pulse first-SSE metadata selection. Existing native projection tests cover first streamable-HTTP priority.

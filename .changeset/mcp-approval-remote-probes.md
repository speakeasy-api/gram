---
"server": minor
---

Evidence gathering for MCP approval requests now probes remote servers directly: their published OAuth metadata through the standard well-known endpoints (auth mode, scopes, dynamic client registration), and their tool declarations through an unauthenticated tools/list assessed for declared and schema-implied capability. Both are the server's own words about itself, gathered without credentials — a server that refuses to answer records a gap, never a clean empty section.

When the server refuses an unauthenticated tools/list, the gather now falls back to its MCP registry catalog entry, which carries the registry's copy of the tool declarations — labeled as registry-sourced, one step further from the server. A catalog match also fills a new provenance section (official flag, lifecycle status, publish/update recency, visitor estimates), and a new `mcpApproval.refreshEvidence` endpoint re-runs every source on demand, replacing the request's current evidence while leaving frozen decision snapshots untouched.

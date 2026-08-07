---
"server": minor
---

Evidence gathering for MCP approval requests now probes remote servers directly: their published OAuth metadata through the standard well-known endpoints (auth mode, scopes, dynamic client registration), and their tool declarations through an unauthenticated tools/list assessed for declared and schema-implied capability. Both are the server's own words about itself, gathered without credentials — a server that refuses to answer records a gap, never a clean empty section.

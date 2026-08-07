---
"server": minor
---

Adds the MCP approval management API. Admins can list servers awaiting a decision, open one to see the evidence gathered for it alongside any previous decisions, and record an approval or denial with a rationale and an explicit set of principals it covers. Gated by two new permissions so reviewing the queue and committing the organisation to a server can be granted separately.

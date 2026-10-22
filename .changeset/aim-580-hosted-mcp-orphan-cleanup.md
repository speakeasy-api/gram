---
"server": patch
---

Add a `-cleanup-orphans` mode to the hosted MCP wrappers backfill that removes toolset-keyed dependents whose toolset or project is deleted, journaled and audited like the dependent move.

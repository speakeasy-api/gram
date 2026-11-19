---
"server": minor
"dashboard": patch
---

Consolidating one MCP Authentication Provider into another no longer refuses when both hold a copy of the same dynamically registered OAuth client and nothing signs in through the source's copy. That copy is retired in favor of the target's, the same way clients resolved from one Client ID Metadata Document already merge. A copy that has a sign-in or approval (even an expired sign-in), that belongs to an assistant, or that an issuance selection profile pins, still blocks. The consolidation preview lists merged client IDs in the new `merged_client_ids` field, the dashboard says how many clients merge, and the consolidation audit event records them.

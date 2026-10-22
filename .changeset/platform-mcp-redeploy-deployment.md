---
"server": minor
---

Add the Platform MCP `redeploy_deployment` tool for previewing and confirming a whole-project deployment retry or rollback. Redeployment preserves the existing authorization and audit workflow, checks the active snapshot before cloning and again atomically before activation, and uses idempotency receipts so retries reuse the same deployment. Processing is dispatched without blocking the MCP call. Results report observed processing status rather than assuming a cloned deployment is ready.

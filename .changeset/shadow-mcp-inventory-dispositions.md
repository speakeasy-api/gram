---
"server": patch
"dashboard": minor
---

Shadow MCP inventory and status surfaces are now disposition-aware. Under an allow-all policy the inventory reports servers as allowed by default and blocked only when a block rule lists them, the policy status banner explains the allow-by-default posture, and the primary per-server action flips from managing allow rules to Block Server / Unblock Server — which add and remove the policy's risk_policy:block grants through dedicated inventory endpoints.

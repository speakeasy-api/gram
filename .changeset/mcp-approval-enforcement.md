---
"server": minor
---

MCP approval decisions now enforce: an approval replaces the server's risk-policy bypass audience with the decision's blast radius (defaulting to everyone, stored explicitly), and a denial revokes it — in the same transaction that records the decision, through the same grant machinery the shadow-MCP allow/block controls use, so the policy evaluator is unchanged. Under allow-by-default policies the directions invert (deny writes the block rule, approve clears it), and an approval narrower than everyone is rejected when no block-by-default policy can express it rather than silently widening. Granted principal URNs are validated at intake now that they become enforcement state.

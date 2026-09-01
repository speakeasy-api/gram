---
"server": minor
---

Authenticated hosted and private MCP tool calls now honor the internal AI-access kill switch alongside the customer-managed MCP tool-call kill switch. Both checks use the authenticated user and canonical MCP server identity in one fail-closed evaluation, while MCP-specific operator messages retain precedence when both switches match.

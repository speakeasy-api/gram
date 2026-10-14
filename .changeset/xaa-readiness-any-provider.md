---
"server": patch
---

The Platform MCP `get_xaa_readiness` tool lists a vendor's EMA providers in order with each one's availability, and reports identity-chaining readiness for vendors whose order names the Speakeasy issuer and for organizations without an Okta connection while the `ema-providers` rollout is on. Okta resource connection details are still reported when every provider is external and the organization has an Okta connection.

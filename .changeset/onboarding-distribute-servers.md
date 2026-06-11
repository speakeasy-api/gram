---
"dashboard": patch
---

Add a Distribute MCP servers step to onboarding. Teams can search the MCP catalog and distribute selected servers to their organization during setup, running the deploy → bundle → publish flow inline. Servers that require OAuth are auto-configured with Speakeasy OAuth, matching the catalog add-server flow. The onboarding list shows only servers Gram can fully auto-configure (OAuth with dynamic client registration); servers needing manual setup (OAuth without DCR, API keys) remain available in the catalog.

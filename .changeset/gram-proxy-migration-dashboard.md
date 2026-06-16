---
"dashboard": patch
---

When migrating a gram-managed OAuth proxy to a user session issuer, the wire flow now lifts the legacy dynamic-client registrations onto the new issuer (via `userSessionIssuers.migrateLegacyGramRegistrations`) before linking the toolset, so already-connected MCP clients keep working without re-registering or re-authenticating.

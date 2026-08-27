---
"server": minor
---

Publishes the first-party Platform MCP package from Speakeasy's public plugin marketplace (`speakeasy-api/marketplace`) rather than injecting it into every organization's private plugin repository. Registering that marketplace once also picks up any first-party plugin published there later.

The package never carried organization identity or credentials — it authenticates through its own OAuth flow — so a single global render serves every organization. A CI job renders the tree and pushes it on each change; installs become the same two commands for everyone (`/plugin marketplace add …`, `/plugin install speakeasy@speakeasy`), and OpenCode gains an install route it never had.

Access is unchanged and still fails closed: the runtime and OAuth paths already required both the `platform-mcp` rollout flag and the organization's `platform_mcp` entitlement, and remain the only gate. What goes away is the per-organization package machinery that decided whether a repository should contain the package — admission with an indeterminate state, a reserved fingerprint, preserve-and-repair carry logic, and the `getPlatformMCPPackageStatus`, `repairPlatformMCPPackage`, and `downloadPlatformMCPPlugin` endpoints.

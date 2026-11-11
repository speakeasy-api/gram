# RBAC decision log

Decisions taken while reworking authorization that a reviewer did not settle in advance. Each entry says what was decided, why, and how to reverse it. Pull requests repeat their entries under "Decisions made without reviewer input".

## Scope catalogue

- **One entry type for presentation and rules.** `ScopeEntry` in `server/internal/authz/catalog.go` now holds a scope's evaluation rules (visibility, grant surface, satisfying scopes, exception scope, resource kind) beside its label and description. `ResourceType` was already exported and stays so; `visibility`, `surface`, `satisfiedBy` and `blockedBy` are unexported, so only the authz package reads them. Callers keep the exported fields they used before. A separate `ScopeDef` type was not added: it would have meant two types describing one scope.
- **The old maps stay as derived indexes.** `scopeExpansions`, `scopeExclusions`, `scopeVisibilityByScope`, `scopeGrantSurfaces` and the new `scopeResourceKinds` are built from `scopeDefinitions` (the catalogue plus `rootScopeEntry`) at package initialisation. They are lookups, not definitions, so they were kept rather than replaced by linear scans.
- **Root is defined beside the catalogue, not in it.** `rootScopeEntry` is registered so stored root grants evaluate, but it is never listed or described.
- **Unregistered scope names keep their prefix-based resource kind.** `ResourceKindForScope` reads the catalogue first and falls back to the old prefix switch, so names the catalogue does not define (such as the retired `remote-mcp:*` family) answer as before.
- **Role defaults stay a role-keyed list** (`adminScopes`, `memberScopes`), as the plan decided. Agent runtime eligibility stays in `runtimepolicy` until its own follow-up.
- **Required check context is not part of this change.** The plan's per-scope `Selectors` and `Requires` fields, and log-only diagnostics for malformed legacy checks, change what the engine logs. They come in a separate pull request so this one stays a pure refactor pinned by the golden tables.

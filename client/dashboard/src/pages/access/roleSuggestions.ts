/** Pure helpers for suggesting roles that satisfy a requested scope. */

import type { Role } from "@gram/client/models/components/role.js";

/**
 * User-visible sub-scopes implied by each higher-privilege scope.
 * Mirrors the inverse of scopeExpansions in server/internal/authz/scopes.go
 * (internal blocklist scopes are intentionally omitted).
 */
const SUB_SCOPES: Record<string, readonly string[]> = {
  "org:admin": ["org:read"],
  "project:write": ["project:read"],
  "mcp:write": ["mcp:read", "mcp:connect"],
  "mcp:read": ["mcp:connect"],
  "environment:write": ["environment:read"],
  "skill:write": ["skill:read"],
};

/**
 * True when a grant of `grantScope` satisfies a check for `scope`, either
 * directly or via scope expansion (e.g. mcp:write covers mcp:connect).
 */
export function grantScopeCovers(grantScope: string, scope: string): boolean {
  if (grantScope === scope) return true;
  return SUB_SCOPES[grantScope]?.includes(scope) ?? false;
}

/**
 * Roles whose grants include the requested scope, preserving role order.
 */
export function rolesCoveringScope(roles: Role[], scope: string): Role[] {
  return roles.filter((role) =>
    role.grants.some((grant) => grantScopeCovers(grant.scope, scope)),
  );
}

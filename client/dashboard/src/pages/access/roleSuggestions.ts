/** Pure helpers for suggesting roles that satisfy a requested scope. */

import { resourceKindForScope, selectorMatches } from "@/hooks/useRBAC";
import type { Role } from "@gram/client/models/components/role.js";
import type { RoleGrant } from "@gram/client/models/components/rolegrant.js";

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
  "mcp_approval:decide": ["mcp_approval:read"],
};

/**
 * True when a grant of `grantScope` satisfies a check for `scope`, either
 * directly or via scope expansion (e.g. mcp:write covers mcp:connect).
 */
function grantScopeCovers(grantScope: string, scope: string): boolean {
  if (grantScope === scope) return true;
  return SUB_SCOPES[grantScope]?.includes(scope) ?? false;
}

/**
 * True when the grant applies to the requested resource. A grant with no
 * selectors is unrestricted; otherwise at least one selector must match the
 * resource (wildcards included), mirroring server-side selector matching.
 */
function grantCoversResource(
  grant: RoleGrant,
  scope: string,
  resourceId: string | undefined,
): boolean {
  if (!resourceId || !grant.selectors) return true;
  const check = { resourceKind: resourceKindForScope(scope), resourceId };
  return grant.selectors.some((selector) =>
    selectorMatches(
      Object.fromEntries(
        Object.entries(selector).filter(
          (entry): entry is [string, string] => typeof entry[1] === "string",
        ),
      ),
      check,
    ),
  );
}

/**
 * Roles whose grants include the requested scope (for the requested resource,
 * when one is given), preserving role order.
 */
export function rolesCoveringScope(
  roles: Role[],
  scope: string,
  resourceId?: string,
): Role[] {
  return roles.filter((role) =>
    role.grants.some(
      (grant) =>
        grantScopeCovers(grant.scope, scope) &&
        grantCoversResource(grant, scope, resourceId),
    ),
  );
}

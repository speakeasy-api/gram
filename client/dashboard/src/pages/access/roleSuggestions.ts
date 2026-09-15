/** Pure helpers for suggesting roles that satisfy a requested scope. */

import {
  resourceKindForScope,
  selectorMatches,
  selectorMatchesStrict,
} from "@/hooks/useRBAC";

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
function grantCoversSelector(
  grant: RoleGrant,
  check: Record<string, string>,
  strict: boolean,
): boolean {
  if (!grant.selectors) return true;
  const matches = strict ? selectorMatchesStrict : selectorMatches;
  return grant.selectors.some((selector) =>
    matches(
      Object.fromEntries(
        Object.entries(selector).filter(
          (entry): entry is [string, string] => typeof entry[1] === "string",
        ),
      ),
      check,
    ),
  );
}

function grantCoversResource(
  grant: RoleGrant,
  scope: string,
  resourceId: string | undefined,
  projectId: string | undefined,
  strict: boolean,
): boolean {
  if (!resourceId) return true;
  const check: Record<string, string> = {
    resourceKind: resourceKindForScope(scope),
    resourceId,
  };
  if (projectId) check.projectId = projectId;
  return grantCoversSelector(grant, check, strict);
}

/**
 * Roles whose grants include the requested scope (for the requested resource,
 * when one is given), preserving role order.
 */
export function rolesCoveringScope(
  roles: Role[],
  scope: string,
  resourceId?: string,
  projectId?: string,
): Role[] {
  return roles.filter((role) =>
    role.grants.some(
      (grant) =>
        grantScopeCovers(grant.scope, scope) &&
        grantCoversResource(grant, scope, resourceId, projectId, false),
    ),
  );
}

function normalizeCapturedSelector(
  selector: Record<string, string>,
): Record<string, string> {
  return Object.fromEntries(
    Object.entries(selector).map(([key, value]) => [
      key.replace(/_([a-z])/g, (_, letter: string) => letter.toUpperCase()),
      value,
    ]),
  );
}

/**
 * Challenge assignment must match every captured selector dimension.
 * Tool-specific or wrong-project roles stay hidden rather than being offered
 * and rejected by the server after confirmation.
 */
export function rolesCoveringChallengeScopes(
  roles: Role[],
  challenges: Array<{
    scope: string;
    selector?: Record<string, string>;
  }>,
): Role[] {
  const captured = challenges.map(({ scope, selector }) => ({
    scope,
    selector: selector ? normalizeCapturedSelector(selector) : undefined,
  }));
  if (
    captured.length === 0 ||
    captured.some(
      ({ selector }) => !selector?.resourceKind || !selector.resourceId,
    )
  ) {
    return [];
  }

  return roles.filter((role) =>
    captured.every(({ scope, selector }) =>
      role.grants.some(
        (grant) =>
          grantScopeCovers(grant.scope, scope) &&
          grantCoversSelector(grant, selector!, true),
      ),
    ),
  );
}

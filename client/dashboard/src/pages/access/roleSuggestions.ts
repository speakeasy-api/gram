/** Pure helpers for suggesting roles that satisfy a requested scope. */

import {
  resourceKindForScope,
  selectorMatches,
  selectorMatchesStrict,
} from "@/hooks/useRBAC";

import type { Role } from "@gram/client/models/components/role.js";
import type { RoleGrant } from "@gram/client/models/components/rolegrant.js";
import {
  isPresentableScope,
  isSensitiveScope,
  scopeClosure,
  scopeCovers,
  type ScopeCatalogue,
} from "./scopeCatalogue";

/**
 * True when the grant applies to the requested resource. A grant with no
 * selectors is unrestricted; otherwise at least one selector must match the
 * resource (wildcards included), mirroring server-side selector matching.
 */
function grantCoversSelector(
  grant: RoleGrant,
  check: Record<string, string>,
  strict: boolean,
  rejectProjectScopedWithoutProject = false,
): boolean {
  if (!grant.selectors) return true;
  const matches = strict ? selectorMatchesStrict : selectorMatches;
  return grant.selectors.some((selector) => {
    const normalized = Object.fromEntries(
      Object.entries(selector).filter(
        (entry): entry is [string, string] => typeof entry[1] === "string",
      ),
    );
    if (
      rejectProjectScopedWithoutProject &&
      normalized.projectId &&
      normalized.projectId !== "*"
    ) {
      return false;
    }
    return matches(normalized, check);
  });
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
  return grantCoversSelector(
    grant,
    check,
    strict,
    scope.startsWith("mcp:") && !projectId,
  );
}

/**
 * Roles whose grants include the requested scope (for the requested resource,
 * when one is given), preserving role order. Inclusion comes from the
 * catalogue: org:admin covers org:read because the server says so.
 */
export function rolesCoveringScope(
  roles: Role[],
  scope: string,
  catalogue: ScopeCatalogue,
  resourceId?: string,
  projectId?: string,
): Role[] {
  return roles.filter((role) =>
    role.grants.some(
      (grant) =>
        scopeCovers(catalogue, grant.scope, scope) &&
        grantCoversResource(grant, scope, resourceId, projectId, false),
    ),
  );
}

/** A role that satisfies the request but grants other permissions too. */
interface BroaderRole {
  role: Role;
  /** Permissions beyond the requested scope and what it includes. */
  extraScopes: string[];
  /** The subset of extraScopes the catalogue marks sensitive. */
  extraSensitive: string[];
}

export interface RoleSuggestions {
  /** Roles that give the requested scope and nothing more. */
  exact: Role[];
  /** Roles that give more, narrowest first. */
  broader: BroaderRole[];
}

/**
 * Everything a role lets a person do, as presentable scopes: each allow grant
 * expanded to the scopes it includes. Exceptions and provisioned auth-client
 * grants are not permissions a person chose, so they are left out.
 */
function presentableScopesOf(
  role: Role,
  catalogue: ScopeCatalogue,
): Set<string> {
  const scopes = new Set<string>();
  for (const grant of role.grants) {
    if (!isPresentableScope(catalogue, grant.scope)) continue;
    for (const scope of scopeClosure(catalogue, grant.scope)) {
      if (isPresentableScope(catalogue, scope)) scopes.add(scope);
    }
  }
  return scopes;
}

/**
 * Splits the roles that satisfy a request into the ones that give exactly
 * the requested permission and the ones that give more. A broad role such as
 * Admin stays usable; what else it grants, and which of that is sensitive,
 * is named so the cost of choosing it is visible.
 */
export function suggestRolesForScope(
  roles: Role[],
  scope: string,
  catalogue: ScopeCatalogue,
  resourceId?: string,
  projectId?: string,
): RoleSuggestions {
  const requested = new Set(scopeClosure(catalogue, scope));
  const exact: Role[] = [];
  const broader: BroaderRole[] = [];

  for (const role of rolesCoveringScope(
    roles,
    scope,
    catalogue,
    resourceId,
    projectId,
  )) {
    const extraScopes = [...presentableScopesOf(role, catalogue)].filter(
      (granted) => !requested.has(granted),
    );
    if (extraScopes.length === 0) {
      exact.push(role);
      continue;
    }
    broader.push({
      role,
      extraScopes,
      extraSensitive: extraScopes.filter((granted) =>
        isSensitiveScope(catalogue, granted),
      ),
    });
  }

  broader.sort((a, b) => a.extraScopes.length - b.extraScopes.length);
  return { exact, broader };
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
 * Challenge assignment keeps roles whose own selector dimensions all match the
 * captured selector. Roles narrower than the captured facts, such as a
 * tool-specific or wrong-project role, stay hidden. Roles that omit a captured
 * dimension remain eligible and are covered by the full-role confirmation.
 */
export function rolesCoveringChallengeScopes(
  roles: Role[],
  challenges: Array<{
    scope: string;
    selector?: Record<string, string>;
  }>,
  catalogue: ScopeCatalogue,
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
          scopeCovers(catalogue, grant.scope, scope) &&
          grantCoversSelector(grant, selector!, true),
      ),
    ),
  );
}

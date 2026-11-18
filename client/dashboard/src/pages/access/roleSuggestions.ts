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

type CheckSelector = Record<string, string>;

/** The grant's selectors as plain string maps; null when it is unrestricted. */
function normalizedSelectors(grant: RoleGrant): CheckSelector[] | null {
  if (!grant.selectors) return null;
  return grant.selectors.map((selector) =>
    Object.fromEntries(
      Object.entries(selector).filter(
        (entry): entry is [string, string] => typeof entry[1] === "string",
      ),
    ),
  );
}

/**
 * True when the grant applies to the requested resource. A grant with no
 * selectors is unrestricted; otherwise at least one selector must match the
 * resource (wildcards included), mirroring server-side selector matching.
 */
function grantCoversSelector(
  grant: RoleGrant,
  check: CheckSelector,
  strict: boolean,
  rejectProjectScopedWithoutProject = false,
): boolean {
  const selectors = normalizedSelectors(grant);
  if (selectors === null) return true;
  const matches = strict ? selectorMatchesStrict : selectorMatches;
  return selectors.some((selector) => {
    if (
      rejectProjectScopedWithoutProject &&
      selector.projectId &&
      selector.projectId !== "*"
    ) {
      return false;
    }
    return matches(selector, check);
  });
}

/** The selector a request for `scope` on a resource is checked with. */
function resourceCheck(
  scope: string,
  resourceId: string | undefined,
  projectId: string | undefined,
): CheckSelector | null {
  if (!resourceId) return null;
  const check: CheckSelector = {
    resourceKind: resourceKindForScope(scope),
    resourceId,
  };
  if (projectId) check.projectId = projectId;
  return check;
}

function grantCoversResource(
  grant: RoleGrant,
  scope: string,
  resourceId: string | undefined,
  projectId: string | undefined,
  strict: boolean,
): boolean {
  const check = resourceCheck(scope, resourceId, projectId);
  if (!check) return true;
  return grantCoversSelector(
    grant,
    check,
    strict,
    scope.startsWith("mcp:") && !projectId,
  );
}

/**
 * True when the role itself blocks the scope on the requested resource: it
 * holds the scope's exclusion scope, or a broader exclusion that includes it,
 * on a selector that matches the request. The permission the role grants
 * elsewhere would not apply there, so the role is no answer to the request.
 * Without a resource only an unrestricted block counts.
 */
function roleBlocksScope(
  role: Role,
  scope: string,
  catalogue: ScopeCatalogue,
  check: CheckSelector | null,
): boolean {
  const exclusion = catalogue.get(scope)?.exclusionScope;
  if (!exclusion) return false;
  return role.grants.some(
    (grant) =>
      scopeCovers(catalogue, grant.scope, exclusion) &&
      (check ? grantCoversSelector(grant, check, true) : !grant.selectors),
  );
}

/**
 * How far a grant reaches within its resource kind: one resource, one
 * project, or every one there is. Narrower is better for a request.
 */
export type GrantBreadth = "resource" | "project" | "all";

const BREADTH_RANK: Record<GrantBreadth, number> = {
  resource: 0,
  project: 1,
  all: 2,
};

function selectorBreadth(
  selector: CheckSelector,
  resourceKind: string,
): GrantBreadth {
  if (selector.resourceId && selector.resourceId !== "*") {
    return resourceKind === "project" ? "project" : "resource";
  }
  if (selector.projectId && selector.projectId !== "*") return "project";
  return "all";
}

function broadest(breadths: GrantBreadth[]): GrantBreadth {
  return breadths.reduce(
    (widest, breadth) =>
      BREADTH_RANK[breadth] > BREADTH_RANK[widest] ? breadth : widest,
    "resource",
  );
}

/** Everything the grant reaches, not only the part that matched the request. */
function grantBreadth(grant: RoleGrant, scope: string): GrantBreadth {
  const selectors = normalizedSelectors(grant);
  if (selectors === null || selectors.length === 0) return "all";
  const kind = resourceKindForScope(scope);
  return broadest(selectors.map((selector) => selectorBreadth(selector, kind)));
}

const KIND_PLURALS: Record<string, string> = {
  project: "projects",
  mcp: "MCP servers",
  skill: "skills",
  assistant: "assistants",
  risk_policy: "risk policies",
  inference: "projects",
};

/**
 * Says how far a role's grant of the scope reaches, so an unrestricted grant
 * is not mistaken for one limited to the request: "across all projects" or
 * "limited to specific MCP servers". Null for kinds that cannot be narrowed.
 */
export function breadthLabel(
  scope: string,
  breadth: GrantBreadth,
): string | null {
  const plural = KIND_PLURALS[resourceKindForScope(scope)];
  if (!plural) return null;
  switch (breadth) {
    case "all":
      return `across all ${plural}`;
    case "project":
      return "limited to specific projects";
    case "resource":
      return `limited to specific ${plural}`;
  }
}

/** A role that satisfies the request, and how far its grant reaches. */
export interface SuggestedRole {
  role: Role;
  breadth: GrantBreadth;
}

/**
 * Roles whose grants include the requested scope (for the requested resource,
 * when one is given) and do not block it there, preserving role order.
 * Inclusion comes from the catalogue: org:admin covers org:read because the
 * server says so.
 */
function coveringRoles(
  roles: Role[],
  scope: string,
  catalogue: ScopeCatalogue,
  resourceId?: string,
  projectId?: string,
): SuggestedRole[] {
  const check = resourceCheck(scope, resourceId, projectId);
  const suggested: SuggestedRole[] = [];
  for (const role of roles) {
    const covering = role.grants.filter(
      (grant) =>
        scopeCovers(catalogue, grant.scope, scope) &&
        grantCoversResource(grant, scope, resourceId, projectId, false),
    );
    if (covering.length === 0) continue;
    if (roleBlocksScope(role, scope, catalogue, check)) continue;
    suggested.push({
      role,
      breadth: broadest(covering.map((grant) => grantBreadth(grant, scope))),
    });
  }
  return suggested;
}

export function rolesCoveringScope(
  roles: Role[],
  scope: string,
  catalogue: ScopeCatalogue,
  resourceId?: string,
  projectId?: string,
): Role[] {
  return coveringRoles(roles, scope, catalogue, resourceId, projectId).map(
    ({ role }) => role,
  );
}

/** A role that satisfies the request but grants other permissions too. */
interface BroaderRole extends SuggestedRole {
  /** Permissions beyond the requested scope and what it includes. */
  extraScopes: string[];
  /** The subset of extraScopes the catalogue marks sensitive. */
  extraSensitive: string[];
}

export interface RoleSuggestions {
  /** Roles that give the requested scope and nothing more, narrowest first. */
  exact: SuggestedRole[];
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
 * is named so the cost of choosing it is visible. Within each group a role
 * limited to the requested resource or project comes before one that grants
 * the scope everywhere.
 */
export function suggestRolesForScope(
  roles: Role[],
  scope: string,
  catalogue: ScopeCatalogue,
  resourceId?: string,
  projectId?: string,
): RoleSuggestions {
  const requested = new Set(scopeClosure(catalogue, scope));
  const exact: SuggestedRole[] = [];
  const broader: BroaderRole[] = [];

  for (const suggested of coveringRoles(
    roles,
    scope,
    catalogue,
    resourceId,
    projectId,
  )) {
    const extraScopes = [
      ...presentableScopesOf(suggested.role, catalogue),
    ].filter((granted) => !requested.has(granted));
    if (extraScopes.length === 0) {
      exact.push(suggested);
      continue;
    }
    broader.push({
      ...suggested,
      extraScopes,
      extraSensitive: extraScopes.filter((granted) =>
        isSensitiveScope(catalogue, granted),
      ),
    });
  }

  exact.sort((a, b) => BREADTH_RANK[a.breadth] - BREADTH_RANK[b.breadth]);
  broader.sort(
    (a, b) =>
      BREADTH_RANK[a.breadth] - BREADTH_RANK[b.breadth] ||
      a.extraScopes.length - b.extraScopes.length,
  );
  return { exact, broader };
}

function normalizeCapturedSelector(selector: CheckSelector): CheckSelector {
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
 * tool-specific or wrong-project role, stay hidden, as do roles that block
 * the scope on the captured resource. Roles that omit a captured dimension
 * remain eligible and are covered by the full-role confirmation.
 */
export function rolesCoveringChallengeScopes(
  roles: Role[],
  challenges: Array<{
    scope: string;
    selector?: CheckSelector;
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
    captured.every(
      ({ scope, selector }) =>
        role.grants.some(
          (grant) =>
            scopeCovers(catalogue, grant.scope, scope) &&
            grantCoversSelector(grant, selector!, true),
        ) && !roleBlocksScope(role, scope, catalogue, selector!),
    ),
  );
}

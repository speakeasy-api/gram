import type {
  RoleGrant,
  Scope,
} from "@gram/client/models/components/rolegrant.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import type { ScopeKey } from "./accessRows";

/** The stored scopes behind one line: the grant and the block that undoes it. */
export const LINE_SCOPES: Record<ScopeKey, { allow: Scope; block: Scope }> = {
  use: { allow: "mcp:connect", block: "mcp:blocked_connect" },
  view: { allow: "mcp:read", block: "mcp:blocked_read" },
  manage: { allow: "mcp:write", block: "mcp:blocked_write" },
};

export interface WildcardConversionInput {
  /** Every grant the role holds, as Role Manager stores them. */
  grants: RoleGrant[];
  /** The lines to take off this server. */
  lines: ScopeKey[];
  /** The server being cleared. */
  resourceId: string;
  /** Every id the server being cleared is granted under. */
  resourceIds: string[];
  /** The project the server belongs to. */
  projectId: string;
  /** Every grant id of the project's servers, the cleared one included. */
  projectServerIds: string[];
  /** The organization's other projects. */
  otherProjectIds: string[];
}

export interface WildcardConversion {
  addGrants: RoleGrant[];
  removeGrants: RoleGrant[];
  /** True when a rule covering every server was among those replaced. */
  convertsWildcard: boolean;
  /** True when a replaced rule reached every project, not only this one. */
  coveredEveryProject: boolean;
}

/**
 * Takes the given lines off one server for a role whose rule covers every
 * server. A rule cannot cover every server but one, so each such rule is
 * replaced: by the same rule naming each other server in this project, and,
 * when it reached every project, by a rule covering every server in each other
 * project, which leaves those projects as they were. The role's own rules
 * naming this server for those lines are removed too. Narrowing on a rule
 * (tools, annotations) is carried onto every rule that replaces it.
 */
export function convertWildcardGrants(
  input: WildcardConversionInput,
): WildcardConversion {
  const scopes = new Set<Scope>(
    input.lines.flatMap((line) => [
      LINE_SCOPES[line].allow,
      LINE_SCOPES[line].block,
    ]),
  );
  const clearedIds = new Set(input.resourceIds);
  const otherServerIds = [...new Set(input.projectServerIds)].filter(
    (id) => !clearedIds.has(id),
  );

  const additions = new Map<Scope, Selector[]>();
  const removeGrants: RoleGrant[] = [];
  let convertsWildcard = false;
  let coveredEveryProject = false;
  const add = (scope: Scope, selectors: Selector[]) => {
    if (selectors.length === 0) return;
    additions.set(scope, [...(additions.get(scope) ?? []), ...selectors]);
  };
  const expand = (selector: Selector): Selector[] => {
    const { projectId, ...rest } = selector;
    const base: Selector = { ...rest, resourceKind: "mcp" };
    const servers = otherServerIds.map((id) => ({ ...base, resourceId: id }));
    if (projectId) return servers;
    coveredEveryProject = true;
    return [
      ...servers,
      ...input.otherProjectIds.map((id) => ({
        ...base,
        resourceId: "*",
        projectId: id,
      })),
    ];
  };

  for (const grant of input.grants) {
    if (!scopes.has(grant.scope)) continue;
    const selectors = grant.selectors ?? [];
    if (selectors.length === 0) {
      // Stored without selectors, the rule is unrestricted, and it is removed
      // the same way. That removal deletes only rules naming no project, so
      // the role's rules confined to other projects stay as they are.
      removeGrants.push({ scope: grant.scope });
      add(grant.scope, expand({ resourceKind: "mcp", resourceId: "*" }));
      convertsWildcard = true;
      continue;
    }
    for (const selector of selectors) {
      if (coversThisProject(selector, input.projectId)) {
        removeGrants.push({ scope: grant.scope, selectors: [selector] });
        add(grant.scope, expand(selector));
        convertsWildcard = true;
      } else if (
        selector.resourceKind === "mcp" &&
        clearedIds.has(selector.resourceId)
      ) {
        removeGrants.push({ scope: grant.scope, selectors: [selector] });
      }
    }
  }

  return {
    addGrants: [...additions].map(([scope, selectors]) => ({
      scope,
      selectors,
    })),
    removeGrants,
    convertsWildcard,
    coveredEveryProject,
  };
}

/** Whether a role holds a rule covering every server for any of these lines. */
export function holdsWildcard(
  grants: RoleGrant[],
  lines: ScopeKey[],
  projectId: string,
): boolean {
  const scopes = new Set<Scope>(
    lines.flatMap((line) => [LINE_SCOPES[line].allow, LINE_SCOPES[line].block]),
  );
  return grants.some(
    (grant) =>
      scopes.has(grant.scope) &&
      ((grant.selectors ?? []).length === 0 ||
        (grant.selectors ?? []).some((selector) =>
          coversThisProject(selector, projectId),
        )),
  );
}

function isWildcard(selector: Selector): boolean {
  return (
    (selector.resourceKind === "*" || selector.resourceKind === "mcp") &&
    selector.resourceId === "*"
  );
}

/** A rule covering every server here: unconfined, or confined to this project. */
function coversThisProject(selector: Selector, projectId: string): boolean {
  return (
    isWildcard(selector) &&
    (selector.projectId === undefined || selector.projectId === projectId)
  );
}

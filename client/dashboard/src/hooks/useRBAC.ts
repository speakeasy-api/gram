import { useIsPlatformAdmin, useSession } from "@/contexts/Auth";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { useGrants } from "@gram/client/react-query/grants.js";
import {
  type Check,
  scopeExclusions,
  scopeOpenKeys,
  scopeResourceKinds,
} from "@/lib/authz.gen";
import { useCallback, useEffect, useMemo, useState } from "react";

// Open SDK enums accept future values, but only understood scopes may authorize.
const knownScopes: ReadonlySet<string> = new Set(Object.values(Scope));

const declaredResourceKinds: ReadonlySet<string> = new Set(
  Object.values(scopeResourceKinds),
);

/**
 * The resource kind a scope's checks and grant selectors name, from the
 * registry (lib/authz.gen.ts). Scopes outside it keep the server's legacy
 * answer (legacyResourceKindForScope in authz/selector.go). mcp:auth is not
 * checkable client-side: the server binds the connection owner.
 */
export function resourceKindForScope(scope: string): string {
  if (Object.hasOwn(scopeResourceKinds, scope))
    return scopeResourceKinds[scope] as string;
  const prefix = scope.split(":")[0] ?? "";
  if (prefix === "plugin" || prefix === "log") return "project";
  if (prefix === "remote-mcp") return "mcp";
  if (declaredResourceKinds.has(prefix) && prefix !== "mcp_auth_client")
    return prefix;
  return "*";
}

/**
 * Check if a grant selector matches a check selector.
 * Mirrors how the server matches a probe (Check.Probe in authz/access.go):
 * the dashboard asks whether to show a control, not whether to perform an
 * action, so it asks the server's discovery question.
 *
 * For each key in the grant selector, if the check also has that key the
 * values must match (or the grant value must be "*"). Keys present in the
 * grant but absent from the check are skipped. The server refuses such an
 * omission on every request it decides, except a probe, a connection and a
 * risk policy audience question.
 */
export function selectorMatches(
  grantSelector: Record<string, string>,
  checkSelector: Record<string, string>,
): boolean {
  for (const [key, grantVal] of Object.entries(grantSelector)) {
    const checkVal = checkSelector[key];
    if (checkVal === undefined) continue;
    if (grantVal !== "*" && grantVal !== checkVal) return false;
  }
  return true;
}

/** Mirrors Selector.StrictMatches for exclusion grants. */
export function selectorMatchesStrict(
  grantSelector: Record<string, string>,
  checkSelector: Record<string, string>,
): boolean {
  for (const [key, grantVal] of Object.entries(grantSelector)) {
    const checkVal = checkSelector[key];
    if (checkVal === undefined) return false;
    if (grantVal !== "*" && grantVal !== checkVal) return false;
  }
  return true;
}

/**
 * The exclusion scopes a check of scope must clear, from the registry
 * (lib/authz.gen.ts): its own exclusion, then the broader blocks that also
 * cover it.
 */
export function exclusionScopesForScope(scope: Scope): readonly string[] {
  return scopeExclusions[scope] ?? [];
}

interface EffectiveGrant {
  scope?: string;
  selectors?: Array<Record<string, string>>;
  /**
   * Selectors granted to the caller by name. For allow scopes these name a
   * concrete resource and outrank blocks inherited from a role or everyone;
   * for blocked scopes they are the caller's own blocks, which always apply.
   */
  directSelectors?: Array<Record<string, string>>;
  subScopes?: string[];
}

function grantSelectorsMatch(
  grant: EffectiveGrant,
  check: Record<string, string>,
  strict: boolean,
): boolean {
  if (!grant.selectors) return true;
  const matches = strict ? selectorMatchesStrict : selectorMatches;
  return grant.selectors.some((selector) => matches(selector, check));
}

function directSelectorsMatch(
  grant: EffectiveGrant,
  check: Record<string, string>,
  strict: boolean,
): boolean {
  const matches = strict ? selectorMatchesStrict : selectorMatches;
  return (grant.directSelectors ?? []).some((selector) =>
    matches(selector, check),
  );
}

/**
 * Blocklist exclusions (`*:blocked_*`) yield to a direct grant naming the
 * resource. Other exclusions such as risk_policy:bypass never do. Mirrors
 * ExclusionYieldsToDirectGrants in server/internal/authz/precedence.go.
 */
function exclusionYieldsToDirectGrants(exclusionScope: string): boolean {
  return exclusionScope.includes(":blocked_");
}

/**
 * Pure equivalent of hasScope for loaded effective grants. Multiple resource IDs
 * accept any allow, but an exclusion on ANY alternative wins (RequireAnyUnblocked).
 * A block inherited from a role or everyone does not win on an alternative
 * that a direct grant naming the resource satisfies; the caller's own block
 * always does.
 */
export function hasScopeInGrants(
  grants: EffectiveGrant[],
  scope: Scope,
  resourceId?: string | readonly string[],
  projectId?: string,
): boolean {
  if (!knownScopes.has(scope)) return false;
  // Only the server can bind the connection owner of an auth-client check.
  if (resourceKindForScope(scope) === "mcp_auth_client") return false;

  const resourceIds =
    typeof resourceId === "string" || resourceId === undefined
      ? [resourceId]
      : resourceId;
  const allowChecks = resourceIds.map((id) => {
    const allowCheck: Record<string, string> = {
      resourceKind: resourceKindForScope(scope),
    };
    if (id) allowCheck.resourceId = id;
    if (projectId) allowCheck.projectId = projectId;
    return allowCheck;
  });
  // Unscoped allows are existential, but strict exclusions must distinguish
  // unrestricted wildcards from exclusions for one concrete resource.
  const exclusionChecks = allowChecks.map((check) =>
    check.resourceId ? check : { ...check, resourceId: "*" },
  );

  const exclusionScopes = exclusionScopesForScope(scope);
  let hasAllow = false;
  // Per alternative: whether a block that cannot be outranked matched, whether
  // a block inherited from a role or everyone matched, and whether a direct
  // grant naming the resource satisfies it.
  const absoluteBlock = allowChecks.map(() => false);
  const inheritedBlock = allowChecks.map(() => false);
  const directAllow = allowChecks.map(() => false);

  for (const grant of grants) {
    // An unknown parent must not authorize through its known sub-scopes either.
    if (grant.scope === undefined || !knownScopes.has(grant.scope)) continue;
    if (exclusionScopes.includes(grant.scope)) {
      const yields = exclusionYieldsToDirectGrants(grant.scope);
      exclusionChecks.forEach((check, i) => {
        if (!grantSelectorsMatch(grant, check, true)) return;
        if (yields && !directSelectorsMatch(grant, check, true)) {
          inheritedBlock[i] = true;
        } else {
          absoluteBlock[i] = true;
        }
      });
    }

    const scopeMatches =
      grant.scope === scope || grant.subScopes?.includes(scope);
    if (scopeMatches) {
      allowChecks.forEach((check, i) => {
        if (grantSelectorsMatch(grant, check, false)) hasAllow = true;
        if (directSelectorsMatch(grant, check, false)) directAllow[i] = true;
      });
    }
  }

  const blocked = allowChecks.some(
    (_, i) => absoluteBlock[i] || (inheritedBlock[i] && !directAllow[i]),
  );
  return hasAllow && !blocked;
}

/** Any allow is sufficient, but an exclusion on any alternative wins. */
export function hasAnyUnblockedScopeInGrants(
  grants: EffectiveGrant[],
  checks: readonly { scope: Scope; resourceId: string; projectId?: string }[],
): boolean {
  return (
    checks.every(({ scope, resourceId, projectId }) =>
      // A synthetic allow probes only exclusions; real allows are checked below.
      hasScopeInGrants([...grants, { scope }], scope, resourceId, projectId),
    ) &&
    checks.some(({ scope, resourceId, projectId }) =>
      hasScopeInGrants(grants, scope, resourceId, projectId),
    )
  );
}

export function hasScopeInProject(
  grants: EffectiveGrant[],
  scope: Scope,
  projectId: string,
): boolean {
  return hasScopeInGrants(grants, scope, "*", projectId);
}

/** One MCP server, such as a gateway, in the project it belongs to. */
export interface MCPServerTarget {
  serverId: string;
  projectId: string;
}

/** Mirrors authz.MCPServer(id, project).Write() on the server. */
export function canWriteMCPServerInGrants(
  grants: EffectiveGrant[],
  { serverId, projectId }: MCPServerTarget,
): boolean {
  return hasScopeInGrants(grants, "mcp:write", serverId, projectId);
}

/**
 * Mirrors authz.MCPServers(project).Write() on the server, which names the
 * project as the resource: write access to the project's MCP servers as a
 * whole, such as adding one.
 */
export function canWriteProjectMCPServersInGrants(
  grants: EffectiveGrant[],
  projectId: string,
): boolean {
  return hasScopeInGrants(grants, "mcp:write", projectId, projectId);
}

/**
 * Whether a grant selector covers a check built by a constructor in
 * lib/authz.gen.ts, as Check.matchesAllowSelector does on the server: every
 * key the grant names must be carried with a matching value, except the keys
 * the check's scope opens on a probe or a connection.
 */
function allowSelectorMatches(
  grantSelector: Record<string, string>,
  checkSelector: Record<string, string>,
  open: readonly string[],
): boolean {
  for (const [key, grantVal] of Object.entries(grantSelector)) {
    const checkVal = checkSelector[key];
    if (checkVal === undefined) {
      if (open.includes(key)) continue;
      return false;
    }
    if (grantVal !== "*" && grantVal !== checkVal) return false;
  }
  return true;
}

function openKeysFor(check: Check): readonly string[] {
  const open = scopeOpenKeys[check.scope];
  if (!open) return [];
  return [
    ...(check.isProbe ? open.probe : []),
    ...(check.connection ? open.connection : []),
  ];
}

function checkSelector(
  check: Check,
  resourceId: string,
): Record<string, string> {
  return {
    resourceKind: check.resourceKind,
    resourceId,
    ...check.dimensions,
  };
}

/** One check on one resource, with blocks and direct-grant precedence. */
function allowedOnResource(
  grants: EffectiveGrant[],
  check: Check,
  resourceId: string,
): boolean {
  const selector = checkSelector(check, resourceId);
  const open = openKeysFor(check);
  const allowMatches = (selectors: Selectors | undefined) =>
    selectors === undefined
      ? true
      : selectors.some((s) => allowSelectorMatches(s, selector, open));
  const exclusionScopes = exclusionScopesForScope(check.scope);

  let allowed = false;
  let directAllow = false;
  let absoluteBlock = false;
  let inheritedBlock = false;
  for (const grant of grants) {
    if (grant.scope === undefined || !knownScopes.has(grant.scope)) continue;
    if (exclusionScopes.includes(grant.scope)) {
      if (grantSelectorsMatch(grant, selector, true)) {
        if (
          exclusionYieldsToDirectGrants(grant.scope) &&
          !directSelectorsMatch(grant, selector, true)
        ) {
          inheritedBlock = true;
        } else {
          absoluteBlock = true;
        }
      }
    }
    if (grant.scope === check.scope || grant.subScopes?.includes(check.scope)) {
      if (allowMatches(grant.selectors)) allowed = true;
      if (allowMatches(grant.directSelectors ?? [])) directAllow = true;
    }
  }
  return allowed && !absoluteBlock && !(inheritedBlock && !directAllow);
}

type Selectors = Array<Record<string, string>>;

/**
 * Whether the server would allow a check built by a constructor in
 * lib/authz.gen.ts. Unlike hasScopeInGrants it never skips a key the check
 * leaves out: the constructors carry every key the server needs. A probe on
 * a collection asks about any resource of the kind in that parent: the
 * collection itself, or a resource some allow grant reaches.
 */
export function canInGrants(grants: EffectiveGrant[], check: Check): boolean {
  if (check.invalid) {
    if (import.meta.env.DEV) {
      console.error("invalid authz check: an id cannot name a resource", check);
    }
    return false;
  }
  if (!knownScopes.has(check.scope)) return false;
  if (allowedOnResource(grants, check, check.resourceId)) return true;
  if (!check.collection || !check.isProbe) return false;

  const candidates = new Set<string>();
  for (const grant of grants) {
    if (grant.scope !== check.scope && !grant.subScopes?.includes(check.scope))
      continue;
    for (const { resourceId = "*" } of grant.selectors ?? [{}]) {
      // A wildcard grant reaches resources no grant names.
      candidates.add(resourceId === "*" ? anyUnnamedResource : resourceId);
    }
  }
  for (const resourceId of candidates) {
    if (allowedOnResource(grants, check, resourceId)) return true;
  }
  return false;
}

/** A resource id no grant names, standing for every resource of that kind. */
const anyUnnamedResource = "\u0000";

/**
 * Core RBAC hook. Fetches the current user's effective grants and provides
 * helpers to check whether the user holds a particular scope.
 */
function useRBACImpl() {
  const isAdmin = useIsPlatformAdmin();
  const session = useSession();

  // Re-render when the toolbar changes scopes in localStorage.
  const [, setOverrideVersion] = useState(0);
  useEffect(() => {
    if (!import.meta.env.DEV && !isAdmin) return;
    const handler = () => setOverrideVersion((v) => v + 1);
    window.addEventListener("rbac-override-change", handler);
    return () => window.removeEventListener("rbac-override-change", handler);
  }, [isAdmin]);

  // Always fetch grants so we can detect a broken org membership (404/403) and
  // show a recovery prompt via MembershipSyncGuard. throwOnError is disabled
  // so the error doesn't crash the app; it's surfaced via `error` instead.
  const { data, isLoading, error } = useGrants(
    { gramSession: session.session },
    undefined,
    {
      staleTime: 30_000,
      throwOnError: false,
    },
  );

  // The toolbar event re-renders this hook; query invalidation refreshes the grants.
  const grants = useMemo(() => {
    return data?.grants;
  }, [data?.grants]);

  /**
   * Check if the user has a given scope, optionally scoped to a resource ID
   * and the project that resource belongs to. Passing `projectId` lets
   * project-wide grants (`{ resourceId: "*", projectId }`) cover the resource
   * and keeps grants for other projects from matching.
   *
   * Uses exclusion-wins semantics: matching internal exclusion grants
   * override matching allow grants.
   *
   * - If grants haven't loaded yet, returns false (safe default).
   * - A grant with `selectors: undefined` (null from the API) means unrestricted.
   * - A grant with `selectors: [...]` means the scope is constrained by selectors.
   */
  const hasScope = useCallback(
    (scope: Scope, resourceId?: string, projectId?: string): boolean => {
      if (!grants) return false;

      return hasScopeInGrants(grants, scope, resourceId, projectId);
    },
    [grants],
  );

  /**
   * Check multiple scopes at once. Returns true if the user has ALL of them.
   */
  const hasAllScopes = useCallback(
    (scopes: Scope[], resourceId?: string, projectId?: string): boolean => {
      return scopes.every((scope) => hasScope(scope, resourceId, projectId));
    },
    [hasScope],
  );

  /**
   * Check multiple scopes at once. Returns true if the user has ANY of them.
   */
  const hasAnyScope = useCallback(
    (scopes: Scope[], resourceId?: string, projectId?: string): boolean => {
      return scopes.some((scope) => hasScope(scope, resourceId, projectId));
    },
    [hasScope],
  );

  /**
   * Whether the server would allow check, built by a constructor in
   * lib/authz.gen.ts: can(authz.mcpServer(id, projectId).write()).
   */
  const can = useCallback(
    (check: Check): boolean => !!grants && canInGrants(grants, check),
    [grants],
  );

  const hasAnyScopeInProject = useCallback(
    (scopes: Scope[], projectId: string): boolean => {
      return scopes.some((scope) =>
        hasScopeInProject(grants ?? [], scope, projectId),
      );
    },
    [grants],
  );

  const canWriteMCPServer = useCallback(
    (target: MCPServerTarget): boolean =>
      !!grants && canWriteMCPServerInGrants(grants, target),
    [grants],
  );

  const canWriteProjectMCPServers = useCallback(
    (projectId: string): boolean =>
      !!grants && canWriteProjectMCPServersInGrants(grants, projectId),
    [grants],
  );

  return useMemo(
    () => ({
      can,
      hasScope,
      hasAllScopes,
      hasAnyScope,
      hasAnyScopeInProject,
      canWriteMCPServer,
      canWriteProjectMCPServers,
      isLoading,
      grants: grants ?? [],
      /** Non-null when the grants query failed (e.g. missing org membership). */
      error: error ?? null,
    }),
    [
      can,
      hasScope,
      hasAllScopes,
      hasAnyScope,
      hasAnyScopeInProject,
      canWriteMCPServer,
      canWriteProjectMCPServers,
      isLoading,
      grants,
      error,
    ],
  );
}

export function useRBAC(): ReturnType<typeof useRBACImpl> {
  return useRBACImpl();
}

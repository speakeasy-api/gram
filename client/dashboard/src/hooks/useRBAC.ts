import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useProductTier } from "@/hooks/useProductTier";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { useGrants } from "@gram/client/react-query/grants.js";
import { useCallback, useEffect, useMemo, useState } from "react";

/**
 * Derive the resource kind from a scope's family prefix.
 * Mirrors the server-side ResourceKindForScope in authz/selector.go.
 */
export function resourceKindForScope(scope: string): string {
  if (scope.startsWith("project:")) return "project";
  if (scope.startsWith("remote-mcp:") || scope.startsWith("mcp:")) return "mcp";
  if (scope.startsWith("org:")) return "org";
  if (scope.startsWith("role:")) return "role";
  if (scope.startsWith("environment:")) return "environment";
  if (scope.startsWith("skill:")) return "skill";
  if (scope.startsWith("risk_policy:")) return "risk_policy";
  if (scope.startsWith("chat:")) return "chat";
  return "*";
}

/**
 * Check if a grant selector matches a check selector.
 * Mirrors the server-side Selector.Matches in authz/selector.go.
 *
 * For each key in the grant selector, if the check also has that key the
 * values must match (or the grant value must be "*"). Keys present in the
 * grant but absent from the check are skipped — the check isn't constraining
 * that dimension.
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

/** Direct exclusion scope plus its one-step server scope expansions. */
const exclusionScopesByScope: Partial<Record<Scope, readonly string[]>> = {
  "org:read": ["org:blocked_read"],
  "org:admin": ["org:blocked_admin", "org:blocked_read"],
  "project:read": ["project:blocked_read"],
  "project:write": ["project:blocked_write", "project:blocked_read"],
  "mcp:read": ["mcp:blocked_read", "mcp:blocked_connect"],
  "mcp:write": ["mcp:blocked_write", "mcp:blocked_read", "mcp:blocked_connect"],
  "mcp:connect": ["mcp:blocked_connect"],
  "environment:read": ["environment:blocked_read"],
  "environment:write": [
    "environment:blocked_write",
    "environment:blocked_read",
  ],
  "skill:read": ["skill:blocked_read"],
  "skill:write": ["skill:blocked_write", "skill:blocked_read"],
  "risk_policy:evaluate": ["risk_policy:bypass"],
};

export function exclusionScopesForScope(scope: Scope): readonly string[] {
  return exclusionScopesByScope[scope] ?? [];
}

interface EffectiveGrant {
  scope?: string;
  selectors?: Array<Record<string, string>>;
  subScopes?: string[];
  effect?: string;
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

/** Pure equivalent of hasScope for loaded effective grants. */
export function hasScopeInGrants(
  grants: EffectiveGrant[],
  scope: Scope,
  resourceId?: string,
): boolean {
  const allowCheck: Record<string, string> = {
    resourceKind: resourceKindForScope(scope),
  };
  if (resourceId) allowCheck.resourceId = resourceId;
  // Unscoped allows are existential, but strict exclusions must distinguish
  // unrestricted wildcards from exclusions for one concrete resource.
  const exclusionCheck = resourceId
    ? allowCheck
    : { ...allowCheck, resourceId: "*" };

  const exclusionScopes = exclusionScopesForScope(scope);
  let hasAllow = false;

  for (const grant of grants) {
    const effect = grant.effect || "allow";

    const isLegacyDeny = effect === "deny" && grant.scope === scope;
    const isExclusion =
      effect === "allow" &&
      grant.scope !== undefined &&
      exclusionScopes.includes(grant.scope);
    if (
      (isLegacyDeny || isExclusion) &&
      grantSelectorsMatch(grant, exclusionCheck, true)
    ) {
      return false;
    }

    const scopeMatches =
      grant.scope === scope || grant.subScopes?.includes(scope);
    if (
      effect === "allow" &&
      scopeMatches &&
      grantSelectorsMatch(grant, allowCheck, false)
    ) {
      hasAllow = true;
    }
  }

  return hasAllow;
}

/**
 * Core RBAC hook. Fetches the current user's effective grants and provides
 * helpers to check whether the user holds a particular scope.
 *
 * When RBAC is disabled via feature flag (and no dev override is active),
 * every scope check returns `true` so existing behaviour is preserved.
 */
function useRBACImpl() {
  const telemetry = useTelemetry();
  const isAdmin = useIsPlatformAdmin();
  const productTier = useProductTier();
  const featureFlagEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;
  // Toolbar is accessible in dev or for admins; pass the flag into the getter
  // so it can also gate the SDK fetcher (which lacks auth context) at the source.
  const devOverrideActive =
    getRBACScopeOverrideHeader(import.meta.env.DEV || isAdmin) !== null;
  // Enterprise gate applies to the feature flag only. The dev override bypasses
  // the tier check entirely (mirroring the server, which applies override grants
  // before checking account type in access/manager.go).
  const isRbacEnabled =
    (featureFlagEnabled && productTier === "enterprise") || devOverrideActive;

  // Re-render when the toolbar changes scopes in localStorage.
  const [, setOverrideVersion] = useState(0);
  useEffect(() => {
    if (!import.meta.env.DEV && !isAdmin) return;
    const handler = () => setOverrideVersion((v) => v + 1);
    window.addEventListener("rbac-override-change", handler);
    return () => window.removeEventListener("rbac-override-change", handler);
  }, [isAdmin]);

  // Always fetch grants — even when RBAC is disabled — so we can detect a
  // broken org membership (404/403) and show a recovery prompt via
  // MembershipSyncGuard. throwOnError is disabled so the error doesn't crash
  // the app; it's surfaced via the returned `error` field instead.
  const { data, isLoading, error } = useGrants(undefined, undefined, {
    staleTime: 30_000,
    throwOnError: false,
  });

  // setOverrideVersion triggers a re-render (and therefore a re-read of devOverrideActive)
  // when the dev toolbar changes; the query invalidation handles the actual refetch.
  const grants = useMemo(() => {
    return data?.grants;
  }, [data?.grants]);

  /**
   * Check if the user has a given scope, optionally scoped to a resource ID.
   *
   * Uses exclusion-wins semantics: matching internal exclusion grants (and
   * legacy deny-effect grants) override matching allow grants.
   *
   * - If RBAC is disabled, always returns true.
   * - If grants haven't loaded yet, returns false (safe default).
   * - A grant with `selectors: undefined` (null from the API) means unrestricted.
   * - A grant with `selectors: [...]` means the scope is constrained by selectors.
   */
  const hasScope = useCallback(
    (scope: Scope, resourceId?: string): boolean => {
      if (!isRbacEnabled) return true;
      if (!grants) return false;

      return hasScopeInGrants(grants, scope, resourceId);
    },
    [isRbacEnabled, grants],
  );

  /**
   * Check multiple scopes at once. Returns true if the user has ALL of them.
   */
  const hasAllScopes = useCallback(
    (scopes: Scope[], resourceId?: string): boolean => {
      return scopes.every((scope) => hasScope(scope, resourceId));
    },
    [hasScope],
  );

  /**
   * Check multiple scopes at once. Returns true if the user has ANY of them.
   */
  const hasAnyScope = useCallback(
    (scopes: Scope[], resourceId?: string): boolean => {
      return scopes.some((scope) => hasScope(scope, resourceId));
    },
    [hasScope],
  );

  return useMemo(
    () => ({
      hasScope,
      hasAllScopes,
      hasAnyScope,
      isRbacEnabled,
      isLoading: isRbacEnabled && isLoading,
      grants: grants ?? [],
      /** Non-null when the grants query failed (e.g. missing org membership). */
      error: error ?? null,
    }),
    [
      hasScope,
      hasAllScopes,
      hasAnyScope,
      isRbacEnabled,
      isLoading,
      grants,
      error,
    ],
  );
}

export function useRBAC(): ReturnType<typeof useRBACImpl> {
  return useRBACImpl();
}

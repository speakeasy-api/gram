import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { useIsAdmin } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useProductTier } from "@/hooks/useProductTier";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { useGrants } from "@gram/client/react-query/grants.js";
import { useCallback, useEffect, useMemo, useState } from "react";

export { Scope };

/**
 * Derive the resource kind from a scope's family prefix.
 * Mirrors the server-side ResourceKindForScope in authz/selector.go.
 */
export function resourceKindForScope(scope: string): string {
  if (scope.startsWith("project:")) return "project";
  if (scope.startsWith("remote-mcp:") || scope.startsWith("mcp:")) return "mcp";
  if (scope.startsWith("org:")) return "org";
  if (scope.startsWith("environment:")) return "environment";
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

/**
 * Core RBAC hook. Fetches the current user's effective grants and provides
 * helpers to check whether the user holds a particular scope.
 *
 * When RBAC is disabled via feature flag (and no dev override is active),
 * every scope check returns `true` so existing behaviour is preserved.
 */
export function useRBAC() {
  const telemetry = useTelemetry();
  const isAdmin = useIsAdmin();
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
   * Check if a grant's scope matches the required scope.
   * For allow grants, sub-scope inheritance applies (e.g. org:admin implies org:read).
   * For deny grants, only exact scope match — deny must not cascade to child scopes.
   */
  const grantMatchesScope = useCallback(
    (
      grant: { scope?: string; effect?: string; subScopes?: string[] },
      scope: Scope,
    ): boolean => {
      if (grant.scope === scope) return true;
      const effect = grant.effect || "allow";
      return effect === "allow" && !!grant.subScopes?.includes(scope);
    },
    [],
  );

  /**
   * Check if the user has a given scope, optionally scoped to a resource ID.
   *
   * Uses deny-wins semantics: if any matching deny grant exists, access is
   * denied regardless of allow grants. Otherwise, at least one matching allow
   * grant must exist.
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

      const check: Record<string, string> = {
        resourceKind: resourceKindForScope(scope),
      };
      if (resourceId) check.resourceId = resourceId;

      let hasAllow = false;

      for (const grant of grants) {
        if (!grantMatchesScope(grant, scope)) continue;

        const effect = (grant as { effect?: string }).effect || "allow";

        // Check if selectors match
        const selectorsMatch = !grant.selectors
          ? true
          : grant.selectors.some((s) => selectorMatches(s, check));

        if (!selectorsMatch) continue;

        if (effect === "deny") return false;
        hasAllow = true;
      }

      return hasAllow;
    },
    [isRbacEnabled, grants, grantMatchesScope],
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

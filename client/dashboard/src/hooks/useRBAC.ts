import { getRBACScopeOverrideHeader } from "@/components/rbac-dev-toolbar";
import { useTelemetry } from "@/contexts/Telemetry";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { useGrants } from "@gram/client/react-query/grants.js";
import { useCallback, useMemo } from "react";

export type { Scope };

/**
 * Core RBAC hook. Fetches the current user's effective grants and provides
 * helpers to check whether the user holds a particular scope.
 *
 * When RBAC is disabled via feature flag (and no dev override is active),
 * every scope check returns `true` so existing behaviour is preserved.
 */
export function useRBAC() {
  const telemetry = useTelemetry();
  const featureFlagEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;
  const devOverrideActive =
    import.meta.env.DEV && getRBACScopeOverrideHeader() !== null;
  const isRbacEnabled = featureFlagEnabled || devOverrideActive;

  const { data, isLoading } = useGrants(undefined, undefined, {
    enabled: isRbacEnabled,
    staleTime: 30_000,
  });

  const grants = data?.grants;

  /**
   * Check if the user has a given scope, optionally scoped to a resource ID.
   *
   * - If RBAC is disabled, always returns true.
   * - If grants haven't loaded yet, returns false (safe default).
   * - A grant with `resources: undefined` (null from the API) means unrestricted.
   * - A grant with `resources: [...]` means the scope only applies to those IDs.
   */
  const hasScope = useCallback(
    (scope: Scope, resourceId?: string): boolean => {
      if (!isRbacEnabled) return true;
      if (!grants) return false;

      return grants.some((grant) => {
        if (grant.scope !== scope) return false;
        // Unrestricted grant — no resource allowlist
        if (!grant.resources) return true;
        // Resource-scoped grant — check if the resource is in the allowlist
        if (resourceId) return grant.resources.includes(resourceId);
        // Caller didn't specify a resource but grant is resource-scoped —
        // still counts as "has scope" for UI visibility purposes
        return true;
      });
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
    }),
    [hasScope, hasAllScopes, hasAnyScope, isRbacEnabled, isLoading, grants],
  );
}

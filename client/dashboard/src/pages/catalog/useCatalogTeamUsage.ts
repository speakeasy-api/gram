import { useShadowMCPPolicyInventory } from "@/components/shadow-mcp/useShadowMCPPolicyInventory";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useCallback, useMemo } from "react";
import type { CatalogServer } from "./hooks";
import { catalogTeamUsage, type CatalogTeamUsageMap } from "./team-usage";

const EMPTY: CatalogTeamUsageMap = new Map();

/**
 * The team's own use of each catalog server, from Shadow MCP, for org admins
 * (the only people who may read the inventory). It starts only once the
 * catalog has loaded, so the cards never wait on it: the usage line fills in
 * afterwards. It shares its cache with the Shadow MCP policy setup.
 */
export function useCatalogTeamUsage(
  catalog: readonly CatalogServer[],
  catalogLoaded: boolean,
): {
  usage: CatalogTeamUsageMap;
  canSeeUsage: boolean;
  refetch: () => void;
} {
  const project = useProject();
  const organization = useOrganization();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canSeeUsage = !rbacLoading && hasScope("org:admin", organization.id);
  const inventory = useShadowMCPPolicyInventory(
    project.id,
    canSeeUsage && catalogLoaded,
  );

  // Gated on canSeeUsage too: the query keeps its cached rows when it is
  // disabled, and they must not reach someone who is no longer an admin.
  const usage = useMemo(
    () =>
      canSeeUsage && inventory.data
        ? catalogTeamUsage(catalog, inventory.data)
        : EMPTY,
    [canSeeUsage, catalog, inventory.data],
  );

  const refetchInventory = inventory.refetch;
  const refetch = useCallback(() => {
    if (canSeeUsage) void refetchInventory();
  }, [canSeeUsage, refetchInventory]);

  return { usage, canSeeUsage, refetch };
}

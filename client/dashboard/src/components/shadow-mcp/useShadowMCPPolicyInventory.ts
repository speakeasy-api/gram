import { useSdkClient } from "@/contexts/Sdk";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

const INVENTORY_PAGE_LIMIT = 200;
const INVENTORY_STALE_TIME_MS = 30_000;

export function useShadowMCPPolicyInventory(
  projectID: string,
  enabled: boolean,
): UseQueryResult<ShadowMCPInventoryServer[], Error> {
  const client = useSdkClient();

  return useQuery({
    queryKey: ["shadow-mcp", "policy-setup-inventory", projectID],
    enabled: enabled && projectID.length > 0,
    staleTime: INVENTORY_STALE_TIME_MS,
    queryFn: async ({ signal }) => {
      const servers: ShadowMCPInventoryServer[] = [];
      let cursor: string | undefined;

      do {
        const page = await client.access.listShadowMCPInventory(
          {
            projectId: projectID,
            limit: INVENTORY_PAGE_LIMIT,
            cursor,
          },
          undefined,
          { signal },
        );
        servers.push(...page.servers);
        cursor = page.nextCursor;
      } while (cursor);

      return servers;
    },
  });
}

export function initialShadowMCPPolicyURLs(
  servers: readonly ShadowMCPInventoryServer[],
  policyID: string,
): Set<string> {
  return new Set(
    servers
      .filter((server) => server.allowedPolicyIds.includes(policyID))
      .map((server) => server.canonicalServerUrl),
  );
}

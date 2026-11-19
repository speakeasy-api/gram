import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

const GROUP = "Gateways";

/**
 * Gateway candidates, one per gateway the MCP page lists, each opening its
 * overview. The list endpoint returns every gateway in the project in one
 * response, so there is no later page to drain.
 */
export function useGatewayCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const gramProject = useProjectSlugForRequests();
  // Keyed by project so caches never cross projects; a failed list yields no candidates.
  const { data, isError } = useMetaMcpServers({ gramProject }, undefined, {
    enabled,
    throwOnError: false,
    retry: false,
  });

  return useMemo(
    () =>
      (!enabled || isError ? [] : (data?.metaMcpServers ?? [])).map(
        (gateway): LauncherCandidate => ({
          id: `gateway:${gateway.id}`,
          kind: "gateway",
          title: gateway.name,
          detail:
            gateway.memberCount === undefined
              ? "MCP gateway"
              : `MCP gateway · ${gateway.memberCount} ${gateway.memberCount === 1 ? "server" : "servers"}`,
          keywords: ["gateway", "mcp", gateway.id],
          verbs: ["open"],
          icon: "network",
          group: GROUP,
          run: () => routes.mcp.gateway.overview.goTo(gateway.id),
        }),
      ),
    [data, enabled, isError, routes],
  );
}

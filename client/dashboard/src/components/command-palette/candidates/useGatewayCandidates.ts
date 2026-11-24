import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

const GROUP = "Gateways";

/**
 * The "Inspect" action for a gateway. Every gateway has an Inspect tab, so
 * both the local list and the search index offer it beside the gateway.
 */
export function gatewayInspectCandidate(
  gateway: LauncherCandidate,
  run: () => void,
): LauncherCandidate {
  return {
    ...gateway,
    id: `gateway-inspect:${gateway.id.replace(/^gateway:/, "")}`,
    title: `${gateway.title} · Inspect`,
    detail: "Inspect · tools per discovery mode and why others are missing",
    keywords: [...gateway.keywords, "inspect", "tools", "debug"],
    verbs: ["open"],
    run,
  };
}

/**
 * Gateway candidates, one per gateway the MCP page lists, each opening its
 * overview, plus an Inspect action per gateway. The list endpoint returns every gateway in the project in one
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
  const { data } = useMetaMcpServers({ gramProject }, undefined, {
    enabled,
    throwOnError: false,
  });

  return useMemo(
    () =>
      (data?.metaMcpServers ?? []).flatMap((gateway): LauncherCandidate[] => {
        const candidate: LauncherCandidate = {
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
        };
        return [
          candidate,
          gatewayInspectCandidate(candidate, () =>
            routes.mcp.gateway.inspect.goTo(gateway.id),
          ),
        ];
      }),
    [data, routes],
  );
}

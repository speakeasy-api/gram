import { useOrganization, useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { buildMetaMcpServersQuery } from "@gram/client/react-query/metaMcpServers.js";
import {
  buildUserSessionClientsInfiniteQuery,
  type UserSessionClientsPageParams,
} from "@gram/client/react-query/userSessionClients.js";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

/** Named gateways and registrations, never a crawl of session history. */
export function useGatewayConnectionCandidates({
  enabled,
  inProject,
}: {
  enabled: boolean;
  inProject: boolean;
}): LauncherCandidate[] {
  const organization = useOrganization();
  const project = useProject();
  const gramProject = useProjectSlugForRequests();
  const sdk = useSdkClient();
  const routes = useRoutes({ projectSlug: gramProject });
  const navigate = useNavigate();
  const { hasScope } = useRBAC();
  const canReadProject = inProject && hasScope("project:read", project.id);
  const canReadGateways = canReadProject && hasScope("mcp:read", project.id);
  const canReadOrg = hasScope("org:read", organization.id);
  const gatewaysQuery = buildMetaMcpServersQuery(sdk, { gramProject });
  const gateways = useQuery({
    ...gatewaysQuery,
    queryKey: [...gatewaysQuery.queryKey, organization.id],
    enabled: enabled && canReadGateways,
    retry: false,
    throwOnError: false,
  });
  const clientsQuery = buildUserSessionClientsInfiniteQuery(sdk, {
    gramProject,
    limit: 100,
  });
  const clientsEnabled =
    enabled &&
    canReadGateways &&
    !!gateways.data?.metaMcpServers.some(
      (gateway) => gateway.userSessionIssuerId,
    );
  const clients = useInfiniteQuery({
    ...clientsQuery,
    queryKey: [...clientsQuery.queryKey, organization.id],
    initialPageParam: undefined as UserSessionClientsPageParams,
    getNextPageParam: (page) => page["~next"],
    enabled: clientsEnabled,
    retry: false,
    throwOnError: false,
  });
  const {
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = clients;
  useEffect(() => {
    if (
      !clientsEnabled ||
      !hasNextPage ||
      isFetchingNextPage ||
      isFetchNextPageError
    )
      return;
    void fetchNextPage().catch(() => undefined);
  }, [
    clientsEnabled,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  ]);

  return useMemo(() => {
    if (!enabled) return [];
    const result: LauncherCandidate[] = [];
    if (canReadOrg)
      result.push({
        id: `gateway-connections:organization:${organization.id}`,
        kind: "page",
        title: "Organization Gateway Connections",
        detail: "Sessions across projects",
        keywords: ["gateway", "sessions", "connections", "organization"],
        verbs: ["open"],
        icon: "network",
        group: "Pages",
        run: () => {
          void navigate(
            `${routes.mcp.organizationGateway.href()}?tab=connections`,
          );
        },
      });
    if (!canReadGateways || gateways.isError) return result;
    for (const gateway of gateways.data?.metaMcpServers ?? []) {
      const href = routes.mcp.gateway.sessions.href(gateway.id);
      result.push({
        id: `gateway-connections:${gateway.id}`,
        kind: "mcp_server",
        title: gateway.name,
        detail: "Gateway connections",
        keywords: ["gateway", "sessions", "connections", gateway.id],
        verbs: ["open"],
        icon: "network",
        group: "Gateway Connections",
        run: () => {
          void navigate(href);
        },
      });
      for (const client of clients.data?.pages.flatMap(
        (page) => page.result.items,
      ) ?? []) {
        if (client.userSessionIssuerId !== gateway.userSessionIssuerId)
          continue;
        result.push({
          id: `gateway-registration:${gateway.id}:${client.id}`,
          kind: "page",
          title: client.clientName || "Unnamed client",
          detail: `Registration · ${gateway.name}`,
          keywords: ["client", "registration", client.id, client.clientId],
          // A client-supplied display name may contain a person's identity.
          fuzzyOnly: true,
          verbs: ["open"],
          icon: "key",
          group: "Client Registrations",
          run: () => {
            void navigate(
              `${href}?registration=${encodeURIComponent(client.id)}`,
            );
          },
        });
      }
    }
    return result;
  }, [
    enabled,
    canReadOrg,
    canReadGateways,
    organization.id,
    gateways.data,
    gateways.isError,
    clients.data,
    routes,
    navigate,
  ]);
}

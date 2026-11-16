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
import { useEffect, useMemo, useRef } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

const GATEWAY_INVENTORY_STALE_TIME_MS = 30_000;

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
    staleTime: GATEWAY_INVENTORY_STALE_TIME_MS,
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
    staleTime: GATEWAY_INVENTORY_STALE_TIME_MS,
    retry: false,
    throwOnError: false,
  });
  const {
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = clients;
  const opens = useRef(0);
  const lastFetchOpen = useRef(0);
  useEffect(() => {
    if (clientsEnabled) opens.current += 1;
  }, [clientsEnabled]);
  useEffect(() => {
    if (!clientsEnabled || !hasNextPage || isFetchingNextPage) return;
    if (isFetchNextPageError && lastFetchOpen.current === opens.current) return;
    lastFetchOpen.current = opens.current;
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
    if (canReadOrg) {
      // Opened from the organization's settings, so it is findable by name.
      result.push({
        id: `gateway:organization:${organization.id}`,
        kind: "page",
        title: "Organization Gateway",
        detail: "Every server your team can reach",
        keywords: ["gateway", "organization", "mcp", "overview"],
        verbs: ["open"],
        icon: "network",
        group: "Pages",
        run: () => {
          void navigate(routes.mcp.organizationGateway.href());
        },
      });
    }
    if (canReadOrg && canReadProject) {
      // The project's MCP list leads with this gateway by name.
      result.push({
        id: `gateway:project:${project.id}`,
        kind: "page",
        title: "Project Gateway",
        detail: "Every server in this project your team can reach",
        keywords: ["gateway", "project", "mcp", "overview"],
        verbs: ["open"],
        icon: "network",
        group: "Pages",
        run: () => {
          void navigate(routes.mcp.projectGateway.href());
        },
      });
      result.push({
        id: `gateway-connections:project:${project.id}`,
        kind: "page",
        title: "Project Gateway Connections",
        detail: "Sessions through this project's gateway",
        keywords: ["gateway", "sessions", "connections", "project"],
        verbs: ["open"],
        icon: "network",
        group: "Pages",
        run: () => {
          void navigate(`${routes.mcp.projectGateway.href()}?tab=connections`);
        },
      });
      result.push({
        id: `gateway-activity:project:${project.id}`,
        kind: "page",
        title: "Project Gateway Activity",
        detail: "Calls through this project's gateway",
        keywords: ["gateway", "activity", "usage", "calls", "project"],
        verbs: ["open"],
        icon: "network",
        group: "Pages",
        run: () => {
          void navigate(`${routes.mcp.projectGateway.href()}?tab=activity`);
        },
      });
    }
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
    canReadProject,
    canReadGateways,
    organization.id,
    project.id,
    gateways.data,
    gateways.isError,
    clients.data,
    routes,
    navigate,
  ]);
}

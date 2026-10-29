import { mcpServerRouteParam } from "@/lib/sources";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

// Credential names are private identity metadata. Keep them out of intent
// service requests and link only to attachments in the current project.
export function useAuthClientCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const organization = useOrganization();
  const project = useProject();
  const sdk = useSdkClient();
  const routes = useRoutes();
  const navigate = useNavigate();
  const {
    data,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
  } = useInfiniteQuery({
    queryKey: ["launcher-auth-clients", organization.id, project.id],
    queryFn: async ({ pageParam, signal }) =>
      (
        await sdk.authClients.list(
          { clientType: "service_account", limit: 100, cursor: pageParam },
          undefined,
          { signal },
        )
      ).result,
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.nextCursor,
    enabled,
    retry: false,
    throwOnError: false,
  });
  useEffect(() => {
    if (!enabled || !hasNextPage || isFetchingNextPage || isFetchNextPageError)
      return;
    void fetchNextPage().catch(() => undefined);
  }, [
    enabled,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  ]);
  return useMemo(() => {
    if (!enabled) return [];
    return (data?.pages ?? []).flatMap((page) =>
      page.items.flatMap((client) =>
        client.servers
          .filter((server) => server.projectId === project.id)
          .map((server): LauncherCandidate => ({
            id: `auth-client:${client.id}:${server.id}`,
            kind: "mcp_server",
            title: client.name,
            detail: `Service account · ${server.name ?? "MCP server"}${client.enabled ? "" : " · disabled"}`,
            keywords: [
              client.id,
              "service account",
              ...(client.githubApp ? ["GitHub App"] : []),
            ],
            verbs: ["open"],
            fuzzyOnly: true,
            group: "Service Accounts",
            run: () => {
              void navigate(
                `${routes.mcp.details.settings.href(mcpServerRouteParam(server))}#authentication`,
              );
            },
          })),
      ),
    );
  }, [enabled, data?.pages, project.id, routes, navigate]);
}

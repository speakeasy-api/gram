import { useOrganization } from "@/contexts/Auth";
import { useListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";

/**
 * One MCP server as the provisioning flow needs it: what to show in a row and
 * what a grant has to name. Servers the gateway cannot serve an API key for
 * (unproxied, disabled, or in a project with no slug) never enter the list —
 * offering them would produce a key that cannot reach them.
 */
export interface InventoryServer {
  id: string;
  /** The id a policy selector names, which for a toolset is the toolset. */
  resourceId: string;
  name: string;
  slug: string;
  projectId: string;
  projectName: string;
  projectSlug: string;
  /** Present when the server authorizes each person separately. */
  issuerId?: string;
  kind: "Hosted" | "Remote" | "Tunneled" | "Toolset";
}

export interface ServerInventory {
  servers: InventoryServer[];
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
}

export function useServerInventory(): ServerInventory {
  const organization = useOrganization();
  const toolsets = useListToolsetsForOrg(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });
  const servers = useListMcpServersForOrg(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });

  const projects = new Map(
    (organization.projects ?? []).map((project) => [project.id, project]),
  );
  // A toolset already exposed as an MCP server would otherwise appear twice,
  // once under each name.
  const servedToolsets = new Set(
    servers.data?.mcpServers.flatMap((server) =>
      server.toolsetId ? [server.toolsetId] : [],
    ) ?? [],
  );

  const inventory: InventoryServer[] = [
    ...(servers.data?.mcpServers ?? []).map((server) => ({
      id: server.id,
      resourceId: server.toolsetId ?? server.id,
      name: server.name ?? server.slug ?? "Unnamed server",
      slug: server.slug ?? "",
      projectId: server.projectId,
      projectName: projects.get(server.projectId)?.name ?? "",
      projectSlug: projects.get(server.projectId)?.slug ?? "",
      issuerId: server.userSessionIssuerId,
      kind: server.unproxiedMcpServerId
        ? ("Hosted" as const)
        : server.tunneledMcpServerId
          ? ("Tunneled" as const)
          : server.remoteMcpServerId
            ? ("Remote" as const)
            : ("Hosted" as const),
      unproxied: Boolean(server.unproxiedMcpServerId),
      disabled: server.visibility === "disabled",
    })),
    ...(toolsets.data?.toolsets ?? [])
      .filter(
        (toolset) =>
          toolset.mcpEnabled === true && !servedToolsets.has(toolset.id),
      )
      .map((toolset) => ({
        id: toolset.id,
        resourceId: toolset.id,
        name: toolset.name,
        slug: toolset.slug ?? "",
        projectId: toolset.projectId,
        projectName: projects.get(toolset.projectId)?.name ?? "",
        projectSlug: projects.get(toolset.projectId)?.slug ?? "",
        issuerId: undefined,
        kind: "Toolset" as const,
        unproxied: false,
        disabled: false,
      })),
  ]
    .filter(
      (server) => !server.unproxied && !server.disabled && server.projectSlug,
    )
    .map(({ unproxied: _unproxied, disabled: _disabled, ...server }) => server);

  return {
    servers: inventory,
    isLoading: toolsets.isLoading || servers.isLoading,
    isError: toolsets.isError || servers.isError,
    refetch: () => {
      void toolsets.refetch();
      void servers.refetch();
    },
  };
}

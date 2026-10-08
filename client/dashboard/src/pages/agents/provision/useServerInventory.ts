import { useOrganization } from "@/contexts/Auth";
import { mcpUrlSuffix } from "@/hooks/useToolsetUrl";
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
  /** The path under /mcp/ the server answers on, which is what a row shows. */
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
  const servableServers = (servers.data?.mcpServers ?? []).filter(
    (server) =>
      !server.unproxiedMcpServerId &&
      server.visibility !== "disabled" &&
      Boolean(projects.get(server.projectId)?.slug),
  );
  // A toolset already exposed as an MCP server would otherwise appear twice,
  // once under each name. Only a wrapper that is itself servable stands in for
  // the toolset: a disabled one would otherwise take the toolset off the list
  // with it, leaving nothing to provision.
  const servedToolsets = new Set(
    servableServers.flatMap((server) =>
      server.toolsetId ? [server.toolsetId] : [],
    ),
  );

  const inventory: InventoryServer[] = [
    ...servableServers.map((server) => ({
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
    })),
    ...(toolsets.data?.toolsets ?? [])
      .filter(
        (toolset) =>
          toolset.mcpEnabled === true && !servedToolsets.has(toolset.id),
      )
      .map((toolset) => {
        const projectSlug = projects.get(toolset.projectId)?.slug ?? "";
        return {
          id: toolset.id,
          resourceId: toolset.id,
          name: toolset.name,
          // A toolset answers on its own mcpSlug, or on the legacy
          // project/toolset/environment route — never on the toolset slug by
          // itself, so the shared resolver decides the path.
          slug: mcpUrlSuffix({ slug: projectSlug }, toolset) ?? "",
          projectId: toolset.projectId,
          projectName: projects.get(toolset.projectId)?.name ?? "",
          projectSlug,
          issuerId: undefined,
          kind: "Toolset" as const,
        };
      })
      // Both have to hold: the resolver returns nothing for a toolset that
      // answers on no route, and offering one would issue a key for an
      // endpoint the agent cannot reach.
      .filter(
        (toolset) => Boolean(toolset.projectSlug) && Boolean(toolset.slug),
      ),
  ];

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

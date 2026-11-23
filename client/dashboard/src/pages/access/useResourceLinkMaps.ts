import { useSession } from "@/contexts/Auth";
import { useListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import { standsInForToolset } from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useMemo } from "react";

/** A toolset, and the MCP server that serves it when the org has one. */
export type ResourceLinkToolset = {
  slug: string;
  name: string;
  projectId: string;
  server?: { id: string; slug?: string };
};

export type ResourceLinkMaps = {
  projectMap: Map<string, { slug: string; name: string }>;
  toolsetMap: Map<string, ResourceLinkToolset>;
  mcpServerMap: Map<
    string,
    { slug?: string; name?: string; projectId: string }
  >;
};

/**
 * The server that stands in for each toolset, by toolset id: the same one the
 * /toolsets/<slug> redirect opens, so a toolset can link straight to its
 * /mcp/<server> page. A toolset can sit behind several servers; only its
 * stand-in counts.
 */
export function hostingServersByToolset(
  servers: {
    id: string;
    slug?: string | undefined;
    toolsetId?: string | undefined;
    toolset?: { hostingMcpServerId?: string | undefined } | undefined;
  }[],
): Map<string, { id: string; slug?: string }> {
  const hosts = new Map<string, { id: string; slug?: string }>();
  for (const server of servers) {
    if (server.toolsetId === undefined || !standsInForToolset(server)) continue;
    if (hosts.has(server.toolsetId)) continue;
    hosts.set(server.toolsetId, { id: server.id, slug: server.slug });
  }
  return hosts;
}

/**
 * The org-wide lookups `ResourceLink` resolves ids against. A failed read
 * leaves a map empty, which degrades a link to its id chip rather than taking
 * the page down. A failed refetch keeps React Query's stale cached `data`, so
 * an errored query is read as an empty list rather than its last success.
 */
export function useResourceLinkMaps(enabled = true): ResourceLinkMaps {
  const { organization } = useSession();
  const toolsets = useListToolsetsForOrg(undefined, undefined, {
    enabled,
    throwOnError: false,
  });
  const mcpServers = useListMcpServersForOrg(undefined, undefined, {
    enabled,
    throwOnError: false,
  });
  const toolsetsData = toolsets.isError ? undefined : toolsets.data;
  const mcpServersData = mcpServers.isError ? undefined : mcpServers.data;
  const projectMap = useMemo(() => {
    const m = new Map<string, { slug: string; name: string }>();
    for (const p of organization.projects) {
      m.set(p.id, { slug: p.slug, name: p.name });
    }
    return m;
  }, [organization.projects]);
  const toolsetMap = useMemo(() => {
    const hosts = hostingServersByToolset(mcpServersData?.mcpServers ?? []);
    const m = new Map<string, ResourceLinkToolset>();
    for (const t of toolsetsData?.toolsets ?? []) {
      m.set(t.id, {
        slug: t.slug,
        name: t.name,
        projectId: t.projectId,
        ...(hosts.has(t.id) ? { server: hosts.get(t.id) } : {}),
      });
    }
    return m;
  }, [toolsetsData, mcpServersData]);
  // Resolves grants stored against mcp_servers row ids (remote/tunneled
  // backends). Toolset-backed grants store the toolset id and resolve via
  // toolsetMap instead.
  const mcpServerMap = useMemo(() => {
    const m = new Map<
      string,
      { slug?: string; name?: string; projectId: string }
    >();
    for (const s of mcpServersData?.mcpServers ?? []) {
      m.set(s.id, { slug: s.slug, name: s.name, projectId: s.projectId });
    }
    return m;
  }, [mcpServersData]);
  return { projectMap, toolsetMap, mcpServerMap };
}

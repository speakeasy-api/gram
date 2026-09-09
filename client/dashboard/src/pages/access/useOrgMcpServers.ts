import { useOrganization } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { useListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import { useMemo } from "react";

import { mergeMcpServersIntoGroups, type ServerGroup } from "./serverMerge";

export interface OrgMcpServers {
  groups: ServerGroup[];
  /**
   * Whether both org listings resolved. An empty `groups` means "this
   * organization has no grantable servers" only when this is true; callers
   * that narrow a grant must not treat a pending or failed read as an empty
   * inventory.
   */
  settled: boolean;
}

/**
 * The org's MCP servers as the grant pickers see them: toolset entries keyed
 * by the id a `mcp` selector must carry, with mcp_servers rows folded in.
 * Shared by the role grant picker and the agent credential grant editor.
 */
export function useOrgMcpServers(enabled: boolean): OrgMcpServers {
  const organization = useOrganization();
  // A failed inventory read is a safe empty inventory for both pickers, which
  // report it through `settled`; it must not escalate to the page error
  // boundary via the shared query error policy.
  const toolsets = useListToolsetsForOrg(undefined, undefined, {
    enabled,
    throwOnError: false,
  });
  const mcpServers = useListMcpServersForOrg(undefined, undefined, {
    enabled,
    throwOnError: false,
  });
  const data = toolsets.data;
  const mcpServersData = mcpServers.data;

  const groups = useMemo((): ServerGroup[] => {
    const projectInfo = new Map(
      organization.projects.map((p) => [p.id, { name: p.name, slug: p.slug }]),
    );
    const baseUrl = getServerURL();
    const byProject = new Map<string, ServerGroup>();
    for (const t of data?.toolsets ?? []) {
      // A toolset with MCP switched off serves nothing, so an mcp grant naming
      // it could never take effect. Absent stays listed: only an explicit
      // false is a decision.
      if (t.mcpEnabled === false) continue;
      const project = projectInfo.get(t.projectId);
      const projectName = project?.name ?? "Unknown";
      let group = byProject.get(t.projectId);
      if (!group) {
        group = { projectId: t.projectId, projectName, servers: [] };
        byProject.set(t.projectId, group);
      }
      const fullUrl = t.mcpSlug
        ? `${baseUrl}/mcp/${t.mcpSlug}`
        : `${baseUrl}/mcp/${project?.slug ?? ""}/${t.slug}/${t.defaultEnvironmentSlug ?? ""}`;
      const mcpUrl = fullUrl.replace(/^https?:\/\//, "");
      // External MCP "proxy" entries (name suffix ":proxy") represent servers
      // whose tools/list requires user auth, so we can't enumerate them at
      // deploy time. They still resolve at call-time via mcp:connect, so the
      // grant model supports them; we just don't surface the proxy entry as a
      // selectable tool in the picker.
      const tools = t.tools
        .filter(
          (tool) =>
            !(tool.type === "externalmcp" && tool.name.endsWith(":proxy")),
        )
        .map((tool) => ({
          id: tool.id,
          name: tool.name,
          type: tool.type,
          httpMethod: tool.httpMethod,
          annotations: tool.annotations,
        }));
      const isExternalMcpProxy = t.tools.some(
        (tool) => tool.type === "externalmcp" && tool.name.endsWith(":proxy"),
      );
      // Skip servers with nothing grantable: zero visible tools and not a
      // proxy server (proxy servers stay listed so users can grant at the
      // server level).
      if (tools.length === 0 && !isExternalMcpProxy) continue;
      group.servers.push({
        id: t.id,
        name: t.name,
        slug: mcpUrl,
        mcpSlug: t.mcpSlug ?? undefined,
        tools,
        dynamicTools: false,
        remoteBacked: false,
      });
    }
    // Fold in mcp_servers rows (remote/tunneled and toolset-backed servers
    // the toolset list doesn't cover). See serverMerge.ts for the grant id
    // invariant this maintains.
    return mergeMcpServersIntoGroups(
      [...byProject.values()],
      mcpServersData?.mcpServers ?? [],
      new Map(organization.projects.map((p) => [p.id, p.name])),
    );
  }, [data, mcpServersData, organization.projects]);

  return { groups, settled: toolsets.isSuccess && mcpServers.isSuccess };
}

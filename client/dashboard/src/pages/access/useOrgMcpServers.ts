import { useOrganization } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { useListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import { useMemo } from "react";

import { mergeMcpServersIntoGroups, type ServerGroup } from "./serverMerge";

export interface OrgMcpServers {
  /**
   * The org's servers, or empty until BOTH listings have succeeded. The two
   * reads are halves of one inventory: serving one half's rows while the other
   * failed would let a picker narrow a grant against servers it cannot see, so
   * a partial inventory is published as no inventory.
   */
  groups: ServerGroup[];
  /**
   * Whether `groups` is authoritative. An empty `groups` means "this
   * organization has no grantable servers" only when this is true; callers
   * that narrow a grant must not treat a pending or failed read as an empty
   * inventory.
   */
  settled: boolean;
  /** Either half failed. `groups` is empty and narrowing must be withheld. */
  isError: boolean;
  /** Retry both halves; used to recover from a transient read failure. */
  refetch: () => void;
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
    const mcpDisabled = new Set<string>();
    for (const t of data?.toolsets ?? []) {
      // A toolset with MCP switched off serves nothing, so an mcp grant naming
      // it could never take effect. Absent stays listed: only an explicit
      // false is a decision.
      if (t.mcpEnabled === false) {
        mcpDisabled.add(t.id);
        continue;
      }
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
    // invariant this maintains. The merge only skips rows it has already seen,
    // so a disabled toolset skipped above would otherwise return through its
    // own mcp_servers row — drop those rows before merging.
    const mcpServerRows = (mcpServersData?.mcpServers ?? []).filter(
      (row) => !(row.toolsetId && mcpDisabled.has(row.toolsetId)),
    );
    return mergeMcpServersIntoGroups(
      [...byProject.values()],
      mcpServerRows,
      new Map(organization.projects.map((p) => [p.id, p.name])),
    );
  }, [data, mcpServersData, organization.projects]);

  // A refetch that has not landed yet keeps the last complete inventory: it may
  // be stale, but every id in it was real, so the worst case is a narrower
  // grant than intended — and issuance revalidates. A refetch that FAILS flips
  // isError and withdraws the inventory immediately.
  const settled =
    toolsets.isSuccess &&
    mcpServers.isSuccess &&
    !toolsets.isError &&
    !mcpServers.isError;
  return {
    groups: settled ? groups : [],
    settled,
    isError: toolsets.isError || mcpServers.isError,
    refetch: () => {
      void toolsets.refetch();
      void mcpServers.refetch();
    },
  };
}

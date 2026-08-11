/**
 * Merging of org toolsets and org mcp_servers rows into the server list used
 * by the mcp:connect grant pickers ("Specific servers" / "Specific tools").
 *
 * GRANT ID INVARIANT — Selector.resourceId for mcp:connect is enforced
 * server-side against different tables depending on the server's backend:
 *   - toolset-backed serving checks the TOOLSET id
 *     (server/internal/mcp/impl.go), and
 *   - remote/tunneled serving checks the mcp_servers row id
 *     (server/internal/mcp/serveendpoint.go).
 * `Server.id` IS the grant resource id: the toolset id for toolset entries and
 * toolset-backed mcp_servers rows, the mcp_servers id for remote/tunneled
 * rows. Every selector the pickers emit (server rows, tool rows, allow-filter
 * matching) flows through `Server.id`, so `grantResourceIdForMcpServer` below
 * is the only place that decides which id a mcp_servers row contributes.
 */

export interface ServerTool {
  id: string;
  name: string;
  type: string;
  httpMethod?: string;
  annotations?: {
    readOnlyHint?: boolean;
    destructiveHint?: boolean;
    idempotentHint?: boolean;
    openWorldHint?: boolean;
  };
}

export interface Server {
  /** The grant resource id — see GRANT ID INVARIANT above. */
  id: string;
  name: string;
  slug: string;
  mcpSlug?: string;
  tools: ServerTool[];
  /**
   * Remote/tunneled backends resolve their tools at call time, so they carry no
   * deploy-time tool list.
   */
  dynamicTools: boolean;
  /**
   * True for servers backed by a remote MCP server (they have a
   * remote_mcp_server_id). Only these carry stored tool metadata — the
   * `listToolMetadata` endpoint rejects everything else — so the "Specific
   * tools" picker fetches their tools from the metadata table on demand.
   * Tunneled-backed dynamic servers are false here: they cannot be
   * individually permissioned and stay a non-selectable row.
   */
  remoteBacked: boolean;
}

export interface ServerGroup {
  projectId: string;
  projectName: string;
  servers: Server[];
}

/** The subset of the McpServer SDK type the merge logic reads. */
export interface McpServerRow {
  id: string;
  projectId: string;
  name?: string | undefined;
  slug?: string | undefined;
  toolsetId?: string | undefined;
  /** Present only for remote-MCP-backed servers; absent for tunneled ones. */
  remoteMcpServerId?: string | undefined;
}

/** See the GRANT ID INVARIANT at the top of this file. */
export function grantResourceIdForMcpServer(
  row: Pick<McpServerRow, "id" | "toolsetId">,
): string {
  return row.toolsetId ?? row.id;
}

/** name and slug are both optional on mcp_servers rows. */
export function mcpServerDisplayName(row: McpServerRow): string {
  return row.name ?? row.slug ?? row.id;
}

/**
 * Merges org mcp_servers rows into the toolset-derived server groups:
 * - toolset-backed rows whose toolset already has an entry are deduped away
 *   (the toolset entry wins — it carries the enumerable tools);
 * - toolset-backed rows whose toolset entry is absent (e.g. filtered out for
 *   having no visible tools) are added with empty tools, still grantable at
 *   the server level;
 * - remote/tunneled rows are added with `dynamicTools: true`, with
 *   `remoteBacked` set only for remote-MCP-backed rows (tunneled ones cannot
 *   carry tool metadata).
 * Does not mutate the input groups; groups left with no servers are dropped.
 */
export function mergeMcpServersIntoGroups(
  groups: ServerGroup[],
  mcpServers: McpServerRow[],
  projectNames: Map<string, string>,
): ServerGroup[] {
  const byProject = new Map(
    groups.map((g) => [g.projectId, { ...g, servers: [...g.servers] }]),
  );
  const seenIds = new Set(groups.flatMap((g) => g.servers.map((s) => s.id)));

  for (const row of mcpServers) {
    const id = grantResourceIdForMcpServer(row);
    if (seenIds.has(id)) continue;
    seenIds.add(id);

    let group = byProject.get(row.projectId);
    if (!group) {
      group = {
        projectId: row.projectId,
        projectName: projectNames.get(row.projectId) ?? "Unknown",
        servers: [],
      };
      byProject.set(row.projectId, group);
    }
    group.servers.push({
      id,
      name: mcpServerDisplayName(row),
      slug: row.slug ?? "",
      mcpSlug: undefined,
      tools: [],
      dynamicTools: !row.toolsetId,
      remoteBacked: !!row.remoteMcpServerId,
    });
  }

  return Array.from(byProject.values()).filter((g) => g.servers.length > 0);
}

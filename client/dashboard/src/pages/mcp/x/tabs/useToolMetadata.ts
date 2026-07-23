import type { ToolMetadata } from "@gram/client/models/components/toolmetadata.js";
import { useListMcpServerToolMetadata } from "@gram/client/react-query/listMcpServerToolMetadata.js";
import { useMemo } from "react";

/** Stored tool metadata keyed by tool name, for O(1) lookup per rendered row. */
export type ToolMetadataByName = Record<string, ToolMetadata>;

export interface UseToolMetadataResult {
  metadataByTool: ToolMetadataByName;
  isLoading: boolean;
}

/**
 * Loads Speakeasy's stored annotation metadata for an MCP server's tools.
 *
 * This is the admin-authoritative side of the Inspect tab: the remote server
 * advertises its own annotation hints over the live MCP session, and these
 * stored entries are what Speakeasy asserts on top of them (read by the runtime
 * proxy to fill the disposition dimension of RBAC checks).
 *
 * Only servers backed by a remote MCP server carry tool metadata — the API
 * rejects toolset-backed ones, which persist hints on their tool-definition
 * tables instead. Callers gate on that via `enabled`.
 */
export function useToolMetadata(
  mcpServerId: string | undefined,
  options?: { enabled?: boolean },
): UseToolMetadataResult {
  const enabled = (options?.enabled ?? true) && !!mcpServerId;

  const { data, isLoading, fetchStatus } = useListMcpServerToolMetadata(
    { mcpServerId: mcpServerId ?? "" },
    undefined,
    {
      enabled,
      // Stored metadata is supplementary to the live tool listing. A failure
      // here shouldn't take down the tools list, so keep it inline — the rows
      // simply fall back to the annotations the server advertised.
      throwOnError: false,
    },
  );

  const metadataByTool = useMemo(() => {
    // Null-prototype: tool names come from the remote server, so a tool called
    // `__proto__` or `constructor` would otherwise either fail to become an own
    // property or make an absent tool look present, and drift would miss it.
    const byName = Object.create(null) as ToolMetadataByName;
    for (const entry of data?.tools ?? []) {
      byName[entry.toolName] = entry;
    }
    return byName;
  }, [data]);

  return {
    metadataByTool,
    // `isLoading` stays true for a disabled query; pair it with fetchStatus so
    // a gated server doesn't hold the section in a permanent skeleton.
    isLoading: isLoading && fetchStatus !== "idle",
  };
}

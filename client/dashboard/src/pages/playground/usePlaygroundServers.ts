import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMemo } from "react";

/**
 * A single selectable server in the playground. The playground presents
 * toolset-backed and remote-MCP-backed servers in one flat, undifferentiated
 * list; the `kind` discriminant only drives how we connect (and which
 * management affordances are available), never how the server is labeled.
 */
export type PlaygroundServerRef =
  | {
      kind: "toolset";
      /** Stable key for the picker + selection state. */
      key: string;
      name: string;
      toolsetSlug: string;
    }
  | {
      kind: "remote";
      key: string;
      name: string;
      mcpServerId: string;
      /** Whether the server gates connections behind a user_session_issuer. */
      isIssuerGated: boolean;
    };

export function toolsetServerKey(slug: string): string {
  return `toolset:${slug}`;
}

export function remoteServerKey(mcpServerId: string): string {
  return `remote:${mcpServerId}`;
}

/**
 * Lists every MCP server the playground can chat with: toolset-backed servers
 * (from `listToolsets`) merged with remote-MCP-backed servers (the
 * `remoteMcpServerId` subset of `mcpServers`), sorted by name. Toolset-backed
 * servers deliberately come only from `listToolsets` and remote-backed only
 * from `mcpServers`, so neither is double-counted.
 */
export function usePlaygroundServers(): {
  servers: PlaygroundServerRef[];
  isLoading: boolean;
} {
  const { data: toolsetsData, isLoading: isLoadingToolsets } =
    useListToolsets();
  const { data: mcpServersData, isLoading: isLoadingMcpServers } =
    useMcpServers();

  const servers = useMemo<PlaygroundServerRef[]>(() => {
    const toolsetServers: PlaygroundServerRef[] = (
      toolsetsData?.toolsets ?? []
    ).map((toolset) => ({
      kind: "toolset",
      key: toolsetServerKey(toolset.slug),
      name: toolset.name,
      toolsetSlug: toolset.slug,
    }));

    const remoteServers: PlaygroundServerRef[] = (
      mcpServersData?.mcpServers ?? []
    )
      .filter((server) => !!server.remoteMcpServerId)
      .map((server) => ({
        kind: "remote",
        key: remoteServerKey(server.id),
        name: server.name ?? server.slug ?? "Remote MCP server",
        mcpServerId: server.id,
        isIssuerGated: !!server.userSessionIssuerId,
      }));

    return [...toolsetServers, ...remoteServers].sort((a, b) =>
      a.name.localeCompare(b.name),
    );
  }, [toolsetsData, mcpServersData]);

  return { servers, isLoading: isLoadingToolsets || isLoadingMcpServers };
}

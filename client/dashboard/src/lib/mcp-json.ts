import type { ExternalMCPServer } from "@gram/client/models/components";

const AUTHORIZATION_HEADER_VALUE = "Bearer ${GRAM_API_KEY}";

type CollectionMcpServerConfig = {
  type: "http";
  url: string;
  headers: Record<string, string>;
};

export type CollectionMcpJsonConfig = {
  mcpServers: Record<string, CollectionMcpServerConfig>;
};

export type CollectionMcpJsonBuildResult = {
  config: CollectionMcpJsonConfig;
  includedCount: number;
  excludedCount: number;
  excludedServers: ExternalMCPServer[];
};

export function buildCollectionMcpJson(
  servers: ExternalMCPServer[],
): CollectionMcpJsonBuildResult {
  const mcpServers: Record<string, CollectionMcpServerConfig> = {};
  const serverNameCounts = new Map<string, number>();
  const excludedServers: ExternalMCPServer[] = [];

  for (const server of servers) {
    const remote = getUsableRemote(server);

    if (!remote) {
      excludedServers.push(server);
      continue;
    }

    const displayName = getUniqueDisplayName(
      getServerDisplayName(server),
      serverNameCounts,
    );

    mcpServers[displayName] = {
      type: "http",
      url: remote.url.trim(),
      headers: getRemoteHeaders(remote),
    };
  }

  return {
    config: { mcpServers },
    includedCount: Object.keys(mcpServers).length,
    excludedCount: excludedServers.length,
    excludedServers,
  };
}

export function formatMcpJson(config: CollectionMcpJsonConfig): string {
  return JSON.stringify(config, null, 2);
}

function getUsableRemote(server: ExternalMCPServer) {
  const remotes =
    server.remotes?.filter((remote) => remote.url.trim().length > 0) ?? [];

  return (
    remotes.find((remote) => remote.transportType === "streamable-http") ??
    remotes[0]
  );
}

function getRemoteHeaders(
  remote: NonNullable<ExternalMCPServer["remotes"]>[number],
): Record<string, string> {
  const headers: Record<string, string> = {};

  for (const header of remote.headers ?? []) {
    const name = header.name.trim();
    const value = header.placeholder?.trim();

    if (!name || !value) {
      continue;
    }

    headers[name] = value;
  }

  if (Object.keys(headers).length > 0) {
    return headers;
  }

  return {
    Authorization: AUTHORIZATION_HEADER_VALUE,
  };
}

function getServerDisplayName(server: ExternalMCPServer): string {
  const title = server.title?.trim();
  if (title) return title;

  const registrySpecifier = server.registrySpecifier.trim();
  if (registrySpecifier) return registrySpecifier;

  return "MCP Server";
}

function getUniqueDisplayName(
  displayName: string,
  serverNameCounts: Map<string, number>,
): string {
  const count = serverNameCounts.get(displayName) ?? 0;
  serverNameCounts.set(displayName, count + 1);

  if (count === 0) {
    return displayName;
  }

  return `${displayName} (${count + 1})`;
}

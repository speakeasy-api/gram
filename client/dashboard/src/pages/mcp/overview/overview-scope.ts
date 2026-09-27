/** Remote MCP calls carry a server ID, not a toolset slug. */
export function overviewScope(server: {
  kind: "toolset" | "mcp-server";
  id: string;
  slug: string;
}):
  | { mcpServerId: string; toolsetSlug?: never }
  | { toolsetSlug: string; mcpServerId?: never } {
  return server.kind === "mcp-server"
    ? { mcpServerId: server.id }
    : { toolsetSlug: server.slug };
}

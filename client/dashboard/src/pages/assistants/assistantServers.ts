/**
 * Slugs the assistants list treats as "MCP servers": toolset-backed
 * attachments and servers joined through `assistant_mcp_servers`.
 */
export function assistantAttachedServerSlugs(assistant: {
  toolsets: ReadonlyArray<{ toolsetSlug: string }>;
  mcpServers?: ReadonlyArray<{ mcpServerSlug: string }>;
}): string[] {
  return [
    ...assistant.toolsets.map((toolset) => toolset.toolsetSlug),
    ...(assistant.mcpServers ?? []).map((server) => server.mcpServerSlug),
  ];
}

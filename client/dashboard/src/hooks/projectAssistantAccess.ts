/**
 * True when the project has neither toolsets nor MCP servers. Either
 * attachment kind is enough for the Project Assistant to have tools.
 * Returns false while either list is still loading so the dock does not
 * flash a "create a server" prompt during the first paint.
 */
export function isNoMcpAccessConfigured({
  projectSlug,
  toolsetsLoading,
  toolsetCount,
  mcpServersLoading,
  mcpServerCount,
}: {
  projectSlug?: string;
  toolsetsLoading: boolean;
  toolsetCount: number;
  mcpServersLoading: boolean;
  mcpServerCount: number;
}): boolean {
  if (!projectSlug || toolsetsLoading || mcpServersLoading) return false;
  return toolsetCount === 0 && mcpServerCount === 0;
}

/**
 * The connecting spinner and the "no servers" notice must not show
 * together — an empty project is a settled state, not an in-flight one.
 */
export function showProjectAssistantConnecting({
  assistantError,
  assistantNeedsAdmin,
  noMcpAccessConfigured,
}: {
  assistantError: unknown;
  assistantNeedsAdmin: boolean;
  noMcpAccessConfigured: boolean;
}): boolean {
  return !assistantError && !assistantNeedsAdmin && !noMcpAccessConfigured;
}

/**
 * Length of a settled list. `undefined` data (loading, failed, or unknown)
 * stays unknown — do not fall back to `?? 0`.
 */
export function settledListCount(
  data: unknown,
  items: readonly unknown[] | undefined,
): number | undefined {
  if (data === undefined) return undefined;
  return items?.length ?? 0;
}

/**
 * True when the project has neither toolsets nor MCP servers. Either
 * attachment kind is enough for the Project Assistant to have tools.
 * Returns false while either list is still loading, failed, or unknown so
 * the dock does not treat a suppressed 403 / failed listing as "empty".
 */
export function isNoMcpAccessConfigured({
  projectSlug,
  toolsetsLoading,
  toolsetCount,
  mcpServersLoading,
  mcpServerCount,
  toolsetsFailed = false,
  mcpServersFailed = false,
}: {
  projectSlug?: string;
  toolsetsLoading: boolean;
  toolsetCount: number | undefined;
  mcpServersLoading: boolean;
  mcpServerCount: number | undefined;
  toolsetsFailed?: boolean;
  mcpServersFailed?: boolean;
}): boolean {
  if (!projectSlug || toolsetsLoading || mcpServersLoading) return false;
  if (toolsetsFailed || mcpServersFailed) return false;
  if (toolsetCount === undefined || mcpServerCount === undefined) return false;
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

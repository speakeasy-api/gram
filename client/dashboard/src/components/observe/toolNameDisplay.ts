/**
 * Some agents (notably Cowork / claude.ai) report tool calls using the fully
 * namespaced MCP name, e.g. `mcp__<connector_uuid>__send_message`, so the tool
 * logs table would otherwise render the raw prefixed string. Strip everything
 * up to and including the last `__` separator so only the final tool segment is
 * shown. Names without a separator are returned unchanged.
 */
export function formatToolName(toolName: string): string {
  const separator = "__";
  const index = toolName.lastIndexOf(separator);
  if (index === -1) {
    return toolName;
  }
  const parsed = toolName.slice(index + separator.length);
  // Guard against a trailing separator producing an empty label.
  return parsed || toolName;
}

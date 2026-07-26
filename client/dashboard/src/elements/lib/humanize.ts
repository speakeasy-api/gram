// Some agents (Cowork / claude.ai) report tool calls with the fully namespaced
// MCP name, e.g. `mcp__<server>__send_message`. Strip through the last `__` so
// only the leaf tool segment shows; names without a separator are unchanged.
// Mirrors `@/components/observe/toolNameDisplay` — elements can't import app
// code across the publish boundary.
export function formatToolName(toolName: string): string {
  const separator = "__";
  const index = toolName.lastIndexOf(separator);
  if (index === -1) return toolName;
  return toolName.slice(index + separator.length) || toolName;
}

// humanize tool name:
// - split camel case into words
// - capitalize first letter of each word
// - remove hyphens / underscores
// - title case the string
export function humanizeToolName(toolName: string): string {
  return toolName
    .replace(/[-_]/g, " ") // Replace hyphens and underscores with spaces
    .split(/(?=[A-Z])/) // Split on camelCase boundaries
    .join(" ") // Join with spaces
    .split(/\s+/) // Split on any whitespace to normalize
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase()) // Title case each word
    .join(" ");
}

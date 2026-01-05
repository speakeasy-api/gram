// humanize tool name:
// - split camel case into words
// - capitalize first letter of each word
// - remove hyphens / underscores
// - title case the string
export function humanizeToolName(toolName: string): string {
  return toolName
    .replace(/[-_]/g, ' ') // Replace hyphens and underscores with spaces
    .split(/(?=[A-Z])/) // Split on camelCase boundaries
    .join(' ') // Join with spaces
    .split(/\s+/) // Split on any whitespace to normalize
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase()) // Title case each word
    .join(' ')
}

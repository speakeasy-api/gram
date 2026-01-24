export const TOOL_NAME_PATTERN = "^[a-z0-9_-]{1,128}$";
export const TOOL_NAME_REGEX = new RegExp(TOOL_NAME_PATTERN);
export const PROMPT_NAME_PATTERN = "^[a-z0-9_-]{1,128}$";
export const PROMPT_NAME_REGEX = new RegExp(PROMPT_NAME_PATTERN);
export const PROMPT_ARG_PATTERN = "^[a-zA-Z]+(?:[_][a-zA-Z0-9]+)*$";
export const PROMPT_ARG_REGEX = new RegExp(PROMPT_ARG_PATTERN);
export const MUSTACHE_VAR_PATTERN = String.raw`\{\{\{?\s*(${PROMPT_ARG_PATTERN.slice(1, -1)})\s*\}\}\}?`;
export const MUSTACHE_VAR_REGEX = new RegExp(MUSTACHE_VAR_PATTERN, "g");

/**
 * Converts a string to match the TOOL_NAME_PATTERN format.
 * The pattern allows: lowercase letters, numbers, underscores, and hyphens (1-128 chars).
 *
 * @param str - The input string to slugify
 * @returns A string that matches TOOL_NAME_PATTERN
 */
export function slugify(str: string): string {
  if (!str) return "a"; // Default fallback for empty strings

  let result = str
    // Convert to lowercase
    .toLowerCase()
    // Replace any non-allowed characters with underscores (preserve hyphens)
    .replace(/[^a-z0-9_-]/g, "_")
    // Replace multiple consecutive underscores with a single one
    .replace(/_+/g, "_")
    // Remove leading and trailing underscores
    .replace(/^_+|_+$/g, "");

  // Truncate to 128 characters
  if (result.length > 128) {
    result = result.slice(0, 128);
  }

  // If empty, return default
  return result || "a";
}

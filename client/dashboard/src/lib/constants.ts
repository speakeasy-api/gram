export const TOOL_NAME_PATTERN = "^[a-zA-Z]+(?:[_][a-zA-Z0-9]+)*$";
export const TOOL_NAME_REGEX = new RegExp(TOOL_NAME_PATTERN);
export const PROMPT_NAME_PATTERN = "^[a-z0-9_-]{1,128}$";
export const PROMPT_NAME_REGEX = new RegExp(PROMPT_NAME_PATTERN);
export const PROMPT_ARG_PATTERN = "^[a-zA-Z]+(?:[_][a-zA-Z0-9]+)*$";
export const PROMPT_ARG_REGEX = new RegExp(PROMPT_ARG_PATTERN);
export const MUSTACHE_VAR_PATTERN = String.raw`\{\{\{?\s*(${PROMPT_ARG_PATTERN.slice(1, -1)})\s*\}\}\}?`;
export const MUSTACHE_VAR_REGEX = new RegExp(MUSTACHE_VAR_PATTERN, "g");

/**
 * Converts a string to match the TOOL_NAME_PATTERN format.
 * The pattern requires: starts with a letter, followed by optional underscores and alphanumeric characters.
 *
 * @param str - The input string to slugify
 * @returns A string that matches TOOL_NAME_PATTERN
 */
export function slugify(str: string): string {
  if (!str) return "a"; // Default fallback for empty strings

  return (
    str
      // Convert to lowercase
      .toLowerCase()
      // Replace any non-alphanumeric characters with underscores
      .replace(/[^a-z0-9]/g, "_")
      // Replace multiple consecutive underscores with a single one
      .replace(/_+/g, "_")
      // Remove leading and trailing underscores
      .replace(/^_+|_+$/g, "")
      // Ensure it starts with a letter (add 'a' if it doesn't)
      .replace(/^[^a-z]/, "a$&")
      // If the result is empty or doesn't start with a letter, add 'a'
      .replace(/^$/, "a")
  );
}

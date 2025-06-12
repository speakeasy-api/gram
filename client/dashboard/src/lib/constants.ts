export const TOOL_NAME_PATTERN = "^[a-zA-Z]+(?:[_][a-zA-Z0-9]+)*$";
export const TOOL_NAME_REGEX = new RegExp(TOOL_NAME_PATTERN);
export const PROMPT_NAME_PATTERN = TOOL_NAME_PATTERN;
export const PROMPT_NAME_REGEX = new RegExp(PROMPT_NAME_PATTERN);
export const PROMPT_ARG_PATTERN = "^[a-zA-Z]+(?:[_][a-zA-Z0-9]+)*$";
export const PROMPT_ARG_REGEX = new RegExp(PROMPT_ARG_PATTERN);
export const MUSTACHE_VAR_PATTERN = String.raw`\{\{\{?\s*(${PROMPT_ARG_PATTERN.slice(1, -1)})\s*\}\}\}?`;
export const MUSTACHE_VAR_REGEX = new RegExp(MUSTACHE_VAR_PATTERN, "g");

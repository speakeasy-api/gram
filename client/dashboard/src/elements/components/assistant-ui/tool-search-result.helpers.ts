/**
 * Pure helpers shared by the `tool_search` card and the run grouping that
 * decides whether to draw it. They live outside the component module because a
 * `.tsx` file may only export components.
 */

export function asRecord(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

export function asString(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined;
}

/**
 * The `browse:` query prefix reserved for a catalog request. The system prompt
 * tells the assistant to use it when the user asked what tools or capabilities
 * exist, and nothing else.
 */
const BROWSE_QUERY_PREFIX = "browse:";

/**
 * Whether a `tool_search` call is a catalog request rather than discovery.
 *
 * Discovery is the common case: the runner tells the model that MCP tools are
 * absent from its declared schema, so most turns open with a search for the
 * tools that turn needs. Those searches are mechanics — the user asked about
 * their logs, not about the catalog — and drawing the card for them put a
 * browsable tool list on top of nearly every answer.
 *
 * The card therefore renders only for a search the model marked as a browse.
 * Intent is the model's to know and the result carries none of it: every
 * search returns the same whole-catalog view whatever was asked for. When the
 * prefix is missing (an older prompt, a model that skipped it), the call falls
 * back to the generic collapsed tool row, which is the safe direction.
 */
export function isCatalogBrowseSearch(args: unknown): boolean {
  const query = asString(asRecord(args)?.["query"]);
  return (
    query !== undefined &&
    query.trimStart().toLowerCase().startsWith(BROWSE_QUERY_PREFIX)
  );
}

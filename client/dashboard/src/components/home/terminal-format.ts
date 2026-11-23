/** Pure formatting for the homepage terminal's tool-call lines. */

export function formatArgs(args: unknown): string {
  if (args === null || typeof args !== "object") return "";
  return (
    Object.entries(args as Record<string, unknown>)
      // Strings read as plain text on one line, not escaped JSON.
      .map(([key, value]) =>
        typeof value === "string"
          ? `${key}: "${value.replace(/\s+/g, " ").trim()}"`
          : `${key}: ${JSON.stringify(value)}`,
      )
      .join(", ")
  );
}

/**
 * `mcp__<server>__<tool>` or `mcp__<server>_<tool>` → `<tool>`, flagged as
 * MCP like Claude Code does. Server slugs use hyphens, never underscores.
 */
export function displayToolName(toolName: string): {
  name: string;
  mcp: boolean;
} {
  const match = /^mcp__[^_]+_{1,2}(.+)$/.exec(toolName);
  return match?.[1]
    ? { name: match[1].replace(/^platform_/, ""), mcp: true }
    : { name: toolName, mcp: false };
}

export function resultText(result: unknown): string {
  if (result === undefined || result === null) return "";
  if (typeof result === "string") return result;
  // Server-side tool results arrive as a tool message: { role, content }.
  if (
    typeof result === "object" &&
    "content" in result &&
    typeof result.content === "string"
  ) {
    return result.content;
  }
  if (
    typeof result === "object" &&
    "content" in result &&
    Array.isArray(result.content)
  ) {
    return result.content
      .map((item: unknown) =>
        item &&
        typeof item === "object" &&
        "text" in item &&
        typeof item.text === "string"
          ? item.text
          : "",
      )
      .filter(Boolean)
      .join("\n");
  }
  return JSON.stringify(result, null, 2);
}

/**
 * A failed call: the runtime marks it incomplete, the result carries isError,
 * or a server-side failure came back as text.
 */
export function isToolError(statusType: string, result: unknown): boolean {
  return (
    statusType === "incomplete" ||
    (typeof result === "object" &&
      result !== null &&
      "isError" in result &&
      result.isError === true) ||
    isFailureEnvelope(resultText(result))
  );
}

const FAILURE = /^tool execution failed/i;

/**
 * Only the failure envelope counts, `{"err": "tool execution failed: …"}` or
 * the bare message, so a successful result that quotes the phrase does not.
 */
function isFailureEnvelope(text: string): boolean {
  const trimmed = text.trim();
  if (FAILURE.test(trimmed)) return true;
  if (!trimmed.startsWith("{")) return false;
  try {
    const parsed: unknown = JSON.parse(trimmed);
    return (
      typeof parsed === "object" &&
      parsed !== null &&
      "err" in parsed &&
      typeof parsed.err === "string" &&
      FAILURE.test(parsed.err)
    );
  } catch {
    return false;
  }
}

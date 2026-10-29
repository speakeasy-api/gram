/**
 * Read a deeply nested value from a log's attributes using a dot-separated
 * path, keeping whatever type the attribute was stored with.
 */
export function getNestedAttribute(
  obj: Record<string, unknown>,
  path: string,
): unknown {
  let current: unknown = obj;
  for (const part of path.split(".")) {
    if (
      current === null ||
      current === undefined ||
      typeof current !== "object"
    )
      return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return current;
}

/**
 * The HTTP status a log recorded, or NaN when it has none. Telemetry stores it
 * as a number, but older rows and other writers may have stored a string.
 */
export function httpStatusCode(attrs: Record<string, unknown>): number {
  const raw = getNestedAttribute(attrs, "http.response.status_code");
  if (typeof raw === "number") return raw;
  if (typeof raw === "string" && raw.trim() !== "") return Number(raw);
  return Number.NaN;
}

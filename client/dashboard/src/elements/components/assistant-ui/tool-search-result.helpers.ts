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
 * Whether a `tool_search` call is a catalog request rather than discovery.
 *
 * Discovery is the common case: the runner tells the model that MCP tools are
 * absent from its declared schema, so most turns open with a search for the
 * tools that turn needs. Those searches are mechanics — the user asked about
 * their logs, not about the catalog — and drawing the card for them put a
 * browsable tool list on top of nearly every answer.
 *
 * The card therefore renders only for a search the model flagged as a browse.
 * Intent is the model's to know and the result carries none of it: every
 * search returns the same whole-catalog view whatever was asked for, so the
 * runner declares a `browse` parameter for the model to set (see
 * `agents/runner/src/tools/tool_search.rs`). Read from the arguments rather
 * than the echo in the result: the flag is then known before the search
 * returns, and on a reloaded transcript whose result shape varies.
 *
 * An unflagged call — an older prompt, a model that skipped it — falls back to
 * the generic collapsed tool row, which is the safe direction.
 */
export function isCatalogBrowseSearch(args: unknown): boolean {
  return asRecord(args)?.["browse"] === true;
}

/**
 * The runner's `tool_search` result: full schemas for the query's matches, a
 * name index of the whole catalog, and the live status of every attached MCP
 * server. See `agents/runner/src/tools/tool_search.rs`.
 */
export interface ToolSearchPayload {
  servers: ServerStatus[];
  /** Tool name -> one-line description, from the catalog index. */
  briefs: Map<string, string>;
}

export interface ServerStatus {
  id: string;
  tools: string[];
}

/**
 * The payload arrives either as the structured object or as a text content
 * item holding that same JSON — which one depends on whether the transport
 * preserved structured content, so both are unwrapped rather than assumed.
 *
 * Null for anything that is not a readable catalog: a call still streaming, a
 * denied one, an error, a search that reached no connected server. Both the
 * card and the rule that picks which browse draws read this, so a call is
 * never chosen to draw a catalog it cannot render.
 */
export function extractPayload(result: unknown): ToolSearchPayload | null {
  // The runner hands the structured body back as a JSON string.
  if (typeof result === "string") {
    try {
      return extractPayload(JSON.parse(result) as unknown);
    } catch {
      return null;
    }
  }

  const record = asRecord(result);
  if (!record) return null;

  const content = record["content"];
  // Live turns deliver the body as `{content: "<json>"}`; a reloaded
  // transcript delivers the same JSON as a bare string.
  if (typeof content === "string") {
    try {
      const payload = extractPayload(JSON.parse(content) as unknown);
      if (payload) return payload;
    } catch {
      // Not the JSON payload — fall through to the structured shapes.
    }
  }
  if (Array.isArray(content)) {
    for (const item of content) {
      const text = asString(asRecord(item)?.["text"]);
      if (!text) continue;
      try {
        const payload = extractPayload(JSON.parse(text) as unknown);
        if (payload) return payload;
      } catch {
        // Not the JSON payload — keep looking through the content items.
      }
    }
  }

  const rawServers = record["servers"];
  if (!Array.isArray(rawServers)) return null;

  // Only servers that answered the handshake carry tools. One that is
  // unavailable or still awaiting authorization has nothing to browse, so it
  // is left out rather than rendered as an empty or disabled group.
  const servers: ServerStatus[] = [];
  for (const item of rawServers) {
    const entry = asRecord(item);
    const id = asString(entry?.["id"]);
    const tools = entry?.["tools"];
    if (!id || !Array.isArray(tools)) continue;
    const names = tools.filter(
      (name): name is string => typeof name === "string",
    );
    if (names.length > 0) servers.push({ id, tools: names });
  }
  if (servers.length === 0) return null;

  const briefs = new Map<string, string>();
  const catalog = record["catalog"];
  if (Array.isArray(catalog)) {
    for (const item of catalog) {
      const entry = asRecord(item);
      const name = asString(entry?.["name"]);
      const brief = asString(entry?.["brief"]);
      if (name && brief) briefs.set(name, brief);
    }
  }

  return { servers, briefs };
}

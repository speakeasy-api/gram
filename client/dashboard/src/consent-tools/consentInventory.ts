import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { CfWorkerJsonSchemaValidator } from "@modelcontextprotocol/sdk/validation/cfworker";

import type { ToolAnnotation } from "@/components/tool-selection/annotations";
import { isRecord, isToolAnnotation } from "./prefill";

export interface ConsentTool {
  name: string;
  annotations: ToolAnnotation[];
  /** The gateway member server the tool comes from; absent on single servers. */
  source?: string;
}

export interface RoleHiddenTools {
  /** Full count of tools the caller's role hides from this inventory. */
  count: number;
  /** Hidden tool names; the server caps the list, so it may trail count. */
  names: string[];
}

export interface ConsentInventory {
  tools: ConsentTool[];
  roleHiddenTools: RoleHiddenTools;
}

/**
 * Parses the server-rendered gateway capture. Every tool is validated, so a
 * malformed bootstrap fails initialization (approval stays disabled and the
 * page offers a reload) instead of reaching the picker and crashing it.
 */
export function parseGatewayInventoryBootstrap(
  raw: string | undefined,
): (ConsentInventory & { attempt: string }) | null {
  let value: unknown;
  try {
    value = JSON.parse(raw ?? "");
  } catch {
    return null;
  }
  if (
    !isRecord(value) ||
    typeof value.attempt !== "string" ||
    value.attempt === "" ||
    !Array.isArray(value.tools)
  )
    return null;
  const tools: ConsentTool[] = [];
  const seen = new Set<string>();
  for (const entry of value.tools) {
    if (!isRecord(entry)) return null;
    const { name, annotations, source } = entry;
    if (typeof name !== "string" || name === "" || seen.has(name)) return null;
    if (!Array.isArray(annotations) || !annotations.every(isToolAnnotation))
      return null;
    if (source !== undefined && typeof source !== "string") return null;
    seen.add(name);
    tools.push({
      name,
      annotations,
      ...(source ? { source } : {}),
    });
  }
  return {
    attempt: value.attempt,
    tools,
    roleHiddenTools: { count: 0, names: [] },
  };
}

/**
 * The island is an ordinary MCP client of the consent-scoped transport: the
 * official SDK performs initialize, paginated tools/list, and session
 * termination against the URL the page provided; state and CSRF ride
 * headers so every message stays pure MCP. The attempt id keys the
 * server-side inventory snapshot this session accumulates — approval later
 * binds to it, so the caller must submit it only after this resolves.
 */
export async function fetchConsentInventory(
  toolsUrl: string,
  state: string,
  csrfToken: string,
  attempt: string,
  signal?: AbortSignal,
): Promise<ConsentInventory> {
  const transport = new StreamableHTTPClientTransport(
    new URL(toolsUrl, window.location.origin),
    {
      // The SDK replaces requestInit.signal with its own controller. Combine
      // the deadline at the fetch boundary so discovery and DELETE both stop.
      fetch: (input, init) =>
        fetch(input, {
          ...init,
          signal: signal
            ? AbortSignal.any([signal, ...(init?.signal ? [init.signal] : [])])
            : init?.signal,
        }),
      requestInit: {
        credentials: "omit",
        headers: {
          "Gram-Consent-State": state,
          "Gram-Consent-Csrf": csrfToken,
          "Gram-Consent-Inventory-Attempt": attempt,
        },
      },
    },
  );
  // The consent page CSP has no unsafe-eval, so the SDK's default Ajv
  // validator (new Function codegen) throws on any upstream tool that
  // declares an outputSchema. The cfworker validator interprets schemas
  // instead of compiling them.
  const client = new Client(
    { name: "gram-consent", version: "1.0.0" },
    { jsonSchemaValidator: new CfWorkerJsonSchemaValidator() },
  );
  try {
    await client.connect(transport);
    const tools: ConsentTool[] = [];
    const seen = new Set<string>();
    const roleHiddenTools: RoleHiddenTools = { count: 0, names: [] };
    let cursor: string | undefined = undefined;
    do {
      const page = await client.listTools(cursor ? { cursor } : undefined);
      for (const tool of page.tools) {
        if (seen.has(tool.name)) throw new Error("duplicate tool name");
        seen.add(tool.name);
        tools.push({
          name: tool.name,
          annotations: annotationsFromHints(tool.annotations),
        });
      }
      const meta = (page as { _meta?: Record<string, unknown> })._meta;
      const hidden = meta?.["gram.dev/roleHiddenTools"];
      if (isRecord(hidden)) {
        if (typeof hidden.count === "number" && hidden.count > 0) {
          roleHiddenTools.count += hidden.count;
        }
        if (Array.isArray(hidden.names)) {
          for (const name of hidden.names) {
            if (typeof name === "string" && name !== "") {
              roleHiddenTools.names.push(name);
            }
          }
        }
      }
      cursor = page.nextCursor;
    } while (cursor);
    return { tools, roleHiddenTools };
  } finally {
    await transport.terminateSession().catch(() => undefined);
    await client.close().catch(() => undefined);
  }
}

const HINT_TO_ANNOTATION: ReadonlyArray<[string, ToolAnnotation]> = [
  ["readOnlyHint", "read_only"],
  ["destructiveHint", "destructive"],
  ["idempotentHint", "idempotent"],
  ["openWorldHint", "open_world"],
];

function annotationsFromHints(value: unknown): ToolAnnotation[] {
  if (!isRecord(value)) return [];
  return HINT_TO_ANNOTATION.filter(([hint]) => value[hint] === true).map(
    ([, annotation]) => annotation,
  );
}

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import {
  StreamableHTTPClientTransport,
  StreamableHTTPError,
} from "@modelcontextprotocol/sdk/client/streamableHttp.js";
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

type DiscoveryStage = "initialize" | "tools/list";

export class ConsentInventoryFailure extends Error {
  readonly stage: DiscoveryStage | "bootstrap";
  readonly reason:
    | "missing_inventory"
    | "timeout"
    | "network"
    | "invalid_inventory"
    | "http";
  readonly status?: number;
  readonly recovery?: "reconnect" | "restart";
  readonly retryAfter?: string | null;

  constructor(
    stage: ConsentInventoryFailure["stage"],
    reason: ConsentInventoryFailure["reason"],
    status?: number,
    recovery?: ConsentInventoryFailure["recovery"],
    retryAfter?: string | null,
  ) {
    super("Consent inventory could not load");
    this.stage = stage;
    this.reason = reason;
    this.status = status;
    this.recovery = recovery;
    this.retryAfter = retryAfter;
  }
}

export function reportConsentInventoryFailure(
  failure: ConsentInventoryFailure,
  flowID?: string,
): void {
  // Never log an SDK error, a URL, server response text, or consent credentials.
  console.warn("Consent inventory could not load", {
    ...(/^[a-zA-Z0-9_-]{1,64}$/.test(flowID ?? "") ? { flowID } : {}),
    stage: failure.stage,
    reason:
      failure.status === undefined ? failure.reason : `http_${failure.status}`,
  });
}

export const RESTART_CONSENT_MESSAGE =
  "This authorization request is no longer valid. Return to the client app and start the connection again.";

export const CONSENT_INVENTORY_DEADLINE_MS = 10000;
const MAX_RETRY_DELAY_MS = 2000;
const TRANSIENT_HTTP_STATUSES = new Set([408, 429, 502, 503, 504]);

function transientRetryDelay(failure: ConsentInventoryFailure): number | null {
  if (
    failure.recovery ||
    failure.status === undefined ||
    !TRANSIENT_HTTP_STATUSES.has(failure.status)
  )
    return null;
  const value = failure.retryAfter?.trim();
  if (!value) return 250;
  const delay = /^\d+$/.test(value)
    ? Number(value) * 1000
    : value.endsWith(" GMT")
      ? Math.max(0, Date.parse(value) - Date.now())
      : NaN;
  // Do not retry sooner than the server requested if it exceeds our budget.
  return delay <= MAX_RETRY_DELAY_MS ? delay : null;
}

export async function loadConsentInventory({
  toolsUrl,
  state,
  csrfToken,
  signal,
  authClient,
  flowID,
}: {
  toolsUrl: string;
  state: string;
  csrfToken: string;
  signal?: AbortSignal;
  authClient?: string;
  flowID?: string;
}): Promise<ConsentInventory & { attempt: string }> {
  const controller = new AbortController();
  const combined = signal
    ? AbortSignal.any([controller.signal, signal])
    : controller.signal;
  let stage: DiscoveryStage = "initialize";
  let retryTimer: ReturnType<typeof setTimeout> | undefined;
  let onAbort: (() => void) | undefined;
  const deadline = setTimeout(() => {
    controller.abort(new ConsentInventoryFailure(stage, "timeout"));
  }, CONSENT_INVENTORY_DEADLINE_MS);
  const interrupted = new Promise<never>((_resolve, reject) => {
    onAbort = () => reject(combined.reason);
    if (combined.aborted) onAbort();
    else combined.addEventListener("abort", onAbort, { once: true });
  });
  const load = async (
    mayRetry: boolean,
  ): Promise<ConsentInventory & { attempt: string }> => {
    combined.throwIfAborted();
    stage = "initialize";
    try {
      const attempt = crypto.randomUUID();
      const inventory = await fetchConsentInventory(
        toolsUrl,
        state,
        csrfToken,
        attempt,
        combined,
        authClient,
        () => {
          stage = "tools/list";
        },
      );
      return { ...inventory, attempt };
    } catch (err) {
      combined.throwIfAborted();
      const failure =
        err instanceof ConsentInventoryFailure
          ? err
          : new ConsentInventoryFailure(stage, "invalid_inventory");
      const delay = mayRetry ? transientRetryDelay(failure) : null;
      if (delay === null) throw failure;
      reportConsentInventoryFailure(failure, flowID);
      await new Promise<void>((resolve) => {
        retryTimer = setTimeout(resolve, delay);
      });
      return load(false);
    }
  };
  try {
    // Bound the entire operation, including a stalled SDK request or cleanup.
    return await Promise.race([interrupted, load(true)]);
  } catch (err) {
    if (err instanceof ConsentInventoryFailure)
      reportConsentInventoryFailure(err, flowID);
    throw err;
  } finally {
    clearTimeout(deadline);
    clearTimeout(retryTimer);
    if (onAbort) combined.removeEventListener("abort", onAbort);
  }
}

/**
 * The island is an ordinary MCP client of the consent-scoped transport: the
 * official SDK performs initialize, paginated tools/list, and session
 * termination against the URL the page provided; state and CSRF ride
 * headers so every message stays pure MCP. The attempt id keys the
 * server-side inventory snapshot this session accumulates — approval later
 * binds to it, so the caller must submit it only after this resolves. Direct
 * typed clients pass their selected auth client on every transport request.
 */
export async function fetchConsentInventory(
  toolsUrl: string,
  state: string,
  csrfToken: string,
  attempt: string,
  signal?: AbortSignal,
  authClient?: string,
  onList?: () => void,
): Promise<ConsentInventory> {
  let stage: DiscoveryStage = "initialize";
  const transport = new StreamableHTTPClientTransport(
    new URL(toolsUrl, window.location.origin),
    {
      // Only the bounded discovery retry above may start a new request after failure.
      reconnectionOptions: {
        initialReconnectionDelay: 0,
        maxReconnectionDelay: 0,
        reconnectionDelayGrowFactor: 1,
        maxRetries: 0,
      },
      // The SDK replaces requestInit.signal with its own controller. Combine
      // the discovery deadline at the fetch boundary. Session termination must
      // survive discovery cancellation, but gets its own bounded deadline.
      fetch: async (input, init) => {
        const requestSignal =
          init?.method === "DELETE"
            ? AbortSignal.timeout(2000)
            : signal && init?.signal
              ? AbortSignal.any([signal, init.signal])
              : (signal ?? init?.signal);
        const isRequest = init?.method === "POST";
        const response = await fetch(input, {
          ...init,
          signal: requestSignal,
        }).catch((err: unknown) => {
          signal?.throwIfAborted();
          if (isRequest) throw new ConsentInventoryFailure(stage, "network");
          throw err;
        });
        // The SDK error discards response headers and may include response
        // text. Preserve only the allowlisted recovery hint on MCP POSTs.
        if (isRequest && !response.ok) {
          void response.body?.cancel().catch(() => undefined);
          const recovery = response.headers.get("X-Gram-Consent-Recovery");
          throw new ConsentInventoryFailure(
            stage,
            "http",
            response.status,
            recovery === "reconnect" || recovery === "restart"
              ? recovery
              : undefined,
            response.headers.get("Retry-After"),
          );
        }
        return response;
      },
      requestInit: {
        credentials: "omit",
        headers: {
          "Gram-Consent-State": state,
          "Gram-Consent-Csrf": csrfToken,
          "Gram-Consent-Inventory-Attempt": attempt,
          ...(authClient ? { "Gram-Consent-Auth-Client": authClient } : {}),
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
    signal?.throwIfAborted();
    stage = "tools/list";
    onList?.();
    const tools: ConsentTool[] = [];
    const seen = new Set<string>();
    const roleHiddenTools: RoleHiddenTools = { count: 0, names: [] };
    let cursor: string | undefined = undefined;
    do {
      const page = await client.listTools(cursor ? { cursor } : undefined);
      signal?.throwIfAborted();
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
  } catch (err) {
    signal?.throwIfAborted();
    if (err instanceof ConsentInventoryFailure) throw err;
    if (
      err instanceof StreamableHTTPError &&
      err.code !== undefined &&
      err.code >= 100 &&
      err.code <= 599
    ) {
      throw new ConsentInventoryFailure(stage, "http", err.code);
    }
    throw new ConsentInventoryFailure(stage, "invalid_inventory");
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

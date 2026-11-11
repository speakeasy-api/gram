import { useQuery } from "@tanstack/react-query";
import { z } from "zod";

/** One member as reported by the gateway's own list_servers tool. */
const listedServerSchema = z.object({
  slug: z.string(),
  name: z.string().optional(),
  sortOrder: z.number().optional(),
  status: z.string().optional(),
});

const toolSchema = z
  .object({
    name: z.string(),
    description: z.string().optional(),
    inputSchema: z.unknown().optional(),
  })
  .passthrough();

type GatewayTool = z.infer<typeof toolSchema>;
type GatewayListedServer = z.infer<typeof listedServerSchema>;

export interface GatewayInspection {
  discoveryMode: "direct" | "progressive" | "code_mode";
  /** Sent to every client on connect; the runtime generates it. */
  instructions: string | undefined;
  protocolVersion: string | undefined;
  serverName: string | undefined;
  tools: GatewayTool[];
  /** The gateway's own list_servers result, or undefined if the call failed. */
  servers: GatewayListedServer[] | undefined;
}

const PROTOCOL_VERSION = "2026-07-28";

/**
 * A cache-key fragment that changes when the credentials do, without putting
 * the bearer itself in the query key (react-query keys are readable in
 * devtools, and a rotating token would also accumulate cache entries).
 */
export function authCacheKey(
  headers: Record<string, string> | undefined,
): string {
  // Either credential the dashboard sends: a bearer, or a chat session token.
  const auth = headers?.["Authorization"] ?? headers?.["Gram-Chat-Session"];
  if (!auth) return "anon";
  let hash = 0;
  for (let i = 0; i < auth.length; i += 1) {
    hash = (Math.imul(31, hash) + auth.charCodeAt(i)) | 0;
  }
  return `auth:${hash}`;
}

/**
 * The gateway answered with an error instead of a result. `code` is the
 * JSON-RPC error code when the answer carried one.
 */
export class GatewayRPCError extends Error {
  readonly status: number;
  readonly code: number | undefined;

  constructor(message: string, status: number, code?: number) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

class UnauthorizedError extends GatewayRPCError {}

const rpcErrorSchema = z.object({
  code: z.number().optional(),
  message: z.string(),
});

/** What the HTTP exchange behind one JSON-RPC call looked like. */
export interface GatewayRPCExchange {
  request: unknown;
  status: number;
  headers: Headers;
  /** The decoded JSON-RPC envelope, when the body was readable. */
  response: unknown;
}

// The endpoint speaks streamable HTTP and answers stateless requests, so one
// fetch per JSON-RPC call is enough — no session to establish or carry.
export async function gatewayRPC(
  url: string,
  headers: Record<string, string> | undefined,
  method: string,
  params: unknown,
  signal?: AbortSignal,
  /** `null` sends a notification: no id, and no result to wait for. */
  requestId: string | null = crypto.randomUUID(),
  /** Sees the raw exchange, for callers that show the wire to the user. */
  observe?: (exchange: GatewayRPCExchange) => void,
): Promise<unknown> {
  const requestBody =
    requestId === null
      ? { jsonrpc: "2.0", method, params }
      : { jsonrpc: "2.0", id: requestId, method, params };
  const response = await fetch(url, {
    method: "POST",
    signal,
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
      ...headers,
    },
    body: JSON.stringify(requestBody),
  });
  const report = (decoded: unknown) =>
    observe?.({
      request: requestBody,
      status: response.status,
      headers: response.headers,
      response: decoded,
    });

  if (response.status === 401 || response.status === 403) {
    report(undefined);
    throw new UnauthorizedError("unauthorized", response.status);
  }
  if (!response.ok) {
    const failure: unknown = await response.json().catch(() => undefined);
    report(failure);
    const message = z.object({ error: rpcErrorSchema }).safeParse(failure);
    if (message.success)
      throw new GatewayRPCError(
        message.data.error.message,
        response.status,
        message.data.error.code,
      );
    throw new GatewayRPCError(
      `${method} failed: ${response.status}`,
      response.status,
    );
  }

  const body = await response.text();
  // A notification is acknowledged with an empty 202.
  if (requestId === null) {
    report(undefined);
    return undefined;
  }
  // A streamable-HTTP server may answer either as JSON or as a one-event SSE
  // stream; the payload is the same either way.
  const payload = response.headers
    .get("content-type")
    ?.includes("text/event-stream")
    ? body
        .split("\n")
        .find((line) => line.startsWith("data:"))
        ?.slice("data:".length)
        .trim()
    : body;

  let parsed: unknown;
  try {
    parsed = JSON.parse(payload ?? "{}");
  } catch (error) {
    report(undefined);
    throw error;
  }
  report(parsed);
  const envelope = z
    .object({
      result: z.unknown().optional(),
      error: rpcErrorSchema.optional(),
    })
    .parse(parsed);

  if (envelope.error) {
    const { code, message } = envelope.error;
    // The runtime reports an expired or absent session as a JSON-RPC error
    // rather than an HTTP status.
    if (/token|unauthor/i.test(message)) {
      throw new UnauthorizedError(message, response.status, code);
    }
    throw new GatewayRPCError(message, response.status, code);
  }
  return envelope.result;
}

/**
 * Reads the gateway's live MCP surface — the instructions sent on connect, the
 * fixed tool set, and the current list_servers result — by talking to the
 * endpoint exactly as a client would. Nothing here is derived from dashboard
 * state, so what it renders is what a client actually receives.
 */
export function useGatewayInspection(
  mcpUrl: string | undefined,
  options?: {
    headers?: Record<string, string>;
    enabled?: boolean;
    configurationKey?: string;
  },
): {
  data: GatewayInspection | undefined;
  isLoading: boolean;
  isError: boolean;
  needsAuth: boolean;
  error: Error | null;
  refetch: () => void;
} {
  const { headers, enabled = true, configurationKey } = options ?? {};
  const headersKey = authCacheKey(headers);

  const query = useQuery<GatewayInspection, Error>({
    queryKey: ["gatewayInspection", mcpUrl, headersKey, configurationKey],
    queryFn: async () => {
      if (!mcpUrl) throw new Error("No gateway URL configured");

      const initResult = await gatewayRPC(mcpUrl, headers, "initialize", {
        protocolVersion: PROTOCOL_VERSION,
        capabilities: {},
        clientInfo: { name: "gram-dashboard-gateway-inspect", version: "1" },
      });
      const init = z
        .object({
          instructions: z.string().optional(),
          protocolVersion: z.string().optional(),
          serverInfo: z.object({ name: z.string().optional() }).optional(),
        })
        .parse(initResult);

      const tools: GatewayTool[] = [];
      const seenCursors = new Set<string>();
      let cursor: string | undefined;
      do {
        const toolsResult = await gatewayRPC(mcpUrl, headers, "tools/list", {
          cursor,
        });
        const listing = z
          .object({
            tools: z.array(toolSchema),
            nextCursor: z.string().optional(),
          })
          .parse(toolsResult);
        tools.push(...listing.tools);
        cursor = listing.nextCursor;
        if (cursor) {
          if (seenCursors.has(cursor) || seenCursors.size >= 100)
            throw new Error(
              "The gateway returned an incomplete tool list. Reconnect to try again.",
            );
          seenCursors.add(cursor);
        }
      } while (cursor);
      let discoveryMode: GatewayInspection["discoveryMode"] = "direct";
      if (tools.some((tool) => tool.name === "list_servers"))
        discoveryMode = "progressive";
      if (tools.length === 1 && tools[0]?.name === "execute")
        discoveryMode = "code_mode";

      // Bundle state is a best-effort extra: a member outage must not blank
      // out the tool surface above, which is the tab's primary content.
      let servers: GatewayListedServer[] | undefined;
      if (discoveryMode === "progressive") {
        try {
          const callResult = await gatewayRPC(mcpUrl, headers, "tools/call", {
            name: "list_servers",
            arguments: {},
          });
          servers = z
            .object({
              structuredContent: z.object({
                servers: z.array(listedServerSchema),
              }),
            })
            .parse(callResult).structuredContent.servers;
        } catch {
          servers = undefined;
        }
      }

      return {
        discoveryMode,
        instructions: init.instructions,
        protocolVersion: init.protocolVersion,
        serverName: init.serverInfo?.name,
        tools,
        servers,
      };
    },
    enabled: enabled && !!mcpUrl,
    retry: false,
    staleTime: 5 * 60 * 1000,
    throwOnError: false,
  });

  return {
    data: query.data,
    isLoading: query.isLoading && query.fetchStatus !== "idle",
    isError: query.isError,
    needsAuth: query.error instanceof UnauthorizedError,
    error: query.error,
    refetch: () => void query.refetch(),
  };
}

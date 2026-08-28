import { useQuery } from "@tanstack/react-query";
import { z } from "zod";

/** One member as reported by the gateway's own list_servers tool. */
const listedServerSchema = z.object({
  slug: z.string(),
  name: z.string().optional(),
  sortOrder: z.number().optional(),
  status: z.string().optional(),
});

const toolSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  inputSchema: z.unknown().optional(),
});

type GatewayTool = z.infer<typeof toolSchema>;
type GatewayListedServer = z.infer<typeof listedServerSchema>;

/** One member's catalog as returned by describe_server. */
export interface GatewayServerDescription {
  server: string;
  /** The tool's structured result, or the error a client would receive. */
  result: unknown;
  error?: string;
}

export interface GatewayInspection {
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
function authCacheKey(headers: Record<string, string> | undefined): string {
  const auth = headers?.["Authorization"];
  if (!auth) return "anon";
  let hash = 0;
  for (let i = 0; i < auth.length; i += 1) {
    hash = (Math.imul(31, hash) + auth.charCodeAt(i)) | 0;
  }
  return `auth:${hash}`;
}

class UnauthorizedError extends Error {}

// The endpoint speaks streamable HTTP and answers stateless requests, so one
// fetch per JSON-RPC call is enough — no session to establish or carry.
async function rpc(
  url: string,
  headers: Record<string, string> | undefined,
  method: string,
  params: unknown,
): Promise<unknown> {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
      ...headers,
    },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
  });

  if (response.status === 401 || response.status === 403) {
    throw new UnauthorizedError("unauthorized");
  }
  if (!response.ok) {
    throw new Error(`${method} failed: ${response.status}`);
  }

  const body = await response.text();
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

  const parsed: unknown = JSON.parse(payload ?? "{}");
  const envelope = z
    .object({
      result: z.unknown().optional(),
      error: z.object({ message: z.string() }).optional(),
    })
    .parse(parsed);

  if (envelope.error) {
    // The runtime reports an expired or absent session as a JSON-RPC error
    // rather than an HTTP status.
    if (/token|unauthor/i.test(envelope.error.message)) {
      throw new UnauthorizedError(envelope.error.message);
    }
    throw new Error(envelope.error.message);
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
  options?: { headers?: Record<string, string>; enabled?: boolean },
): {
  data: GatewayInspection | undefined;
  isLoading: boolean;
  isError: boolean;
  needsAuth: boolean;
  error: Error | null;
  refetch: () => void;
} {
  const { headers, enabled = true } = options ?? {};
  const headersKey = authCacheKey(headers);

  const query = useQuery<GatewayInspection, Error>({
    queryKey: ["gatewayInspection", mcpUrl, headersKey],
    queryFn: async () => {
      if (!mcpUrl) throw new Error("No gateway URL configured");

      const initResult = await rpc(mcpUrl, headers, "initialize", {
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

      const toolsResult = await rpc(mcpUrl, headers, "tools/list", {});
      const { tools } = z
        .object({ tools: z.array(toolSchema) })
        .parse(toolsResult);

      // Bundle state is a best-effort extra: a member outage must not blank
      // out the tool surface above, which is the tab's primary content.
      let servers: GatewayListedServer[] | undefined;
      try {
        const callResult = await rpc(mcpUrl, headers, "tools/call", {
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

      return {
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

/**
 * describe_server for one member — the drill-down step the instructions tell
 * an agent to take after list_servers. Kept separate from the handshake so
 * switching members re-runs only this call. A member the runtime can't
 * describe yet (proxied members, until their upstream sessions land) answers
 * with an error, which is itself what a client would see.
 */
export function useGatewayDescribeServer(
  mcpUrl: string | undefined,
  serverSlug: string | undefined,
  options?: { headers?: Record<string, string>; enabled?: boolean },
): {
  data: GatewayServerDescription | undefined;
  isLoading: boolean;
} {
  const { headers, enabled = true } = options ?? {};
  const headersKey = authCacheKey(headers);

  const query = useQuery<GatewayServerDescription, Error>({
    queryKey: ["gatewayDescribeServer", mcpUrl, serverSlug, headersKey],
    queryFn: async () => {
      if (!mcpUrl || !serverSlug) throw new Error("No member selected");
      try {
        const described = await rpc(mcpUrl, headers, "tools/call", {
          name: "describe_server",
          arguments: { server: serverSlug },
        });
        const { structuredContent, content, isError } = z
          .object({
            structuredContent: z.unknown().optional(),
            content: z
              .array(z.object({ text: z.string().optional() }))
              .optional(),
            isError: z.boolean().optional(),
          })
          .parse(described);
        // A tool-level failure answers 200 with isError and a text body, so
        // without this branch it would render as a normal catalog.
        if (isError) {
          return {
            server: serverSlug,
            result: undefined,
            error: content?.[0]?.text ?? "describe_server reported an error",
          };
        }
        return {
          server: serverSlug,
          result: structuredContent ?? content?.[0]?.text,
        };
      } catch (describeError) {
        return {
          server: serverSlug,
          result: undefined,
          error:
            describeError instanceof Error
              ? describeError.message
              : "describe_server failed",
        };
      }
    },
    enabled: enabled && !!mcpUrl && !!serverSlug,
    retry: false,
    staleTime: 5 * 60 * 1000,
    throwOnError: false,
  });

  return {
    data: query.data,
    isLoading: query.isLoading && query.fetchStatus !== "idle",
  };
}

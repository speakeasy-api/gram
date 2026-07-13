import { createMCPClient } from "@ai-sdk/mcp";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { z } from "zod";

/**
 * MCP tool annotation hints (the optional `annotations` object on a tool in the
 * `tools/list` response). All fields are advisory and may be absent; malformed
 * hint values are dropped (`.catch`) rather than failing the whole decode.
 */
const remoteMcpToolAnnotationsSchema = z.object({
  /** Human-friendly display name for the tool. */
  title: z.string().optional(),
  readOnlyHint: z.boolean().optional().catch(undefined),
  destructiveHint: z.boolean().optional().catch(undefined),
  idempotentHint: z.boolean().optional().catch(undefined),
  openWorldHint: z.boolean().optional().catch(undefined),
});

/** A single tool from the `tools/list` response (extra fields are ignored). */
const remoteMcpToolSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  // Raw JSON Schema for the tool's input; kept opaque and parsed lazily in the
  // UI when rendering parameters.
  inputSchema: z.unknown().optional(),
  annotations: remoteMcpToolAnnotationsSchema.optional(),
});

/** The decoded shape of an MCP `tools/list` result. */
const listToolsResultSchema = z.object({
  tools: z.array(remoteMcpToolSchema),
});

export type RemoteMcpToolAnnotations = z.infer<
  typeof remoteMcpToolAnnotationsSchema
>;

/** The slice of an MCP tool we surface for the dashboard listing. */
export interface RemoteMcpTool {
  description?: string;
  inputSchema?: unknown;
  annotations?: RemoteMcpToolAnnotations;
}

/** A record of tools keyed by tool name, mirroring the `tools/list` response. */
type RemoteMcpToolSet = Record<string, RemoteMcpTool>;

export interface UseRemoteMcpToolsResult {
  tools: RemoteMcpToolSet | undefined;
  isLoading: boolean;
  isError: boolean;
  /**
   * True when the connection was rejected for missing/expired credentials
   * (an MCP `initialize` / `tools/list` that yields a 401). The Authenticate
   * affordance hangs off this — wired up in a later increment.
   */
  needsAuth: boolean;
  error: Error | null;
  refetch: () => void;
}

export interface UseRemoteMcpToolsOptions {
  /**
   * Extra request headers for the MCP connection — e.g. the user-session JWT
   * (`Authorization: Bearer …`) the runtime gateway uses to resolve the
   * dashboard user's stored upstream credentials.
   */
  headers?: Record<string, string>;
}

/**
 * Connects to a Gram-proxied remote MCP endpoint and lists its tools.
 *
 * Issuer-gated servers need a user-session JWT passed via `options.headers`
 * (tunnelled through RemoteMcpConnection); without it they surface as
 * `needsAuth`.
 */
export function useRemoteMcpTools(
  mcpUrl: string | undefined,
  options?: UseRemoteMcpToolsOptions,
): UseRemoteMcpToolsResult {
  const { headers } = options ?? {};

  // Key on the header values so the query refetches once the JWT arrives or
  // rotates, without keying on object identity.
  const headersKey = headers
    ? Object.entries(headers)
        .map(([k, v]) => `${k}:${v}`)
        .sort()
    : [];

  const query: UseQueryResult<RemoteMcpToolSet, Error> = useQuery({
    queryKey: ["remoteMcpTools", mcpUrl, headersKey],
    queryFn: async () => {
      // The query is disabled for an undefined URL, but narrow for the type.
      if (!mcpUrl) throw new Error("No MCP URL configured");

      const client = await createMCPClient({
        name: "gram-dashboard-remote-mcp-client",
        transport: {
          type: "http",
          url: mcpUrl,
          headers,
          // @ai-sdk/mcp stores this on the transport instance and calls it as
          // `this.fetchFn(...)`; handing it bare `window.fetch` throws
          // "Illegal invocation" in browsers, so bind it via a wrapper.
          fetch: (...args: Parameters<typeof fetch>) =>
            globalThis.fetch(...args),
        },
      });
      try {
        // Use the raw `tools/list` call rather than client.tools(): the latter
        // strips annotations (it forwards only name/description/inputSchema),
        // and we want the MCP annotation hints. inputSchema comes through
        // unwrapped, which the section's schema reader already handles.
        //
        // `listTools` isn't on the public `MCPClient` type (which only ships
        // *executable* tools via `tools()`), so we reach it structurally and
        // decode its untyped result with Zod rather than asserting a shape.
        // Tracked for the ai-v6 migration in AIS-169.
        const raw = await (
          client as unknown as { listTools: () => Promise<unknown> }
        ).listTools();
        const { tools } = listToolsResultSchema.parse(raw);

        const toolSet: RemoteMcpToolSet = {};
        for (const tool of tools) {
          toolSet[tool.name] = {
            description: tool.description,
            inputSchema: tool.inputSchema,
            annotations: tool.annotations,
          };
        }
        return toolSet;
      } finally {
        // Streamable HTTP keeps a connection open; release it once we have the
        // tool list so we don't leak sockets across refetches.
        await client.close().catch(() => {});
      }
    },
    enabled: !!mcpUrl,
    // Auth-related failures shouldn't be hammered; the user re-triggers via the
    // Authenticate flow or a manual refetch.
    retry: false,
    staleTime: 5 * 60 * 1000,
    // The dashboard QueryClient throws query errors to the nearest error
    // boundary by default. A 401 is an expected state here — it means the user
    // must connect upstream — so keep it inline (`needsAuth`) and only let
    // genuinely unexpected failures escape to the boundary.
    throwOnError: (error) => !isUnauthorizedError(error),
  });

  return {
    tools: query.data,
    isLoading: query.isLoading && query.fetchStatus !== "idle",
    isError: query.isError,
    needsAuth: query.isError && isUnauthorizedError(query.error),
    error: query.error,
    refetch: () => void query.refetch(),
  };
}

/**
 * Best-effort detection of a 401 from the AI SDK MCP client. The SDK wraps the
 * transport error rather than exposing the HTTP status directly, so we sniff
 * the message. Good enough to drive the empty state now; the later auth-challenge
 * increment can tighten this against the protected-resource metadata.
 */
function isUnauthorizedError(error: Error | null): boolean {
  if (!error) return false;
  const message = error.message.toLowerCase();
  return message.includes("401") || message.includes("unauthorized");
}

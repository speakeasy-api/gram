import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { z } from "zod";
import { createMCPClientWithVersionFallback } from "@/lib/mcp-client";

const INSPECT_META_KEY = "com.speakeasy/inspect";
const UNAVAILABLE_META_KEY = "com.speakeasy/unavailable";
const UNAVAILABLE_REASON_META_KEY = "com.speakeasy/unavailable_reason";

/** Why an inspected listing marked a tool unavailable to this viewer. */
const unavailableReasonSchema = z.enum(["blocked", "read_only", "not_granted"]);
export type ProxiedMcpToolUnavailableReason = z.infer<
  typeof unavailableReasonSchema
>;

/**
 * Most tools an inspected listing may return, counting repeats. Real servers list
 * at most hundreds; this stops a server repeating tools from holding the tab open.
 */
const MAX_LISTED_TOOLS = 10_000;

/**
 * Most pages an inspected listing may read. Ten tools a page reaches 2,000 tools,
 * well past real servers; this stops a server returning empty pages with fresh
 * cursors, which the tool limit can't see.
 */
const MAX_LISTED_PAGES = 200;

/**
 * MCP tool annotation hints (the optional `annotations` object on a tool in the
 * `tools/list` response). All fields are advisory and may be absent; malformed
 * hint values are dropped (`.catch`) rather than failing the whole decode.
 */
const proxiedMcpToolAnnotationsSchema = z.object({
  /** Human-friendly display name for the tool. */
  title: z.string().optional(),
  readOnlyHint: z.boolean().optional().catch(undefined),
  destructiveHint: z.boolean().optional().catch(undefined),
  idempotentHint: z.boolean().optional().catch(undefined),
  openWorldHint: z.boolean().optional().catch(undefined),
});

/** A single tool from the `tools/list` response (extra fields are ignored). */
const proxiedMcpToolSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  // Raw JSON Schema for the tool's input; kept opaque and parsed lazily in the
  // UI when rendering parameters.
  inputSchema: z.unknown().optional(),
  annotations: proxiedMcpToolAnnotationsSchema.optional(),
  _meta: z
    .object({
      [UNAVAILABLE_META_KEY]: z.boolean().optional().catch(undefined),
      [UNAVAILABLE_REASON_META_KEY]: unavailableReasonSchema
        .optional()
        .catch(undefined),
    })
    .optional()
    .catch(undefined),
});

/** The decoded shape of one page of an MCP `tools/list` result. */
const listToolsResultSchema = z.object({
  tools: z.array(proxiedMcpToolSchema),
  nextCursor: z.string().optional(),
});

export type ProxiedMcpToolAnnotations = z.infer<
  typeof proxiedMcpToolAnnotationsSchema
>;

/** The slice of an MCP tool we surface for the dashboard listing. */
export interface ProxiedMcpTool {
  description?: string;
  inputSchema?: unknown;
  annotations?: ProxiedMcpToolAnnotations;
  /** The server offers this tool, but this viewer's access doesn't include it. */
  unavailable?: boolean;
  /** Why it is unavailable, when the server says. */
  unavailableReason?: ProxiedMcpToolUnavailableReason;
}

/** A record of tools keyed by tool name, mirroring the `tools/list` response. */
type ProxiedMcpToolSet = Record<string, ProxiedMcpTool>;

export interface UseProxiedMcpToolsResult {
  tools: ProxiedMcpToolSet | undefined;
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

export interface UseProxiedMcpToolsOptions {
  /**
   * Extra request headers for the MCP connection — e.g. the user-session JWT
   * (`Authorization: Bearer …`) the runtime gateway uses to resolve the
   * dashboard user's stored upstream credentials.
   */
  headers?: Record<string, string>;
  /**
   * Gate the connection. Pass `false` while a required credential (the minted
   * JWT) is still loading so we don't fire an unauthenticated request and cache
   * a spurious `needsAuth`.
   */
  enabled?: boolean;
  /**
   * By default non-401 failures throw to the nearest error boundary (callers
   * like RemoteMcpToolsSection provide one). Pass `false` to keep every failure
   * inline via `isError` instead — for callers with no boundary that render a
   * retryable error state themselves.
   */
  throwOnError?: boolean;
  /**
   * Read the server's whole catalog: every page, with tools this viewer can't use
   * marked `unavailable` rather than dropped. Needed by anything that writes stored
   * tool metadata from the result, where a partial list reads as removals. Without
   * it the result is what this viewer can use, from the first page.
   */
  inspect?: boolean;
}

/**
 * Connects to a Speakeasy-proxied MCP endpoint and lists its tools.
 *
 * Issuer-gated servers need a user-session JWT passed via `options.headers`
 * (minted by useUserSessionToken); without it they surface as
 * `needsAuth`.
 */
export function useProxiedMcpTools(
  mcpUrl: string | undefined,
  options?: UseProxiedMcpToolsOptions,
): UseProxiedMcpToolsResult {
  const {
    headers,
    enabled = true,
    throwOnError,
    inspect = false,
  } = options ?? {};

  // Key on the header values so the query refetches once the JWT arrives or
  // rotates, without keying on object identity.
  const headersKey = headers
    ? Object.entries(headers)
        .map(([k, v]) => `${k}:${v}`)
        .sort()
    : [];

  const query: UseQueryResult<ProxiedMcpToolSet, Error> = useQuery({
    queryKey: ["proxiedMcpTools", mcpUrl, headersKey, inspect],
    queryFn: async () => {
      // `enabled` guards against an undefined URL, but narrow for the type.
      if (!mcpUrl) throw new Error("No MCP URL configured");

      const client = await createMCPClientWithVersionFallback({
        clientName: "gram-dashboard-proxied-mcp-client",
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
        const lister = client as unknown as {
          listTools: (args: {
            params?: { cursor?: string; _meta: Record<string, unknown> };
          }) => Promise<unknown>;
        };

        const toolSet: ProxiedMcpToolSet = {};
        const seenCursors = new Set<string>();
        let toolsReceived = 0;
        let pagesRead = 0;
        let cursor: string | undefined;
        do {
          const raw = await lister.listTools(
            inspect
              ? {
                  params: {
                    ...(cursor ? { cursor } : {}),
                    _meta: { [INSPECT_META_KEY]: true },
                  },
                }
              : {},
          );
          const page = listToolsResultSchema.parse(raw);
          for (const tool of page.tools) {
            toolSet[tool.name] = {
              description: tool.description,
              inputSchema: tool.inputSchema,
              annotations: tool.annotations,
              unavailable: tool._meta?.[UNAVAILABLE_META_KEY] === true,
              unavailableReason: tool._meta?.[UNAVAILABLE_REASON_META_KEY],
            };
          }

          if (!inspect) break;

          toolsReceived += page.tools.length;
          pagesRead += 1;
          if (
            toolsReceived > MAX_LISTED_TOOLS ||
            pagesRead >= MAX_LISTED_PAGES
          ) {
            throw new Error("MCP server listed too many tools");
          }

          cursor = page.nextCursor || undefined;
          if (cursor && seenCursors.has(cursor)) {
            throw new Error("MCP server repeated a tools/list cursor");
          }
          if (cursor) seenCursors.add(cursor);
        } while (cursor);

        return toolSet;
      } finally {
        // Streamable HTTP keeps a connection open; release it once we have the
        // tool list so we don't leak sockets across refetches.
        await client.close().catch(() => {});
      }
    },
    enabled: enabled && !!mcpUrl,
    // Auth-related failures shouldn't be hammered; the user re-triggers via the
    // Authenticate flow or a manual refetch.
    retry: false,
    staleTime: 5 * 60 * 1000,
    // The dashboard QueryClient throws query errors to the nearest error
    // boundary by default. A 401 is an expected state here — it means the user
    // must connect upstream — so keep it inline (`needsAuth`) and only let
    // genuinely unexpected failures escape to the boundary. Callers without a
    // boundary pass `throwOnError: false` to keep every failure inline.
    throwOnError:
      throwOnError === false ? false : (error) => !isUnauthorizedError(error),
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

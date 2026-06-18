import { createMCPClient } from "@ai-sdk/mcp";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

/**
 * The tool map returned by the AI SDK MCP client's `tools()` call: a record
 * keyed by tool name. We keep the SDK's shape rather than re-modeling it so the
 * connection stays as vanilla as the playground / elements path
 * (see elements/src/hooks/useMCPTools.ts).
 */
export type RemoteMcpToolSet = Awaited<
  ReturnType<Awaited<ReturnType<typeof createMCPClient>>["tools"]>
>;

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
  /**
   * Gate the connection. Pass `false` while a required credential (the minted
   * JWT) is still loading so we don't fire an unauthenticated request and cache
   * a spurious `needsAuth`.
   */
  enabled?: boolean;
}

/**
 * Connects to a Gram-proxied remote MCP endpoint and lists its tools.
 *
 * Issuer-gated servers need a user-session JWT passed via `options.headers`
 * (minted by useRemoteMcpUserSessionToken); without it they surface as
 * `needsAuth`.
 */
export function useRemoteMcpTools(
  mcpUrl: string | undefined,
  options?: UseRemoteMcpToolsOptions,
): UseRemoteMcpToolsResult {
  const { headers, enabled = true } = options ?? {};

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
      // `enabled` guards against an undefined URL, but narrow for the type.
      if (!mcpUrl) throw new Error("No MCP URL configured");

      const client = await createMCPClient({
        name: "gram-dashboard-remote-mcp-client",
        transport: { type: "http", url: mcpUrl, headers },
      });
      try {
        return await client.tools();
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

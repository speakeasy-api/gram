import { assert } from "@/lib/utils";
import type { MCPServerEntry } from "@/types";
import { ToolsFilter } from "@/types";
import { experimental_createMCPClient as createMCPClient } from "@ai-sdk/mcp";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useMemo, useRef } from "react";
import { trackError } from "@/lib/errorTracking";
import { Auth } from "./useAuth";

type MCPToolsResult = Awaited<
  ReturnType<Awaited<ReturnType<typeof createMCPClient>>["tools"]>
>;

interface NormalizedServer {
  url: string;
  /** Namespace prefix for this server's tools; only consulted when more than one server is configured. */
  name: string | undefined;
  environment: string | undefined;
}

export function useMCPTools({
  auth,
  mcp,
  mcps,
  environment,
  toolsToInclude,
  gramEnvironment,
}: {
  auth: Auth;
  mcp: string | undefined;
  mcps: MCPServerEntry[] | undefined;
  environment: Record<string, unknown>;
  toolsToInclude?: ToolsFilter;
  /** Fallback `Gram-Environment` for the legacy single-`mcp` path only; ignored when `mcps` is set. */
  gramEnvironment?: string;
}): UseQueryResult<MCPToolsResult, Error> & {
  mcpHeaders: Record<string, string>;
} {
  const servers = useMemo(
    () => normalizeServers(mcp, mcps, gramEnvironment),
    [mcp, mcps, gramEnvironment],
  );

  const envQueryKey = Object.entries(environment ?? {}).map(
    ([k, v]) => `${k}:${v}`,
  );
  const authQueryKey = Object.entries(auth.headers ?? {}).map(
    ([k, v]) => `${k}:${v}`,
  );
  const serversQueryKey = servers.map(
    (s) => `${s.url}|${s.name ?? ""}|${s.environment ?? ""}`,
  );

  // Each MCP transport stores a direct reference to its headers object and
  // spreads it on every send, so writes propagate to subsequent tool-call
  // requests. `mcpHeaders` is a write-only proxy that fans cross-cutting
  // header writes (e.g. Gram-Chat-ID) out to every per-server record.
  const perServerHeadersRef = useRef<Record<string, string>[]>([]);
  // When some servers fail and some succeed we still resolve with the partial
  // tool map, but flip this so the query is treated as stale — natural refetch
  // triggers (window focus, reconnect) can then recover the missing servers
  // instead of the partial result being cached for the session.
  const partialFailureRef = useRef(false);
  const headersProxyRef = useRef<Record<string, string> | null>(null);
  if (!headersProxyRef.current) {
    headersProxyRef.current = new Proxy<Record<string, string>>(
      {},
      {
        set(_, key, value) {
          for (const h of perServerHeadersRef.current) {
            h[key as string] = value as string;
          }
          return true;
        },
        deleteProperty(_, key) {
          for (const h of perServerHeadersRef.current) {
            delete h[key as string];
          }
          return true;
        },
      },
    );
  }
  const mcpHeaders = headersProxyRef.current;

  const queryResult = useQuery({
    queryKey: ["mcpTools", serversQueryKey, envQueryKey, authQueryKey],
    queryFn: async () => {
      assert(!auth.isLoading, "No auth found");
      assert(servers.length > 0, "No MCP server configured");

      const envHeaders = transformEnvironmentToHeaders(environment ?? {});
      const authHeaders = auth.headers ?? {};

      const serverEntries = servers.map((server) => ({
        server,
        headers: {
          ...envHeaders,
          ...authHeaders,
          ...(server.environment && {
            "Gram-Environment": server.environment,
          }),
        } satisfies Record<string, string>,
      }));
      perServerHeadersRef.current = serverEntries.map((s) => s.headers);

      const settled = await Promise.allSettled(
        serverEntries.map(async ({ server, headers }) => {
          const client = await createMCPClient({
            name: "gram-elements-mcp-client",
            transport: { type: "http", url: server.url, headers },
          });
          return { server, tools: await client.tools() };
        }),
      );

      const merged: MCPToolsResult = {};
      const namespaced = servers.length > 1;
      let rejected = 0;
      for (const [i, { server }] of serverEntries.entries()) {
        const result = settled[i]!;
        if (result.status === "rejected") {
          rejected++;
          trackError(result.reason, {
            source: "custom",
            scope: "mcp-tools",
            serverName: server.name ?? deriveNameFromUrl(server.url),
            serverUrl: server.url,
          });
          continue;
        }
        const prefix = namespaced
          ? (server.name ?? deriveNameFromUrl(server.url))
          : null;
        for (const [toolName, tool] of Object.entries(result.value.tools)) {
          const key = prefix ? `${prefix}__${toolName}` : toolName;
          merged[key] = tool;
        }
      }

      partialFailureRef.current =
        rejected > 0 && rejected < serverEntries.length;

      // Surface as a query rejection only when every server failed, so React
      // Query's retry/backoff still recovers from total outages (e.g. expired
      // auth hitting every server). Partial failures resolve normally.
      if (rejected > 0 && rejected === serverEntries.length) {
        const first = settled.find(
          (r): r is PromiseRejectedResult => r.status === "rejected",
        );
        throw first?.reason instanceof Error
          ? first.reason
          : new Error("All MCP servers failed to list tools");
      }

      return merged;
    },
    enabled: !auth.isLoading && servers.length > 0,
    staleTime: () => (partialFailureRef.current ? 0 : Infinity),
    gcTime: Infinity,
  });

  // Filter tools outside of the query to ensure filtering is applied whenever
  // toolsToInclude changes, even when the cached query result is reused.
  const tools = useMemo(() => {
    if (!queryResult.data || !toolsToInclude) {
      return queryResult.data;
    }

    return Object.fromEntries(
      Object.entries(queryResult.data).filter(([name]) =>
        typeof toolsToInclude === "function"
          ? toolsToInclude({ toolName: name })
          : toolsToInclude.includes(name),
      ),
    );
  }, [queryResult.data, toolsToInclude]);

  return {
    ...queryResult,
    data: tools,
    mcpHeaders,
  } as UseQueryResult<MCPToolsResult, Error> & {
    mcpHeaders: Record<string, string>;
  };
}

function normalizeServers(
  mcp: string | undefined,
  mcps: MCPServerEntry[] | undefined,
  fallbackEnv: string | undefined,
): NormalizedServer[] {
  if (mcps && mcps.length > 0) {
    if (mcp) warnMcpAndMcpsBothSet();
    return mcps.map((entry) => ({
      url: entry.url,
      name: entry.name,
      environment: entry.environment,
    }));
  }
  if (mcp) {
    return [{ url: mcp, name: undefined, environment: fallbackEnv }];
  }
  return [];
}

let warnedAboutBoth = false;
function warnMcpAndMcpsBothSet() {
  if (warnedAboutBoth) return;
  warnedAboutBoth = true;
  console.warn(
    "[gram-elements] Both `mcp` and `mcps` are set on ElementsConfig; `mcps` takes precedence and `mcp` is ignored.",
  );
}

function deriveNameFromUrl(url: string): string {
  let path: string;
  try {
    path = new URL(url).pathname;
  } catch {
    path = url;
  }
  const after = path.split("/mcp/")[1] ?? path;
  const sanitized = after
    .replace(/^\/+|\/+$/g, "")
    .replace(/[^a-zA-Z0-9_-]+/g, "_");
  return sanitized || "mcp";
}

const HEADER_PREFIX = "MCP-";

function transformEnvironmentToHeaders(environment: Record<string, unknown>) {
  if (typeof environment !== "object" || environment === null) {
    return {};
  }
  return Object.entries(environment).reduce(
    (acc, [key, value]) => {
      // Normalize key: replace underscores with dashes
      const normalizedKey = key.replace(/_/g, "-");

      // Add MCP- prefix if it doesn't already have it
      const headerKey = normalizedKey.startsWith(HEADER_PREFIX)
        ? normalizedKey
        : `${HEADER_PREFIX}${normalizedKey}`;

      acc[headerKey] = value as string;
      return acc;
    },
    {} as Record<string, string>,
  );
}

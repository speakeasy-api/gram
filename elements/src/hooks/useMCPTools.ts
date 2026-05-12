import { assert } from "@/lib/utils";
import type { MCPConfig, MCPServerEntry } from "@/types";
import { ToolsFilter } from "@/types";
import { experimental_createMCPClient as createMCPClient } from "@ai-sdk/mcp";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useMemo, useRef } from "react";
import { Auth } from "./useAuth";

type MCPToolsResult = Awaited<
  ReturnType<Awaited<ReturnType<typeof createMCPClient>>["tools"]>
>;

interface NormalizedServer {
  url: string;
  name: string;
  gramEnvironment: string | undefined;
}

export function useMCPTools({
  auth,
  mcp,
  environment,
  toolsToInclude,
  gramEnvironment,
}: {
  auth: Auth;
  mcp: MCPConfig | undefined;
  environment: Record<string, unknown>;
  toolsToInclude?: ToolsFilter;
  gramEnvironment?: string;
}): UseQueryResult<MCPToolsResult, Error> & {
  mcpHeaders: Record<string, string>;
} {
  const servers = useMemo(
    () => normalizeServers(mcp, gramEnvironment),
    [mcp, gramEnvironment],
  );

  const envQueryKey = Object.entries(environment ?? {}).map(
    ([k, v]) => `${k}:${v}`,
  );
  const authQueryKey = Object.entries(auth.headers ?? {}).map(
    ([k, v]) => `${k}:${v}`,
  );
  const serversQueryKey = servers.map(
    (s) => `${s.url}|${s.name}|${s.gramEnvironment ?? ""}`,
  );

  // Each MCP transport stores a direct reference to its headers object and
  // spreads it on every send, so writes propagate to subsequent tool-call
  // requests. `mcpHeaders` is a write-only proxy that fans cross-cutting
  // header writes (e.g. Gram-Chat-ID) out to every per-server record.
  const perServerHeadersRef = useRef<Record<string, string>[]>([]);
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
    queryKey: ["mcpTools", ...serversQueryKey, ...envQueryKey, ...authQueryKey],
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
          ...(server.gramEnvironment && {
            "Gram-Environment": server.gramEnvironment,
          }),
        } satisfies Record<string, string>,
      }));
      perServerHeadersRef.current = serverEntries.map((s) => s.headers);

      const perServerTools = await Promise.all(
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
      for (const { server, tools } of perServerTools) {
        for (const [toolName, tool] of Object.entries(tools)) {
          const key = namespaced ? `${server.name}__${toolName}` : toolName;
          merged[key] = tool;
        }
      }

      return merged;
    },
    enabled: !auth.isLoading && servers.length > 0,
    staleTime: Infinity,
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
  mcp: MCPConfig | undefined,
  fallbackEnv: string | undefined,
): NormalizedServer[] {
  if (!mcp) return [];
  const entries: Array<string | MCPServerEntry> = Array.isArray(mcp)
    ? mcp
    : [mcp];

  return entries.map((entry) => {
    if (typeof entry === "string") {
      return {
        url: entry,
        name: deriveNameFromUrl(entry),
        gramEnvironment: fallbackEnv,
      };
    }
    return {
      url: entry.url,
      name: entry.name ?? deriveNameFromUrl(entry.url),
      gramEnvironment: entry.gramEnvironment ?? fallbackEnv,
    };
  });
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

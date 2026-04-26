import { asMcpUrls } from "@/lib/api";
import { assert } from "@/lib/utils";
import { ServerUrl, ToolsFilter } from "@/types";
import { experimental_createMCPClient as createMCPClient } from "@ai-sdk/mcp";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useMemo, useRef } from "react";
import { Auth } from "./useAuth";

type MCPToolsResult = Awaited<
  ReturnType<Awaited<ReturnType<typeof createMCPClient>>["tools"]>
>;

export function useMCPTools({
  auth,
  mcp,
  environment,
  toolsToInclude,
  gramEnvironment,
}: {
  auth: Auth;
  mcp: ServerUrl | ServerUrl[] | undefined;
  environment: Record<string, unknown>;
  toolsToInclude?: ToolsFilter;
  gramEnvironment?: string;
}): UseQueryResult<MCPToolsResult, Error> & {
  mcpHeaders: Record<string, string>;
} {
  const urls = asMcpUrls(mcp);
  const envQueryKey = Object.entries(environment ?? {}).map(
    ([k, v]) => `${k}:${v}`,
  );
  const authQueryKey = Object.entries(auth.headers ?? {}).map(
    ([k, v]) => `${k}:${v}`,
  );

  // Mutable headers object shared with the MCP transport. The transport stores
  // a direct reference (`this.headers = headers`) and spreads it on every
  // send() call, so mutating properties on this object (e.g. setting
  // Gram-Chat-ID later) will be picked up by subsequent tool call requests.
  //
  // One shared headers object is reused across every MCP client instance.
  // That's safe here because all MCPs configured in `mcp` live on the same
  // Gram server in practice and share auth (session token, Gram-Chat-ID).
  // If a future consumer wants independent per-MCP headers we'd need a map
  // keyed by URL — flagged in the ElementsConfig docstring.
  const mcpHeaders = useRef<Record<string, string>>({}).current;

  const queryResult = useQuery({
    queryKey: [
      "mcpTools",
      ...urls,
      gramEnvironment,
      ...envQueryKey,
      ...authQueryKey,
    ],
    queryFn: async () => {
      assert(!auth.isLoading, "No auth found");
      assert(urls.length > 0, "No MCP URL found");

      // Populate the shared headers object (mutate in place so the same
      // reference is used by every transport).
      Object.keys(mcpHeaders).forEach((k) => delete mcpHeaders[k]);
      Object.assign(mcpHeaders, {
        ...transformEnvironmentToHeaders(environment ?? {}),
        ...auth.headers,
        ...(gramEnvironment && { "Gram-Environment": gramEnvironment }),
      });

      // Fetch tools from each MCP in parallel, then merge. We keep a
      // record of which URL each tool came from; the AI SDK's execute()
      // closure already routes back through the creating client, so merely
      // returning the merged tool set is sufficient for dispatch.
      const perUrl = await Promise.all(
        urls.map(async (url) => {
          const mcpClient = await createMCPClient({
            name: "gram-elements-mcp-client",
            transport: {
              type: "http",
              url,
              headers: mcpHeaders,
            },
          });
          const tools = await mcpClient.tools();
          return { url, tools };
        }),
      );

      return mergeMcpTools(perUrl);
    },
    enabled: !auth.isLoading && urls.length > 0,
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

/**
 * Merges per-MCP tool maps into one. On name collision the later source wins
 * (matches spread-merge semantics) and a warning is logged with the losing
 * URL. Callers that need disambiguation can filter via `toolsToInclude`.
 */
export function mergeMcpTools(
  sources: Array<{ url: string; tools: MCPToolsResult }>,
): MCPToolsResult {
  const merged: MCPToolsResult = {} as MCPToolsResult;
  const origin: Record<string, string> = {};

  for (const { url, tools } of sources) {
    for (const [name, tool] of Object.entries(tools)) {
      if (name in merged) {
        // eslint-disable-next-line no-console
        console.warn(
          `[useMCPTools] tool "${name}" defined by both ${origin[name]} and ${url}; using ${url}.`,
        );
      }
      (merged as Record<string, unknown>)[name] = tool;
      origin[name] = url;
    }
  }

  return merged;
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

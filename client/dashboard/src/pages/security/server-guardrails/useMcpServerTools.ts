import { useProjectSlugForRequests } from "@/contexts/Sdk";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useListMcpServerToolMetadata } from "@gram/client/react-query/listMcpServerToolMetadata.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useMemo } from "react";
import type { ServerTool } from "./server-guardrail-policy";

interface McpServerTools {
  tools: ServerTool[];
  isLoading: boolean;
}

/** The concrete tools of one MCP server, with whether each is annotated
 *  destructive. Toolset-backed servers read their toolset; remote servers read
 *  the discovered tool metadata. Mirrors what the policy scope picker lists. */
export function useMcpServerTools(
  server: Pick<McpServer, "id" | "toolsetId"> | undefined,
): McpServerTools {
  const gramProject = useProjectSlugForRequests();
  const toolsetId = server?.toolsetId;
  const toolsetsQuery = useListToolsets({ gramProject }, undefined, {
    enabled: !!toolsetId,
    throwOnError: false,
  });
  const metadataQuery = useListMcpServerToolMetadata(
    { mcpServerId: server?.id ?? "", gramProject },
    undefined,
    { enabled: !!server && !toolsetId, throwOnError: false },
  );

  const tools = useMemo<ServerTool[]>(() => {
    if (!server) return [];
    const list = toolsetId
      ? (toolsetsQuery.data?.toolsets
          .find((toolset) => toolset.id === toolsetId)
          ?.tools.map((tool) => ({
            name: tool.name,
            destructive: tool.annotations?.destructiveHint === true,
          })) ?? [])
      : (metadataQuery.data?.tools.map((tool) => ({
          name: tool.toolName,
          destructive: tool.destructiveHint === true,
        })) ?? []);
    return list.sort((left, right) => left.name.localeCompare(right.name));
  }, [server, toolsetId, toolsetsQuery.data, metadataQuery.data]);

  return {
    tools,
    isLoading: toolsetId ? toolsetsQuery.isLoading : metadataQuery.isLoading,
  };
}

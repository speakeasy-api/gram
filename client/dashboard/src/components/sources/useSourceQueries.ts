import { useListTools } from "@/hooks/toolTypes";
import type { Tool } from "@/lib/toolTypes";
import { useMemo } from "react";
import type { SourceKind } from "./sourceVersions";

export type SourceTool = Extract<Tool, { type: "http" | "function" }>;

// Tools name their source by the deployment asset's id, not the file's, so
// the match is on the id the page is keyed by.
function isToolOfSource(
  tool: Tool,
  sourceKind: SourceKind,
  assetId: string,
): tool is SourceTool {
  switch (sourceKind) {
    case "openapi":
      return tool.type === "http" && tool.openapiv3DocumentId === assetId;
    case "function":
      return tool.type === "function" && tool.functionId === assetId;
  }
}

/**
 * The tools generated from one source, read from the active deployment.
 *
 * Shared by the source page and the panel beside the create-from-source flow,
 * so both count and list the same tools.
 */
export function useSourceTools(
  sourceKind: SourceKind,
  assetId: string,
): {
  tools: SourceTool[];
  toolUrns: string[];
  isLoading: boolean;
  isError: boolean;
} {
  const { data, isLoading, isError } = useListTools();

  const tools = useMemo(
    () =>
      (data?.tools ?? []).filter((tool: Tool) =>
        isToolOfSource(tool, sourceKind, assetId),
      ),
    [data, sourceKind, assetId],
  );
  const toolUrns = useMemo(() => tools.map((tool) => tool.toolUrn), [tools]);

  return { tools, toolUrns, isLoading, isError };
}

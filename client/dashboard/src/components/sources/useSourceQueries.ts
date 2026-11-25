import { useListTools } from "@/hooks/toolTypes";
import type { Tool } from "@/lib/toolTypes";
import { useCallback, useMemo } from "react";
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
  refetch: () => void;
} {
  // A failed read is reported inline (retry state) rather than thrown to the
  // route boundary, which would replace the whole source page.
  const { data, isLoading, isError, refetch } = useListTools(
    undefined,
    undefined,
    { throwOnError: false },
  );

  const tools = useMemo(
    () =>
      (data?.tools ?? []).filter((tool: Tool) =>
        isToolOfSource(tool, sourceKind, assetId),
      ),
    [data, sourceKind, assetId],
  );
  const toolUrns = useMemo(() => tools.map((tool) => tool.toolUrn), [tools]);
  const retry = useCallback(() => {
    void refetch();
  }, [refetch]);

  return { tools, toolUrns, isLoading, isError, refetch: retry };
}

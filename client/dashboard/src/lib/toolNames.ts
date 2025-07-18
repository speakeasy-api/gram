import { ToolDefinition, useToolDefinitions } from "@/pages/toolsets/types";
import { HTTPToolDefinition, Toolset } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query";
import { useMemo } from "react";

export type Tool = ToolDefinition & {
  displayName: string;
};

export type ToolGroup = {
  key: string;
  tools: Tool[];
};

type HttpToolGroup = {
  key: string;
  tools: (HTTPToolDefinition & { displayName: string })[];
};

export const useGroupedToolDefinitions = (
  toolset: Toolset | undefined
): ToolGroup[] => {
  const toolDefinitions = useToolDefinitions(toolset);
  return useGroupedTools(toolDefinitions);
};

export const useGroupedHttpTools = (
  tools: HTTPToolDefinition[]
): HttpToolGroup[] => {
  const wrapped = tools.map((tool) => ({
    ...tool,
    type: "http",
  }));

  return useGroupedTools(wrapped as ToolDefinition[]) as HttpToolGroup[];
};

export const useGroupedTools = (tools: ToolDefinition[]): ToolGroup[] => {
  const { data: deployment } = useLatestDeployment(undefined, undefined, {
    staleTime: 1000 * 60 * 60,
  });

  const documentIdToSlug = useMemo(() => {
    return deployment?.deployment?.openapiv3Assets?.reduce((acc, asset) => {
      acc[asset.id] = asset.slug;
      return acc;
    }, {} as Record<string, string>);
  }, [deployment]);

  const toolGroups = useMemo(() => {
    return tools?.reduce((acc, tool) => {
      let groupKey = "unknown";

      if (tool.packageName) {
        groupKey = tool.packageName;
      } else if (tool.type === "http") {
        const documentSlug = tool.openapiv3DocumentId
          ? documentIdToSlug?.[tool.openapiv3DocumentId]
          : undefined;
        groupKey = documentSlug || "unknown";
      } else if (tool.type === "prompt") {
        groupKey = "custom";
      }

      const toolWithDisplayName = {
        ...tool,
        displayName: tool.name.replace(groupKey + "_", ""), // TODO account for _-
      };

      const group = acc.find((g) => g.key === groupKey);

      if (group) {
        group.tools.push(toolWithDisplayName);
      } else {
        acc.push({
          key: groupKey,
          tools: [toolWithDisplayName],
        });
      }
      return acc;
    }, [] as ToolGroup[]);
  }, [deployment, tools]);

  return toolGroups;
};

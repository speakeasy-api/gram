import { HTTPToolDefinition } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query";
import { useMemo } from "react";

export type Tool = HTTPToolDefinition & {
  displayName: string;
};

export type ToolGroup = {
  key: string;
  tools: Tool[];
};

export const useGroupedTools = (tools: HTTPToolDefinition[]): ToolGroup[] => {
  const { data: deployment } = useLatestDeployment();

  const documentIdToSlug = useMemo(() => {
    return deployment?.deployment?.openapiv3Assets?.reduce((acc, asset) => {
      acc[asset.id] = asset.slug;
      return acc;
    }, {} as Record<string, string>);
  }, [deployment]);

  const toolGroups = useMemo(() => {
    return tools?.reduce((acc, tool) => {
      const documentSlug = tool.openapiv3DocumentId
        ? documentIdToSlug?.[tool.openapiv3DocumentId]
        : undefined;
      const groupKey = tool.packageName || documentSlug || "unknown";

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

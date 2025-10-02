import {
  HTTPToolDefinition,
  PromptTemplate,
  PromptTemplateEntry,
  PromptTemplateKind,
  Tool,
  Toolset,
} from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query";
import { useMemo } from "react";

export type ToolWithDisplayName = Tool & { displayName: string };
export type HttpToolWithDisplayName = HTTPToolDefinition & { displayName: string };

export type ToolGroup = {
  key: string;
  tools: ToolWithDisplayName[];
};

export type HttpToolGroup = {
  key: string;
  tools: HttpToolWithDisplayName[];
};

export const useGroupedToolDefinitions = (
  toolset: Toolset | undefined
): ToolGroup[] => {
  const toolDefinitions = toolset?.tools ?? [];
  return useGroupedTools(toolDefinitions);
};

export const useGroupedHttpTools = (
  tools: HTTPToolDefinition[]
): HttpToolGroup[] => {
  return useGroupedTools(tools) as HttpToolGroup[];
};

export const useGroupedTools = (tools: Tool[]): ToolGroup[] => {
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

      if (tool.type === "http") {
        const documentSlug = tool.openapiv3DocumentId
          ? documentIdToSlug?.[tool.openapiv3DocumentId]
          : undefined;
        groupKey = documentSlug || "unknown";
      } else {
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

const templateName = (template: PromptTemplateEntry) => template.name;

export const isPrompt = (template: PromptTemplateEntry) =>
  template.kind === PromptTemplateKind.Prompt;

export const isHigherOrderTool = (template: PromptTemplateEntry) =>
  template.kind === PromptTemplateKind.HigherOrderTool;

export const promptNames = (promptTemplates: PromptTemplateEntry[]): string[] =>
  promptTemplates.filter(isPrompt).map(templateName);

export const isHttpTool = (tool: Tool) => tool.type === "http";

export const isPromptTemplate = (tool: Tool) => tool.type === "prompt_template";

export const filterHttpTools = (tools: Tool[] | undefined): HTTPToolDefinition[] => {
  return tools?.filter(isHttpTool) ?? [];
};

export const filterPromptTools = (tools: Tool[] | undefined): PromptTemplate[] => {
  return tools?.filter(isPromptTemplate) ?? [];
};

export const toolNames = (toolset: { tools: Tool[] }) => {
  const { tools } = toolset;
  return tools.map((tool) => tool.name);
};

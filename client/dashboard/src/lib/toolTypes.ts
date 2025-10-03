import {
  Tool as GeneratedTool,
  Toolset as GeneratedToolset,
  HTTPToolDefinition,
  PromptTemplate,
  PromptTemplateEntry,
  PromptTemplateKind,
} from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query";
import { useMemo } from "react";

export type ToolWithDisplayName = Tool & { displayName: string };
export type HttpToolWithDisplayName = Tool & { type: "http" } & {
  displayName: string;
};

export type Toolset = Omit<GeneratedToolset, "tools"> & {
  tools: Tool[];
};

export type Tool =
  | ({ type: "http" } & HTTPToolDefinition)
  | ({ type: "prompt" } & PromptTemplate);

export type ToolGroup = {
  key: string;
  tools: ToolWithDisplayName[];
};

export type HttpToolGroup = {
  key: string;
  tools: HttpToolWithDisplayName[];
};

export const asTool = (tool: GeneratedTool): Tool => {
  if (tool.httpToolDefinition) {
    return { type: "http", ...tool.httpToolDefinition };
  } else if (tool.promptTemplate) {
    return { type: "prompt", ...tool.promptTemplate };
  } else {
    throw new Error("Unexpected tool type");
  }
};

export const useGroupedToolDefinitions = (
  toolset: GeneratedToolset | undefined
): ToolGroup[] => {
  const tools = toolset?.tools ?? [];
  return useGroupedTools(tools.map(asTool));
};

export const useGroupedHttpTools = (
  tools: HTTPToolDefinition[]
): HttpToolGroup[] => {
  return useGroupedTools(
    tools.map((t) => asTool({ httpToolDefinition: t }))
  ) as HttpToolGroup[];
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
export const isPromptTool = (tool: Tool) => tool.type === "prompt";

export const filterHttpTools = (
  tools: Tool[] | undefined
): HTTPToolDefinition[] => {
  return tools?.filter(isHttpTool) ?? [];
};

export const filterPromptTools = (
  tools: Tool[] | undefined
): PromptTemplate[] => {
  return tools?.filter(isPromptTool) ?? [];
};

export const toolNames = (toolset: { tools: Tool[] }) => {
  const { tools } = toolset;
  return tools.map((tool) => tool.name);
};

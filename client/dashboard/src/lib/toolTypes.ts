import { useLatestDeployment } from "@/hooks/toolTypes";
import {
  FunctionResourceDefinition,
  FunctionToolDefinition,
  Resource as GeneratedResource,
  Tool as GeneratedTool,
  Toolset as GeneratedToolset,
  HTTPToolDefinition,
  PromptTemplate,
  PromptTemplateEntry,
  PromptTemplateKind,
} from "@gram/client/models/components";
import { useMemo } from "react";

export type ToolWithDisplayName = Tool & { displayName: string };
export type HttpToolWithDisplayName = Tool & { type: "http" } & {
  displayName: string;
};

export type Toolset = Omit<GeneratedToolset, "tools" | "resources"> & {
  tools: Tool[];
  rawTools: GeneratedTool[];
  resources?: Resource[];
};

export type Tool =
  | ({ type: "http" } & HTTPToolDefinition)
  | ({ type: "prompt" } & PromptTemplate)
  | ({ type: "function" } & FunctionToolDefinition);

export type ToolGroup = {
  key: string;
  tools: ToolWithDisplayName[];
};

export type HttpToolGroup = {
  key: string;
  tools: HttpToolWithDisplayName[];
};

export const asTool = (tool: GeneratedTool): Tool | undefined => {
  if (tool.httpToolDefinition) {
    return { type: "http", ...tool.httpToolDefinition };
  } else if (tool.promptTemplate) {
    return { type: "prompt", ...tool.promptTemplate };
  } else if (tool.functionToolDefinition) {
    return { type: "function", ...tool.functionToolDefinition };
  } else if (tool.externalMcpToolDefinition) {
    return undefined; // Omit external MCP tools, as they require special handling
  } else {
    throw new Error("Unexpected tool type");
  }
};

export const asTools = (tools: GeneratedTool[]): Tool[] => {
  return tools.map(asTool).filter((t) => t !== undefined);
};

export type Resource = { type: "function" } & FunctionResourceDefinition;

export const asResource = (resource: GeneratedResource): Resource => {
  if (resource.functionResourceDefinition) {
    return {
      type: "function",
      ...resource.functionResourceDefinition,
    };
  } else {
    throw new Error("Unexpected resource type");
  }
};

export const useGroupedToolDefinitions = (
  toolset: GeneratedToolset | undefined,
): ToolGroup[] => {
  const tools = toolset?.tools ?? [];
  return useGroupedTools(asTools(tools));
};

export const useGroupedHttpTools = (
  tools: HTTPToolDefinition[],
): HttpToolGroup[] => {
  return useGroupedTools(
    asTools(
      tools.map((t) => ({
        httpToolDefinition: t,
      })),
    ),
  ) as HttpToolGroup[];
};

export const useGroupedTools = (tools: Tool[]): ToolGroup[] => {
  const { data: deployment } = useLatestDeployment();

  const documentIdToSlug = useMemo(() => {
    return deployment?.deployment?.openapiv3Assets?.reduce(
      (acc, asset) => {
        acc[asset.id] = asset.slug;
        return acc;
      },
      {} as Record<string, string>,
    );
  }, [deployment]);

  const toolGroups = useMemo(() => {
    return tools?.reduce((acc, tool) => {
      let groupKey = "unknown";

      if (tool.type === "http") {
        const documentSlug = tool.openapiv3DocumentId
          ? documentIdToSlug?.[tool.openapiv3DocumentId]
          : undefined;
        groupKey = documentSlug || "unknown";
      } else if (tool.type === "function") {
        // TODO: As the UX gets built out this should get more granular, tying to which function asset
        groupKey = "functions";
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
export const isFunctionTool = (tool: Tool) => tool.type === "function";

export const filterHttpTools = (
  tools: Tool[] | undefined,
): HTTPToolDefinition[] => {
  return tools?.filter(isHttpTool) ?? [];
};

export const filterPromptTools = (
  tools: Tool[] | undefined,
): PromptTemplate[] => {
  return tools?.filter(isPromptTool) ?? [];
};

export const filterFunctionTools = (
  tools: Tool[] | undefined,
): FunctionToolDefinition[] => {
  return tools?.filter(isFunctionTool) ?? [];
};

export const toolNames = (toolset: { tools: Tool[] }) => {
  const { tools } = toolset;
  return tools.map((tool) => tool.name);
};

import { useLatestDeployment } from "@/hooks/toolTypes";
import {
  ExternalMCPToolDefinition,
  FunctionResourceDefinition,
  FunctionToolDefinition,
  PlatformToolDefinition,
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
  | ({ type: "function" } & FunctionToolDefinition)
  | ({ type: "platform" } & PlatformToolDefinition)
  | ({ type: "external-mcp" } & ExternalMCPToolDefinition & {
        isProxy: boolean;
      });
export type ToolWithAnnotations = Extract<
  Tool,
  { type: "http" | "function" | "platform" }
>;

export type ToolGroup = {
  key: string;
  tools: ToolWithDisplayName[];
};

export type HttpToolGroup = {
  key: string;
  tools: HttpToolWithDisplayName[];
};

type ToolDisplayContext = {
  documentIdToName?: Record<string, string>;
  documentIdToSlug?: Record<string, string>;
  functionIdToName?: Record<string, string>;
};

const PLATFORM_SOURCE_LABELS: Record<string, string> = {
  logs: "MCP Logs",
};

export const asTool = (tool: GeneratedTool): Tool | undefined => {
  if (tool.httpToolDefinition) {
    return { type: "http", ...tool.httpToolDefinition };
  } else if (tool.promptTemplate) {
    return { type: "prompt", ...tool.promptTemplate };
  } else if (tool.functionToolDefinition) {
    return { type: "function", ...tool.functionToolDefinition };
  } else if (tool.platformToolDefinition) {
    return { type: "platform", ...tool.platformToolDefinition };
  } else if (tool.externalMcpToolDefinition) {
    if (tool.externalMcpToolDefinition.type !== "proxy") {
      return {
        ...tool.externalMcpToolDefinition, // Has to be done in this order because externalMcpToolDefinition also has a type field
        type: "external-mcp",
        isProxy: tool.externalMcpToolDefinition.type === "proxy",
      };
    } else {
      return undefined; // Omit external MCP proxy tools, as they require special handling
    }
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

  const documentIdToName = useMemo(() => {
    return deployment?.deployment?.openapiv3Assets?.reduce(
      (acc, asset) => {
        acc[asset.id] = asset.name;
        return acc;
      },
      {} as Record<string, string>,
    );
  }, [deployment]);

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
      const groupKey = getToolSourceLabel(tool, { documentIdToName });
      const sourcePrefix = getToolSourcePrefix(tool, {
        documentIdToSlug,
      });

      const toolWithDisplayName = {
        ...tool,
        displayName:
          sourcePrefix && tool.name.startsWith(sourcePrefix + "_")
            ? tool.name.slice(sourcePrefix.length + 1)
            : tool.name,
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
  }, [documentIdToName, documentIdToSlug, tools]);

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
export const toolSupportsAnnotations = (
  tool: Tool,
): tool is ToolWithAnnotations =>
  tool.type === "http" || tool.type === "function" || tool.type === "platform";

export const getToolSourceLabel = (
  tool: Tool,
  context: ToolDisplayContext = {},
) => {
  const { documentIdToName, functionIdToName } = context;

  switch (tool.type) {
    case "http":
      if (tool.packageName) return tool.packageName;
      if (tool.openapiv3DocumentId && documentIdToName) {
        return documentIdToName[tool.openapiv3DocumentId] || "OpenAPI";
      }
      if (tool.deploymentId) return tool.deploymentId;
      return "Custom";
    case "function":
      if (tool.functionId && functionIdToName) {
        return functionIdToName[tool.functionId] || "Functions";
      }
      return "Functions";
    case "platform":
      return PLATFORM_SOURCE_LABELS[tool.sourceSlug] ?? "Platform";
    case "external-mcp":
      return tool.registryServerName || "External MCP";
    case "prompt":
      return "Prompts";
  }
};

const getToolSourcePrefix = (tool: Tool, context: ToolDisplayContext = {}) => {
  const { documentIdToSlug } = context;

  switch (tool.type) {
    case "http":
      if (tool.packageName) return tool.packageName;
      if (tool.openapiv3DocumentId && documentIdToSlug) {
        return documentIdToSlug[tool.openapiv3DocumentId];
      }
      return null;
    default:
      return null;
  }
};

export const getToolTypeLabel = (tool: Tool) => {
  switch (tool.type) {
    case "http":
      return "HTTP";
    case "function":
      return "Function";
    case "platform":
      return "Platform";
    case "external-mcp":
      return "External MCP";
    case "prompt":
      return "Prompt";
  }
};

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

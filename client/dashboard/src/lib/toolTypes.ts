import {
  Tool as GeneratedTool,
  Toolset as GeneratedToolset,
  HTTPToolDefinition,
  FunctionResourceDefinition,
  PromptTemplate,
  PromptTemplateEntry,
  PromptTemplateKind,
  FunctionToolDefinition,
  ExternalMCPToolDefinition,
  Resource as GeneratedResource,
} from "@gram/client/models/components";
import { useMemo } from "react";
import { useLatestDeployment, ToolsetKind } from "@/hooks/toolTypes";

// Base tool union including all tool types
export type Tool =
  | ({ type: "http" } & HTTPToolDefinition)
  | ({ type: "prompt" } & PromptTemplate)
  | ({ type: "function" } & FunctionToolDefinition)
  | ({ type: "external-mcp" } & ExternalMCPToolDefinition);

/**
 * Standard tools that can be edited, have descriptions, variations, etc.
 * Excludes external-mcp proxy tools which have different properties.
 */
export type StandardTool =
  | ({ type: "http" } & HTTPToolDefinition)
  | ({ type: "prompt" } & PromptTemplate)
  | ({ type: "function" } & FunctionToolDefinition);

export const isStandardTool = (tool: Tool): tool is StandardTool => {
  return tool.type !== "external-mcp";
};

export const filterStandardTools = (tools: Tool[]): StandardTool[] => {
  return tools.filter(isStandardTool);
};

/**
 * Asserts that all tools are standard tools (not external-mcp).
 * Throws if any external-mcp tools are encountered.
 *
 * Use this at boundaries where external-mcp tools are not yet supported.
 * Grep for usages to find places that need incremental external-mcp support.
 */
export const assertStandardTools = (tools: Tool[]): StandardTool[] => {
  const externalMcpTools = tools.filter((t) => t.type === "external-mcp");
  if (externalMcpTools.length > 0) {
    throw new Error(
      `Unexpected external-mcp tool(s) encountered: ${externalMcpTools.map((t) => t.name).join(", ")}. This code path does not yet support external MCP tools.`
    );
  }
  return tools as StandardTool[];
};

/**
 * Asserts that a single tool is a standard tool (not external-mcp).
 * Throws if an external-mcp tool is encountered.
 */
export const assertStandardTool = (tool: Tool): StandardTool => {
  if (tool.type === "external-mcp") {
    throw new Error(
      `Unexpected external-mcp tool encountered: ${tool.name}. This code path does not yet support external MCP tools.`
    );
  }
  return tool as StandardTool;
};

// Derived types using StandardTool (these tools have displayName, description, etc.)
export type ToolWithDisplayName = StandardTool & { displayName: string };
export type HttpToolWithDisplayName = StandardTool & { type: "http" } & {
  displayName: string;
};

export type Toolset = Omit<GeneratedToolset, "tools" | "resources"> & {
  kind: ToolsetKind;
  tools: Tool[];
  resources?: Resource[];
};

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
  } else if (tool.functionToolDefinition) {
    return { type: "function", ...tool.functionToolDefinition };
  } else if (tool.externalMcpToolDefinition) {
    return { type: "external-mcp", ...tool.externalMcpToolDefinition };
  } else {
    throw new Error("Unexpected tool type");
  }
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
  return useGroupedTools(tools.map(asTool));
};

export const useGroupedHttpTools = (
  tools: HTTPToolDefinition[],
): HttpToolGroup[] => {
  return useGroupedTools(
    tools.map((t) => asTool({ httpToolDefinition: t })),
  ) as HttpToolGroup[];
};

export const useGroupedTools = (tools: Tool[]): ToolGroup[] => {
  const { data: deployment } = useLatestDeployment();

  // Filter to standard tools only - external-mcp tools are displayed differently
  const standardTools = filterStandardTools(tools);

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
    return standardTools?.reduce((acc, tool) => {
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
  }, [deployment, standardTools]);

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

// Re-export ToolsetKind for convenience
export type { ToolsetKind } from "@/hooks/toolTypes";

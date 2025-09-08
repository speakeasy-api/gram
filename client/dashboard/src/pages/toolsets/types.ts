import { isHigherOrderTool } from "@/lib/toolNames";
import {
  HTTPToolDefinition,
  PromptTemplate,
  PromptTemplateKind,
  Toolset,
} from "@gram/client/models/components";

type Base = Pick<
  HTTPToolDefinition,
  | "name"
  | "canonicalName"
  | "description"
  | "packageName"
  | "projectId"
  | "schema"
  | "tags"
  | "createdAt"
  | "updatedAt"
>;

export type ToolDefinition =
  | (HTTPToolDefinition & {
      type: "http";
    })
  | (PromptTemplate &
      Base & {
        type: PromptTemplateKind;
      });

/**
 * For full toolsets with complete tool definitions
 */
export const useToolDefinitions = (
  toolset: Toolset | undefined
): ToolDefinition[] => {
  if (!toolset) {
    return [];
  }

  const toolDefinitions: ToolDefinition[] = toolset.httpTools.map((tool) => ({
    type: "http",
    ...tool,
    httpTool: tool,
  }));

  toolset.promptTemplates.filter(isHigherOrderTool).forEach((template) => {
    toolDefinitions.push({
      type: template.kind,
      ...template,
      canonicalName: template.name,
      projectId: toolset.projectId,
      schema: template.arguments ?? "",
      description: template.description ?? "",
      tags: ["Custom"],
    });
  });

  return toolDefinitions;
};

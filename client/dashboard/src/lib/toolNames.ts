import { ToolDefinition, useToolDefinitions } from "@/pages/toolsets/types";
import {
  HTTPToolDefinition,
  PromptTemplateKind,
  PromptTemplate,
  PromptTemplateEntry,
  Toolset,
  ToolsetEntry,
  HTTPToolDefinitionEntry,
  PromptTemplateEntry,
} from "@gram/client/models/components";
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
  toolset: Toolset | undefined,
): ToolGroup[] => {
  const toolDefinitions = useToolDefinitions(toolset);
  return useGroupedTools(toolDefinitions);
};

export const useGroupedHttpTools = (
  tools: HTTPToolDefinition[],
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

      if (tool.packageName) {
        groupKey = tool.packageName;
      } else if (tool.type === "http") {
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

export const higherOrderToolNames = (
  promptTemplates: PromptTemplateEntry[],
): string[] => promptTemplates.filter(isHigherOrderTool).map(templateName);

export const httpToolNames = (toolset: {
  httpTools: HTTPToolDefinitionEntry[];
}) => {
  const { httpTools } = toolset;

  return httpTools.map((tool) => tool.name);
};

// Quinn you better make a note of this one in the PR
export const userFacingToolnames = (toolset: {
  httpTools: HTTPToolDefinitionEntry[];
  promptTemplates: PromptTemplateEntry[];
}) => {
  return [
    ...higherOrderToolNames(toolset.promptTemplates),
    ...httpToolNames(toolset),
  ];
};

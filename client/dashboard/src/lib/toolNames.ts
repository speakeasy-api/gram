import { HTTPToolDefinition } from "@gram/client/models/components";

type Tool = Pick<HTTPToolDefinition, "name" | "openapiv3DocumentId">;
type ToolGroup<T extends Tool> = {
  key: string;
  tools: (T & { displayName: string })[];
};

export const groupTools = <T extends Tool>(tools: T[]): ToolGroup<T>[] => {
  const toolGroups = tools.reduce((acc, tool) => {
    const toolWithDisplayName = {
      ...tool,
      displayName: tool.name,
    };
    // TODO: package name?
    const group = acc.find((g) => g.key === tool.openapiv3DocumentId);

    if (group) {
      group.tools.push(toolWithDisplayName);
    } else {
      acc.push({
        key: tool.openapiv3DocumentId ?? "unknown",
        tools: [toolWithDisplayName],
      });
    }
    return acc;
  }, [] as ToolGroup<T>[]);

  // Transform the tool groups to have a displayName without the prefix
  const toolGroupsFinal: ToolGroup<T>[] = toolGroups.map((group) => {
    const { key, tools } = reduceToolNames(group.tools);

    return {
      ...group,
      key,
      tools,
    };
  });

  return toolGroupsFinal;
};

export const reduceToolNames = <T extends { name: string }>(
  tools: T[]
): {
  key: string;
  tools: (T & { displayName: string })[];
} => {
  // Cannot reduce tool names if there is only one tool
  if (tools.length < 2) {
    return {
      key: "None",
      tools: tools.map((tool) => ({
        ...tool,
        displayName: tool.name,
      })),
    };
  }

  // Find the longest common prefix among all tool names in the group
  const findLongestCommonPrefix = (strings: string[]): string => {
    if (strings.length === 0) return "";
    if (strings.length === 1) return strings[0] || "";

    let prefix = "";
    const firstString = strings[0] || "";

    for (let i = 0; i < firstString.length; i++) {
      const char = firstString[i];
      if (strings.every((str) => str[i] === char)) {
        prefix += char;
      } else {
        break;
      }
    }

    return prefix;
  };

  const toolNames = tools.map((tool) => tool.name);
  const commonPrefix = findLongestCommonPrefix(toolNames).replace(/_$/, "");

  // If prefix is meaningful (not empty and at least includes an underscore or similar separator)
  const prefixToUse =
    commonPrefix && (commonPrefix.includes("_") || commonPrefix.length >= 3)
      ? commonPrefix
      : "";

  // Update all items in the group to have a displayName without the prefix
  const updatedItems = tools.map((tool) => ({
    ...tool,
    displayName: prefixToUse
      ? tool.name.substring(prefixToUse.length).replace(/^_/, "")
      : tool.name,
  }));

  return {
    key: prefixToUse || "None",
    tools: updatedItems,
  };
};

import { ToolsetHeader } from "./Toolset";
import { Page } from "@/components/page-layout";
import { Column, Stack, Table } from "@speakeasy-api/moonshine";
import { useListTools } from "@gram/client/react-query/listTools.js";
import { Type } from "@/components/ui/type";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Checkbox } from "@/components/ui/checkbox";
import { useToolset, useUpdateToolsetMutation } from "@gram/client/react-query";
import { useEffect, useState } from "react";
import { ToolEntry } from "@gram/client/models/components";
import { Badge } from "@/components/ui/badge";
import { HttpMethod } from "@/components/http-route";
import { useParams } from "react-router";
import { SkeletonTable } from "@/components/ui/skeleton";

type Tool = ToolEntry & {
  enabled: boolean;
  setEnabled: (enabled: boolean) => void;
  displayName?: string;
};

type ToolGroup = {
  key: string;
  tools: Tool[];
};

const groupColumns: Column<ToolGroup>[] = [
  {
    header: "Tool Source",
    key: "name",
    render: (row) => (
      <Stack direction={"horizontal"} gap={4}>
        <Type className="capitalize">{row.key}</Type>
        {row.tools[0]?.packageName && <Badge>Third Party</Badge>}
      </Stack>
    ),
    width: "0.5fr",
  },
  {
    header: "# Tools",
    key: "numTools",
    render: (row) => (
      <Type>
        {row.tools.filter((t) => t.enabled).length}{" "}
        <span className="text-muted-foreground">
          / {row.tools.length} Tools
        </span>
      </Type>
    ),
  },
];

const columns: Column<Tool>[] = [
  {
    header: "Enable",
    key: "enabled",
    render: (row) => (
      <SimpleTooltip
        tooltip={row.enabled ? "Remove from toolset" : "Add to toolset"}
      >
        <Checkbox checked={row.enabled} onCheckedChange={row.setEnabled} />
      </SimpleTooltip>
    ),
    width: "48px",
  },
  {
    header: "Name",
    key: "name",
    render: (row) => (
      <Type className="text-wrap break-all">{row.displayName || row.name}</Type>
    ),
    width: "250px",
  },
  {
    header: "Method",
    key: "httpMethod",
    render: (row) => (
      <HttpMethod method={row.httpMethod} variant="badge" path={row.path} />
    ),
    width: "0.1fr",
  },
  {
    header: "Description",
    key: "description",
    render: (row) => (
      <SimpleTooltip tooltip={row.description}>
        <Type muted className="line-clamp-1">
          {row.description}
        </Type>
      </SimpleTooltip>
    ),
  },
];

export function ToolSelect() {
  const { toolsetSlug } = useParams();
  const { data: toolset, refetch } = useToolset({
    slug: toolsetSlug ?? "",
  });
  const { data: tools } = useListTools();
  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      refetch();
    },
  });

  useEffect(() => {
    setSelectedTools(toolset?.httpTools.map((t) => t.name) ?? []);
  }, [toolset]);

  if (!toolsetSlug) {
    return <div>Toolset not found</div>;
  }

  const toolGroups = tools?.tools.reduce((acc, tool) => {
    const toolWithEnabled = {
      ...tool,
      enabled: selectedTools.includes(tool.name),
      setEnabled: (enabled: boolean) => {
        setSelectedTools((prev) => {
          const updated = enabled
            ? [...prev, tool.name]
            : prev.filter((t) => t !== tool.name);
          updateToolsetMutation.mutate({
            request: {
              slug: toolsetSlug,
              updateToolsetRequestBody: {
                httpToolNames: updated,
              },
            },
          });
          return updated;
        });
      },
    };

    const group = acc.find((g) => g.key === tool.openapiv3DocumentId);

    if (group) {
      group.tools.push(toolWithEnabled);
    } else {
      acc.push({
        key: tool.openapiv3DocumentId ?? "unknown",
        tools: [toolWithEnabled],
      });
    }
    return acc;
  }, [] as Omit<ToolGroup, "setAllEnabled">[]);

  toolGroups?.sort((a, b) => {
    const aEnabled = a.tools.some((t) => t.enabled);
    const bEnabled = b.tools.some((t) => t.enabled);
    if (aEnabled && !bEnabled) return -1;
    if (!aEnabled && bEnabled) return 1;
    return b.key.localeCompare(a.key);
  });

  const toolGroupsFinal: ToolGroup[] | undefined = toolGroups?.map((group) => {
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

    const toolNames = group.tools.map((tool) => tool.name);
    const commonPrefix = findLongestCommonPrefix(toolNames).replace(/_$/, "");

    // If prefix is meaningful (not empty and at least includes an underscore or similar separator)
    const prefixToUse =
      commonPrefix && (commonPrefix.includes("_") || commonPrefix.length >= 3)
        ? commonPrefix
        : "";

    // Update all items in the group to have a displayName without the prefix
    const updatedItems = group.tools.map((tool) => ({
      ...tool,
      displayName: prefixToUse
        ? tool.name.substring(prefixToUse.length).replace(/^_/, "")
        : tool.name,
    }));

    return {
      ...group,
      key: prefixToUse || group.key,
      tools: updatedItems,
      defaultExpanded:
        toolGroups.length < 3 || group.tools.some((tool) => tool.enabled),
    };
  });

  return (
    <Page.Body>
      <ToolsetHeader toolsetSlug={toolsetSlug} />
      {toolGroupsFinal ? (
        <Table
          columns={groupColumns}
          data={toolGroupsFinal}
          rowKey={(row) => row.key}
          className="mb-6"
          hideHeader
          renderExpandedContent={(group) => (
            <Table
              columns={columns}
              data={group.tools}
              rowKey={(row) => row.id}
              hideHeader
              className="bg-card max-h-[600px] overflow-y-auto"
            />
          )}
        />
      ) : (
        <SkeletonTable />
      )}
    </Page.Body>
  );
}

import { ToolsetHeader } from "./Toolset";
import { Page } from "@/components/page-layout";
import { Column, Stack, Table } from "@speakeasy-api/moonshine";
import { useListTools } from "@gram/client/react-query/listTools.js";
import { Type } from "@/components/ui/type";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Checkbox } from "@/components/ui/checkbox";
import { useToolset, useUpdateToolsetMutation } from "@gram/client/react-query";
import { useEffect, useMemo, useState } from "react";
import { ToolEntry } from "@gram/client/models/components";
import { Badge } from "@/components/ui/badge";
import { HttpMethod } from "@/components/http-route";
import { useParams } from "react-router";
import { SkeletonTable } from "@/components/ui/skeleton";
import { groupTools } from "@/lib/toolNames";
import { Button } from "@/components/ui/button";

type Tool = ToolEntry & {
  enabled: boolean;
  setEnabled: (enabled: boolean) => void;
  displayName?: string;
};

type ToolGroup = {
  key: string;
  defaultExpanded: boolean;
  toggleAll: () => void;
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
  {
    header: "Toggle All",
    key: "toggleAll",
    width: "0.35fr",
    render: (row) => {
      const allEnabled = row.tools.every((t) => t.enabled);

      return (
        <Button
          variant="outline"
          size="sm"
          icon={allEnabled ? "x" : "check"}
          onClick={(e) => {
            e.stopPropagation();
            row.toggleAll();
          }}
        >
          {allEnabled ? "Disable All" : "Enable All"}
        </Button>
      );
    },
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
  const { data: tools, isLoading: isLoadingTools } = useListTools();
  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      refetch();
    },
  });

  useEffect(() => {
    setSelectedTools(toolset?.httpTools.map((t) => t.name) ?? []);
  }, [toolset]);

  const setToolsEnabled = (tools: string[], enabled: boolean) => {
    setSelectedTools((prev) => {
      const excluded = prev.filter((t) => !tools.includes(t));

      // Append from excluded so we don't add duplicates to the list
      const updated = enabled ? [...excluded, ...tools] : excluded;

      updateToolsetMutation.mutate({
        request: {
          slug: toolsetSlug ?? "",
          updateToolsetRequestBody: {
            httpToolNames: updated,
          },
        },
      });
      return updated;
    });
  };

  const toolGroups = useMemo(() => {
    const toggleAll = (tools: Tool[]) => {
      setToolsEnabled(
        tools.map((t) => t.name),
        tools.some((t) => !t.enabled) // Disable iff all are already enabled
      );
    };

    const toolGroups = groupTools(tools?.tools ?? []).map((group) => ({
      ...group,
      tools: group.tools.map((tool) => ({
        ...tool,
        enabled: selectedTools.includes(tool.name),
        setEnabled: (enabled: boolean) => setToolsEnabled([tool.name], enabled),
      })),
    }));

    const toolGroupsFinal = toolGroups.map((group) => ({
      ...group,
      toggleAll: () => toggleAll(group.tools),
      defaultExpanded:
        toolGroups.length < 3 || group.tools.some((tool) => tool.enabled),
    }));

    toolGroupsFinal.sort((a, b) => {
      const aEnabled = a.tools.some((t) => t.enabled);
      const bEnabled = b.tools.some((t) => t.enabled);
      if (aEnabled && !bEnabled) return -1;
      if (!aEnabled && bEnabled) return 1;
      return b.key.localeCompare(a.key);
    });

    return toolGroupsFinal;
  }, [tools, selectedTools, toolsetSlug]);

  if (!toolsetSlug) {
    return <div>Toolset not found</div>;
  }

  return (
    <Page.Body>
      <ToolsetHeader toolsetSlug={toolsetSlug} />
      {!isLoadingTools ? (
        <Table
          columns={groupColumns}
          data={toolGroups}
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

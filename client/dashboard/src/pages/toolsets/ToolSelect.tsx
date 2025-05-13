import { HttpRoute } from "@/components/http-route";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dot } from "@/components/ui/dot";
import { SkeletonTable } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { groupTools } from "@/lib/toolNames";
import { ToolEntry } from "@gram/client/models/components";
import { useToolset, useUpdateToolsetMutation } from "@gram/client/react-query";
import { useListTools } from "@gram/client/react-query/listTools.js";
import { Column, Stack, Table } from "@speakeasy-api/moonshine";
import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router";
import { ToolsetHeader } from "./Toolset";

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
      <Stack direction={"horizontal"} gap={4} align={"center"}>
        <Type className="capitalize">{row.key}</Type>
        {row.tools[0]?.packageName && (
          <Badge
            variant={"outline"}
            size={"sm"}
            className="text-muted-foreground"
          >
            Third Party
          </Badge>
        )}
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
      <Stack gap={2} className="break-all min-w-[175px] mr-[-24px]">
        <Type className="text-wrap break-all font-medium ">
          {row.displayName || row.name}
        </Type>
        <HttpRoute method={row.httpMethod} path={row.path} />
      </Stack>
    ),
    width: "0.5fr",
  },
  {
    header: "Description",
    key: "description",
    render: (row) => (
      <Type muted className="line-clamp-2 overflow-scroll self-start">
        <span className="text-foreground">
          {row.summary}
          {row.summary && <Dot className="mx-2" />}
        </span>
        {row.description}
        {!row.summary && !row.description && (
          <span className="text-muted-foreground italic">No description.</span>
        )}
      </Type>
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

      // Groups that contain enabled tools first
      if (aEnabled && !bEnabled) return -1;
      if (!aEnabled && bEnabled) return 1;
      // First party tools first
      if (a.tools[0]?.packageName && !b.tools[0]?.packageName) return 1;
      if (!a.tools[0]?.packageName && b.tools[0]?.packageName) return -1;
      // Alphabetical
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
          className="mb-6 overflow-y-auto"
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

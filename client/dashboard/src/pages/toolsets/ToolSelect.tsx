import { HttpRoute } from "@/components/http-route";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Dot } from "@/components/ui/dot";
import { Heading } from "@/components/ui/heading";
import { SkeletonTable } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import {
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { HumanizeDateTime } from "@/lib/dates";
import { Tool, useGroupedTools } from "@/lib/toolNames";
import {
  HTTPToolDefinition,
  PromptTemplate,
} from "@gram/client/models/components";
import { useToolset, useUpdateToolsetMutation } from "@gram/client/react-query";
import { useListTools } from "@gram/client/react-query/listTools.js";
import { Column, Grid, Stack, Table } from "@speakeasy-api/moonshine";
import { AlertTriangleIcon, Check, CheckCircleIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router";
import { useCustomTools } from "../toolBuilder/CustomTools";
import { MustacheHighlight } from "../toolBuilder/ToolBuilder";
import { ToolsetHeader } from "./Toolset";

type ToggleableTool = Tool & {
  enabled: boolean;
  setEnabled: (enabled: boolean) => void;
};

type ToggleableToolGroup = {
  key: string;
  defaultExpanded: boolean;
  toggleAll: () => void;
  tools: ToggleableTool[];
};

const sourceColumn: Column<{ key: string; tools: Tool[] }> = {
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
};

const groupColumns: Column<{ key: string; tools: Tool[] }>[] = [
  sourceColumn,
  {
    header: "# Tools",
    key: "numTools",
    render: (row) => <Type muted>{row.tools.length} Tools</Type>,
  },
];

const groupColumnsToggleable: Column<ToggleableToolGroup>[] = [
  sourceColumn,
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

const toggleableColumns: Column<ToggleableTool>[] = [
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
  ...columns,
];

export function ToolSelect() {
  const { toolsetSlug } = useParams();

  if (!toolsetSlug) {
    return <div>Toolset not found</div>;
  }

  return (
    <Page.Body>
      <ToolsetHeader toolsetSlug={toolsetSlug} />
      <ToolSelector toolsetSlug={toolsetSlug} />
    </Page.Body>
  );
}

export function ToolSelector({ toolsetSlug }: { toolsetSlug: string }) {
  const telemetry = useTelemetry();
  useRegisterToolsetTelemetry({
    toolsetSlug: toolsetSlug,
  });

  const { data: toolset, refetch } = useToolset({
    slug: toolsetSlug,
  });
  const { data: tools, isLoading: isLoadingTools } = useListTools();
  const customTools = useCustomTools();

  const [selectedTools, setSelectedTools] = useState<string[]>([]);

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", {
        action: "tools_added_removed",
      });
      refetch();
    },
  });

  useEffect(() => {
    setSelectedTools(toolset?.httpTools.map((t) => t.canonicalName) ?? []);
  }, [toolset]);

  const setToolsEnabled = (tools: string[], enabled: boolean) => {
    setSelectedTools((prev) => {
      const excluded = prev.filter((t) => !tools.includes(t));

      // Append from excluded so we don't add duplicates to the list
      const updated = enabled ? [...excluded, ...tools] : excluded;

      updateToolsetMutation.mutate({
        request: {
          slug: toolsetSlug,
          updateToolsetRequestBody: {
            httpToolNames: updated,
            promptTemplateNames:
              toolset?.promptTemplates.map((t) => t.name) ?? [],
          },
        },
      });
      return updated;
    });
  };

  const toggleTemplateEnabled = (template: string) => {
    const cur = toolset?.promptTemplates.map((t) => t.name) ?? [];
    const updated = cur.includes(template)
      ? cur.filter((t) => t !== template)
      : [...cur, template];

    console.log(updated);

    updateToolsetMutation.mutate({
      request: {
        slug: toolsetSlug,
        updateToolsetRequestBody: {
          httpToolNames: selectedTools,
          promptTemplateNames: updated,
        },
      },
    });
  };

  const groupedTools = useGroupedTools(tools?.tools ?? []);

  const toolGroups = useMemo(() => {
    const toggleAll = (tools: ToggleableTool[]) => {
      setToolsEnabled(
        tools.map((t) => t.canonicalName),
        tools.some((t) => !t.enabled) // Disable iff all are already enabled
      );
    };

    const toolGroups = groupedTools.map((group) => ({
      ...group,
      tools: group.tools.map((tool) => ({
        ...tool,
        enabled: selectedTools.includes(tool.canonicalName),
        setEnabled: (enabled: boolean) =>
          setToolsEnabled([tool.canonicalName], enabled),
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

  return (
    <Stack gap={4}>
      {!isLoadingTools ? (
        // This div is necessary to make sure the table gets the room it needs
        <div className="min-h-fit mb-6">
          <Table
            columns={groupColumnsToggleable}
            data={toolGroups}
            rowKey={(row) => row.key}
            hideHeader
            renderExpandedContent={(group) => (
              <Table
                columns={toggleableColumns}
                data={group.tools}
                rowKey={(row) => row.id}
                hideHeader
                className="bg-stone-50 border-b-1 dark:bg-card max-h-[500px] overflow-y-auto"
              />
            )}
          />
        </div>
      ) : (
        <SkeletonTable />
      )}
      {customTools && (
        <>
          <Heading variant="h3">Custom Tools</Heading>
          <Grid columns={{ sm: 1, md: 2, lg: 3 }} gap={4}>
            {customTools?.map((template) => (
              <Grid.Item key={template.id} className="h-52">
                <CustomToolCard
                  template={template}
                  currentTools={toolset?.httpTools ?? []}
                  currentTemplates={toolset?.promptTemplates ?? []}
                  toggleEnabled={() => toggleTemplateEnabled(template.name)}
                />
              </Grid.Item>
            ))}
          </Grid>
        </>
      )}
    </Stack>
  );
}

export function ToolsTable({
  tools,
  isLoading,
  additionalColumns,
  onRowClick,
}: {
  tools: HTTPToolDefinition[];
  isLoading: boolean;
  additionalColumns?: Column<Tool>[];
  onRowClick?: (row: Tool) => void;
}) {
  const toolGroups = useGroupedTools(tools ?? []);

  return !isLoading ? (
    <Table
      columns={groupColumns}
      data={toolGroups}
      rowKey={(row) => row.key}
      className="mb-6 overflow-y-auto bg-card"
      hideHeader
      renderExpandedContent={(group) => (
        <Table
          columns={[...columns, ...(additionalColumns ?? [])]}
          data={group.tools}
          rowKey={(row) => row.id}
          hideHeader
          className="bg-stone-100 dark:bg-background max-h-[600px] overflow-y-auto"
          {...(onRowClick ? { onRowClick } : {})}
        />
      )}
    />
  ) : (
    <SkeletonTable />
  );
}

function CustomToolCard({
  template,
  currentTools,
  currentTemplates,
  toggleEnabled,
}: {
  template: PromptTemplate;
  currentTools: HTTPToolDefinition[];
  currentTemplates: PromptTemplate[];
  toggleEnabled: () => void;
}) {
  const { data: tools } = useListTools();

  const isToolInToolset = (t: string) =>
    currentTools.some((t2) => t2.canonicalName === t);

  const templateIsInToolset = currentTemplates.some(
    (t) => t.name === template.name
  );

  const allToolsAvailable = template.toolsHint.every(isToolInToolset);

  const addButton = (
    <Button
      variant="outline"
      disabled={!templateIsInToolset && !allToolsAvailable}
      onClick={toggleEnabled}
    >
      {templateIsInToolset && <Check className="w-4 h-4 text-emerald-500" />}
      {templateIsInToolset ? "Added" : "Add to toolset"}
    </Button>
  );

  const variedNames = template.toolsHint.map(
    (t) => tools?.tools.find((t2) => t2.canonicalName === t)?.name ?? t
  );

  const badge = allToolsAvailable ? (
    <Badge
      variant="secondary"
      tooltip={
        <Stack>
          <p>
            All tools required by this custom tool are available in your
            toolset:
          </p>
          {variedNames.map((t) => (
            <p key={t}>- {t}</p>
          ))}
        </Stack>
      }
    >
      <CheckCircleIcon className="w-4 h-4 text-emerald-500" />
      Ready to use
    </Badge>
  ) : (
    <Badge
      variant="warning"
      tooltip={
        <Stack>
          <p>
            This custom tool requires tools that are not currently in your
            toolset:
          </p>
          {variedNames
            .filter((t) => !isToolInToolset(t))
            .map((t) => (
              <p key={t}>- {t}</p>
            ))}
        </Stack>
      }
    >
      <AlertTriangleIcon className="w-4 h-4" />
      Missing tools
    </Badge>
  );

  return (
    <Card className="h-52">
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Card.Title className="normal-case">{template.name}</Card.Title>
          {badge}
        </Stack>
        <Type variant="body" muted className="text-sm italic">
          {"Updated "}
          <HumanizeDateTime date={new Date(template.updatedAt)} />
        </Type>
        {template.description ? (
          <Card.Description className="line-clamp-2">
            <MustacheHighlight>{template.description}</MustacheHighlight>
          </Card.Description>
        ) : null}
      </Card.Header>
      <Card.Content className="h-full w-full flex items-end justify-end">
        {addButton}
      </Card.Content>
    </Card>
  );
}

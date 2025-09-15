import { HttpRoute } from "@/components/http-route";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@speakeasy-api/moonshine";
import { Check, X } from "lucide-react";
import { Card, Cards } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Dot } from "@/components/ui/dot";
import { Heading } from "@/components/ui/heading";
import { MultiSelect } from "@/components/ui/multi-select";
import { SearchBar } from "@/components/ui/search-bar";
import { SkeletonTable } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import {
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { useGroupedHttpTools } from "@/lib/toolNames";
import { useRoutes } from "@/routes";
import {
  HTTPToolDefinition,
  PromptTemplate,
} from "@gram/client/models/components";
import { useToolset, useUpdateToolsetMutation } from "@gram/client/react-query";
import { useListTools } from "@gram/client/react-query/listTools.js";
import { Column, Stack, Table } from "@speakeasy-api/moonshine";
import { AlertTriangleIcon, CheckCircleIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router";
import { useCustomTools } from "../toolBuilder/CustomTools";
import { MustacheHighlight } from "../toolBuilder/ToolBuilder";
import { ToolsetHeader } from "./ToolsetHeader";
import { UpdatedAt } from "@/components/updated-at";
import { onboardingStepStorageKeys } from "../home/Home";

type Tool = HTTPToolDefinition & { displayName: string };

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
          variant="secondary"
          size="sm"
          onClick={(e) => {
            e.stopPropagation();
            row.toggleAll();
          }}
        >
          <Button.LeftIcon>
            {allEnabled ? <X className="w-4 h-4" /> : <Check className="w-4 h-4" />}
          </Button.LeftIcon>
          <Button.Text>{allEnabled ? "Disable All" : "Enable All"}</Button.Text>
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
      <Type muted className="line-clamp-2 overflow-auto self-start">
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

  const { data: toolset, refetch } = useToolset(
    { slug: toolsetSlug },
    undefined,
    { enabled: !!toolsetSlug }
  );
  const { data: tools, isLoading: isLoadingTools } = useListTools();
  const { customTools } = useCustomTools();

  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [tagFilters, setTagFilters] = useState<string[]>([]);

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", {
        action: "tools_added_removed",
      });
      refetch();
    },
  });

  useEffect(() => {
    localStorage.setItem(onboardingStepStorageKeys.curate, "true");
  }, []);

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

  const groupedTools = useGroupedHttpTools(tools?.tools ?? []);

  const tagFilterOptions = groupedTools.flatMap((group) =>
    group.tools.flatMap((t) => t.tags.map((tag) => `${group.key}/${tag}`))
  );
  const uniqueTags = [...new Set(tagFilterOptions)];
  const tagFilterItems = uniqueTags.map((tag) => ({
    label: tag,
    value: tag,
  }));
  const tagsFilter = (
    <MultiSelect
      options={tagFilterItems}
      onValueChange={setTagFilters}
      placeholder="Filter by tag"
      className="w-fit capitalize"
    />
  );

  const filteredGroups = useMemo(() => {
    const normalize = (s: string) => s.toLowerCase().replace(/[^a-z0-9]/g, "");
    const filteredGroups = groupedTools.map((g) => ({
      ...g,
      tools: g.tools.filter((t) => {
        if (
          tagFilters.length > 0 &&
          !t.tags.some((tag) => tagFilters.includes(`${g.key}/${tag}`))
        ) {
          return false;
        }
        const tags = t.tags.join(",");
        return (
          normalize(t.name).includes(normalize(search)) ||
          normalize(tags).includes(normalize(search))
        );
      }),
    }));
    return filteredGroups.filter((g) => g.tools.length > 0);
  }, [tools, search, tagFilters]);

  const toolGroups = useMemo(() => {
    const toggleAll = (tools: ToggleableTool[]) => {
      setToolsEnabled(
        tools.map((t) => t.canonicalName),
        tools.some((t) => !t.enabled) // Disable iff all are already enabled
      );
    };

    const toolGroups = filteredGroups.map((group) => ({
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
  }, [filteredGroups, selectedTools, toolsetSlug]);

  return (
    <Stack gap={4} className="mb-8">
      {!isLoadingTools ? (
        <>
          <Stack direction="horizontal" gap={2} className="h-fit">
            {tagsFilter}
            <SearchBar
              value={search}
              onChange={setSearch}
              placeholder="Search tools"
              className="w-1/3"
            />
          </Stack>
          {/* This div is necessary to make sure the table gets the room it needs */}
          <div className="min-h-fit mb-6">
            <Table
              columns={groupColumnsToggleable}
              data={toolGroups}
              rowKey={(row) => row.key}
              hideHeader
              renderExpandedContent={(group) => (
                // This div is necessary to apply the bottom border to the table
                <div className="bg-stone-50 border-b-1 dark:bg-card max-h-[500px] overflow-y-auto">
                  <Table
                    columns={toggleableColumns}
                    data={group.tools}
                    rowKey={(row) => row.id}
                    hideHeader
                  />
                </div>
              )}
            />
          </div>
        </>
      ) : (
        <SkeletonTable />
      )}
      {customTools && customTools.length > 0 && (
        <>
          <Heading variant="h3">Custom Tools</Heading>
          <Cards>
            {customTools?.map((template) => (
              <CustomToolCard
                template={template}
                currentTools={toolset?.httpTools ?? []}
                currentTemplates={toolset?.promptTemplates ?? []}
                toggleEnabled={() => toggleTemplateEnabled(template.name)}
              />
            ))}
          </Cards>
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
  const toolGroups = useGroupedHttpTools(tools);

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
  const routes = useRoutes();
  const isToolInToolset = (t: string) =>
    currentTools.some((t2) => t2.name === t);

  const templateIsInToolset = currentTemplates.some(
    (t) => t.name === template.name
  );

  const allToolsAvailable = template.toolsHint.every(isToolInToolset);

  const addButton = (
    <Button variant="secondary"
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
    <Card>
      <Card.Header>
        <routes.customTools.toolBuilder.Link params={[template.name]}>
          <Card.Title className="normal-case">{template.name}</Card.Title>
        </routes.customTools.toolBuilder.Link>
        {addButton}
      </Card.Header>
      <Card.Content className="h-full w-full flex items-end justify-end">
        {template.description ? (
          <Card.Description>
            <MustacheHighlight>{template.description}</MustacheHighlight>
          </Card.Description>
        ) : null}
      </Card.Content>
      <Card.Footer>
        {badge}
        <UpdatedAt date={new Date(template.updatedAt)} />
      </Card.Footer>
    </Card>
  );
}

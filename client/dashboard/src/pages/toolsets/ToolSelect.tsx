import { HttpRoute } from "@/components/http-route";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Card, Cards } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Heading } from "@/components/ui/heading";
import { MultiSelect } from "@/components/ui/multi-select";
import { SearchBar } from "@/components/ui/search-bar";
import { SkeletonTable } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import {
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { useGroupedHttpTools } from "@/lib/toolNames";
import { useRoutes } from "@/routes";
import {
  HTTPToolDefinition,
  PromptTemplate,
  PromptTemplateKind,
} from "@gram/client/models/components";
import { useToolset, useUpdateToolsetMutation } from "@gram/client/react-query";
import { useListTools } from "@gram/client/react-query/listTools.js";
import { Button, Column, Stack, Table } from "@speakeasy-api/moonshine";
import { AlertTriangleIcon, Check, CheckCircleIcon, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router";
import { onboardingStepStorageKeys } from "../home/Home";
import { MustacheHighlight } from "../toolBuilder/ToolBuilder";
import { ToolsetHeader } from "./ToolsetHeader";

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
            {allEnabled ? (
              <X className="w-4 h-4" />
            ) : (
              <Check className="w-4 h-4" />
            )}
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
        {row.description}
        {!row.description && (
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
  const {
    data: { httpTools: tools, promptTemplates } = {
      httpTools: [],
      promptTemplates: [],
    },
    isLoading: isLoadingTools,
  } = useListTools();
  const customTools = promptTemplates.filter(
    (t: PromptTemplate) => t.kind === PromptTemplateKind.HigherOrderTool
  );

  const [selectedToolUrns, setSelectedToolUrns] = useState<string[]>([]);
  const selectedToolUrnsRef = useRef<string[]>([]);
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
    if (toolset?.toolUrns) {
      // Use the tool URNs directly from the toolset
      setSelectedToolUrns(toolset.toolUrns);
      selectedToolUrnsRef.current = toolset.toolUrns;
    }
  }, [toolset]);

  const setToolsEnabled = useCallback(
    (toolUrns: string[], enabled: boolean) => {
      // Calculate the updated URNs
      const excludedUrns = selectedToolUrnsRef.current.filter(
        (urn) => !toolUrns.includes(urn)
      );
      const updatedUrns = enabled
        ? [...excludedUrns, ...toolUrns]
        : excludedUrns;

      // Update state and ref
      setSelectedToolUrns(updatedUrns);
      selectedToolUrnsRef.current = updatedUrns;

      // Call mutation with tool URNs
      updateToolsetMutation.mutate({
        request: {
          slug: toolsetSlug,
          updateToolsetRequestBody: {
            toolUrns: updatedUrns,
          },
        },
      });
    },
    [toolsetSlug, tools, updateToolsetMutation]
  );

  const toggleTemplateEnabled = (templateUrn: string) => {
    // Check if template is currently enabled
    const isCurrentlyEnabled =
      selectedToolUrnsRef.current.includes(templateUrn);

    // Calculate updated URNs
    const updatedUrns = isCurrentlyEnabled
      ? selectedToolUrnsRef.current.filter((urn) => urn !== templateUrn)
      : [...selectedToolUrnsRef.current, templateUrn];

    // Update state and ref
    setSelectedToolUrns(updatedUrns);
    selectedToolUrnsRef.current = updatedUrns;

    updateToolsetMutation.mutate({
      request: {
        slug: toolsetSlug,
        updateToolsetRequestBody: {
          toolUrns: updatedUrns,
        },
      },
    });
  };

  const groupedTools = useGroupedHttpTools(tools ?? []);

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
        tools.map((t) => t.toolUrn),
        tools.some((t) => !t.enabled) // Disable iff all are already enabled
      );
    };

    const toolGroups = filteredGroups.map((group) => ({
      ...group,
      tools: group.tools.map((tool) => ({
        ...tool,
        enabled: selectedToolUrns.includes(tool.toolUrn),
        setEnabled: (enabled: boolean) =>
          setToolsEnabled([tool.toolUrn], enabled),
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
  }, [filteredGroups, selectedToolUrns, setToolsEnabled]);

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
                toggleEnabled={() => toggleTemplateEnabled(template.toolUrn)}
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
    <Button
      variant="secondary"
      disabled={!templateIsInToolset && !allToolsAvailable}
      onClick={toggleEnabled}
    >
      {templateIsInToolset && <Check className="w-4 h-4 text-emerald-500" />}
      {templateIsInToolset ? "Added" : "Add to toolset"}
    </Button>
  );

  const variedNames = template.toolsHint.map(
    (t) =>
      tools?.httpTools.find((t2: HTTPToolDefinition) => t2.canonicalName === t)
        ?.name ?? t
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

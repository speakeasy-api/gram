import { HttpRoute } from "@/components/http-route";
import { MultiSelect } from "@/components/ui/multi-select";
import { SearchBar } from "@/components/ui/search-bar";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useListTools } from "@/hooks/toolTypes";
import { ToolWithDisplayName, useGroupedTools } from "@/lib/toolTypes";
import { Column, Stack, Table } from "@speakeasy-api/moonshine";
import { useMemo, useState } from "react";

const sourceColumn: Column<{ key: string; tools: ToolWithDisplayName[] }> = {
  header: "Tool Source",
  key: "name",
  render: (row) => (
    <Stack direction={"horizontal"} gap={4} align={"center"}>
      <Type className="capitalize">{row.key}</Type>
    </Stack>
  ),
  width: "0.5fr",
};

const groupColumns: Column<{ key: string; tools: ToolWithDisplayName[] }>[] = [
  sourceColumn,
  {
    header: "# Tools",
    key: "numTools",
    render: (row) => <Type muted>{row.tools.length} Tools</Type>,
  },
];

const columns: Column<ToolWithDisplayName>[] = [
  {
    header: "Name",
    key: "name",
    render: (row) => (
      <Stack gap={2} className="break-all min-w-[175px] mr-[-24px]">
        <Type className="text-wrap break-all font-medium ">
          {row.displayName || row.name}
        </Type>
        {row.type === "http" && (
          <HttpRoute method={row.httpMethod} path={row.path} />
        )}
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

export function ToolsList(props: { deploymentId?: string }) {
  const { data: tools, isLoading: isLoadingTools } = useListTools({
    deploymentId: props.deploymentId,
  });
  const [search, setSearch] = useState("");
  const [tagFilters, setTagFilters] = useState<string[]>([]);
  const groupedTools = useGroupedTools(tools?.tools ?? []);

  const tagFilterOptions = groupedTools.flatMap((group) =>
    group.tools.flatMap((t) =>
      t.type === "http" ? t.tags.map((tag) => `${group.key}/${tag}`) : [],
    ),
  );
  const uniqueTags = [...new Set(tagFilterOptions)];
  const tagFilterItems = uniqueTags.map((tag) => ({
    label: tag,
    value: tag,
  }));
  const tagsFilter = (
    <MultiSelect
      options={tagFilterItems}
      selectedValues={tagFilters}
      setSelectedValues={setTagFilters}
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
          !(t.type === "http"
            ? t.tags.some((tag) => tagFilters.includes(`${g.key}/${tag}`))
            : false)
        ) {
          return false;
        }
        const tags = t.type === "http" ? t.tags.join(",") : "";
        return (
          normalize(t.name).includes(normalize(search)) ||
          normalize(tags).includes(normalize(search))
        );
      }),
    }));
    return filteredGroups.filter((g) => g.tools.length > 0);
  }, [tools, search, tagFilters]);

  const toolGroups = useMemo(() => {
    const toolGroups = filteredGroups.map((group) => ({
      ...group,
      defaultExpanded: true,
      tools: group.tools,
    }));

    toolGroups.sort((a, b) => {
      // Alphabetical
      return b.key.localeCompare(a.key);
    });

    return toolGroups;
  }, [filteredGroups]);

  return (
    <Stack gap={4}>
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
          <div className="min-h-fit">
            <Table
              columns={groupColumns}
              data={toolGroups}
              rowKey={(row) => row.key}
              hideHeader
              renderExpandedContent={(group) => (
                // This div is necessary to apply the bottom border to the table
                <div className="bg-stone-50 border-b-1 dark:bg-card max-h-[500px] overflow-y-auto">
                  <Table
                    columns={columns}
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
    </Stack>
  );
}

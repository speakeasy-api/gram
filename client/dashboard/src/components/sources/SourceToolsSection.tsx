import { InlineEmptyState } from "@/components/inline-empty-state";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { MethodBadge } from "@/components/tool-list/MethodBadge";
import { ToolVariationBadge } from "@/components/tool-variation-badge";
import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { SearchBar } from "@/components/ui/SearchBar";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useToolUpdate } from "@/hooks/useToolUpdate";
import { cn } from "@/lib/utils";
import { invalidateAllListTools } from "@gram/client/react-query/listTools.js";
import { useQueryClient } from "@tanstack/react-query";
import { useDeferredValue, useMemo, useState } from "react";
import { SourceSectionError } from "./SourceSectionError";
import type { SourceKind } from "./sourceVersions";
import { useSourceToolActions } from "./useSourceToolActions";
import type { SourceTool } from "./useSourceQueries";

type HttpTool = Extract<SourceTool, { type: "http" }>;
type FunctionTool = Extract<SourceTool, { type: "function" }>;

// Every method the OpenAPI extractor generates tools for, in reading order.
const HTTP_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"] as const;

function facetOf(tool: SourceTool): string {
  switch (tool.type) {
    case "http":
      return tool.httpMethod.toUpperCase();
    case "function":
      return tool.runtime;
  }
}

function matchesSearch(tool: SourceTool, query: string): boolean {
  if (query === "") return true;
  const haystack =
    tool.type === "http"
      ? `${tool.name} ${tool.path}`
      : `${tool.name} ${tool.description}`;
  return haystack.toLowerCase().includes(query);
}

// One pill per facet value (an HTTP method, a runtime), counted so the row of
// pills doubles as a breakdown of the source.
function FilterPill({
  label,
  count,
  active,
  onClick,
}: {
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
}): JSX.Element {
  return (
    <button type="button" onClick={onClick} aria-pressed={active}>
      <Badge
        variant={label === "DELETE" ? "destructive" : "neutral"}
        className={cn("py-2", !active && "opacity-50 hover:opacity-100")}
      >
        <Badge.Text>
          {label} ({count})
        </Badge.Text>
      </Badge>
    </button>
  );
}

function FacetPills({
  facets,
  total,
  selected,
  onSelect,
}: {
  facets: Array<{ label: string; count: number }>;
  total: number;
  selected: string | null;
  onSelect: (facet: string | null) => void;
}): JSX.Element {
  return (
    <div className="flex flex-wrap gap-2">
      <FilterPill
        label="All"
        count={total}
        active={selected === null}
        onClick={() => onSelect(null)}
      />
      {facets.map((facet) => (
        <FilterPill
          key={facet.label}
          label={facet.label}
          count={facet.count}
          active={selected === facet.label}
          onClick={() =>
            onSelect(selected === facet.label ? null : facet.label)
          }
        />
      ))}
    </div>
  );
}

function ToolNameCell({ tool }: { tool: SourceTool }): JSX.Element {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span className="truncate font-mono text-sm">{tool.name}</span>
      <ToolVariationBadge tool={tool} />
    </div>
  );
}

function toolColumns(
  sourceKind: SourceKind,
  actionsFor: ((tool: SourceTool) => Action[]) | null,
): Column<SourceTool>[] {
  const columns: Column<SourceTool>[] =
    sourceKind === "openapi"
      ? [
          {
            key: "method",
            header: "Method",
            width: "90px",
            render: (tool) => (
              <MethodBadge method={(tool as HttpTool).httpMethod} />
            ),
          },
          {
            key: "path",
            header: "Endpoint",
            width: "2fr",
            render: (tool) => (
              <Text muted className="truncate font-mono text-sm">
                {(tool as HttpTool).path}
              </Text>
            ),
          },
          {
            key: "name",
            header: "Tool",
            width: "2fr",
            render: (tool) => <ToolNameCell tool={tool} />,
          },
        ]
      : [
          {
            key: "runtime",
            header: "Runtime",
            width: "140px",
            render: (tool) => (
              <Badge variant="neutral">
                <Badge.Text>{(tool as FunctionTool).runtime}</Badge.Text>
              </Badge>
            ),
          },
          {
            key: "name",
            header: "Tool",
            width: "1.5fr",
            render: (tool) => <ToolNameCell tool={tool} />,
          },
          {
            key: "description",
            header: "Description",
            width: "2fr",
            render: (tool) => (
              <Text muted className="truncate text-sm">
                {tool.description}
              </Text>
            ),
          },
        ];
  if (actionsFor) {
    columns.push({
      key: "actions",
      header: "",
      width: "48px",
      render: (tool) => <MoreActions actions={actionsFor(tool)} />,
    });
  }
  return columns;
}

/**
 * The tools generated from one source, with the same per-tool overrides the
 * MCP server pages offer: names, descriptions, annotations and tags.
 *
 * Edits go through the global variation upsert, so they follow the tool into
 * every server that carries it — which is why they are offered here, on the
 * source, and not only on a server.
 */
export function SourceToolsSection({
  sourceKind,
  tools,
  isLoading,
  isError = false,
  onRetry,
}: {
  sourceKind: SourceKind;
  tools: SourceTool[];
  isLoading: boolean;
  /** The tool list failed to load; shown ahead of the empty state. */
  isError?: boolean;
  onRetry?: () => void;
}): JSX.Element {
  const [facet, setFacet] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);

  // Edits go through the global variation upsert, which the server gates
  // on project:write for this project.
  const project = useProject();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write", project.id);

  const queryClient = useQueryClient();
  const { updateTool, isUpdating } = useToolUpdate({
    telemetryEvent: "source_event",
    onSuccess: () => void invalidateAllListTools(queryClient),
  });
  const { actionsFor, dialog } = useSourceToolActions({
    onUpdate: updateTool,
    isUpdating,
  });

  const facets = useMemo(() => {
    const counts = new Map<string, number>();
    for (const tool of tools) {
      const key = facetOf(tool);
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
    // Methods keep their conventional order, with any the list doesn't
    // know trailing so no tool is left unfilterable; runtimes read
    // alphabetically.
    const known: readonly string[] = HTTP_METHODS;
    const ordered =
      sourceKind === "openapi"
        ? [
            ...known.filter((method) => counts.has(method)),
            ...Array.from(counts.keys())
              .filter((method) => !known.includes(method))
              .sort(),
          ]
        : Array.from(counts.keys()).sort();
    return ordered.map((label) => ({ label, count: counts.get(label) ?? 0 }));
  }, [tools, sourceKind]);

  const filtered = useMemo(() => {
    const query = deferredSearch.trim().toLowerCase();
    return tools.filter(
      (tool) =>
        (facet === null || facetOf(tool) === facet) &&
        matchesSearch(tool, query),
    );
  }, [tools, facet, deferredSearch]);

  const columns = useMemo(
    () => toolColumns(sourceKind, canWrite ? actionsFor : null),
    [sourceKind, canWrite, actionsFor],
  );

  const count = tools.length;

  return (
    <Card.Dashboard
      title="Tools"
      tooltip="Every tool generated from this source. A server built from it starts with all of them."
      bodyClassName={count === 0 ? undefined : "p-0"}
      action={
        <Text muted className="text-xs">
          {isLoading ? "Loading…" : `${count} tool${count === 1 ? "" : "s"}`}
        </Text>
      }
    >
      <SourceToolsBody
        isLoading={isLoading}
        isError={isError}
        onRetry={onRetry}
        count={count}
        toolbar={
          <div className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-3">
            {/* A single runtime is not a filter worth offering. */}
            {facets.length > 1 || sourceKind === "openapi" ? (
              <FacetPills
                facets={facets}
                total={count}
                selected={facet}
                onSelect={setFacet}
              />
            ) : (
              <span />
            )}
            <SearchBar
              value={search}
              onChange={setSearch}
              placeholder="Search tools"
              className="w-56"
            />
          </div>
        }
      >
        <Table
          columns={columns}
          data={filtered}
          rowKey={(tool) => tool.toolUrn}
          noResultsMessage={<Text muted>No matching tools</Text>}
          renderRow={(tool, rowElement) =>
            canWrite ? (
              <TableRowContextMenu
                key={tool.toolUrn}
                actions={actionsFor(tool)}
              >
                {rowElement}
              </TableRowContextMenu>
            ) : (
              rowElement
            )
          }
        />
      </SourceToolsBody>
      {dialog}
    </Card.Dashboard>
  );
}

function SourceToolsBody({
  isLoading,
  isError,
  onRetry,
  count,
  toolbar,
  children,
}: {
  isLoading: boolean;
  isError: boolean;
  onRetry?: () => void;
  count: number;
  toolbar: React.ReactNode;
  children: React.ReactNode;
}): JSX.Element {
  if (isLoading && count === 0) {
    return (
      <div className="p-6">
        <SkeletonTable />
      </div>
    );
  }
  // A failed read is not a source without tools.
  if (isError && count === 0) {
    return (
      <SourceSectionError
        heading="Couldn't load tools"
        description="The project's tools could not be fetched, so this source's tools are unknown."
        onRetry={onRetry}
      />
    );
  }
  if (count === 0) {
    return (
      <InlineEmptyState
        icon="wrench"
        heading="No tools yet"
        description="A server built from this source starts empty, and picks up its tools on the next deployment."
      />
    );
  }
  return (
    <>
      {toolbar}
      {children}
    </>
  );
}
